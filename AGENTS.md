# AGENTS.md — Working Guide for This Repository

Instructions for AI coding agents (and humans) working on the IMS Go API Starter. Read this before
writing code. Architecture detail lives in [`docs/architecture.md`](docs/architecture.md); the
developer guide is [`docs/developer-guide.md`](docs/developer-guide.md) and per-phase notes live in
[`specs/`](specs/README.md).

## What this repo is

- **Go monolith**: feature-first modules, explicit dependency injection, typed
  request validation, RFC 9457 problem+json errors, `{data}` envelopes, multi-tenant + branch
  isolation, RBAC, billing with feature limits, cache drivers, River queue, cron scheduler, Cobra
  CLI — all in **one binary** (`cmd/app`), deployed with systemd (no containers).
- **Implemented phases:** 0 (kernel), 1 (database), 2 (auth/tenancy/RBAC), 3 (platform), 4
  (billing), 5 (notifications, i18n, files, import/export, mail), 6 (contract/observability),
  7 (deploy/generators), 10 (OpenAPI spec generator), 11 (settings/groups/feature flags),
  9 core (developer dashboard: hidden Developer access, queue admin, telemetry capture, `/dev`
  SPA with Telescope/queue/logs/commands/system screens). Next: 8, then 9 management panels
  (sessions/settings/flags/files/developers).

## Golden commands

```bash
make help                  # list every task (dev, build, test, lint, generate, db, ...)
make dev                   # API + SPA hot reload (air + Vite HMR; UI at localhost:5173/dev/)
make dev-api               # API only, hot reload via air
make build                 # bin/ims (static-ish, CGO-free)
make run                   # run the API once (loads .env if present; defaults otherwise)
make check                 # format-check + vet + lint + unit tests
make format                # gofmt -w .
make lint                  # golangci-lint run ./...
make generate              # sqlc + oapi-codegen (regenerates dbgen + contract)
make sqlc                  # regenerate internal/dbgen from sql/ + migrations/ (sqlc v1.31.1)
make migrate               # goose + River migrations

# Integration tests (real Postgres; each package gets its own schema):
#   make db-up                                         (docker compose; or any local PG)
#   make test-integration                              (uses DATABASE_TEST_DSN)
$env:DATABASE_TEST_DSN = "postgres://postgres:postgres@127.0.0.1:5432/ims_go_api_test?sslmode=disable"
go test -count=1 ./...
go test -count=1 -race -p 2 ./...   # -p 2 locally: parallel race builds can exhaust memory
```

Always finish work with `make check` (gofmt-clean, `go vet`, `golangci-lint` exit 0, unit tests
green) plus the integration suite when a DSN is available. Never commit; the user commits.

## Repository map

```text
cmd/app/                      Single binary entry point
web/dev/                      TanStack Start developer dashboard (SPA mode, embedded via go:embed)
internal/
  app/        composition root: platform.go (shared deps), wiring.go (HTTP deps), routes.go
              (middleware chain + route groups), lifecycle.go
  cli/        Cobra tree: root.go, registry.go, one file per command group
              (serve, migrate*, db:seed, queue*, schedule*, cache*, settings*, billing*, ...)
  config/     typed config; .env precedence: --env-file → APP_ENV_FILE → .env → code defaults
  apperr/     application error codes + *apperr.Error (stable, machine-readable codes)
  authn/      opaque tokens (Authenticator), OTP, login throttle, bearer middleware, Identity
  authz/      permission Catalog, Checker (Can/MustCan, super-admin bypass),
              permission cache + invalidation, Require middleware
  tenancy/    Scope (identity vs impersonation), Resolve/Exempt middleware, cached checks
  platform/   technical infra:
    database/   named pgx pools (registry), TxManager, goose+River migrate, dbtest helpers
    cache/      Store interface + factory/ + memory|file|redis|database drivers
    cachekeys/  versioned, scope-aware cache keys
    queue/      queue.Engine (sync | River), job registry, River dispatch + health/stats
    scheduler/  task registry, cron runner, heartbeat + health
    httpx/      BindJSON, validator + message mapping, Problem (RFC 9457), response helpers,
                pagination, middleware (RequestID, AccessLog, Recover, Timeout)
    health/     check registry + /api/health handler
    i18n/       Accept-Language → locale in context (embedded go-i18n en/bn catalogs)
    mail/       Driver interface: log | smtp; mail.send queue job
    storage/    Disk interface: private/public local roots + s3 (aws-sdk-go-v2); /storage static
    ratelimit/  Limiter interface: memory + cache-backed fixed window
  modules/    business features (see "Module anatomy")
    system/ auth/ identity/ access/ tenants/ branches/ settings/ dataprocessing/ billing/
    files/ notifications/
  dbgen/      sqlc generated code (never edit by hand)
  contract/   oapi-codegen generated code (never edit by hand)
  scaffold/   `ims gen module` + `ims gen openapi` generators (templates, analyzer, region splice)
migrations/   goose SQL, embedded (also sqlc's schema source)
sql/          sqlc queries
tests/        docker-compose.yml for local Postgres
specs/        per-phase specs (accepted before each phase starts)
```

## Module anatomy (the pattern to follow)

Small/medium modules are flat; large modules (billing) split into subpackages:

```text
internal/modules/<name>/            (e.g. identity, tenants, branches, access, auth)
├── routes.go        Route registration ONLY (RegisterRoutesResolve / RegisterRoutesExempt /
│                    RegisterPublicRoutes methods or free functions). No business logic.
├── handler.go       HTTP transport: bind → validate → call service → respond. No SQL, no rules.
├── request.go       Request DTOs (+ response DTOs & mappers).
├── service.go       Business rules, authorization checks, transactions.
├── repository.go    Repo interface(s) + sqlc-backed implementation. No HTTP concepts.
└── model.go         Domain structs + row mappers (never expose dbgen types outside).

internal/modules/billing/           Big-module layout (HTTP stays at the module root):
├── routes.go                        Route registration ONLY
├── handler.go, request.go           HTTP transport + request/response DTOs
├── orchestrator.go, drivers.go      gateway registry + drivers (implement domain.PaymentDriver)
├── domain/                          shared types: features.go, model.go (domain structs),
│                                    repository.go (interfaces + input types),
│                                    gateway.go (driver contract + result structs), invoice.go
└── service/                         All services: guard.go, package_service.go,
                                     subscription_service.go, payment_service.go,
                                     approval_service.go (+ helpers.go, gateway.go interface)
```

Why a `domain/` leaf exists: services live in a subpackage and must not import the HTTP root
(import cycle), so everything shared (domain types, repository interfaces, feature catalog,
driver contract) lives in `domain/`; the root imports `domain/` + `service/` and nothing imports
the root.

Rules of the layering (enforced by review, not the compiler):

1. `handler → service → repository → dbgen`. Services never import `httpx`; repositories never
   import `httpx`/`authn`.
2. `routes.go` files only map paths to handler methods. If routes need a dependency, it is the
   handler, not middleware construction.
3. Services are callable from HTTP, CLI, jobs and scheduled tasks — no `http.Request` anywhere
   below the handler layer.
4. Repositories take explicit typed params (`tenantID int64`, `scope tenancy.Scope` where needed);
   no hidden global state. Services own transactions (`platform.Tx.WithTx`).

### Adding a new module (checklist)

1. Skim `specs/` for the phase that owns the feature; if behavior is unclear, read the design
   notes referenced in that spec.
2. Migration: `migrations/000NN_*.sql` (goose `-- +goose Up/Down`). Include
   `tenant_id`/`branch_id` where the resource is scoped, audit columns (`created_by`,
   `updated_by`), `deleted_at` for soft deletes, and partial unique indexes
   (`WHERE deleted_at IS NULL`) for soft-deleted uniqueness.
3. Queries: `sql/queries/<name>.sql` with sqlc annotations; run `make sqlc`.
4. `model.go` (domain + mapper), `repository.go` (interface + impl, `database.MapError` for
   driver errors, `ErrNoRows` → `apperr.ResourceNotFound`).
5. `service.go` (or `service/`): authorization via `authz.Checker.MustCan`, tenancy via the
   `tenancy.Scope` argument, stamping `tenant_id`/`branch_id`/actor from scope/identity.
6. `request.go` + `handler.go`: strict JSON binding (`httpx.Binder`), `validate:` tags,
   `httpx.PageParams`/`OrderParams` for lists, `httpx.NewPagination` for paginated responses.
7. `routes.go`: register in the right group (see below); wiring in `internal/app/wiring.go`;
   route mounting in `internal/app/routes.go`.
8. OpenAPI: if the module was scaffolded with `ims gen module` its paths + schemas are written into
   the managed regions of `api/openapi.yaml` automatically. For a hand-written module run
   `ims gen openapi --module=<name>` (or `make openapi-gen` for all). Never edit text between the
   `BEGIN/END GENERATED` markers by hand — copy an entry outside the region to customize it.
   `make gen-check` / `ims openapi:check` fail when a region is stale.
9. Tests: service unit tests with fakes where possible; repository/service integration tests via
   `dbtest` (gated by `DATABASE_TEST_DSN`); handler coverage through the app e2e test if it is a
    critical flow.
10. Commands/scheduled tasks if the feature needs them (`internal/cli/`, `internal/app/platform.go`).
11. Update the phase spec's implementation notes; mark the checklist item.

## Route groups (registered in `internal/app/routes.go`)

| Group | Middleware | Use for |
|---|---|---|
| Public | none | login/OTP, public packages, gateway callbacks, `/api/health`, `/up` |
| Authenticated + tenant-resolved | `authn.Authenticate` → `tenancy.Resolve` | tenant-scoped resources (users, branches, roles in-tenant, subscription status, payments) |
| Authenticated + tenant-exempt | `authn.Authenticate` → `tenancy.Exempt` | global/system management (tenants CRUD, permissions, internal users/roles, packages/admin billing) |

Middleware order (outermost first): `RequestID → AccessLog → Recover → Timeout → RateLimit →
Locale`. Handlers inside tenant-resolved groups read the scope with
`tenancy.FromContext(ctx)`; impersonators use `X-Acting-Tenant` / `X-Acting-Branch` (writes
without acting headers on resolved routes are 403).

## Cross-cutting patterns (memorize these)

**Errors** — services return `*apperr.Error` (codes in `internal/apperr`). Handlers call
`responder.Error(w, r, err)`; the responder renders RFC 9457 problem+json with `error_code`,
`timestamp`, `trace_id` (5xx), `errors` (422), `meta`
(feature limits), `retry_after` (429). Never write raw JSON errors. 5xx details are masked
outside debug; the real error is attached via `apperr.Wrap(...)` for logs.

**Validation** — `binder.JSON` (size limit, content type, strict unknown fields, trailing
garbage) then `validator.Struct`. Messages are human-readable (validator tags never leak).
Business validation (uniqueness, ownership) lives in services and returns
`apperr.Validation(map[string][]string{...})` — dot-path field errors.

**Tenancy** — `tenancy.Scope` carries identity (TenantID/BranchID), impersonation
(Acting*) and `Internal`. `EffectiveTenantID()/EffectiveBranchID()` is what repositories filter
on; `0` means "no filter" (global). Tenant/branch membership checks are cached 5 minutes with
invalidation from branch writes. Repos receive explicit ids; never build queries from ambient
state.

> **Tenant isolation is explicit — there is no RLS.** Postgres row-level security was evaluated
> and dropped (see `docs/architecture.md` §11.18). Explicit scoping in SQL is the *only* isolation
> boundary: **every tenant-scoped query MUST receive and filter on an explicit `tenant_id`** (plus
> `branch_id` when branch-scoped). A query that forgets the filter is a security bug — not a style
> issue. Cover each module with a cross-tenant isolation test (reads, updates and deletes) modeled
> on `internal/modules/branches/isolation_integration_test.go`.

**Auth** — opaque tokens behind `authn.Authenticator` (`id|secret`, SHA-256 at rest, per-device).
`authn.IdentityFromContext(ctx)` gives the caller; `authz.Checker.MustCan(ctx, userID, perm)`
enforces permissions (super-admin bypasses). The permission catalog lives in
`internal/authz/catalog.go` — add new permissions there and run `permission:sync`.

**Pagination** — offset lists use `page` + `per_page` (default 10, max 100) via
`httpx.PageParams`, whitelisted `order_by`/`order_direction` via `httpx.OrderParams`, response via
`httpx.NewPagination` → `{data, links, meta}`. Cursor feeds (notification feed) use
`limit` (default 20, max 100) + `cursor` (plus `unread_only`/`priority`) and
`responder.Cursor`; `per_page` is intentionally ignored there.

**Cache** — depend on `cache.Store` (never a driver directly). Namespace keys with
`cachekeys.Key(ctx, store, scope, namespace, args...)` for anything derived from tenant data;
`cachekeys.Bump` invalidates a namespace. `Remember` degrades to computing on cache errors.

**Queue** — declare jobs as `queue.Definition{Kind, Queue, Validate, Handle}` in a
`queue.Registry`. Enqueue with the `queue.Engine` (sync executes inline; River persists).
Payloads are small JSON structs; handlers receive a context with tenancy restored from the
envelope. Prefer `EnqueueTx` for enqueue-with-your-write.

**Scheduler** — add tasks to the registry in `internal/app/platform.go`:
`{ID, Spec (cron), Description, Timeout, Overlap, Run}`. The runner logs start/finish and writes
the heartbeat (health check warns if stale; a shared cache driver is required to see it across
processes).

**Settings** — typed catalog in `internal/modules/settings/registry.go`; resolution is tenant
override → global row → code default, cached and invalidated on set. `settings:sync` seeds
globals.

**Billing** — features resolve overrides → package → default via `service.Guard`
(`Can/Require/Limit/WithinLimit/RequireWithinLimit`, 402 problems with
`meta{feature_key,limit,current}`). Resource services enforce limits through small
`LimitChecker` interfaces (see identity/branches). Payments go through
`billing.Orchestrator` + a `PaymentDriver` (bkash HTTP with cached token; cash/bank manual with
approval; nagad stub). Subscription lifecycle lives in `billing/service`.

## Database workflow (critical rules)

1. **Never edit `internal/dbgen` by hand.** Change `sql/` queries or `migrations/`, then
   `make sqlc`. CI fails on a generated-code diff.
2. **Goose migrations are the only schema source** (sqlc parses them). Keep `-- +goose Up/Down`.
3. **Explicit-id seeding does not advance bigserial sequences.** After inserting rows with
   explicit ids (like the seeded tenant/branch), call `SyncIDSequence` — see
   `internal/seeder/seeder.go`.
4. **Soft deletes**: partial unique indexes (`WHERE deleted_at IS NULL`); every read filters
   `deleted_at IS NULL`.
5. Timestamps come back as `pgtype.Timestamptz`; map to `*time.Time` in `model.go`.
6. **Integration tests get their own schema per package** (`dbtest`): call
   `dbtest.Queries(t)`/`dbtest.Pool(t)` and `dbtest.Clean(t, pool, "table", ...)` at the start.
   Tests **skip** (never fail) when `DATABASE_TEST_DSN` is unset. `dbtest.DSN(t)` returns the
   schema-scoped DSN for tests that build the full app.
7. River owns its tables; they are created through `ims migrate` (`rivermigrate`), never by hand.

## Testing conventions

- Unit tests: stdlib `testing`, table-driven, fakes for repository interfaces (see
  `internal/authn/service_test.go`). No mocking frameworks.
- HTTP behavior is covered via `httptest` against `deps.Router()` (see
  `internal/app/routes_test.go`) and the full e2e flow in
  `internal/app/identity_integration_test.go`.
- Cache drivers: shared contract suite in `internal/platform/cache/cachetest` — every driver
  must pass it (Redis is env-gated: `REDIS_TEST_ADDR`).
- Gateway HTTP is tested against `httptest.Server` stubs (`billing/drivers_test.go`).
- CLI commands get a smoke test through `BuildRoot` (`internal/cli/root_test.go`).
- Local machines may need `go test -race -p 2 ./...` (parallel race builds are memory-hungry);
  CI runs full parallelism.

## Design decisions (deliberate)

Keep this list accurate; add to it when you make a non-obvious choice (also update
`docs/architecture.md`).

- Query scoping is explicit (parameters/SQL) instead of ORM global scopes.
- No Postgres RLS (evaluated and dropped in Phase 6): explicit tenant-scoped SQL is the only
  isolation boundary; every tenant-scoped query must take and filter an explicit `tenant_id`.
- Tenant/audit stamping happens in services, not model observers.
- `ActingGlobal` impersonation yields tenant `0`.
- Bigserial sequences are synced after explicit-id seeding.
- Subscriptions are created/charged before old ones are deactivated (safer ordering); gateway
  failures mark payments `failed` and return 503.
- `users.email` is immutable via `PATCH /user/{id}` in the current phase.
- List endpoints whitelist `order_by`.
- Error responses are unified RFC 9457 (no separate "bare message" error shape).
- OTP codes are cache-only (not stored on the user row).

## When a task is ambiguous

1. Check the relevant `specs/phase-N-*.md` (decisions are recorded there) and
   `docs/architecture.md`.
2. Prefer the established pattern in this codebase over inventing a new one.
3. Record decisions you had to make in the spec's implementation notes and, if architectural,
   in `docs/architecture.md`.
