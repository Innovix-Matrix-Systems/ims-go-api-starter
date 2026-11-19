# Architecture

How the IMS Go API Starter actually works. This document describes the system;
[`AGENTS.md`](../AGENTS.md) and the [developer guide](developer-guide.md) describe how to work on it.

## 1. Shape of the system

One static binary (`cmd/app`) that serves HTTP, runs queue workers, runs the scheduler, applies
migrations and hosts administrative commands. No containers are required; production runs it
under systemd with an `EnvironmentFile`.

```text
cmd/app ──► internal/cli (Cobra tree, command registry, lazy dependency providers)
              └── serve ─────► internal/app (composition root)
                                 ├── platform.go   shared: DB pools, cache, queue/schedule
                                 │                 registries, settings, billing services
                                 ├── wiring.go     HTTP deps: checkers, module services,
                                 │                 handlers
                                 ├── routes.go     middleware chain + route groups
                                 └── lifecycle.go  graceful shutdown, server timeouts
```

`NewPlatform` deliberately connects lazily: `serve` boots while the database is down and surfaces
the outage through `/api/health`. `help`/`version` construct no infrastructure at all.

Module dependency direction: HTTP roots import `service/` and (for big modules) `domain/`, but
never the other way around — a big module such as billing keeps routes/handler/request plus its
gateway adapters at the module root, with shared types in `domain/` and all business services in
`service/`, which is what keeps the packages cycle-free. See `AGENTS.md` "Module anatomy".

## 2. Request lifecycle

Middleware order (outermost first, `internal/app/routes.go`; the dashboard Inspector shows the
live table with each route's middleware and permission):

```text
request-id → public-origin (non-local) → real-ip → security-headers → cors → access-log
  → recover → telescope capture → metrics → timeout → throttle → locale
   │
   ├─ public: /up, /api/health, /api/v1/login|verify-otp, forgot/reset-password,
   │          e-mail verification, /api/v1/public/packages, gateway callbacks, /storage/*
   ├─ auth (bearer token), no tenancy: logout, logout-all
   ├─ auth → tenant.resolve: users, branches, tenant roles, settings, feature flags,
   │          activity, notifications, files, import/export, billing status/payments/subscribe
   ├─ auth → tenant.exempt (+ tenant.internal): tenants, permissions, internal users/roles,
   │          packages, internal billing (subscriptions, payments, approvals)
   └─ /dev, /dev/api: dev.enabled → auth → tenant.exempt → can:dev.view
                      (mutations add can:dev.manage)
```

Each route declares its permission as route middleware (`r.With(h.can(perm))`, Laravel's `can:`);
`internal/app/route_permissions_integration_test.go` asserts every protected route answers 403 to
a user without permissions.

Resolve behavior (port of `TenantResolveMiddleware`):

- Impersonators (Super-Admin or `user.tenant.switch`): read `X-Acting-Tenant`/`X-Acting-Branch`.
  No headers = global **read** view (tenant 0); writes without acting headers are 403.
- Tenant users: require `tenant_id` and an active branch; the branch-belongs-to-tenant check is
  cached 5 minutes (`branch_check:*`) and invalidated on branch writes.
- Exempt routes set `Scope.Internal`; only internal or global-impersonation scopes read all
  tenants (`Scope.Filter()` → `tenancy.Unscoped()`).

Services and repositories never read ambient state: handlers pass the resolved
`tenancy.Scope` into services, which pass explicit ids into repositories.

## 3. Configuration and lifecycle

- `.env` precedence: `--env-file` → `APP_ENV_FILE` → `.env` → code defaults. `godotenv.Load`
  never overrides the process environment, so systemd/shell values win.
- Typed config (`internal/config`) validates per mode; a missing default `.env` is fine because
  every setting has a code default (dev-oriented; production supplies real values).
- `serve` shuts down gracefully within `HTTP_SHUTDOWN_TIMEOUT`; queue workers drain; the scheduler
  waits for in-flight tasks.

## 4. Identity: auth, tenancy, RBAC

**AuthN** (`internal/authn`): opaque personal access tokens stored SHA-256-hashed in
`personal_access_tokens` with per-device rows; wire format `{id}|{secret}`.
`Authenticator` interface (`Issue/Verify/Revoke/RevokeAll`) isolates the engine so a JWT driver
can be added later. Login supports lockout throttling (cache-backed, 5 attempts/min per
email|IP), active/email-verified checks, optional OTP (cache-only, 6 digits, 5 min), and updates
`last_login_at`/device. `last_used_at` is touched at most once per minute.

**Tenancy** (`internal/tenancy`): `Scope{TenantID, BranchID, Acting*, Internal}`; `Acting` is
`None|Tenant|Branch|Global`. Single-row repository methods take `scope.Filter()`, rendered in SQL
as `(@all_tenants OR tenant_id = @tenant_id)`; a zero `Filter{}` matches **no** tenant rows, so a
forgotten scope fails closed. Lists filter on `EffectiveTenantID()/EffectiveBranchID()`. Scoping
is explicit parameters rather than ORM global scopes.

**Account recovery** (`authn.Recovery`): forgot/reset password and e-mail verification mail a
link with a random token; only its SHA-256 lives in the cache, it works once, and responses never
reveal whether an e-mail exists. A reset signs the user out everywhere; changing your own
password signs out your other devices.

**Activity log** (`modules/activity`): services record changes tenant admins should see
(`activity.Recorder.Record`, nil-safe, never fails the caller; actor, IP and request id from the
context; secret-looking keys redacted). `GET /api/v1/activity` is a tenant-scoped cursor feed;
`activity.prune` applies `ACTIVITY_RETENTION_DAYS`.

**RBAC** (`internal/authz`): permissions catalog in Go, seeded by `permission:sync`.
`Checker.Can/MustCan` bypasses for Super-Admin
(`roles.name='Super-Admin' AND is_system`), caches per-user permission sets (10 min) and
invalidates on role/permission mutations. System roles/permissions are immutable from tenant
surfaces. `model_has_roles.model_type` is the constant `user`.

## 5. Data layer

- **Connections**: `database.Registry` opens named pgx pools from `DATABASE_*` +
  `DATABASE_CONNECTIONS` extras; pools are lazy, health-checked, closed on shutdown.
- **Migrations**: goose SQL under `migrations` is the single schema source, embedded in
  the binary; River's migrations run in the same `ims migrate` step (River first on rollback).
  A Postgres advisory lock serializes concurrent migrations.
- **Queries**: sqlc (`make sqlc`, pinned v1.31.1) generates `internal/dbgen` from the
  same migrations + `sql/queries`. CI regenerates and fails on a diff.
- **Transactions**: `database.TxManager.WithTx(ctx, fn(q *dbgen.Queries) error)` shares one
  transaction handle across repositories; services own transaction boundaries.
- **Errors**: repositories translate driver errors via `database.MapError` (23505→409, 23503→409,
  23502→422) and `pgx.ErrNoRows` → `apperr.ResourceNotFound`.
- **Conventions**: column-level audit (`created_by`/`updated_by`) stamped by services;
  soft deletes with partial unique indexes (`WHERE deleted_at IS NULL`); timestamps mapped from
  `pgtype.Timestamptz` in `model.go`; ids are int64 bigserial.

## 6. Platform services

- **Cache** (`platform/cache`): `Store` interface; drivers `memory`, `file`, `redis`,
  `database` selected by `CACHE_DRIVER` via `cache/factory` (default `memory` in local
  environments, `database` elsewhere — `memory`/`file` are per-process). The memory driver is
  bounded (`CACHE_MEMORY_MAX_ENTRIES`, default 100 000). Keys are plain strings owned by the
  module that caches (e.g. `authz` permission sets, `tenancy` membership checks, settings);
  each module deletes its own keys on writes — there is no global versioned-key layer.
  Contract tests (`cachetest`) run against every driver.
- **Queue** (`platform/queue`): typed job registry + `Engine` interface. `sync` executes inline
  (tests/CLI); `river` persists to Postgres with transactional enqueue (`EnqueueTx`). A single
  River job kind (`ims.job`) carries `{kind, payload, tenant_id, branch_id}`; the dispatcher
  restores tenancy before calling the typed handler. Queue names `critical/default/heavy` each
  carry their own tries/timeouts. `queue:work` (optionally `--once`),
  `queue:stats`, health check on `river_job`.
- **Scheduler** (`platform/scheduler`): central registry with validated cron specs, overlap-skip,
  timeouts and structured logs. `schedule` runs the daemon; `schedule:list`/`schedule:run`
  inspect. Tasks registered in `internal/app/platform.go`: `scheduler.heartbeat` (1 min),
  `billing.expire-subscriptions` (every 6h), `billing.send-renewal-reminders` (08:00),
  `backup.run`/`backup.clean` (when enabled), and retention — `dataprocessing.cleanup-completed`,
  `notifications.purge-expired`, `activity.prune`, `devdashboard.prune` (telemetry + dashboard
  audit), `cache.purge-expired` (database cache). Only one scheduler runs at a time (Postgres
  advisory lock). The heartbeat lives in the cache — with `memory` it is only visible inside the
  same process.
- **Settings** (`modules/settings`): typed, grouped catalog with a polymorphic target
  (`target_type`/`target_id`); resolution tenant override → global row → default (two levels);
  one cache blob per target (global/tenant) plus a groups blob; grouped `GET /settings`,
  tenant-admin `PATCH /settings/tenant/{key}`, internal target-aware CRUD; internal settings-group
  CRUD (`/internal/settings-groups`) plus key→group assignment
  (`PUT /internal/settings/{key}/group`, guarded by `settings_group.*`); `settings:sync` upserts
  groups and missing globals without overwriting dev-panel assignments.
- **Feature flags** (`modules/featureflags`): separate boolean-flag catalog/table so flag reads
  never load settings; resolution user → tenant → global → default; one small cache blob per
  target; internal-only writes with the target in the payload; `feature:<key>` gate middleware
  (`platform/featureflags`); `feature-flags:sync` seeds globals. An enabled value can carry a
  **percentage rollout** (`rollout: {"feature.x": 60}` on the internal/dev PATCH): each user (or
  tenant, with `Flag.RolloutBy = RolloutByTenant`) hashes to a fixed bucket 0-99 per flag, so the
  same user always gets the same answer and raising 20% → 60% only adds users. 100% is stored as a
  plain "on" (`rollout` NULL). The rollout is decided per request, after user → tenant → global
  resolution, so a user or tenant override still pins the value for that target.
- **Storage** (`platform/storage`): named disks with a driver (`local` | `s3`) and a
  visibility — see decision 15. `Disk` = `Put/Get/Delete/Exists/Size/URL/TemporaryURL/Public`.
- **Health** (`platform/health`): registry with checks for environment, debug mode, application,
  database, migrations+River schema, cache, queue, scheduler and backups; advisory checks never
  make the endpoint unready. `/api/health` answers 503 only on critical failures (`?fresh=1`
  forces a run from loopback), `/up` is a bare liveness probe.
- **Telemetry** (`platform/telemetry`): when the dashboard is enabled, requests, SQL, cache and
  queue events, logs (warn+) and exceptions are captured (sampled, bodies redacted) for the
  dashboard and pruned after `DEV_DASHBOARD_RETENTION_DAYS`.
- **Developer dashboard** (`modules/dev` + `web/dev`): a React SPA embedded in the binary.
  Handlers call panel services (`dev/services.go`, `inspector.go`, `commands.go`); every mutation
  needs `dev.manage` on the route and is audited in `dev_actions` with IP and browser, as are
  dashboard sign-ins. The Commands panel runs the allowlist in `internal/app/dev_commands.go`
  in-process (options, confirmation, background runs, output history).
- **Rate limiting** (`platform/ratelimit`): `Limiter` interface; the cache-backed fixed-window
  limiter guards the API globally (per IP; user-keyed once auth lands in front of it),
  `Retry-After` + problem 429 on rejection.

## 7. Billing

- **Catalog**: features in Go (`features.go`), limits = `users.max`, `branches.max`,
  `storage.mb`; resolution overrides → package → denied. Guard returns 402 problems with
  `meta {feature_key, limit, current}`.
- **Packages**: CRUD + features (replace-on-update), public endpoint for active packages.
- **Subscriptions**: `trial|pending|active|past_due|cancelled|expired`; `nextExpiry` monthly/
  yearly/one-time(≈100y). Subscribe creates a pending subscription, charges, then records a
  manual approval for cash/bank. Subscribe is idempotent per tenant + package + method: while a
  checkout is pending, a retry returns it (serialized by a per-tenant advisory lock). Activation
  deactivates rivals in the same transaction. **Cancel is immediate** (the guard honours only
  active/trial); `cancelled` is terminal — `ActivateSubscription` refuses it in SQL.
- **Payments**: orchestrator + drivers (`bkash` tokenized checkout with a 55-min token cache shared
  under a mutex and dropped on 401/403, `cash`, `bank_transfer`, `nagad` stub); invoice numbers
  `INV-XXXX-timestamp` (crypto/rand, errors never fall back). Payment status changes follow
  allowed transitions only (pending → any, failed → completed, completed → refunded).
- **Callbacks** are public and resolve payments by the unique `gateway_ref`. The callback body is
  only a hint: bKash is always asked (execute on "success", status query otherwise); only a
  status bKash reports as Failed/Cancelled fails a payment, anything unfinished stays pending,
  and a gateway non-2xx is a retryable 503 that changes nothing. Cash/bank never accept callbacks
  and get unguessable `CASH-`/`BANK-` references. Completion + activation is one transaction.
- **Approvals**: transactional approve/reject (payment status + subscription status atomically).
- **Enforcement**: resource services (identity, branches, files) call small limiter interfaces
  implemented by the billing guard — no module-to-module coupling. `users.max`/`branches.max`
  run count → check → insert under a per-tenant advisory lock; `storage.mb` is checked on
  library uploads when the plan defines it.

## 8. Error and validation contract

All errors are RFC 9457 `application/problem+json` with `type/title/status/detail/instance`,
plus `error_code`, `timestamp`, `trace_id` (5xx only) and extension members:
`errors` (422 field maps with dot paths), `meta` (e.g. feature limits), `retry_after`.
Success envelopes are `{"data": ...}`; lists add `links` and `meta`. `debug` is added outside
production when `APP_DEBUG=true`.
Validation is structural in the transport (`h.Bind`: strict JSON + validator with humanized messages);
business validation lives in services.

## 9. CLI surface

`serve`, `queue:work`, `queue:stats`, `schedule`, `schedule:list`, `schedule:run`,
`migrate`, `migrate:status`, `migrate:rollback`, `db:seed`, `permission:sync`, `settings:sync`,
`feature-flags:sync`, `cache:clear`, `health:check`, `config:show`, `backup:run|list|clean|monitor`,
`billing:expire-subscriptions`, `billing:send-renewal-reminders`, `data-processing:cleanup-completed`,
`dev-dashboard:prune`, `gen module`, `gen openapi`, `openapi:check`, `version`.
Commands boot the platform through `withPlatform(state, fn)` (validate config → `NewPlatform` →
run → clean up); the common maintenance commands are also runnable from the dashboard.

## 10. Testing topology

- Unit tests in-package; integration tests skip without `DATABASE_TEST_DSN`.
- `dbtest` gives every test package its own Postgres schema (deterministic per directory) with
  goose+River migrations applied — `go test ./...` is parallel-safe against one database.
- `internal/testkit` builds the real app on that schema (seeded, unlimited plan) with factories
  and a request builder; the `internal/app` suite and module integration tests use it.
- `route_permissions_integration_test.go` walks every route and asserts a permissionless user is
  refused, so a route without `can:` fails CI.
- Cache drivers share a contract suite; gateways are tested against `httptest` stubs; CLI has
  smoke tests through `BuildRoot`.

## 11. Design decisions

The running list (keep it in sync with `AGENTS.md`):

1. Explicit query scoping instead of global scopes; explicit tenancy params everywhere.
2. Tenant/audit stamping in services, not model observers.
3. `ActingGlobal` impersonation is a read-only, all-tenant view; writes need acting headers.
4. Bigserial sequences are synced after explicit-id seeding (`SyncIDSequence`).
5. Subscribe validates and charges before deactivating previous subscriptions; gateway failure
   cancels the fresh pending subscription.
6. E-mail is not changeable through user update.
7. `order_by`/`order_direction` are whitelisted per endpoint.
8. One error universe: RFC 9457 everywhere.
9. OTP codes are cache-only; delivery goes through the queue (`mail.send`).
10. Local scheduler heartbeat visibility requires a shared cache driver (memory is per-process).
11. Notification fan-out counters are idempotent (`INSERT ... ON CONFLICT ... RETURNING`); reads
    are user-scoped (never branch-scoped); mark-all-read is scoped + idempotent; unread counters
    recompute from the database on cache miss. Import/export job rows report real progress and an
    honest FAILED status; downloads require COMPLETED + file.
12. One `files` table with polymorphic attachments
    (`owner_type`/`owner_id`/`collection`) with a Go collection registry, single-file collections
    enforced by service + partial unique index; no conversions/custom-properties. The resource is
    `/v1/files` with `file.*` permissions (legacy `upload.*` rows are retired by migration).
13. i18n catalogs cover the API-relevant message groups (`messages`, `http-statuses`,
    `validation`; ~390 keys × en/bn); validator tags map to `validation.*` keys via go-i18n.
14. Legacy `.xls` imports are rejected (xlsx/csv supported); per-row import errors use a
    `[{row, errors:{...}}]` shape; filter snapshots live in `data_processing_jobs`.
15. **Storage disks** (`config/storage.go`, `platform/storage`) work like Laravel's
    `config/filesystems.php`: each disk has a name, a driver (`local` | `s3`) and a visibility.
    Built-ins are `private`, `public` and `s3` (from `S3_*`); `STORAGE_DISKS=r2,minio` adds more
    from `STORAGE_<NAME>_*`. `FILES_DISK`/`DATA_PROCESSING_DISK` name any disk, so moving uploads
    to S3, Cloudflare R2, MinIO, Spaces or B2 is a `.env` change. Private disks (local or bucket)
    never hand out URLs; files are read through the authorized download route, and avatars go to
    `FILES_PUBLIC_DISK`. The S3 driver supports a key prefix (`ROOT`), presigned
    `TemporaryURL`s, and only sends checksums when required (S3-compatible services reject the
    SDK default). File URLs are computed from disk + path (no stored `url` snapshot).
16. **Contract.** `api/openapi.yaml` is canonical and served verbatim; oapi-codegen runs
    types-only (the handwritten chi chain + `Responder` own the HTTP layer) and its output is
    diff-gated in CI. Docs are served via Scalar from a CDN (config-gated with `DOCS_ENABLED`).
17. **Observability.** `/metrics` is a small dependency-free Prometheus text exporter
    (`internal/platform/metrics`, low cardinality: method/route/status only), config-gated with
    `METRICS_ENABLED`. A Content-Security-Policy header is deliberately omitted so the docs UI can
    load its CDN assets; baseline headers (nosniff, frame-deny, referrer, HSTS-over-TLS) are on.
18. **Tenant isolation (no RLS).** Postgres row-level security was evaluated and
    deliberately dropped: a correct setup needs a dedicated non-owner application role, a
    fail-closed policy, and the tenant GUC set on *every* connection (not just transactions) —
    and even then it would only backstop the explicit filters below. Instead, **explicit tenant
    scoping is the only isolation boundary**: every tenant-scoped query MUST receive and filter on
    an explicit `tenant_id` (and `branch_id` when branch-scoped), and a missing filter is a
    security bug. Coverage is enforced by the per-module isolation tests, not by the database.
19. **Deployment.** Single binary + systemd (units: `ims-go-api`, `-scheduler`,
    `-worker`); a **build-on-server** `deploy/scripts/release.sh` (no container/artifact host) that
    builds a static binary, runs migrations as a gated release step, atomically switches the
    `current` symlink, restarts units and health-checks. Writable state lives under
    `/var/lib/ims/ims-go-api` (never inside a release directory). Runbooks live in
    `docs/runbooks/`.
20. **Trusted proxies.** Forwarded headers (`X-Forwarded-For`/`X-Real-IP`) are honoured
    **only** when the immediate peer is listed in `HTTP_TRUSTED_PROXIES`, and the client IP is
    resolved right-to-left skipping trusted hops (an explicit, reviewable policy rather than
    chi's default `middleware.RealIP`). The resolved IP drives login throttling, rate limiting
    and access logs.
21. **Generators.** `ims gen module --fields="name:string,price:decimal,…"` scaffolds a
    goose migration, sqlc queries and a full module package, non-interactive and CI-friendly,
    then runs `sqlc generate` so the output compiles. Generated modules use explicit `tenant_id`
    scoping, `httpx.Kit` handlers and route-level permissions (`r.With(h.can(...))`); the
    generated service test needs no database.
22. **SQLite deferred.** v1 is **PostgreSQL-only**. SQLite was scoped and
    deliberately deferred to a future version: with sqlc it needs a second, permanently-duplicated
    repository set (per-module), and an ORM-style alternative would mean rewriting the whole data
    layer and giving up compile-time SQL + explicit tenant scoping. `DATABASE_DRIVER=sqlite` is
    rejected at startup; the schema/queries remain Postgres-only.
23. **Authorization at the HTTP edge.** Permissions are declared on routes
    (`r.With(h.can(perm))` → `authz.Require`), mirroring Laravel's `can:` middleware; a handler
    calls `MustCan` itself only when the permission depends on the payload. CLI commands, queue jobs and scheduled tasks are trusted callers
    and do not re-check permissions. Services remain permission-free unless they need a Gate-style
    decision, in which case they take `*authz.Checker` explicitly. Services and SQL **always**
    enforce tenant scoping and business invariants (Super-Admin protection, no self-escalation)
    regardless of caller. A route-coverage integration test asserts every protected route returns
    403 to a token without permissions, so a forgotten handler check fails CI.
24. **Laravel-style handlers.** Handlers embed `httpx.Kit` (`Bind`, `Validate`, `PathID` and the
    responder methods) and read `authn.UserID(r)` / `tenancy.From(r)`; config is split one file per
    section like `config/*.php`; CLI commands share one `withPlatform` helper.
25. **Feature-flag rollout.** An enabled flag can carry a percentage; subjects hash to a stable
    bucket per flag, so raising the percentage only adds users (see §6).
26. **Quotas under advisory locks.** `users.max`/`branches.max` count-check-insert runs under a
    per-tenant Postgres advisory lock (`TxManager.WithTenantLock`), so concurrent creates cannot
    exceed a plan limit.
27. **Dashboard audit.** Every dashboard mutation and dashboard sign-in (including failures, never
    the password) is written to `dev_actions` with IP and browser, kept for
    `DEV_DASHBOARD_AUDIT_RETENTION_DAYS`.
28. **Inactive tenants.** `tenants.is_active` is stored and shown but not yet enforced at request
    time.
