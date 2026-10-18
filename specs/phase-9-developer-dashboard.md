# Phase 9 — Developer dashboard (lightweight Telescope)

**Status:** Accepted (rev 2 — ready to implement)
**Roadmap:** PLAN.md §7.21 + §11 Phase 9
**Depends on:** Phases 0–7, 10; builds on Phase 3 (queue/scheduler/cache) and Phase 6
(observability, contract). The admin dashboard (Phase 8) is deliberately deferred — Phase 9 is
self-contained and ships the developer surface first.
**Spec owner:** —

> **Revision 2 (2026-10-18).** Research + design pass. Changes from rev 1: River UI is dropped in
> favor of a first-party queue admin API built on the River client (see Decision 1); scope is
> developer-dashboard-only (admin stays a stub spec); a **hidden `Developer` role and seeded
> developer user** are added as the non-super-admin access path (Decision 2); the SPA stack is
> **TanStack Start (SPA mode) + TanStack Query/Table/Form/Virtual** (Decision 8); the two-gate
> model (`OBSERVABILITY_ENABLED` + `DEV_DASHBOARD_ENABLED`) collapses to one flag (Decision 3);
> config, routes, migration, CI and acceptance sections are now concrete.

## Goal

A debugging surface for developers, embedded in the same binary and gated so it is unreachable in
production unless explicitly enabled:

1. **Identity:** the dashboard is reachable by super-admins and by a seeded **hidden `Developer`
   role/user** that holds every catalog permission (a deliberate second-super-admin used for
   day-to-day development; it never appears in role responses).
2. **Telemetry pipeline:** capture HTTP requests, SQL queries, cache/queue activity and correlated
   logs per request, redacted before persistence.
3. **Queue & ops admin:** first-party queue management (jobs, retries, cancels, queue
   pause/resume) built on River's client admin API — **no River UI dependency**.
4. **SPA at `/dev`:** a React dashboard (TanStack Start in SPA mode, TanStack Query/Table/Form)
   browsing telemetry, managing the queue, running whitelisted developer actions, and linking the
   existing Scalar docs and health endpoints.

## Reference (Laravel)

No direct parity — this phase is **new capability** (the Laravel starter used Telescope / Pulse /
Horizon; we deliberately do not port them). Behavior bars to match:

- `config/queue.php`, `app/Registry/QueueRegistry.php` — channel names/semantics
  (`critical` / `default` / `heavy`) our queue screen must reflect.
- Horizon's queue dashboard scope (failed jobs, retry, recent jobs, queue pause) — the feature bar
  for Deliverable B, not a code source.

## Decisions (resolved)

1. **No River UI; first-party queue admin.** `riverqueue.com/riverui` (latest v0.21.0) pins
   `river v0.48.0` while this repo uses `river v0.49.0`, and drags in a heavy transitive set
   (SQLite drivers, embedded UI). Our needs are fully covered by the River client's admin API
   already present in v0.49.0: `JobList/JobGet/JobRetry/JobCancel/JobDelete/JobDeleteMany`,
   `QueueGet/QueueList/QueuePause/QueueResume`. We build a thin `queue.Admin` wrapper and expose
   it as `/dev/api/queue/*` + a SPA screen. The `/observability` aggregation index is dropped; the
   SPA links `/docs` and `/api/health` itself.
2. **Hidden `Developer` role + user.** `authz.DeveloperRole = "Developer"`: global system role
   (`tenant_id NULL`, `is_system true`), new `roles.hidden` column set `true`, assigned **every
   catalog permission** (same explicit mechanism as `Super-Admin`). Seeded user
   `developer@ims.com` (password `AUTH_DEFAULT_PASSWORD`, no tenant) holds it. Hidden roles are
   excluded from every role **list** response (SQL predicate) and 404 on the tenant-surface
   `GET /api/v1/role/{id}`; the `hidden` flag itself is never serialized. Developer user is a
   capability-equivalent of super-admin — documented in the runbook, prod seeding note included.
3. **One flag.** `DEV_DASHBOARD_ENABLED` (default `true` outside production, `false` in production)
   gates the SPA, the JSON API and the capture pipeline. Disabled ⇒ **404** on all `/dev*` routes
   (register-then-404 so the route table is stable for `openapi:check`). `observability.view`
   becomes **`dev.view`** and new **`dev.manage`** (both `is_system`, group `dev`); super-admin
   bypasses, hidden Developer role holds both. Missing permission ⇒ 403 problem.
4. **Routes:** `/dev` + `/dev/*` (SPA shell/assets) and `/dev/api/*` (JSON). Registered under
   `authn.Authenticate` → `tenancy.Exempt` (both super-admin and developer users are tenant-less;
   tenant-resolved routes reject them). Static SPA paths are excluded from route-drift like
   `/storage/*`; `/dev/api/*` paths are hand-written into `api/openapi.yaml` **outside** the
   generated regions (`ims gen openapi` cannot emit non-`/api/v1` paths — documented limitation).
   A request under `/dev/api/` that matches no API route returns problem+json 404, never the shell.
5. **Capture hooks, not a rewrite.** HTTP: a non-blocking middleware after `AccessLog`. SQL: pgx
   `QueryTracer` attached via `poolCfg.ConnConfig.Tracer` in `database/registry.go`. Cache: a
   decorator around `cache.Store` applied at wiring. Queue: decorator around `queue.Engine` for
   dispatch + a wrapper on `Definition.Handle` for worker-side results. Logs: a `slog.Handler`
   wrapper buffering by request id, flushed by the request middleware. No kernel restructuring.
6. **Storage: DB tables + prune.** Seven tables (`dev_requests`, `dev_queries`,
   `dev_cache_events`, `dev_queue_events`, `dev_logs`, `dev_exceptions`, `dev_actions`) with
   `created_at` indexes and a cursor id; a `dev-dashboard:prune` CLI command + a daily scheduled
   task (`devdashboard.prune`) delete rows older than `DEV_DASHBOARD_RETENTION_DAYS`. Best-effort:
   writes go through a bounded channel; drops are counted (`telemetry_dropped_total` counter) and
   logged, never block a request. The optional file/ring-buffer store is deferred (out of scope
   v1); `DEV_DASHBOARD_STORE` is parsed but only `database` is accepted.
7. **Redaction before persistence** for `Authorization`, `Cookie`, `Set-Cookie`, `password`,
   `password_confirmation`, `token`, `otp`, `secret`, `api_key`, `X-Api-Key`, plus
   `DEV_DASHBOARD_REDACT_KEYS`; applied to headers, request/response bodies, SQL args, cache
   values and job args. Redactor has a dedicated test suite.
8. **SPA stack (confirmed): TanStack Start in SPA mode.** `web/dev` contains a TanStack Start
   app built with `spa: { enabled: true }` (Vite under the hood — PLAN's "pure React, no Next.js"
   holds), producing a static shell (`_shell.html`) + hashed assets. Basepath `/dev` must be set in
   **three places** (`vite.config.ts` `base`, `tanstackStart({ router: { basepath } })`, and
   `createRouter({ basepath })`) — a known TanStack gotcha; the spec fixes all three. Data via
   **TanStack Query** (typed with `openapi-typescript` + `openapi-fetch`), tables via **TanStack
   Table**, virtualized streams via **TanStack Virtual**, forms via **TanStack Form**. Auth:
   bearer token (same tokens as the API) held in `localStorage` — acceptable for a flag-gated,
   super-admin/equivalent-only tool; Phase 8 may revisit token-vs-cookie for the admin SPA.
9. **Developer actions are service calls, never shell.** Run a scheduled task
   (`ScheduleRunner.RunNow`), queue pause/resume, job retry/cancel/delete, `cache:clear`
   (`Store.Clear`), `permission:sync`, `settings:sync`, run health checks (`Health.Run`). Every
   mutating action writes a `dev_actions` audit row (actor, action, params, status, result,
   duration) which is itself visible in the SPA.
10. **Contract stays hand-written for `/dev/api`.** `ims gen openapi` only emits `/api/v1/*`
    (scaffold `APIBase`); `/dev/api` paths live in `api/openapi.yaml` outside the managed regions.
    `openapi:check` keeps passing because `/dev` routes are always registered; drift excludes the
    two static SPA patterns.

## Deliverables

Four independently mergeable deliverables. B and C are parallel; D consumes B/C endpoints and can
ship incrementally (queue screen first).

### Deliverable A — Developer identity & access

- Migration `migrations/00013_add_roles_hidden.sql`: `roles.hidden boolean NOT NULL DEFAULT false`
  (+ partial index untouched; no data backfill needed).
- `authz` catalog: `DeveloperRole = "Developer"`; permissions `dev.view` ("View developer
  dashboard") and `dev.manage` ("Execute developer actions"), group `dev`, `IsSystem: true`
  (new rows — remember `permission:sync` does not update `is_system` on conflict).
- `sql/queries/roles.sql`: `ListRoles` gains `AND hidden = false`. Tenant-surface `GET
  /api/v1/role/{id}` returns 404 for hidden roles (guard in handler/service); update/delete/assign
  already 403 for system roles. `RoleResponse` unchanged — no `hidden` field is serialized.
- Seeder: `ensureDeveloperRole` (find-or-create hidden system role, then assign **all** catalog
  permissions via the same helper that serves `Super-Admin`), `ensureDeveloperUser`
  (`developer@ims.com`, `AUTH_DEFAULT_PASSWORD` hashed with bcrypt rounds, `IsActive`,
  `EmailVerifiedAt`, role assignment, `checker.InvalidateUser`). Idempotent; runs after the
  super-admin block in `seeder.Run`.
- Route gate: dev routes always registered; middleware returns 404 when `DEV_DASHBOARD_ENABLED` is
  false, else requires `dev.view` (`dev.manage` for mutating endpoints) via `authz.Require`.
- Config: `DEV_DASHBOARD_ENABLED` (default `!IsProduction()`), plus the capture keys listed below.
- Runbook note: the developer account is a second super-admin; rotate/disable per environment;
  `db:seed` remains the only place it is created.

### Deliverable B — Queue & ops admin API (replaces River UI)

- `internal/platform/queue`: new narrow `Admin` interface + `AdminJob`/`AdminQueue` DTOs
  (application kind decoded from the `ims.job` envelope, state, queue, priority, attempts,
  max attempts, run-at, timestamps, **redacted** args, error history truncated to last N).
  `RiverEngine` implements it by delegating to the River client (`JobList` cursor pagination via
  `After/First`, filters `Queues/States/Kinds/OrderBy`, `JobGet`, `JobRetry`, `JobCancel`,
  `JobDelete`, `QueueList`, `QueuePause`/`QueueResume` incl. `"*"`). The `*river.Client` stays
  private; `Platform.QueueAdmin queue.Admin` is populated at wiring and is `nil` when
  `QUEUE_DRIVER=sync` (routes then return 503 problem `queue admin unavailable`).
- Endpoints (all under the dev gate):
  - `GET /dev/api/queue/queues` — list with counts (reuse `queue.Stats`).
  - `POST /dev/api/queue/queues/{name}/pause|resume` — `dev.manage`, audited.
  - `GET /dev/api/queue/jobs` — cursor list; filters `queue`, `state`, `kind`, `limit`
    (default 25, max 100).
  - `GET /dev/api/queue/jobs/{id}`; `POST /dev/api/queue/jobs/{id}/retry|cancel`;
    `DELETE /dev/api/queue/jobs/{id}` — mutations `dev.manage`, audited.
- `make queue-stats` convenience target (wraps `ims queue:stats`); CLI stays as the fallback.

### Deliverable C — Telemetry capture + store

- `internal/platform/telemetry`: collector (bounded channel, batch insert, drop counter),
  redactor, sampler, per-capture toggles, retention constants. `Sink` interface; the DB sink
  implementation lives in the dev module (queries owner) and is wired in `internal/app`.
- HTTP middleware capture: method/path/route-pattern/status/duration/bytes, request + response
  bodies (bounded by `DEV_DASHBOARD_MAX_BODY_BYTES`, default 64 KiB), headers (redacted), user +
  tenant, request id, client IP.
- pgx `QueryTracer`: statement, redacted args, duration, connection, rows affected; attached only
  when capture includes `sql`.
- Cache hooks (get/set/delete/hit-miss/increment) and queue hooks (dispatched/executed/failed +
  worker process); correlated `slog` capture with level filter; exceptions with sanitized stack
  (debug detail masked outside debug, same rule as the error responder).
- `dev-dashboard:prune` CLI (deletes each table older than retention) + daily scheduled task
  `devdashboard.prune` registered when the dashboard is enabled.
- Overhead budget: ≤ 3% p95 added latency at sample rate 1.0 for `http,sql` (measured by a
  `bench/` load smoke recorded with the PR); capture drops counted, never blocks.

### Deliverable D — Developer SPA (`/dev`)

- `web/dev`: TanStack Start (SPA mode) app, basepath `/dev`, built to `web/dev/dist`, embedded
  with `go:embed all:dist` (placeholder kept in git so Go builds without Node). New
  `internal/platform/webui` package serves the FS: hashed assets `Cache-Control: immutable`,
  shell `no-cache`, SPA fallback for `/dev` and `/dev/*` (GET/HEAD; `/dev/api/*` never falls back
  to the shell).
- Screens (TanStack Query polling ~2 s toggleable per screen):
  - Login (bearer via `POST /api/v1/login`) + `GET /dev/api/me` bootstrap (identity + permissions;
    403 screen when neither super-admin nor `dev.view`).
  - Overview: health summary, queue counts, recent exceptions, request volume.
  - Requests list/detail: filters (status, method, path, tenant, user, request id), detail tabs for
    SQL, cache events, queue events, logs and exceptions correlated by request id.
  - Queue: queues with pause/resume; jobs table (TanStack Table, cursor pagination) + detail
    drawer; retry/cancel/delete actions with confirm forms.
  - Logs (level filter, virtualized), Exceptions, Scheduler (task list + run-now with confirm),
    Health (fresh run), Actions (audit log).
- Types: `npm run openapi:gen` (`openapi-typescript api/openapi.yaml`) committed as
  `web/dev/src/api/schema.d.ts`; CI regen + diff gate. `openapi-fetch` client injects
  `Authorization` and points at `/dev/api`.
- Makefile/CI: `make web-build` (`npm ci && npm run build` in `web/dev`), `make dev-web` (Vite dev
  server on :5173 with proxy `/dev/api` → `:8080`), release script gains the web build before
  `go build`; CI adds a Node job (install → typecheck → build → openapi types diff).

## Contracts touched

- **Migrations:** `00013` roles.hidden; `00014` dev_* tables (`dev_requests`, `dev_queries`,
  `dev_cache_events`, `dev_queue_events`, `dev_logs`, `dev_exceptions`, `dev_actions`) each with
  `id bigserial`, `request_id text NULL`, `tenant_id bigint NULL`, JSONB payload columns,
  `created_at timestamptz`, index `(created_at)` and `(request_id)`; `dev_actions` also
  `actor_user_id`, `action`, `status`, `duration_ms`.
- **API endpoints (hand-written in `api/openapi.yaml`):** `GET /dev/api/me`, `GET /dev/api/health`,
  `GET /dev/api/requests`, `GET /dev/api/requests/{id}`, `GET /dev/api/logs`,
  `GET /dev/api/exceptions`, `GET /dev/api/queue/queues` (+ pause/resume), `GET /dev/api/queue/jobs`
  (+ get/retry/cancel/delete), `GET /dev/api/scheduler/tasks` (+ run), `GET /dev/api/actions`,
  `POST /dev/api/cache/clear`, `POST /dev/api/sync/permissions`, `POST /dev/api/sync/settings`.
  Responses use the standard `{data}` envelope / problem+json; telemetry and job lists use cursor
  pagination (`limit` + `cursor`) like the notification feed.
- **Route-drift exclusions:** exact patterns `/dev` and `/dev/*` added next to `/storage/*`.
- **Env vars:** `DEV_DASHBOARD_ENABLED`, `DEV_DASHBOARD_CAPTURE` (csv: `http,sql,cache,queue,logs`),
  `DEV_DASHBOARD_SAMPLE_RATE` (0–1, default 1.0), `DEV_DASHBOARD_RETENTION_DAYS` (default 7),
  `DEV_DASHBOARD_MAX_BODY_BYTES` (default 65536), `DEV_DASHBOARD_REDACT_KEYS` (csv, extra),
  `DEV_DASHBOARD_STORE` (`database`, default/only value in v1). `.env.example` updated.
- **Permissions:** `dev.view`, `dev.manage` (catalog, group `dev`, system).
- **CLI:** `dev-dashboard:prune`; existing `queue:stats`, `permission:sync`, `settings:sync`,
  `schedule:run` reused by dashboard actions through services (not shell).
- **Scheduler task:** `devdashboard.prune` (daily).

## Scope checklist

Deliverable A:

- [ ] `roles.hidden` migration + `ListRoles` filter + tenant-surface 404 guard
- [ ] `dev.view` / `dev.manage` in catalog (+ `permission:sync` verified)
- [ ] Seeder: hidden `Developer` role (all permissions) + `developer@ims.com` user; idempotent
- [ ] Dev route gate middleware (404 when disabled, 403 without permission), config keys
- [ ] Tests: hidden role absent from role lists; developer login reaches `/dev/api/me`

Deliverable B:

- [ ] `queue.Admin` interface + RiverEngine implementation (client stays private)
- [ ] `/dev/api/queue/*` handlers (cursor lists, filters, audited mutations, sync-driver 503)
- [ ] `make queue-stats`; runbook note

Deliverable C:

- [ ] `internal/platform/telemetry` (collector/redactor/sampler/toggles/retention)
- [ ] HTTP middleware + pgx tracer + cache/queue decorators + slog handler capture
- [ ] Dev tables + prune CLI + `devdashboard.prune` task; drop counter in metrics
- [ ] Redactor test suite; overhead smoke recorded

Deliverable D:

- [ ] `web/dev` TanStack Start SPA (basepath `/dev` in all three places), embed + `webui` handler
- [ ] Screens: login/overview/requests/queue/logs/exceptions/scheduler/health/actions
- [ ] `openapi-typescript` types + CI diff; `make web-build`, `make dev-web`; CI Node job
- [ ] e2e: `/dev` 404 when disabled; 403 for a tenant `Admin`; OK for super-admin **and**
      developer user

## Out of scope

- Admin dashboard (Phase 8 stays a stub), public marketing pages.
- Telescope model watchers / dump server; production APM (OTel exporter stays unwired).
- River UI / any package-UI aggregation; file/ring-buffer telemetry store (deferred).
- Cross-process telemetry fan-in beyond the shared DB store (worker/scheduler capture writes to
  the same tables; no live websocket).
- CSP rework: `SecurityHeaders` deliberately sets no CSP today (Scalar CDN); if one is added later,
  `/dev` and `/docs` need scoping — noted, not done here.

## Acceptance criteria

- `db:seed` creates the hidden `Developer` role (all catalog permissions) and the developer user;
  `GET /api/v1/role` **never** lists it; `GET /api/v1/role/{id}` on the tenant surface 404s for it;
  no response body contains a `hidden` field.
- `/dev`, `/dev/api/*` return 404 with `DEV_DASHBOARD_ENABLED=false`, 403 for a tenant `Admin`
  (`dev.view` absent), and work for super-admin and the developer user.
- Queue screens/endpoints: list/filter jobs (application kind decoded from `ims.job` envelope),
  retry a discarded job (it becomes `available`), cancel an `available` job, pause/resume a queue;
  every mutation appears in `dev_actions`.
- With capture at sample rate 1.0 for `http,sql`, added p95 latency ≤ 3% in the recorded smoke;
  drops are counted and logged; a slow/blocked sink never stalls a request.
- Redaction proven by tests: `Authorization`, cookies, passwords, OTPs, tokens and configured keys
  never appear in any stored payload (requests, SQL args, cache values, job args, logs).
- `openapi:check` (validates `/dev/api` paths, runs drift with the SPA exclusions) and the CI web
  job (typecheck + build + OpenAPI types diff) are green; `make check` unchanged otherwise.

## Open questions

- Overhead budget verification method (recorded `bench/` smoke vs in-repo benchmark test) — decide
  at implementation; the ≤ 3% target stands.
- Whether Phase 8 reuses `web/dev` scaffolding (TanStack Start basepath `/`) or keeps a separate
  Vite app — Phase 8's spec decides; nothing in this phase blocks either.
- Retention for `dev_actions` (business-equivalent audit could warrant longer than telemetry
  retention); default is the same `DEV_DASHBOARD_RETENTION_DAYS` until an audit requirement lands.
