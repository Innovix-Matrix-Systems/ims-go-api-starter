# AGENTS.md — Working Guide for This Repository

Instructions for AI coding agents (and humans) working on the IMS Go API Starter. Read this before
writing code. Architecture detail lives in [`docs/architecture.md`](docs/architecture.md) and the
developer guide is [`docs/developer-guide.md`](docs/developer-guide.md). Task recipes live in
**skills**: `.agents/skills/` (canonical) and the identical copy in `.claude/skills/`
(`make skills-sync` copies, `make gen-check` fails on drift).

| Skill | Use when |
|---|---|
| `add-module` | adding a feature/CRUD module or an endpoint to one |
| `db-change` | migrations, sqlc queries, `internal/dbgen` regeneration |
| `dev-dashboard` | dashboard panels, `/dev/api` endpoints, dashboard commands |
| `verify-changes` | finishing any task: the gates to run before reporting done |

## What this repo is

- **Go monolith** with Laravel ergonomics: feature-first modules, explicit dependency injection,
  typed request validation, RFC 9457 problem+json errors, `{data}` envelopes, multi-tenant +
  branch isolation, RBAC, billing with feature limits, settings + feature flags (with percentage
  rollout), activity log, cache drivers, swappable storage disks (local/S3/R2/MinIO), River
  queue, cron scheduler, Cobra CLI — all in **one binary** (`cmd/app`), deployed with systemd
  (no containers).
- **Developer dashboard** (`/dev`, React SPA embedded via `go:embed`): Telescope-style requests,
  logs, exceptions, queue, backups, commands, inspector (routes, roles, config, packages),
  system, audit log, tenants, sessions, settings, feature flags, files, developers, playground;
  Ctrl+K command palette.
- **Known gaps** (do not assume they exist): `tenants.is_active` is not enforced at request time,
  the dashboard has no frontend test suite, golangci-lint is still v1.

## Golden commands

```bash
make help                  # list every task (dev, build, test, lint, generate, db, ...)
make dev                   # SPA dev server on :8080 (HMR, /dev) + API on :8081 (air hot reload)
make dev-api               # API only, hot reload via air
make build                 # bin/ims (CGO-free)
make check                 # format-check + vet + lint + unit tests
make generate              # sqlc + oapi-codegen + OpenAPI regions
make gen-check             # fails when generated code, OpenAPI regions or skills drifted
make openapi-check         # spec valid + no route drift (every route must be in api/openapi.yaml)
make migrate               # goose + River migrations
make web-typecheck         # dashboard: tsc --noEmit
make web-build             # dashboard: build web/dev/dist (embedded by the binary)

# Integration tests (real Postgres; each package gets its own schema):
$env:DATABASE_TEST_DSN = "postgres://postgres:postgres@127.0.0.1:5432/ims_go_api_test?sslmode=disable"
go test -count=1 ./...
go test -count=1 -race -p 2 ./...   # -p 2 locally: parallel race builds can exhaust memory
```

Finish work with the `verify-changes` skill. Commit only when the user asks; then one commit per
feature with a message that says what changed and why.

## Repository map

```text
cmd/app/                      single binary entry point
module.go                     root package: embeds go.mod (dashboard Inspector → Packages)
web/dev/                      dashboard SPA (TanStack Router/Query, cmdk); embed.go also embeds package.json
  src/components/ui.tsx       Card, Button, Chip, Drawer, Modal, Tabs, StatCard, Icon, ...
  src/components/inputs.tsx   Select, DatePicker, TimePicker (use these, never native <select>/date)
internal/
  app/        composition root: platform.go (shared deps + scheduled tasks), wiring.go (HTTP deps),
              routes.go (middleware chain + route groups), dev_commands.go (dashboard commands)
  cli/        Cobra tree; withPlatform(state, fn) boots the app for a command (platform.go)
  config/     typed config, one file per section (app, database, cache, queue, auth, billing,
              storage, backup, mail, ops, activity, env); config.go = Load + cross-section Validate
  apperr/     application error codes + *apperr.Error
  authn/      opaque tokens, OTP, throttle, recovery (password reset / e-mail verify), bearer
              middleware, Identity, UserID(r) / UserIDFromContext(ctx)
  authz/      permission Catalog, Checker (Can/MustCan, super-admin bypass), Require → *Gate
  tenancy/    Scope, Filter (fails closed), Resolve/Exempt/InternalOnly middleware, From(r)
  testkit/    integration-test app + factories (User, Role, Tenant, SuperAdmin) + request builder
  platform/   technical infra:
    database/   pgx registry, TxManager (WithTx, WithTenantLock, TryHoldLock), migrate, dbtest
    cache/      Store interface + memory|file|redis|database drivers
    queue/      Engine (sync | River), job registry, admin
    scheduler/  task registry, cron runner, heartbeat
    httpx/      Kit (Bind/Validate/PathID + Responder), Binder, Validator, Problem (RFC 9457),
                pagination, middleware, ClientIP(ctx)/UserAgent(ctx)
    storage/    Disk interface (Put/Get/Delete/URL/TemporaryURL/Public) + local and S3 drivers
    telemetry/  dashboard capture (requests, SQL, cache, queue, logs) + Redactor
    health/ i18n/ mail/ ratelimit/ metrics/
  modules/    business features (see "Module anatomy"):
    access activity auth backup billing branches dataprocessing dev featureflags files
    identity notifications settings system tenants
  scaffold/   `ims gen module` + `ims gen openapi` generators
  dbgen/      sqlc generated code (never edit)
  contract/   oapi-codegen generated code (never edit)
api/openapi.yaml              canonical API contract (served at /docs)
migrations/                   goose SQL, embedded (also sqlc's schema source)
sql/queries/                  sqlc queries
```

## Module anatomy (the pattern to follow)

Small/medium modules are flat; large modules (billing) split into subpackages:

```text
internal/modules/<name>/
├── routes.go        Route registration + each route's permission:
│                    r.With(h.can(authz.PermBranchCreate)).Post("/branch", h.store)
├── handler.go       HTTP transport: h.Bind → service → h.Data/h.Error. No SQL, no rules.
│                    Large handlers split by resource (identity: users_/profile_/import_export_handler.go).
├── request.go       Request DTOs (validate tags) + response DTOs & mappers.
├── service.go       Business rules, tenancy, invariants, transactions, activity records.
├── repository.go    Repo interface + sqlc-backed impl. No HTTP concepts.
└── model.go         Domain structs + row mappers (never expose dbgen types outside).

internal/modules/billing/           Big-module layout (HTTP stays at the module root):
├── routes.go, handler.go, request.go, orchestrator.go, drivers.go
├── domain/   shared types: features.go, model.go, repository.go, gateway.go, invoice.go
└── service/  guard.go, package_service.go, subscription_service.go, payment_service.go, ...
```

A handler action, the whole shape:

```go
func (h *Handler) store(w http.ResponseWriter, r *http.Request) {
	var req InsertUpdateRequest
	if !h.Bind(w, r, &req) { // JSON + validate; writes the 400/415/422 itself
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

Rules of the layering (enforced by review and `route_permissions_integration_test.go`):

1. `handler → service → repository → dbgen`. Services never import `httpx`; repositories never
   import `httpx`/`authn`.
2. Handlers embed `httpx.Kit`; each module defines `func (h *Handler) can(perm string)` returning
   `authz.Require(h.checker, perm, h.Responder)`.
3. Permissions are declared on the route. Call `h.checker.MustCan(ctx, authn.UserID(r), perm)`
   inside a handler only when the permission depends on the payload (e.g. sending `roles`).
4. Services are callable from HTTP, CLI, jobs and scheduled tasks — no `http.Request` below the
   handler. Optional collaborators are `WithX(...)` options (`WithTx`, `WithActivity`, ...).
5. Repositories take explicit typed params (`tenancy.Filter`, `tenantID`); services own
   transactions (`TxManager.WithTx`, `WithTenantLock` for quota checks).

### Adding a new module (checklist — details in the `add-module` skill)

1. Prefer `ims gen module --name=x --fields="..."` (writes migration, queries, model, repository,
   service, request, handler, routes, service test and OpenAPI regions), then adapt.
2. Migration + queries (`db-change` skill); `make sqlc`.
3. Service: tenancy from `tenancy.Scope`, stamping actor/tenant, `activity.Recorder` for changes
   tenant admins should see.
4. Handler + routes with `h.can(...)`; mount in `internal/app/routes.go`, wire in
   `internal/app/wiring.go`.
5. Permissions in `internal/authz/catalog.go` (+ `AdminTenantPermissions` group if tenant admins
   get it); `permission:sync`.
6. OpenAPI: `ims gen openapi --module=<name>` (or hand-write outside the generated regions);
   `make openapi-check`.
7. Tests: unit tests with fakes; an integration test with `testkit` including a cross-tenant
   isolation check.
8. Record non-obvious decisions in `docs/architecture.md` §11 and the list below.

## Route groups (registered in `internal/app/routes.go`)

| Group | Middleware | Use for |
|---|---|---|
| Public | none | login/OTP, password reset, e-mail verify, public packages, gateway callbacks, `/api/health`, `/up` |
| Authenticated (no tenancy) | `auth` | logout / logout-all (tenantless accounts must reach them) |
| Authenticated + tenant-resolved | `auth` → `tenant.resolve` | tenant-scoped resources (users, branches, roles, settings, flags, activity, billing status) |
| Authenticated + tenant-exempt | `auth` → `tenant.exempt` (+ `tenant.internal`) | global management (tenants, permissions, internal users/roles, packages, approvals) |
| Dashboard `/dev/api` | `dev.enabled` → `auth` → `tenant.exempt` → `can:dev.view` | dashboard JSON API; mutations add `can:dev.manage` |

Global middleware (outermost first): `request-id → public-origin (non-local) → real-ip →
security-headers → cors → access-log → recover → telescope → metrics → timeout → throttle →
locale`. The dashboard Inspector shows the live table.

## Cross-cutting patterns (memorize these)

**Errors** — services return `*apperr.Error`; handlers call `h.Error(w, r, err)`, which renders
RFC 9457 problem+json (`error_code`, `errors` for 422, `meta` for feature limits, `retry_after`
for 429, `trace_id` for 5xx). Never write raw JSON errors. Wrap causes with `apperr.Wrap`.

**Validation** — `h.Bind` (size limit, content type, strict fields, then `validate:` tags).
Business validation lives in services: `apperr.Validation(map[string][]string{...})`.

**Tenancy** — `tenancy.From(r)` gives the `Scope`; `scope.Filter()` is what repositories use for
single-row reads: one tenant, or every tenant only for internal/global reads. A zero `Filter`
matches **no** tenant rows (fails closed). Impersonators send `X-Acting-Tenant` /
`X-Acting-Branch`.

> **Tenant isolation is explicit — there is no RLS.** Every tenant-scoped query MUST filter on
> an explicit tenant (SQL predicate `(@all_tenants OR tenant_id = @tenant_id)`). A query that
> forgets it is a security bug. Cover each module with a cross-tenant isolation test.

**Auth** — opaque tokens (`id|secret`, SHA-256 at rest, per device). `authn.UserID(r)` is
`auth()->id()`. Super-admins bypass permission checks. CLI/jobs/scheduled tasks are trusted and do
not re-check permissions. Password reset and e-mail verification use single-use hashed tokens in
the cache (`authn.Recovery`); responses never reveal whether an e-mail exists.

**Activity log** — services call `s.activity.Record(ctx, activity.Entry{TenantID, Action:
"branch.created", Subject: "branch", SubjectID, Changes})` after a change succeeds. A nil recorder
records nothing; actor, IP and request id come from the context; secret-looking keys are redacted.
Read at `GET /api/v1/activity` (`activity.view`).

**Pagination** — `page`/`per_page` (default 10, max 100) via `httpx.PageParams`, whitelisted
ordering via `httpx.OrderParams`, `h.Paginated(...)` with `httpx.NewPagination`. Feeds
(notifications, activity, dashboard streams) use `limit` + opaque `cursor` and `h.Cursor(...)`.

**Cache** — depend on `cache.Store`. Keys are owned by the module that caches; include the tenant
id in tenant-derived keys and delete the key in the service method that writes the data. Default
driver: `memory` locally, `database` elsewhere.

**Queue / Scheduler** — jobs are `queue.Definition{Kind, Queue, Validate, Handle}`; prefer
`EnqueueTx`. Scheduled tasks are registered in `internal/app/platform.go`
(`{ID, Spec, Description, Timeout, Overlap, Run}`); one scheduler process at a time (advisory lock).

**Settings & feature flags** — settings: typed catalog (`modules/settings/registry.go`), tenant →
global → default, groups + order editable in the dashboard. Flags: catalog in
`modules/featureflags/registry.go`, user → tenant → global → default, optional percentage rollout
(stable per-user or per-tenant buckets); gate routes with `featureflags.Require`.

**Storage** — code names a disk (`FILES_DISK=r2`); disks are declared in config
(`private`, `public`, `s3`, plus `STORAGE_DISKS=...`). Visibility, not the name, decides whether a
file gets a URL; private files go through the authorized download route.

**Billing** — `service.Guard` resolves features/limits (overrides → package); resource services
enforce limits through small `LimitChecker` interfaces under `WithTenantLock`. Gateway callbacks
are always confirmed with the gateway.

## Database workflow (critical rules — details in the `db-change` skill)

1. Never edit `internal/dbgen` by hand; change `sql/queries/` or `migrations/` and `make sqlc`.
2. Goose migrations are the only schema source.
3. Explicit-id seeding needs `SyncIDSequence` (tenants/branches repositories; used by the seeder).
4. Soft deletes: partial unique indexes (`WHERE deleted_at IS NULL`); reads filter `deleted_at`.
5. Map `pgtype.Timestamptz` to `time.Time`/`*time.Time` in `model.go`.
6. River owns its tables (`ims migrate`).

## Testing conventions

- Unit tests: stdlib `testing`, table-driven, fakes for repository interfaces. No mock frameworks.
- **Integration tests use `internal/testkit`** in the package's external test package
  (`package x_test`): `app := testkit.New(t)`, `app.User(t, testkit.Role(...), testkit.InTenant(...))`,
  `app.As(user).Post(path, body).Status(t, 201).Data(t, &v)`. Tests skip without
  `DATABASE_TEST_DSN`. `dbtest.Queries/Pool/Clean` remain for repository-level tests.
- `internal/app/route_permissions_integration_test.go` asserts every protected route answers 403 to
  a user without permissions — new routes must be protected or allowlisted there.
- Cache drivers share the `cachetest` contract suite; gateway HTTP is tested against
  `httptest.Server` stubs; CLI commands get a smoke test through `BuildRoot`.

## Design decisions (deliberate)

Keep this list accurate; add to it when you make a non-obvious choice (also update
`docs/architecture.md`).

- Explicit query scoping (SQL parameters) instead of ORM global scopes; no Postgres RLS.
- Permission checks on routes/handlers (Laravel `can:` / controller style); services enforce
  tenancy + invariants. CLI/jobs are trusted callers.
- Tenant/audit stamping in services, not model observers. Activity is recorded explicitly.
- `tenancy.Filter{}` fails closed; all-tenant reads are explicit (`tenancy.Unscoped()`).
- Nobody changes their own roles; the Super-Admin cannot be stripped or deleted.
- Quota checks (users.max, branches.max) run under a per-tenant advisory lock.
- Billing callbacks are confirmed with the gateway; subscribe is idempotent per tenant.
- Error responses are unified RFC 9457.
- OTP and recovery tokens are cache-only.
- Backups are local-first with optional S3/R2 copies.
- Dashboard: handlers call services (`dev/services.go`), mutations need `dev.manage` on the route,
  every mutation and dashboard sign-in is audited (`dev_actions`, with IP and browser).
- Tenant `is_active` is not enforced at request time yet (dashboard shows it read-only).

## When a task is ambiguous

1. Check `docs/architecture.md` (§11 lists the deliberate decisions) and the developer guide.
2. Prefer the established pattern in this codebase over inventing a new one — the reference
   modules are `branches` (small), `identity` (rich) and `billing` (big).
3. Record a new non-obvious decision in `docs/architecture.md` §11 and in the list above.
