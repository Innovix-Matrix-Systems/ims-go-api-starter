# Phase 9 — Developer dashboard (lightweight Telescope)

**Status:** In progress — A (identity/access), B (queue admin), C (telemetry capture) and D (SPA,
core screens + detail views) are implemented and green (`make check` + integration suite + live
smoke); E–I (management panels) are placeholder routes pending their backends. Rev 6 adds
**Deliverable J — database backups** (CLI + scheduler + dev panel, Laravel-backup principles) to
the plan.
**Roadmap:** PLAN.md §7.21 + §11 Phase 9
**Depends on:** Phases 0–7, 10, 11; builds on Phase 3 (queue/scheduler/cache), Phase 6
(observability, contract), and Phase 11 (settings groups + feature flags). The admin dashboard
(Phase 8) is deliberately deferred — Phase 9 is self-contained and ships the developer surface
first.
**Spec owner:** —

> **Revision 3 (2026-10-18).** Adds four management panels from the Dev Dashboard mock: **Sessions**
> (token/device management with stats, force-logout, idle prune), **Settings** (grouped board with
> drag-and-drop, global/tenant/**per-user** overrides), **Feature Flags** (global/tenant/user
> toggles) and **Files** (file-manager browser over all uploads), plus a **Developers** screen.
> Notes from research: Phase 11 already ships settings groups + runtime feature flags
> (`settings_groups`, `feature_flags`, `internal/settings*`, `internal/feature-flags*`), so those
> panels are thin dev APIs + UI; only **per-user settings overrides** (currently 422) are a backend
> dependency. Migration numbering fixed: `00013` is taken by Phase 11 — this phase uses `00014+`.
>
> **Revision 6 (2026-10-19).** Adds **Deliverable J — database backups** modelled on
> spatie/laravel-backup: `pg_dump` archive, multiple destinations (local dir + S3 prefix),
> `backup:run/list/clean/monitor` CLI, DefaultStrategy retention, max-age/max-storage monitoring,
> a queue-backed run job with an advisory lock, and a Backups screen + command-center entry in the
> dev panel. Backup is opt-in (`BACKUP_ENABLED=false`); restore is out of scope in v1.
>
> **Revision 2 (2026-10-18).** River UI dropped in favor of a first-party queue admin API built on
> the River client (Decision 1); developer-dashboard-only scope; hidden `Developer` role/user
> (Decision 2); TanStack Start SPA (Decision 8); one gate flag (Decision 3); concrete config,
> routes, migrations, CI and acceptance criteria.

## Progress tracker (rev 4 — 2026-10-19)

| Slice | Status | What remains |
|---|---|---|
| A — identity & access | **Done** | — |
| B — queue admin | **Done** | Job-id deep links + full job detail page shipped |
| C — telemetry backend | **Done** | ≤3% p95 overhead smoke not measured; optional file store deferred |
| D — SPA | **Done (core screens)** | E–I panels are placeholder routes |
| E — Sessions | Not started | `TokenID` on Identity, token stats/directory/device + prune queries, `/dev/api/sessions/*`, screen |
| F — Settings | Not started | Per-user settings target (external settings-module dependency), dev endpoints + drag-and-drop board |
| G — Feature flags | Not started | Thin dev endpoints + grouped toggle UI (Phase 11 backend exists) |
| H — Files | Not started | Dev listing/stats/download endpoints + file-manager UI |
| I — Developers | Not started | Developers list/assign endpoints + screen |
| J — Backups | Not started | Spec complete (rev 6); independent of E–I, requires host `pg_dump` |

**Next-session starting point — E (Sessions).** No external dependency: read Decision 11 and
Deliverable E, add `TokenID` to `authn.Identity`, then the token aggregate/directory/device/prune
queries, endpoints and the screen. After E: G (thin wrapper over the existing featureflags module),
H, I; F last unless the per-user settings extension lands. **J (Backups) is independent** and can
be implemented in parallel or first if backups are operationally urgent (needs host `pg_dump`).

**Delivered beyond the original checklist (rev 4):** per-entity detail views with eye-icon deep
links (`/jobs/{id}`, `/logs/{id}`, `/exceptions/{id}`), copy buttons on every code block, River job
ids on dispatch events (observer-based enqueue capture), state-aware queue actions, animated
error/loading states + page transitions, dual hot reload via `make dev` (air + Vite HMR), and
`POST /dev/api/simulate-events` for generating synthetic logs/exceptions.

**Rev 5 (session wrap-up):** topbar + sidebar runtime info (environment, version from
`/api/v1/system/info`), and a **command center** (`cmdk`, Ctrl/Cmd+K) that browses every dashboard
page, docs/health links, recent requests/jobs/exceptions, and runs actions (generate sample
telemetry, refresh all data, sign out). Logs/Exceptions empty states now include simulator CTAs.
Session paused here; resume at **E (Sessions)** using the starting point above.

**Rev 6 (backup plan):** Deliverable J defines the DB backup system (archive + local/S3
destinations + retention + monitoring + CLI + dev screen). It is independent and can be picked up
between/alongside E–I; `pg_dump` must be available on the host.

## Implementation status (rev 4)

- **A — done.** `00014_add_roles_hidden.sql`; `roles.hidden` plumbed through model/query/response
  (hidden roles absent from lists and 404 on the tenant surface, `UserRoleRefs` filtered);
  `DeveloperRole` + `dev.view`/`dev.manage`; seeder creates the hidden Developer role (all
  permissions) and `developer@ims.com`; `DEV_DASHBOARD_ENABLED` gate middleware (404 when
  disabled) with `GET /dev/api/me`. Covered by unit + integration tests
  (`internal/app/dev_gate_test.go`, `dev_integration_test.go`).
- **B — done.** `queue.Admin` (`internal/platform/queue/admin.go`) over the River client with
  cursor pagination, state/queue/kind filters, retry/cancel/delete, queue list + pause/resume;
  `dev_actions` audit (migration `00015`, `sql/queries/dev.sql`); `/dev/api/queue/*` + `/dev/api/actions`
  endpoints; `Platform.QueueAdmin` is nil under `QUEUE_DRIVER=sync` (503). Dispatch events now carry
  the River job id (observer-based enqueue capture) and deep-link to the `/jobs/{id}` page; actions
  are state-aware (no cancel on finalized jobs). Integration coverage for actions and job-id
  capture; mapping unit tests.
- **C — done (backend).** `00016_create_dev_telemetry_tables.sql` (+ `sql/queries/dev_telemetry.sql`);
  `internal/platform/telemetry` (redactor, bounded collector with drop counter, HTTP middleware,
  pgx `QueryTracer` via lazy attach, cache captures, queue enqueue/worker observer, slog handler);
  `/dev/api/requests`, `/requests/{id}`, `/logs` (+`/{id}`), `/exceptions` (+`/{id}`),
  `/telemetry/prune`, `/simulate-events`;
  `dev-dashboard:prune` CLI + `devdashboard.prune` daily task. Redactor + collector test suites.
  Notes: capture skips `/dev*` traffic; logs are captured at Warn+; multipart/binary bodies are
  skipped and non-JSON bodies collapse to `[redacted]`; SQL args are redacted structurally (map
  keys) — scalars pass through. The ≤3% p95 overhead smoke is still pending.
- **D — done (core screens).** `web/dev` is a TanStack Start SPA-mode app (Vite 8, React 19),
  basepath `/dev` set in the three required places, typed against the generated
  `src/api/schema.d.ts` (openapi-typescript; CI regenerates + diffs). Design-system CSS + shared
  UI kit (buttons, cards, chips, stat cards, tabs, drawer, confirm dialog, toasts, code blocks).
  Implemented screens: login (TanStack Form, bearer), overview, Telescope (request list + detail
  with SQL/cache/queue/logs/exceptions tabs), logs (TanStack Virtual), exceptions, queue (queues,
  pause/resume, job table + drawer), job detail page, commands/audit log (with telemetry
  simulator), system (live health + runtime info). Every code block has a copy bar; logs,
  exceptions and queue events link to full entity views; error/loading states are animated.
  Sessions/Settings/Flags/Files/Developers render "panel contract ready" placeholders until E–I
  land. Go side: `web/dev/embed.go` (`go:embed all:dist/client`), `internal/platform/webui`
  (immutable hashed assets, `no-cache` shell, SPA fallback, API-path problem 404, styled setup
  page), mounted behind the dev gate at `/dev` + `/dev/*`; route-drift exclusions added.
  Makefile (`web-build`, `web-typecheck`, `dev-web`, `build-all`, `dev` = air + Vite HMR), CI
  `web` job, release script builds the SPA before Go. Topbar/sidebar show environment + version;
  Ctrl/Cmd+K command center (cmdk) navigates pages, requests, jobs and exceptions and runs the
  simulator/refresh/sign-out actions. Verified live: developer login → `/dev` shell →
  `/dev/api/me` → queue list → captured requests, with immutable asset caching.
- **Remaining:** E–I (sessions/settings/flags/files/developers panels; F's per-user settings
  target is the external dependency), the ≤3% p95 overhead smoke, and a runbook note for the
  seeded developer account.

## Goal

A debugging surface for developers, embedded in the same binary and gated so it is unreachable in
production unless explicitly enabled:

1. **Identity:** reachable by super-admins and by a seeded **hidden `Developer` role/user** holding
   every catalog permission (a deliberate second super-admin; never appears in role responses).
2. **Telemetry pipeline:** HTTP requests, SQL queries, cache/queue activity and correlated logs per
   request, redacted before persistence.
3. **Queue & ops admin:** first-party queue management (jobs, retries, cancels, queue
   pause/resume) built on River's client admin API — **no River UI dependency**.
4. **Management panels:** sessions/devices, settings (groups + global/tenant/user overrides),
   feature flags, file browser, developer-role holders — mirroring the Dev Dashboard mock.
5. **SPA at `/dev`:** React dashboard (TanStack Start SPA mode + Query/Table/Form/Virtual) tying it
   all together, with the existing Scalar docs and health linked.

## Reference (Laravel)

No direct parity — this phase is **new capability** (the Laravel starter used Telescope / Pulse /
Horizon; we deliberately do not port them). Behavior bars:

- `config/queue.php`, `app/Registry/QueueRegistry.php` — channel names/semantics
  (`critical` / `default` / `heavy`) the queue screen must reflect.
- Horizon's queue dashboard scope (failed jobs, retry, recent jobs, queue pause) — feature bar for
  Deliverable B, not a code source.
- The Dev Dashboard mock (supplied): sidebar `Overview / Feature Flags / Settings / Sessions /
  Developers / System / Commands / Telescope / API Docs` and the Sessions page (stat cards, force
  logout, idle prune, user directory, per-user device panel) define the expected UX.
- spatie/laravel-backup (Deliverable J principle source): archive = database dump + selected
  files, copied to one or more destination disks; `backup:run --only-db/--only-files`,
  `backup:clean`, `backup:list`, `backup:monitor`; DefaultStrategy retention (keep all 7d → daily
  16d → weekly 8w → monthly 4m → yearly 2y, never delete the newest, optional max MB); health
  checks = maximum age + maximum storage per destination; destination copy failures do not abort
  the other destinations; PostgreSQL supports binary (`pg_dump --format=c`) dumps.

## Decisions (resolved)

1. **No River UI; first-party queue admin.** `riverqueue.com/riverui` (v0.21.0) pins river v0.48
   while this repo uses v0.49 and drags in SQLite drivers + embedded UI. River v0.49 already has
   `JobList/JobGet/JobRetry/JobCancel/JobDelete/JobDeleteMany` and
   `QueueGet/QueueList/QueuePause/QueueResume`. Thin `queue.Admin` wrapper + `/dev/api/queue/*` +
   SPA screen. No `/observability` index; the SPA links `/docs` and `/api/health`.
2. **Hidden `Developer` role + user.** `authz.DeveloperRole = "Developer"`: global system role
   (`tenant_id NULL`, `is_system true`), new `roles.hidden` column `true`, assigned **every
   catalog permission** (same explicit mechanism as `Super-Admin`). Seeded user
   `developer@ims.com` (password `AUTH_DEFAULT_PASSWORD`, no tenant). Hidden roles are excluded
   from every role **list** response (SQL predicate) and 404 on the tenant-surface
   `GET /api/v1/role/{id}`; the flag is never serialized. Documented as a second super-admin.
3. **One flag.** `DEV_DASHBOARD_ENABLED` (default `true` outside production, `false` in production)
   gates SPA, JSON API and capture. Disabled ⇒ **404** on all `/dev*` routes (register-then-404 so
   the route table stays stable for `openapi:check`). New permissions **`dev.view`** and
   **`dev.manage`** (system, group `dev`); super-admin bypass; hidden Developer role holds both.
   Missing permission ⇒ 403.
4. **Routes:** `/dev` + `/dev/*` (SPA shell/assets) and `/dev/api/*` (JSON), under
   `authn.Authenticate` → `tenancy.Exempt` (super-admin and developer are tenant-less).
   `/dev/api/*` paths are hand-written in `api/openapi.yaml` outside the generated regions (the
   generator only emits `/api/v1`); drift excludes exact patterns `/dev` and `/dev/*` like
   `/storage/*`. Unknown `/dev/api/*` returns problem+json 404, never the shell.
5. **Capture hooks, not a rewrite.** HTTP: non-blocking middleware after `AccessLog`. SQL: pgx
   `QueryTracer` via `poolCfg.ConnConfig.Tracer` in `database/registry.go`. Cache: decorator
   around `cache.Store`. Queue: decorator around `queue.Engine` + wrapper on `Definition.Handle`.
   Logs: `slog.Handler` wrapper buffering by request id, flushed by the request middleware.
6. **Storage: DB tables + prune.** Seven tables (`dev_requests`, `dev_queries`,
   `dev_cache_events`, `dev_queue_events`, `dev_logs`, `dev_exceptions`, `dev_actions`) with
   `created_at`/`request_id` indexes; `dev-dashboard:prune` CLI + daily `devdashboard.prune` task.
   Best-effort bounded-channel writes (drops counted, never block). File/ring-buffer store
   deferred; `DEV_DASHBOARD_STORE=database` only in v1.
7. **Redaction before persistence** for `Authorization`, `Cookie`, `Set-Cookie`, `password`,
   `password_confirmation`, `token`, `otp`, `secret`, `api_key`, `X-Api-Key`, plus
   `DEV_DASHBOARD_REDACT_KEYS`; applied to headers, bodies, SQL args, cache values, job args.
   Dedicated redactor test suite.
8. **SPA stack (confirmed): TanStack Start in SPA mode.** `web/dev` builds a static shell
   (`_shell.html`) + hashed assets with `spa: { enabled: true }` (Vite under the hood; PLAN's "pure
   React, no Next.js" holds). Basepath `/dev` set in **all three places** (`vite.config.ts` `base`,
   `tanstackStart({ router: { basepath } })`, `createRouter({ basepath })`). Data via **TanStack
   Query** (typed with `openapi-typescript` + `openapi-fetch`), **TanStack Table**, **TanStack
   Virtual** (logs), **TanStack Form**. Auth: bearer tokens (same as API) in `localStorage`.
9. **Developer actions are service calls, never shell**, and every **mutating** dashboard action
   (queue ops, revokes, setting/flag changes, syncs, cache clear, task runs, file deletes) writes
   a `dev_actions` audit row (actor, action, params, status, result, duration) visible in the SPA.
10. **Contract stays hand-written for `/dev/api`.** `ims gen openapi` only emits `/api/v1/*`;
    `/dev/api` paths live in `api/openapi.yaml` outside the managed regions. Drift exclusions as in
    Decision 4.
11. **Sessions manager semantics** (match the mock):
    - `USERS` = distinct users owning ≥1 non-expired token.
    - `ACTIVE TOKENS` = non-expired tokens.
    - `TOKENS USED (24H)` = tokens with `last_used_at ≥ now-24h`.
    - `IDLE TOKENS` = active tokens not used in the last 24h (never used counts as idle).
    - `INACTIVE (30D+)` = active tokens idle ≥ 30 days (or never used and created ≥ 30 days ago).
    - **Force logout all** deletes all tokens **except the caller's current token** (added
      `authn.Identity.TokenID`), so the console stays usable; the audit row records the count and
      the exclusion. **Clear inactive** deletes only tokens idle beyond the threshold (default 30
      days) plus expired tokens — active tokens are never touched; `dry_run` preview first.
    - Tokens are per-device rows with no unique `(user, device)`; repeated logins accumulate rows —
      the manager exposes them individually (`Clear inactive` / per-device revoke).
12. **Settings manager reuses Phase 11.** Groups (`settings_groups`, sort order), key→group
    assignment and global/tenant values already exist; dev handlers **delegate to
    `settings.Service`** (`ListGroups/CreateGroup/UpdateGroup/DeleteGroup/AssignKeyGroup/SetTarget/
    DeleteTarget/EffectiveForTarget`) — no duplicated rules. Drag-and-drop = `AssignKeyGroup` +
    group `sort_order`; keys stay registry-owned (`settings:sync`), the UI edits values/grouping
    only. **Per-user overrides are the one backend dependency (in development):** the `settings`
    table already has `target_type/target_id`, but `validateTarget` rejects `user` today. Required
    extension: allow `user` for tenant-scoped keys, resolve **user → tenant → global → default**
    (matching feature flags), add the `settings:blob:u:<id>` cache blob + invalidation. Until it
    lands, the user-scope option is hidden in the UI (panel ships without it).
13. **Feature flags reuse Phase 11.** `feature_flags` + `internal/modules/featureflags` already
    support global/tenant/**user** targets (`Effective` user→tenant→global→default, 5m cache), the
    internal endpoints and `feature-flags:sync`. The dev panel is a thin API + UI on
    `EffectiveForTarget/SetBatch/Delete`; flags are runtime toggles and are **not** billing package
    entitlements (`package_features`, `billing.Guard`) — those stay out of scope.
14. **File browser is new read/ops surface.** `files` is already **global** (no `tenant_id`) with
    `created_by` as uploader and owner/collection attachment columns. The dev panel adds richer
    listing (search, disk, type, owner, uploader, date, whitelisted ordering), a stats aggregate,
    and an **authenticated download/preview** endpoint streaming through `storage.Disk.Get` (works
    for public, s3 and a future private disk) — none exist today. Delete goes through
    `files.Service.Delete` and is audited. No schema change required (optional
    `files(created_at)` index in the dev migration). Upload from the panel is an optional stretch.
15. **Developers screen** lists users holding the hidden `Developer` role and assigns/unassigns it
    through the identity service (audited; self-unassign is refused). Super-admin remains the
    fallback, so lockout is impossible.
16. **Navigation mirrors the mock:** Overview · Settings · Feature Flags · Sessions · Files ·
    Queue · Backups · Developers · Telescope (Requests / Logs / Exceptions) · System (health +
    scheduler) · Commands (one-off actions) · API Docs (link to `/docs`).
17. **Backups follow Laravel-backup principles, DB-first (Deliverable J).**
    - `backup:run` streams a **`pg_dump --format=custom --compress=9 --no-owner --no-privileges`**
      dump into a `.tar.gz` with `manifest.json`; optional file paths (`BACKUP_INCLUDE_FILES`,
      default **off**) are added under `files/`. `pg_dump` is a host dependency: its path is
      validated (`BACKUP_PG_DUMP_PATH`, default `pg_dump`) and its version recorded in the
      manifest. Credentials are passed through `PGHOST/PGPORT/PGUSER/PGPASSWORD/PGDATABASE/
      PGSSLMODE` (parsed from the DSN), never in argv, so they cannot leak via process listings.
    - **Destinations are independent:** local directory (`BACKUP_DIR`, 0700) and optional S3
      object-storage prefix (`BACKUP_DESTINATIONS=local,s3`, key `BACKUP_S3_PREFIX/<env>/<file>`)
      reusing the existing S3 disk. Every destination is attempted; a single failure records
      `partial` instead of losing the successful copies (Laravel behavior).
    - **One row per run** in `backup_runs` (status, trigger/actor, filename, size, SHA-256,
      `pg_dump` version, database, per-destination results, timings, error, soft-delete marker), so
      the dashboard can list/download/delete without an S3 List API. Runs execute as the
      **`backup.run` River job on the `heavy` queue** (manual API `202`; scheduler enqueues; CLI
      runs synchronously) behind a Postgres **advisory lock** so backup runs never overlap.
    - **Retention mirrors DefaultStrategy:** keep everything for 7 days, then daily for 16 days,
      weekly for 8 weeks, monthly for 4 months, yearly for 2 years; **never delete the newest**;
      optional total-size cap (`BACKUP_MAX_TOTAL_MB`, 0 = unlimited). Cleanup removes files on
      every destination and marks rows `deleted`; `--dry-run` reports only.
    - **Monitoring mirrors the package:** a `Backups` health check + `backup:monitor` evaluate the
      newest successful backup age (`BACKUP_MAX_AGE_HOURS`, default 26) and optional total size
      per destination; stale → warning, never-backed-up → failed; the CLI exits non-zero.
    - **Developer surface:** Backups screen (runs, status, size, age, destinations, manifest,
      download/delete, retention + `pg_dump` config), `POST /dev/api/backups` to enqueue,
      `POST /dev/api/backups/clean` (dry-run preview), plus a command-center action. Reads use
      `dev.view`, mutations `dev.manage`, and every run/delete/clean is audited in `dev_actions`.
    - **Out of scope v1:** restore/PITR/WAL archiving, archive encryption, managed-database
      snapshots, per-tenant backups, S3 lifecycle management, and failure notifications (P2).

## Deliverables

Ten mergeable slices. A–D form the original core; E–I are the management panels and can ship
after D's shell. B/C are parallel; F is blocked only on the per-user settings extension; E/G/H/I
have no external blockers. **J (backups) is independent** and can ship anytime — it only needs a
host `pg_dump`.

### Deliverable A — Developer identity & access

- Migration `migrations/00014_add_roles_hidden.sql` (`00013` is Phase 11's):
  `roles.hidden boolean NOT NULL DEFAULT false`.
- `authz` catalog: `DeveloperRole = "Developer"`; `dev.view`, `dev.manage` (group `dev`,
  `IsSystem: true`; new rows — `permission:sync` does not update `is_system` on conflict).
- `sql/queries/roles.sql`: `ListRoles` gains `AND hidden = false`; tenant-surface
  `GET /api/v1/role/{id}` → 404 for hidden roles; update/delete/assign already 403 for system
  roles. `RoleResponse` unchanged (no `hidden` field).
- Seeder: `ensureDeveloperRole` (hidden global system role + **all** catalog permissions, same
  helper as `Super-Admin`), `ensureDeveloperUser` (`developer@ims.com`, bcrypt
  `AUTH_DEFAULT_PASSWORD`, active, verified, role assignment, cache invalidation). Idempotent.
- Gate: routes always registered; 404 when disabled; `dev.view` / `dev.manage` via
  `authz.Require`. Config key `DEV_DASHBOARD_ENABLED` (default `!IsProduction()`).
- Runbook note: developer account is a second super-admin; rotate/disable per environment.

### Deliverable B — Queue & ops admin API

- `internal/platform/queue`: `Admin` interface + `AdminJob`/`AdminQueue` DTOs (application kind
  decoded from the `ims.job` envelope, state, queue, priority, attempts, run-at, timestamps,
  **redacted** args, truncated error history). `RiverEngine` delegates to the River client
  (`JobList` cursor + filters, `JobGet/Retry/Cancel/Delete`, `QueueList/Pause/Resume` incl. `"*"`);
  the client stays private; `Platform.QueueAdmin` is `nil` for `QUEUE_DRIVER=sync` (routes → 503).
- Endpoints: `GET /dev/api/queue/queues` (+ counts), `POST .../queues/{name}/pause|resume`,
  `GET /dev/api/queue/jobs` (cursor; `queue`, `state`, `kind`), `GET .../jobs/{id}`,
  `POST .../jobs/{id}/retry|cancel`, `DELETE .../jobs/{id}`. Mutations audited.
- `make queue-stats`; CLI remains the fallback.

### Deliverable C — Telemetry capture + store

- `internal/platform/telemetry`: collector (bounded channel, batch insert, drop counter), redactor,
  sampler, per-capture toggles, retention. `Sink` interface; DB sink in the dev module.
- HTTP middleware (method/path-route/status/duration/bytes, bounded bodies, redacted headers,
  user/tenant, request id, IP); pgx `QueryTracer` (statement, redacted args, duration, rows);
  cache hooks; queue dispatch/worker hooks; correlated `slog` capture; sanitized exceptions.
- `dev-dashboard:prune` + daily `devdashboard.prune`; drops exposed in metrics.
- Budget: ≤ 3% p95 at sample 1.0 for `http,sql` (recorded smoke); never blocks.

### Deliverable D — Developer SPA shell (`/dev`)

- `web/dev` TanStack Start SPA, basepath `/dev` (three places), build to `dist`, `go:embed
  all:dist` with a git placeholder. New `internal/platform/webui` serves it: immutable hashed
  assets, `no-cache` shell, SPA fallback (`/dev`, `/dev/*`; never `/dev/api/*`); 404 when disabled.
- Shell: login (bearer via `POST /api/v1/login`), `GET /dev/api/me` bootstrap (identity +
  permissions), nav per Decision 16, dark Dev Dashboard styling per the mock.
- Overview: health summary, queue counts, recent exceptions, request volume.
- Telescope: request list/detail (filters; tabs for SQL, cache, queue, logs, exceptions),
  Logs (level filter, virtualized), Exceptions.
- System: health (fresh run), scheduler tasks + run-now, queue stats (links to Queue).
- Commands: one-off actions (cache clear, permission/settings/feature-flag sync, session prune,
  telemetry prune) with confirm + `dev_actions` history.
- Types: `openapi-typescript` → committed `schema.d.ts`; CI regen diff; `openapi-fetch` client
  injects `Authorization` and targets `/dev/api`.
- Makefile/CI: `make web-build`, `make dev-web` (proxy `/dev/api` → `:8080`), release builds web
  before Go; CI Node job (install → typecheck → build → types diff).

### Deliverable E — Sessions manager

Backend gaps (all new): no token listing/count/revoke-by-id/prune queries; `Identity` lacks the
current token id; no expired-token cleanup.

- `authn.Identity` gains `TokenID int64` (set in `Verify` from the loaded token row).
- `sql/queries/tokens.sql` additions: session stats aggregate (users/active/used24h/idle/
  inactive30d), user directory with per-user token counts (search by name/email), per-user token
  list, `DeleteTokenByID`, `DeleteAllTokensExcept(id)`, idle/expired prune count + delete
  (`idle_days` interval; active tokens untouched).
- Endpoints (`dev.view` reads, `dev.manage` mutations, all audited):
  - `GET /dev/api/sessions/stats`
  - `GET /dev/api/sessions/users?search=&cursor=`
  - `GET /dev/api/sessions/users/{id}/tokens` (device/name, created/last-used/expires,
    `current` flag)
  - `DELETE /dev/api/sessions/tokens/{id}`
  - `DELETE /dev/api/sessions/users/{id}` (logout all devices for one user)
  - `DELETE /dev/api/sessions` (force logout all; excludes caller's current token)
  - `POST /dev/api/sessions/prune` `{idle_days, dry_run}` → preview/execute count.
- UI: stat cards (Users, Active tokens, Tokens used 24h, Idle tokens, Inactive 30d+ like the mock),
  force-logout-all card, clear-inactive card with threshold input + preview, searchable user
  directory with per-user counts/actions, right-hand device panel with per-device revoke.

### Deliverable F — Settings manager (depends on per-user settings extension)

- Dev API delegating to `settings.Service` (`dev.view`/`dev.manage`, mutations audited):
  - `GET /dev/api/settings/groups`, `POST /dev/api/settings/groups`,
    `PATCH /dev/api/settings/groups/{id}` (incl. `sort_order`), `DELETE .../{id}`
  - `GET /dev/api/settings?target_type=global|tenant|user&target_id=...` → registry metadata
    (type/default/description/scope/group) + effective value + source
  - `PATCH /dev/api/settings/{key}` (set value for target),
    `DELETE /dev/api/settings/{key}?target_type=&target_id=` (clear override)
  - `PATCH /dev/api/settings/{key}/group` `{group_id}` (drag-and-drop assignment)
- Backend dependency (settings module, in development): accept `user` targets for tenant-scoped
  keys, resolve **user → tenant → global → default**, cache blob `settings:blob:u:<id>` +
  invalidation. The UI hides per-user editing until available (capability probe).
- UI: group columns with drag-and-drop (settings between groups, group reorder), per-key editor
  with the resolution chain and override badges, target switcher (global / tenant search / user
  search), sync button (`settings:sync`, audited). Key creation/deletion stays in the registry.

### Deliverable G — Feature flags manager

- Reuses `internal/modules/featureflags` (global/tenant/user targets, `SetBatch` with `nil` =
  clear, `Delete`, `EffectiveForTarget`, 5m cache, `feature-flags:sync`).
- Endpoints (mutations audited): `GET /dev/api/flags?target_type=&target_id=` (effective + source +
  default + description + group), `PATCH /dev/api/flags` (batch set/clear for a target),
  `DELETE /dev/api/flags/{key}?target_type=&target_id=`.
- UI: table grouped by the code-defined group, effective toggle with source badge, override drawer
  per tenant/user ("reset to default"), sync action. No drag-and-drop (flag groups are code
  metadata, not a table). Explicitly separate from billing entitlements.

### Deliverable H — File browser

- New dev queries (no schema change; optional `files(created_at)` index): paginated list with
  `search` (name/file_name), `type`, `disk`, `attached`, `owner_type`, `owner_id`, `created_by`,
  date range, whitelisted `order_by` (`created_at`, `size`, `name`); aggregated stats
  (count/size by type and disk).
- Endpoints (`dev.view` reads, `dev.manage` mutations): `GET /dev/api/files`,
  `GET /dev/api/files/stats`, `GET /dev/api/files/{id}/download` (`Content-Disposition:
  attachment`; `?inline=1` for image preview), `DELETE /dev/api/files/{id}` (service delete + disk
  object, audited). Optional stretch: `POST /dev/api/files` upload.
- DTO enrichment: disk, path, owner (`type/id/collection`), uploader (`created_by` + name/email),
  public URL/presigned URL, timestamps — the current files response omits these.
- UI: file-manager grid/list with image thumbnails (inline preview), search + filters, pagination,
  detail drawer (metadata, owner/attachment, storage), download, delete with confirm, header stat
  cards (files/size by type), optional upload.

### Deliverable I — Developers screen

- `GET /dev/api/developers?search=` (users holding the hidden `Developer` role),
  `POST /dev/api/developers/{user_id}` (assign), `DELETE /dev/api/developers/{user_id}`
  (unassign; self-unassign refused). Delegates to the identity role-assignment service; audited.
- UI: developer list with search, assign/unassign with confirm; note that super-admins also access
  the dashboard.

### Deliverable J — Database backups (Laravel-backup parity)

Backend:
- Migration `00017_create_backup_runs.sql`:
  `id bigserial`, `uuid uuid UNIQUE`, `status` (`pending|running|completed|partial|failed|deleted`),
  `trigger` (`cli|schedule|dashboard`), `actor_user_id NULL`, `filename`, `scope`
  (`database|database_files`), `size_bytes`, `checksum_sha256`, `pg_dump_version`, `database_name`,
  `destinations jsonb` (per-destination status/path/key/size/error), `error`, `started_at`,
  `finished_at`, `deleted_at`, `created_at`; indexes `(created_at DESC)`, `(status)`,
  partial `(deleted_at) WHERE deleted_at IS NULL`. sqlc queries for insert/update/list/get/prune.
- `internal/modules/backup` (`routes.go`, `handler.go`, `request.go`, `service.go`, `repository.go`,
  `jobs.go`, `retention.go`, `dump.go`): service callable from HTTP, CLI, queue and scheduler.
- **Dump:** parse `DATABASE_DSN` with `pgx.ParseConfig` → `PG*` env; run
  `pg_dump --format=custom --compress=9 --no-owner --no-privileges --file=<tmp>` via
  `exec.CommandContext` (`BACKUP_TIMEOUT`, default 1h); validate `pg_dump --version` first and fail
  with an actionable error. Temp artifacts live under `BACKUP_DIR/tmp` and are always removed.
- **Archive:** `.tar.gz` with `manifest.json` (`name`, `environment`, `app_version`, `database`,
  `pg_dump_version`, `created_at`, `scope`) + `database.dump` (+ `files/` when enabled). SHA-256 +
  size computed while streaming. Filename `<name>-<env>-<YYYY-MM-DD-HHmmss>.tar.gz`.
- **Destinations:** local `BACKUP_DIR` (resolved under `APP_STORAGE_DIR`, 0700) and S3
  (`BACKUP_S3_PREFIX/<env>/<filename>`) through `storage.Registry`; each attempted independently,
  results recorded in `destinations`, status `partial` when at least one succeeds, `failed` when
  all fail.
- **Retention:** pluggable `Strategy` with the DefaultStrategy rules (keep all/daily/weekly/
  monthly/yearly, never delete newest, optional total MB) implemented as a pure, unit-tested
  function over run timestamps; cleanup deletes both destinations and marks rows `deleted`.
- **Execution:** `backup.run` queue job (kind `backup.run`, queue `heavy`, 1 retry, 1h timeout,
  payload `{run_id}`); Postgres advisory lock keyed by backup name prevents overlap; manual
  trigger returns `202` with the pending run, scheduler enqueues the same job.
- **Scheduling/health:** when `BACKUP_ENABLED=true` register `backup.run` (cron
  `BACKUP_RUN_CRON`, default `30 1 * * *`) and `backup.clean` (`BACKUP_CLEAN_CRON`, default
  `30 2 * * *`) in `platform.go`; add a `Backups` health check (newest age, optional total size,
  never-backed-up = failed) and `backup:monitor`.
- **Optional files:** `BACKUP_INCLUDE_FILES=true` adds `BACKUP_FILES_PATHS` (relative paths) under
  `files/`, excluding `node_modules`, `.git`, `tmp`, `var/backups`; scope becomes
  `database_files`.

CLI (Cobra, registered in `internal/cli/backup.go`):
- `backup:run [--with-files] [--destinations=local,s3] [--filename=...]` — synchronous; prints run
  id, filename, size, per-destination results.
- `backup:list [--limit=20]`, `backup:clean [--dry-run]`, `backup:monitor` (exit 1 when unhealthy).

Dev API (hand-written in OpenAPI; `dev.view` reads, `dev.manage` mutations, audited):
- `GET /dev/api/backups` (cursor list), `POST /dev/api/backups` (enqueue; `202`),
  `GET /dev/api/backups/{id}`, `GET /dev/api/backups/{id}/download` (stream local or S3 with
  `Content-Disposition: attachment`), `DELETE /dev/api/backups/{id}` (files + record),
  `POST /dev/api/backups/clean` (`{dry_run}` preview/execute).
- `GET /dev/api/backups/config` — destinations, retention, schedule, `pg_dump` availability/version.

Dev UI:
- New nav **Backups** (Operations group) + command-center action “Create backup”.
- Screen: stat cards (last backup age, last size, total stored, destination health), runs table
  (created, status chip, trigger/actor, filename, size, duration, destination badges, checksum),
  run drawer (manifest, per-destination results, error), download/delete, “Create backup” primary
  action, “Clean old backups” with dry-run preview, and a config card (pg_dump, destinations,
  retention). Poll every 3s while a run is pending/running. Empty state with a Create CTA.

## Contracts touched

- **Migrations:** `00014` roles.hidden; `00015` dev_* tables (`dev_requests`, `dev_queries`,
  `dev_cache_events`, `dev_queue_events`, `dev_logs`, `dev_exceptions`, `dev_actions`) each with
  `id bigserial`, `request_id text NULL`, `tenant_id bigint NULL`, JSONB payloads, `created_at
  timestamptz`, indexes `(created_at)`, `(request_id)`; `dev_actions` also `actor_user_id`,
  `action`, `status`, `duration_ms`; optional `files(created_at)` index; **`00017`
  `backup_runs`** (see Deliverable J). Per-user **settings**
  needs **no** migration (existing `target_type/target_id` columns) — a settings-module service
  extension (dependency, in development).
- **API endpoints (hand-written in `api/openapi.yaml`):** core — `GET /dev/api/me`,
  `GET /dev/api/health`, `GET /dev/api/requests`, `GET /dev/api/requests/{id}`,
  `GET /dev/api/logs`, `GET /dev/api/exceptions`, `GET /dev/api/queue/queues`
  (+ pause/resume), `GET /dev/api/queue/jobs` (+ get/retry/cancel/delete),
  `GET /dev/api/scheduler/tasks` (+ run), `GET /dev/api/actions`, `POST /dev/api/cache/clear`,
  `POST /dev/api/sync/permissions|settings|feature-flags`; panels —
  `GET /dev/api/sessions/stats`, `GET /dev/api/sessions/users`,
  `GET /dev/api/sessions/users/{id}/tokens`, `DELETE /dev/api/sessions/tokens/{id}`,
  `DELETE /dev/api/sessions/users/{id}`, `DELETE /dev/api/sessions`,
  `POST /dev/api/sessions/prune`;
  `GET/POST /dev/api/settings/groups`, `PATCH/DELETE /dev/api/settings/groups/{id}`,
  `GET /dev/api/settings`, `PATCH/DELETE /dev/api/settings/{key}`,
  `PATCH /dev/api/settings/{key}/group`;
  `GET /dev/api/flags`, `PATCH /dev/api/flags`, `DELETE /dev/api/flags/{key}`;
  `GET /dev/api/files`, `GET /dev/api/files/stats`, `GET /dev/api/files/{id}/download`,
  `DELETE /dev/api/files/{id}`;
  `GET /dev/api/developers`, `POST/DELETE /dev/api/developers/{user_id}`;
  `GET /dev/api/backups`, `POST /dev/api/backups`, `GET /dev/api/backups/{id}`,
  `GET /dev/api/backups/{id}/download`, `DELETE /dev/api/backups/{id}`,
  `POST /dev/api/backups/clean`, `GET /dev/api/backups/config`.
  Standard `{data}` envelopes / problem+json; streams (requests, jobs, sessions, actions, backups)
  use cursor pagination, management lists use page/per_page.
- **Route-drift exclusions:** exact patterns `/dev` and `/dev/*` next to `/storage/*`.
- **Env vars:** `DEV_DASHBOARD_ENABLED`, `DEV_DASHBOARD_CAPTURE` (csv `http,sql,cache,queue,logs`),
  `DEV_DASHBOARD_SAMPLE_RATE` (default 1.0), `DEV_DASHBOARD_RETENTION_DAYS` (default 7),
  `DEV_DASHBOARD_MAX_BODY_BYTES` (default 65536), `DEV_DASHBOARD_REDACT_KEYS` (csv),
  `DEV_DASHBOARD_STORE` (`database` only in v1). `.env.example` updated.
  **Backups:** `BACKUP_ENABLED` (default false), `BACKUP_NAME` (default app name),
  `BACKUP_DIR` (default `var/backups`), `BACKUP_DESTINATIONS` (csv, default `local`),
  `BACKUP_S3_PREFIX` (default `backups`), `BACKUP_PG_DUMP_PATH` (default `pg_dump`),
  `BACKUP_TIMEOUT` (default 1h), `BACKUP_RUN_CRON` (default `30 1 * * *`),
  `BACKUP_CLEAN_CRON` (default `30 2 * * *`), `BACKUP_KEEP_ALL_DAYS` (7),
  `BACKUP_KEEP_DAILY_DAYS` (16), `BACKUP_KEEP_WEEKLY_WEEKS` (8), `BACKUP_KEEP_MONTHLY_MONTHS` (4),
  `BACKUP_KEEP_YEARLY_YEARS` (2), `BACKUP_MAX_TOTAL_MB` (0 = unlimited),
  `BACKUP_MAX_AGE_HOURS` (26), `BACKUP_INCLUDE_FILES` (false), `BACKUP_FILES_PATHS` (csv).
- **Permissions:** `dev.view`, `dev.manage` (group `dev`, system). Panels reuse Phase 11 modules;
  no `session.*`, `settings_group.*` additions needed (dev gate + dev.manage).
- **CLI:** `dev-dashboard:prune`; existing `queue:stats`, `permission:sync`, `settings:sync`,
  `feature-flags:sync`, `schedule:run` reused through services (not shell). **Backups:**
  `backup:run`, `backup:list`, `backup:clean`, `backup:monitor`.
- **Scheduler tasks:** `devdashboard.prune` (daily); when backups are enabled `backup.run` and
  `backup.clean` (crons above). **Queue kind:** `backup.run` on the `heavy` queue
  (1 retry, 1h timeout, advisory-locked). **Health:** `Backups` check (age/size/never-backed-up).

## Scope checklist

Deliverable A:

- [x] `roles.hidden` migration + `ListRoles` filter + tenant-surface 404 guard
- [x] `dev.view` / `dev.manage` in catalog (+ `permission:sync` verified)
- [x] Seeder: hidden `Developer` role (all permissions) + `developer@ims.com` user; idempotent
- [x] Dev gate middleware (404 disabled, 403 without permission) + config keys
- [x] Tests: hidden role absent from role lists; developer login reaches `/dev/api/me`

Deliverable B:

- [x] `queue.Admin` + RiverEngine implementation (client stays private)
- [x] `/dev/api/queue/*` (cursor lists, filters, audited mutations, sync-driver 503)
- [x] `make queue-stats`; runbook note
- [x] Job ids on dispatch events + `/jobs/{id}` detail page (eye-icon deep links)

Deliverable C:

- [x] `internal/platform/telemetry` (collector/redactor/sampler/toggles/retention)
- [x] HTTP middleware + pgx tracer + cache/queue capture + slog handler capture
- [x] Dev tables + prune CLI + `devdashboard.prune`; drop counter in metrics registry (pending: expose)
- [x] Redactor suite; overhead smoke recorded (pending)
- [x] Per-entity `GET /dev/api/logs/{id}` + `/exceptions/{id}`; `POST /dev/api/simulate-events`

Deliverable D:

- [x] `web/dev` TanStack Start SPA (three-place basepath), embed + `webui` handler
- [x] Shell/nav/login/overview/telescope/logs/exceptions/system/commands
- [x] Types + CI diff; `make web-build`, `make dev-web`; CI Node job
- [x] e2e: `/dev` 404 disabled; 403 tenant `Admin`; OK super-admin + developer
- [x] Eye-icon deep links, copy buttons on every code block, animated error/loading states,
      dual hot reload (`make dev` = air + Vite HMR)

Deliverable E (Sessions):

- [ ] TokenID on Identity; token stats/directory/device queries; prune queries
- [ ] `/dev/api/sessions/*` endpoints (force logout excludes caller; prune dry-run)
- [ ] Sessions screen matching the mock (cards, force logout, clear inactive, directory, devices)

Deliverable F (Settings):

- [ ] Dev settings/group endpoints delegating to `settings.Service` (audited mutations)
- [ ] Drag-and-drop board + group reorder + resolution-chain editor + target switcher
- [ ] Per-user override supported when the settings extension lands (UI capability probe)

Deliverable G (Flags):

- [ ] Dev flags endpoints (effective list, batch set/clear) + UI grouped toggles with override drawer

Deliverable H (Files):

- [ ] Dev file queries (filters/order/stats) + enriched DTO
- [ ] `download`/`preview` streaming endpoint (Disk.Get) + audited delete
- [ ] File-manager UI (grid/list, thumbnails, filters, detail, download/delete, stats)

Deliverable I (Developers):

- [ ] Developers list/assign/unassign endpoints + screen (self-unassign refused, audited)

Deliverable J (Backups):

- [ ] `config.Backup` + `.env.example` (destinations, retention, cron, pg_dump, timeouts)
- [ ] `00017_create_backup_runs.sql` + sqlc queries + repository
- [ ] `pg_dump` service (PG* env, custom format, validated binary, timeout) + tar.gz manifest +
      SHA-256
- [ ] Local + S3 destinations with independent results and `partial` status
- [ ] DefaultStrategy retention + `--dry-run` cleanup (never deletes the newest)
- [ ] `backup.run` queue job + advisory lock + scheduler tasks + `Backups` health check
- [ ] CLI `backup:run/list/clean/monitor`
- [ ] `/dev/api/backups*` endpoints + Backups screen + command-center action + audit
- [ ] Tests: retention table-driven, manifest/archive contents, DSN→PG* env, partial failure,
      integration (skipped when `pg_dump` missing), e2e create/download/delete

## Out of scope

- Admin dashboard (Phase 8 stays a stub), public marketing pages.
- Telescope model watchers / dump server; production APM (OTel exporter stays unwired).
- River UI / package-UI aggregation; file/ring-buffer telemetry store (deferred).
- Cross-process telemetry fan-in beyond the shared DB store (no live websocket).
- Billing package entitlements (`package_features`, `billing.Guard`) — feature flags here are
  runtime toggles only.
- Settings key creation/deletion (registry-owned; `settings:sync`), file rename/move, generic
  attachment CRUD (out of scope; avatar flow remains the only attachment producer).
- CSP rework (`SecurityHeaders` deliberately sets none today); if added later, `/dev` and `/docs`
  need scoping.
- **Backups v1:** restore / point-in-time recovery / WAL archiving, archive encryption, managed-DB
  native snapshots, per-tenant backups, S3 lifecycle rules, and failure notifications (P2). File
  backups are opt-in (`BACKUP_INCLUDE_FILES`) and are a whole-tree snapshot, not incremental.

## Acceptance criteria

- `db:seed` creates the hidden `Developer` role (all catalog permissions) and the developer user;
  `GET /api/v1/role` never lists it; tenant-surface `GET /api/v1/role/{id}` 404s; no response body
  contains a `hidden` field.
- `/dev`, `/dev/api/*`: 404 with `DEV_DASHBOARD_ENABLED=false`; 403 for a tenant `Admin`; OK for
  super-admin and developer user.
- Queue: list/filter jobs (kind decoded from `ims.job`), retry a discarded job (becomes
  `available`), cancel an `available` job, pause/resume a queue; all mutations in `dev_actions`.
- Sessions: stats match the documented formulas; revoke one device, revoke one user, force logout
  all (caller's current token survives), and prune with dry-run/execute; prune never deletes a
  token used within the threshold.
- Settings: groups CRUD + drag assignment persist; effective values match
  user → tenant → global → default once the per-user extension lands; every write audited.
- Flags: toggling a flagship flag changes `Effective` for the target and resets cleanly; billing
  entitlements unaffected.
- Files: list/filter/search/stats work over all uploads; download streams for public and s3 disks;
  delete removes row + disk object; every delete audited.
- **Backups:** `backup:run` (CLI) and `POST /dev/api/backups` produce a `.tar.gz` containing a valid
  `pg_dump` custom-format dump + manifest, stored locally and (when configured) in S3; SHA-256 and
  size match the stored object; the run row reaches `completed`.
- **Backups:** a destination failure still succeeds on the other destination and records `partial`
  with the error; all-destination failure records `failed`; concurrent triggers are blocked by the
  advisory lock; a missing/incompatible `pg_dump` fails fast with an actionable message.
- **Backups:** retention never deletes the newest backup, honors the DefaultStrategy windows and
  optional size cap, removes files on every destination, and `--dry-run` deletes nothing.
- **Backups:** `backup:monitor` and the `Backups` health check flag stale or never-backed-up
  states; the dev API/UI list, create (202), download, delete and clean with dry-run; every
  mutation is audited and gated by the dashboard.
- Telemetry: ≤ 3% p95 overhead at sample 1.0 (`http,sql`); drops counted; sink never stalls a
  request.
- Redaction proven by tests across requests, SQL args, cache values, job args and logs.
- `openapi:check` + CI web job (typecheck/build/types diff) green; `make check` unchanged.

## Open questions

- Timing of the per-user settings backend extension (owned by the settings module) — Deliverable F
  ships globally/tenant-scoped first and enables user targets when available.
- Whether session idle thresholds stay fixed (24h used / 30d inactive) or become config keys; the
  prune threshold is a request parameter either way.
- Whether file upload from the panel is wanted in v1 (currently an optional stretch) and whether
  bulk delete is needed; rename/move stay out of scope.
- Overhead budget verification method (recorded `bench/` smoke vs in-repo benchmark test); the
  ≤ 3% target stands.
- Whether Phase 8 reuses `web/dev` scaffolding (TanStack Start basepath `/`) — Phase 8's spec
  decides; nothing here blocks either.
- Retention for `dev_actions` (audit may warrant longer than telemetry retention); default is
  `DEV_DASHBOARD_RETENTION_DAYS` until an audit requirement lands.
- **Backups:** archive encryption approach (age/gpg vs chunked AES-GCM) — deferred; whether file
  backups should default on for small installs; failure notification channels (mail/notification
  module) and whether `BACKUP_MAX_TOTAL_MB` should default to non-zero; priority of a future
  `backup:restore` command.
