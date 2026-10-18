# Developer guide — modules & data flow

A short, practical guide for working on this codebase. For the "why" and architecture detail see
[`architecture.md`](architecture.md); for build/run commands see the
[local development runbook](runbooks/local-development.md). This file answers: **what do I change,
where, and in what order?**

## 1. The one rule

Data flows in one direction, layer by layer:

```
HTTP request
  → middleware (request id → logging → recover → metrics → timeout → rate limit → locale → auth)
  → router group (Public | tenant-resolved | tenant-exempt)
  → handler        (bind + validate → call service → respond)   internal/modules/<m>/handler.go
  → service        (business rules, authz, tenancy, transactions) internal/modules/<m>/service.go
  → repository     (SQL via sqlc, maps rows ↔ domain)             internal/modules/<m>/repository.go
  → dbgen.Queries  (generated; never edit)                        internal/dbgen
  → PostgreSQL
```

**Never skip a layer.** Handlers don't touch SQL; repositories don't know about HTTP; services are
callable from HTTP, CLI, jobs and scheduled tasks (so no `*http.Request` below the handler).

## 2. Module anatomy

Small/medium modules are **flat** (`internal/modules/<name>/`); only large modules split
subpackages (see `internal/modules/billing/`). Files:

| File | Holds | Must NOT |
|---|---|---|
| `routes.go` | path → handler method registration only | contain logic |
| `handler.go` | bind, validate, call service, respond | contain SQL or business rules |
| `request.go` | request DTOs + response DTOs + `NewXResponse` mappers | contain logic |
| `service.go` | business rules, `MustCan`, tenancy, `WithTx` | import `httpx` or `authn` |
| `repository.go` | `Repository` interface + sqlc-backed impl, `database.MapError` | import `httpx`/`authn` |
| `model.go` | domain structs + row mappers (`dbgen` types never leak out) | — |
| `commands.go`, `scheduled.go` | optional CLI/scheduled contributions | — |

## 3. Conventions cheat-sheet

**Errors** — services return `*apperr.Error` (`internal/apperr`); handlers call
`responder.Error(w, r, err)`. Never write raw JSON errors. Map driver errors with
`database.MapError("module.op", err)`; turn `pgx.ErrNoRows` into `apperr.ResourceNotFound`.

**Validation** — structural validation is `binder.JSON(w, r, &req)` then `validator.Struct(ctx, &req)`
(validate tags; messages are human-readable). Business validation (uniqueness, ownership) lives in
the service and returns `apperr.Validation(map[string][]string{"field": {"message"}})`.

**Tenancy** — read the scope with `tenancy.FromContext(ctx)`; filter with
`scope.EffectiveTenantID()` (and `EffectiveBranchID()`). `0` means "no filter". Repositories take
explicit ids — **every tenant-scoped query must filter `tenant_id`**. `scope.AllTenants()` (internal
or a global impersonation read) is the only case that skips the filter. Writes without acting
headers on resolved routes are 403 at the middleware.

**Authorization** — `checker.MustCan(ctx, identity.UserID, "feature.verb")` in the **service**
(so CLI/jobs are covered too). Permissions live in `internal/authz/catalog.go`; add one there and run
`ims permission:sync`.

**Pagination** — `page, perPage, offset := httpx.PageParams(r)` (`per_page` default 10, max 100),
whitelist ordering with `httpx.OrderParams(r, "id", "name", "created_at")`, respond with
`responder.Paginated(w, r, status, data, meta, links)` where `meta, links := httpx.NewPagination(r, page, perPage, total)`.

**Cursor pagination** — only the notification feed. It uses `limit` (default 20, max 100) +
`cursor` (+ `unread_only`/`priority`), not `page`/`per_page`, and responds with
`responder.Cursor(w, r, status, data, meta, links)`; links come from `httpx.CursorURL`.
`per_page` is ignored on cursor endpoints on purpose.

**Transactions** — services own them: `s.tx.WithTx(ctx, func(q *dbgen.Queries) error { … })`.

**Related data** — there is no lazy loading; load relations explicitly in bulk (see §4). Never run a
query per row inside a response loop.

**Cache** — depend on `cache.Store`; build keys with `cachekeys.Key(ctx, store, scope, "ns", args…)`
and invalidate with `cachekeys.Bump(ctx, store, "ns")`.

**Queue jobs** — declare `queue.Definition{Kind, Queue, Validate, Handle}` in a `queue.Registry`;
enqueue via the `queue.Engine` (prefer `EnqueueTx` with your write). Jobs re-resolve tenancy from the
envelope.

**Scheduled tasks** — register `{ID, Spec, Description, Timeout, Overflow, Run}` in
`internal/app/platform.go`; they run in the `ims schedule` process.

**Naming** — Go: `PascalCase` exported, package `snake`/single word. DB: `snake_case`, plural tables.
JSON: `snake_case`. Migrations: `000NN_verb_noun.sql` (goose up/down). Rules listed in the
[`db-change` skill](../.agents/skills/db-change/SKILL.md).

## 4. Loading related data (eager loading — avoid N+1)

There is no ORM and no lazy loading: a repository method returns exactly the columns its query
selects, and the **handler/service decides** what related data to attach. Do it in bulk.

**Preferred — batch by ids, then map (one extra query per list):**

```go
// internal/modules/identity/handler.go — profile photos
ids := make([]int64, 0, len(users))
for _, u := range users {
    ids = append(ids, u.ID)
}
photos, err := h.photos.PhotoURLs(ctx, ids) // map[int64]string
// …then attach per row while building the response DTOs
```

The role list does exactly this for the owning tenant:

```go
// internal/modules/access/handler.go
byTenant := h.tenantSlims(ctx, roles)                 // one ListTenantsByIDs query
data = append(data, NewRoleResponse(role, byTenant[*role.TenantID]))
```

**Also fine — a join in the repository** when the relation is naturally part of the row set
(`UserRoleRefs`, `ResolveUserIDs`); map it in `model.go`. No extra round-trip, no change to the
handler.

**Avoid — a query per row inside a response loop.** `checker.RoleRefs(ctx, user.ID)` inside a
`for` over users works, but it is an N+1 (one role query per user in the list).

Rules of thumb:

- Related data appears in response DTOs as a **slim** object (`RoleSlim`, `TenantSlim`) — an id plus
  a few display fields, never the full resource.
- Keep the bulk query on a repository or a small `…Lookup` interface owned by the module that needs
  it (e.g. `access.TenantLookup`) — no cross-module service imports.
- A missing or soft-deleted relation is omitted (`*Slim` nil + `omitempty`), never faked.

## 5. Recipe — add a new CRUD module

Step 1 — scaffold (migration + sqlc queries + module package, and it runs `sqlc generate`):

```bash
ims gen module --name=product --fields="name:string,description:text?,price:decimal,is_active:bool"
```

Step 2 — fix up the generated SQL if needed, then regenerate generated code:

```bash
make sqlc            # after editing sql/queries/product.sql or migrations/
```

Step 3 — wire it (the generated "next steps" remind you):

1. `internal/app/wiring.go` — construct the service/handler: add the field to `Dependencies` and
   assign it in `newDependencies`.
2. `internal/app/routes.go` — mount it in the right group:
   - tenant-resolved → `d.Product.RegisterRoutes(r)` inside the `tenancy.Resolve` group
   - tenant-exempt/global → the `tenancy.Exempt` group
   - public → the public section
3. `internal/authz/catalog.go` — add `product.view`, `product.create`, `product.update`,
   `product.delete`; run `ims permission:sync` (or `make seed`).

> **Composition root has two wiring points.** Shared services are built and attached in
> `internal/app/platform.go` (the `Platform` struct); HTTP handlers are attached in
> `internal/app/wiring.go` (the `Dependencies` struct). Adding a cross-cutting service means doing
> **both** — construct it *and* put it on `Platform`. Some handlers guard an optional dependency with
> `if h.x == nil → 503 "… are not available"`, so a missed assignment surfaces as a misleading `503`
> instead of a crash. `internal/app/platform_wiring_test.go` asserts the shared services are non-nil.

Step 4 — tests:

- Service unit test with a fake `Repository` (no mocking framework; see `internal/authn/service_test.go`).
- Repository/service integration test via `internal/platform/database/dbtest`, gated by
  `DATABASE_TEST_DSN` (skips when unset). Model isolation tests on
  `internal/modules/branches/isolation_integration_test.go`.

Step 5 — verify:

```bash
make check                    # format-check + vet + lint + unit tests
make test-integration         # with DATABASE_TEST_DSN set
make dev                      # then curl the new routes
```

## 6. Recipe — add an endpoint to an existing module

1. `request.go` — add the request/response DTO + mapper.
2. `service.go` — add the method (`MustCan`, tenancy, rules, `WithTx` if it writes).
3. `handler.go` — bind → validate → service → respond.
4. `repository.go` + `sql/queries/<name>.sql` — add the query (only if it needs new SQL); `make sqlc`.
5. `routes.go` — register the path.
6. `api/openapi.yaml` — document it by hand, or `ims gen openapi --module=<name>` if the module
   follows the scaffold convention.
7. Tests + `make check`.

## 7. Recipe — add a field to an existing resource

1. `migrations/000NN_add_<field>_to_<table>.sql` (goose up/down).
2. `sql/queries/<name>.sql` — include the column in selects/inserts/updates.
3. `make sqlc` → `internal/dbgen` updates.
4. `model.go` — add to the domain struct + row mapper.
5. `request.go` — add to the request/response DTO (the `NewXResponse` mapper too).
6. Update the OpenAPI schema, tests, and `api/openapi.yaml`.

> Cross-cutting rule: when you touch the DB, always finish with `make sqlc` and commit the regenerated
> `internal/dbgen`. Never edit `internal/dbgen` by hand.

## 8. Where things live

| Concern | Location |
|---|---|
| HTTP bind/validate/respond, pagination, problems | `internal/platform/httpx` |
| Shared services built once (attach to `Platform`) | `internal/app/platform.go` |
| HTTP handlers + route mounting (attach to `Dependencies`) | `internal/app/{wiring,routes}.go` |
| Middleware chain | `internal/app/routes.go` |
| Tenancy scope + resolve middleware | `internal/tenancy` |
| Auth (tokens, OTP, throttle) | `internal/authn` |
| Permissions catalog + checker | `internal/authz` |
| Error codes | `internal/apperr` |
| DB registry, TxManager, migrate | `internal/platform/database` |
| Cache stores + versioned keys | `internal/platform/cache`, `internal/platform/cachekeys` |
| Queue engines + registry | `internal/platform/queue` |
| Scheduler + tasks | `internal/platform/scheduler`, `internal/app/platform.go` |
| Storage disks, mail, i18n, health, metrics | `internal/platform/{storage,mail,i18n,health,metrics}` |
| Business modules | `internal/modules/<name>` |
| Generated (never edit) | `internal/dbgen`, `internal/contract` |
| SQL schema / queries | `migrations/`, `sql/queries/` |
| Canonical API contract | `api/openapi.yaml` |

## 9. Before you open a PR

- [ ] New/changed SQL → `make sqlc` ran, `internal/dbgen` committed.
- [ ] New permission → in `catalog.go` and `ims permission:sync` run.
- [ ] Tenant-scoped reads/writes filter on the explicit tenant id (add an isolation test).
- [ ] Related data is batch-loaded (no query per row in a list loop — see §4).
- [ ] Handler is thin; business rules live in the service; no `httpx`/`authn` below the handler.
- [ ] Endpoint documented in `api/openapi.yaml`.
- [ ] `make check` green; `make test-integration` green when a DSN is available.
- [ ] No secrets logged; tokens/passwords never appear in logs or payloads.
