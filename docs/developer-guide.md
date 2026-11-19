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
| `routes.go` | path → handler registration, with the route's permission: `r.With(h.can(perm)).Post(...)` | contain logic |
| `handler.go` | bind (`h.Bind`), call service, respond (`h.Data`/`h.Error`) | contain SQL or business rules |
| `request.go` | request DTOs + response DTOs + `NewXResponse` mappers | contain logic |
| `service.go` | business rules, invariants, tenancy, `WithTx` | import `httpx` or `authn` |
| `repository.go` | `Repository` interface + sqlc-backed impl, `database.MapError` | import `httpx`/`authn` |
| `model.go` | domain structs + row mappers (`dbgen` types never leak out) | — |
| `commands.go`, `scheduled.go` | optional CLI/scheduled contributions | — |

## 3. Conventions cheat-sheet

**Handlers** — every handler embeds `httpx.Kit`, so a typical action reads like a Laravel
controller method:

```go
func (h *Handler) store(w http.ResponseWriter, r *http.Request) {
	var req StoreRequest
	if !h.Bind(w, r, &req) { // decode + validate; the 400/415/422 is already written
		return
	}
	branch, err := h.service.Create(r.Context(), tenancy.From(r), authn.UserID(r), req.toInput())
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusCreated, NewResponse(*branch))
}
```

`h.PathID(w, r, "id")` reads a positive id from the route; `authn.UserID(r)` is `auth()->id()`;
`tenancy.From(r)` is the resolved tenant scope.

**Errors** — services return `*apperr.Error` (`internal/apperr`); handlers call
`h.Error(w, r, err)`. Never write raw JSON errors. Map driver errors with
`database.MapError("module.op", err)`; turn `pgx.ErrNoRows` into `apperr.ResourceNotFound`.

**Validation** — structural validation is `h.Bind(w, r, &req)` (JSON + `validate` tags; messages
are human-readable; `h.Validate` for query/multipart structs). Business validation (uniqueness, ownership) lives in
the service and returns `apperr.Validation(map[string][]string{"field": {"message"}})`.

**Tenancy** — read the scope with `tenancy.FromContext(ctx)`; filter with
`scope.EffectiveTenantID()` (and `EffectiveBranchID()`). `0` means "no filter". Repositories take
explicit ids — **every tenant-scoped query must filter `tenant_id`**. `scope.AllTenants()` (internal
or a global impersonation read) is the only case that skips the filter. Writes without acting
headers on resolved routes are 403 at the middleware.

**Authorization** — permissions are checked at the HTTP edge, like Laravel: declare them on the
route, `r.With(h.can(authz.PermBranchCreate)).Post("/branch", h.store)` (the `can:` middleware;
`h.can` wraps `authz.Require`). Only when the permission depends on the payload (e.g. sending
`roles` needs `user.role.assign`) call `h.checker.MustCan(ctx, authn.UserID(r), perm)` inside the
handler. CLI commands, queue jobs and scheduled tasks are
trusted code and do not re-check permissions — exactly like artisan commands and queued jobs.
Services stay permission-free by default; a service that needs a Gate-style decision (ownership,
"can this actor touch that record") takes `*authz.Checker` (or a one-method interface) explicitly.
What services **always** enforce regardless of caller: tenant scoping (`tenant_id` in SQL) and
business invariants (e.g. Super-Admin cannot be stripped/deleted, no self role escalation) — those
are data rules, not permissions. Permissions live in `internal/authz/catalog.go`; add one there and
run `ims permission:sync`. A route-coverage test asserts every protected route returns 403 for a
token without permissions.

**Pagination** — `page, perPage, offset := httpx.PageParams(r)` (`per_page` default 10, max 100),
whitelist ordering with `httpx.OrderParams(r, "id", "name", "created_at")`, respond with
`responder.Paginated(w, r, status, data, meta, links)` where `meta, links := httpx.NewPagination(r, page, perPage, total)`.

**Cursor pagination** — the notification feed and developer-dashboard streams (requests, logs,
exceptions, jobs, sessions, actions, backups). They use `limit` (default 20/25, max 100) + `cursor`,
not `page`/`per_page`, and respond with `responder.Cursor(w, r, status, data, meta, links)`; links
come from `httpx.CursorURL`. Management lists (users, developers, targets, files) use
`page`/`per_page`.

**Activity log** — after a change succeeds, a service records it (like spatie/laravel-activitylog):
`s.activity.Record(ctx, activity.Entry{TenantID: t, Action: "branch.created", Subject: "branch",
SubjectID: id, Changes: map[string]any{"name": name}})`. Give the service a
`WithActivity(*activity.Recorder)` option; a nil recorder records nothing, so unit tests need no
setup. The actor, IP and request id come from the context; secret-looking keys are redacted.
Tenant admins read it at `GET /api/v1/activity` (`activity.view`); old rows are pruned after
`ACTIVITY_RETENTION_DAYS`.

**Transactions** — services own them: `s.tx.WithTx(ctx, func(q *dbgen.Queries) error { … })`.

**Related data** — there is no lazy loading; load relations explicitly in bulk (see §4). Never run a
query per row inside a response loop.

**Cache** — depend on `cache.Store` (like Laravel's `Cache` facade): `cache.Remember(...)` to read
through, `store.Delete(ctx, key)` in the same service method that changes the data. Put the tenant id
in every key derived from tenant data (`"settings:tenant:" + id`) and keep key builders next to the
code that owns them.

**Queue jobs** — declare `queue.Definition{Kind, Queue, Validate, Handle}` in a `queue.Registry`;
enqueue via the `queue.Engine` (prefer `EnqueueTx` with your write). Jobs re-resolve tenancy from the
envelope.

**Scheduled tasks** — register `{ID, Spec, Description, Timeout, Overlap, Run}` in
`internal/app/platform.go`; `Overlap` is `scheduler.OverlapSkip` (default) or
`scheduler.OverlapAllow`. Tasks run in the `ims schedule` process only.

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

The generator is intentionally conservative: it creates a **tenant-scoped, soft-deleted** module
with `id`, `tenant_id`, audit columns and timestamps. Adapt it if the resource is global, branch
scoped or read-only.

Step 1 — scaffold:

```bash
# binary: ims gen module ...   |   from source: go run ./cmd/app gen module ...
ims gen module --name=product \
  --fields="name:string,description:text?,price:decimal,is_active:bool"
```

This writes:

- `migrations/000NN_create_products.sql` (goose up/down, tenant index, `deleted_at` soft delete)
- `sql/queries/products.sql` (tenant-filtered CRUD)
- `internal/modules/product/{model,repository,service,request,handler,routes}.go`
- `internal/modules/product/service_test.go`
- the managed `/api/v1/product` OpenAPI region

It runs `sqlc generate` and refreshes the OpenAPI region by default. Supported field types:
`string`, `text`, `int`, `bigint`, `bool`, `decimal`, `date`, `datetime`, `json`; append `?` for
nullable (`notes:text?`). Use `--table`, `--package`, `--no-sqlc` or `--no-openapi` to override.
The scaffold uses a naive plural table name (`product` → `products`).

Step 2 — adjust the generated SQL if needed:

```bash
make sqlc            # after editing sql/queries/products.sql or migrations/000NN_*.sql
```

Step 3 — wire the handler in `internal/app/wiring.go`:

```go
// Dependencies struct
Products *product.Handler

// NewDependencies, inside the deps literal:
Products: product.NewHandler(
    product.NewService(product.NewRepository(queries), checker),
    binder, validator, responder,
),
```

Mount it in `internal/app/routes.go` inside the tenant-resolved group:

```go
r.Use(tenancy.Resolve(d.TenancyOpts))
// ...
d.Products.RegisterRoutes(r)   // GET/POST /api/v1/product, GET/PATCH/DELETE /api/v1/product/{id}
```

For a global/tenant-exempt module, mount it in the `tenancy.Exempt` group **and** change the
generated repository/service to stop filtering on `tenant_id` (the scaffold assumes tenant scope).
Public routes go in the public section.

Step 4 — register permissions in `internal/authz/catalog.go`:

```go
{Name: "product.view", Group: "product", Description: "Can view products"},
{Name: "product.create", Group: "product", Description: "Can create products"},
{Name: "product.update", Group: "product", Description: "Can update products"},
{Name: "product.delete", Group: "product", Description: "Can delete products"},
```

Then run `ims permission:sync` (or `make seed`) and assign the permissions to the relevant roles.

Step 5 — regenerate the HTTP contract:

```bash
make openapi          # internal/contract from api/openapi.yaml
make openapi-check    # spec validity, route drift, region freshness
```

Step 6 — tests:

- The generated `service_test.go` covers tenant scoping with a fake repository; extend it for your
  business rules.
- Add a `dbtest` repository integration test and a cross-tenant isolation test (model it on
  `internal/modules/branches/isolation_integration_test.go`). Call `dbtest.Clean(t, pool, "products", ...)`
  with the tables you own.
- Add an HTTP flow to `internal/app/*_integration_test.go` for critical routes.

Step 7 — verify:

```bash
make check                    # format-check + vet + lint + unit tests
DATABASE_TEST_DSN="postgres://postgres:postgres@127.0.0.1:5432/ims_go_api_test?sslmode=disable" make test-integration
make dev                      # log in and exercise the new routes
```

If you also touched the dashboard, run `make web-typecheck` and `make web-build`.

> **Composition root has two wiring points.** Shared services are built and attached in
> `internal/app/platform.go` (the `Platform` struct); HTTP handlers are attached in
> `internal/app/wiring.go` (the `Dependencies` struct). Adding a cross-cutting service means doing
> **both** — construct it *and* put it on `Platform`. Some handlers guard an optional dependency with
> `if h.x == nil → 503 "… are not available"`, so a missed assignment surfaces as a misleading `503`
> instead of a crash. `internal/app/platform_wiring_test.go` asserts the shared services are non-nil.

## 6. Recipe — add an endpoint to an existing module

1. `request.go` — add the request/response DTO + mapper.
2. `service.go` — add the method (tenancy, rules, `WithTx` if it writes).
3. `handler.go` — `MustCan` → bind → validate → service → respond.
4. `repository.go` + `sql/queries/<name>.sql` — add the query (only if it needs new SQL); `make sqlc`.
5. `routes.go` — register the path.
6. `api/openapi.yaml` — document it by hand, or `ims gen openapi --module=<name>` if the module
   follows the scaffold convention (this only refreshes the managed region).
7. `make openapi` — regenerate `internal/contract` from the spec; `make openapi-check` verifies
   route drift and freshness.
8. Tests + `make check`.

## 7. Recipe — add a field to an existing resource

1. `migrations/000NN_add_<field>_to_<table>.sql` (goose up/down).
2. `sql/queries/<name>.sql` — include the column in selects/inserts/updates.
3. `make sqlc` → `internal/dbgen` updates.
4. `model.go` — add to the domain struct + row mapper.
5. `request.go` — add to the request/response DTO (the `NewXResponse` mapper too).
6. Update `api/openapi.yaml`, then run `make openapi` and `make openapi-check`.
7. Update tests (including the generated response mapping tests) and finish with `make check`.

> Cross-cutting rule: when you touch the DB, always finish with `make sqlc` and commit the regenerated
> `internal/dbgen`; when you touch the API contract, run `make openapi`. Never edit `internal/dbgen`
> or `internal/contract` by hand.

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
| Cache stores | `internal/platform/cache` |
| Queue engines + registry | `internal/platform/queue` |
| Scheduler + tasks | `internal/platform/scheduler`, `internal/app/platform.go` |
| Storage disks, mail, i18n, health, metrics | `internal/platform/{storage,mail,i18n,health,metrics}` |
| Business modules | `internal/modules/<name>` (`backup/` is transport-free) |
| Developer dashboard SPA | `web/dev` (embedded by `web/dev/embed.go`) |
| Module + OpenAPI generators | `internal/scaffold`, `ims gen` |
| Generated (never edit) | `internal/dbgen`, `internal/contract` |
| SQL schema / queries | `migrations/`, `sql/queries/` |
| Canonical API contract | `api/openapi.yaml` |

## 8a. Integration tests with `testkit`

`internal/testkit` gives a migrated, seeded app with factories, like Laravel feature tests. Tests
skip unless `DATABASE_TEST_DSN` is set; put them in the module's external test package
(`package branches_test`).

```go
func TestBranchCreate(t *testing.T) {
	app := testkit.New(t)                       // clean schema, seeded, router built
	admin := app.User(t)                        // Admin of the seeded tenant, signed in
	app.As(admin).Post("/api/v1/branch", `{"name":"Dhaka"}`).Status(t, http.StatusCreated)

	viewer := app.User(t, testkit.Role(app.Role(t, "Viewer", "branch.view.all")))
	app.As(viewer).Post("/api/v1/branch", `{"name":"Sylhet"}`).Status(t, http.StatusForbidden)

	other := app.User(t, testkit.InTenant(app.Tenant(t, "Tenant B"))) // second tenant
	_ = other
}
```

`app.SuperAdmin(t)`, `app.As(u).InTenant(tenantID, branchID)` (acting headers),
`.Get/Post/Patch/Put/Delete`, `.Status(t, code)`, `.JSON(t, &v)` and `.Data(t, &v)` cover most
tests; `app.Deps` and `app.Pool` are there for anything else.

## 9. Before you open a PR

- [ ] New/changed SQL → `make sqlc` ran, `internal/dbgen` committed.
- [ ] New permission → in `catalog.go` and `ims permission:sync` run.
- [ ] Tenant-scoped reads/writes filter on the explicit tenant id (add an isolation test).
- [ ] Related data is batch-loaded (no query per row in a list loop — see §4).
- [ ] Handler is thin; business rules live in the service; no `httpx`/`authn` below the handler.
- [ ] Endpoint documented in `api/openapi.yaml`; `make openapi` + `make openapi-check` ran.
- [ ] `make gen-check` green (sqlc/OpenAPI output committed).
- [ ] `make check` green; `make test-integration` green when a DSN is available.
- [ ] Dashboard touched → `make web-typecheck` + `make web-build` green.
- [ ] Spec's implementation notes updated for behavior/scope changes.
- [ ] No secrets logged; tokens/passwords never appear in logs or payloads.
