# Phase 3 — Platform: cache, queue, scheduler, CLI depth

**Status:** Done (2026-10-17)
**Roadmap:** PLAN.md §11 Phase 3
**Depends on:** Phase 2

## Goal

Production-grade platform services: multi-driver cache with versioned keys, cache-backed rate
limiting, the queue engine (sync + River) with a typed job registry and progress tracking, the
central scheduler, the settings module, and the full CLI surface.

## Reference (Laravel)

- `config/queue.php` (incl. custom `channels` block) + `app/Registry/QueueRegistry.php`
- `app/Jobs/*` — job shapes and the tenant-restore pattern inside `handle()`
- `app/Helpers/CacheHelper.php` — versioned cache keys, tenant-aware key scoping
- `routes/console.php` — schedule definitions to mirror
- `app/Console/Commands/*` — command surface, naming, flags
- `config/setanjo.php` + `app/Enums/SettingKey.php` — typed settings

## Decisions (locked)

1. **Cache drivers this phase:** `file`, `redis`, `database` join the existing `memory` driver
   behind `cache.Store`; a factory selects by `CACHE_DRIVER`. Contract tests run against every
   driver (redis/database gated by env DSNs, like integration tests).
2. **Versioned keys** (port of `CacheHelper`): `cachekeys.Version/Bump/Key` — namespace version
   stored in the cache itself; key shape `{scope}{namespace}:v{n}:{md5(args)}` with scope
   `tenant:T:` / `tenant:T:branch:B:` / `global:`. Primary invalidation strategy; driver-agnostic.
3. **Rate limiting:** cache-backed limiter (`Increment` + TTL) replaces the in-memory stub for
   the global throttle; the memory limiter stays for tests. Login lockout (Phase 2) already uses
   the cache store and is unchanged.
4. **River:** `riverqueue/river` + `riverpgxv5` (version pinned in go.mod). River migrations run
   inside `ims migrate`/`migrate:status`/`migrate:rollback` via `rivermigrate` — one migration
   entry point (goose first, then River; rollback reverses River first, then goose).
   `EnqueueTx` uses River's transactional insert.
5. **Queues:** River queues `critical`, `default`, `heavy`; per-queue `MaxWorkers` from
   `QUEUE_WORKER_CONCURRENCY`; tries/timeouts from `QUEUE_*_TRIES`/`QUEUE_*_TIMEOUT`
   (defaults 3/60s, 5/120s, 1/1800s — Laravel parity).
6. **Typed jobs:** `queue.Registry` maps kind → `Definition{Queue, MaxRetries, Timeout, Validate,
   Handle}`; a River `JobArgs` wrapper stores kind + JSON payload. Handlers validate payloads and
   restore tenant/branch context from payload before calling services (Laravel parity).
7. **Scheduling:** periodic work stays in our `schedule.Registry` (robfig/cron/v3); River
   periodic jobs are deliberately not used. Overlap policy defaults to `skip`. The scheduler
   heartbeat is written to the cache for health checks.
8. **Settings:** `settings` table (`key`, `tenant_id` nullable, `value` JSON) + typed registry in
   Go (type/default/validation). Resolution: tenant override → global row → code default.
   `settings:sync` upserts global defaults; reads cached with invalidation on set.
9. **`data_processing_jobs`:** table + repository + service (create/status/progress/file) used by
   import/export in Phase 5; cleanup scheduled task (`data-processing:cleanup-completed`,
   daily 02:00) driven by the `data_processing_job.cleanup_days` setting; user-facing routes and
   Excel processing stay in Phase 5.
10. **`queue:stats`** reports River queue depth/failed counts via SQL; `queue:retry` is deferred
    (the Phase 9 queue admin covers retry via the River client API).

## Scope checklist

- [x] cache drivers: file, redis, database + factory + config wiring + contract test suite
- [x] versioned cache keys (`cachekeys`) + `cache:clear [--store=]` command
- [x] cache-backed global rate limiter
- [x] `queue.Engine` (sync + River), `queue.Registry`, `queue:work` (--queues/--max-workers/--once),
      tenant restore in handlers, `queue:stats`
- [x] River migrations inside `migrate`/`migrate:status`/`migrate:rollback`
- [x] `data_processing_jobs` table + repository/service + cleanup task
- [x] scheduler registry + `schedule` daemon + `schedule:list` / `schedule:run` + heartbeat
- [x] settings module (typed registry, service, `settings:sync`)
- [x] health: cache ping, queue depth, schedule heartbeat checks

## Out of scope

- Business jobs (billing reminders, notification fan-out) — they arrive with their modules.
- Asynq engine; queue admin UI (Phase 9, first-party on the River client API); Excel
  import/export endpoints (Phase 5).

## Contracts touched

- DB: goose migrations (`cache`, `settings`, `data_processing_jobs`) + River tables
- Env: `CACHE_*`, `REDIS_*`, `QUEUE_*`, `SCHEDULE_TIMEZONE` (all already present in `.env.example`)
- CLI: `queue:work`, `queue:stats`, `schedule`, `schedule:list`, `schedule:run`, `cache:clear`,
      `settings:sync`, `data-processing:cleanup-completed`

## Acceptance criteria

- Cache contract suite green for memory, file, database (and redis when a server is available)
- Versioned keys invalidate a namespace without touching other keys; verified per driver
- Queued jobs survive a worker restart (River); duplicate/empty job kinds fail at startup
- Scheduler runs each task once per cluster instance, skips overlaps, writes heartbeats
- `settings:sync` idempotent; tenant overrides win over global rows
- `go test -race ./...` green with and without `DATABASE_TEST_DSN`; lint/gofmt clean

## Implementation notes

- **River v0.49** pinned. One River argument kind (`ims.job`) carries the logical job kind + JSON
  payload + tenant/branch envelope, preserving the typed registry without generating River types
  per kind. `EnqueueTx` uses River's transactional insert (verified: rolled-back jobs are not
  visible).
- **Live-testing find:** River's `river_job_state` enum uses `retryable` (not `retrying`) —
  `queue:stats`/health query fixed against the real enum.
- **Cache factory** lives in `internal/platform/cache/factory` (the `cache` package importing its
  own drivers would cycle).
- **Integration tests are schema-isolated per package** (`dbtest` creates a deterministic schema
  per working directory, migrates goose + River into it): `go test ./...` is now safe to run in
  parallel against one test database — this fixed deadlock/truncation flakiness across packages.
  `dbtest.DSN(t)` exposes the schema-scoped DSN for full-app tests.
- **Scheduler heartbeat visibility:** the heartbeat lives in the configured cache, so it is only
  visible to other processes with a shared driver (file/redis/database). With `memory` (default),
  `health:check` reports a warning unless the scheduler runs in the same process — documented
  behavior, not a bug.
- **Local noise:** with `APP_DEBUG=true`, River's internal debug logs are verbose (expected;
  set `APP_DEBUG=false` or `APP_LOG_LEVEL=info` for quiet runs).
- Verified live: migrate added the three tables + River schema v8, `migrate:status` shows both
  layers, `settings:sync` idempotent (3 then 0), `schedule:list/run`, `queue:stats`,
  `cache:clear`, `data-processing:cleanup-completed --dry-run`, `health:check` all-ok with a
  shared cache, `queue:work --once` clean start/stop; `go test -race` green (with and without
  DSN).

## Open questions

None outstanding — decisions 1–10 resolved; the notes above are recorded for
`docs/architecture.md`.
