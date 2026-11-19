# Phase 19 — Simplicity pass, activity log, test helpers, flag rollout, storage disks

**Status:** Done (2026-11-19), except the dev-module service layer (see Deferred)
**Source:** project-owner goal ("Laravel-simple code with Go performance"); deferred items from
Phases 15–17 (M44–M46, M53–M54, M73); owner requests for feature-flag percentage rollout and
swappable storage disks (S3 / Cloudflare R2 / MinIO)
**Depends on:** Phases 12–18

## Goal

Make every module read like a small Laravel controller + service, so a new developer can copy one
module and be productive in an afternoon. Add the things almost every API built on this starter
needs next: an activity log, test helpers that make an integration test a few lines, percentage
rollouts for feature flags, and storage disks you can swap with a `.env` change.

## 1. Handler helpers

- [x] `httpx.Kit` (embedded in every handler): `Bind` (decode + validate + write the 4xx),
      `Validate`, `PathID`, plus the Responder methods (`Data`, `Error`, `NoContent`, ...).
      `authn.UserID(r)` / `authn.UserIDFromContext(ctx)`, `tenancy.From(r)`.
- [x] Route-level permissions `r.With(h.can(perm))` for single-permission routes; `MustCan` stays in
      handlers only where the permission depends on the payload (roles on user update, file
      ownership).
- [x] Migrated: branches, tenants, access, identity, billing, files, notifications, settings,
      featureflags, dataprocessing, auth. A script checked that all 74 moved checks kept the same
      permission; `route_permissions_integration_test.go` is green.
- [x] Generator templates emit the same shape (and generated modules are lint-clean with and
      without date fields).

## 2. Consistent module layout

- [x] `internal/config/config.go` split into `app.go`, `database.go`, `cache.go`, `queue.go`,
      `auth.go`, `billing.go`, `storage.go`, `backup.go`, `mail.go`, `ops.go`, `activity.go`,
      `env.go`; `config_test.go` unchanged and green.
- [x] `identity/handler.go` split into `users_handler.go`, `profile_handler.go`,
      `import_export_handler.go`.
- [x] **M44:** `NewPlatform` has one cleanup owner (closers registered as resources are created;
      any error closes them all — the telemetry collector no longer leaks on error paths).
- [x] **M45:** `withPlatform(state, fn)` replaces 15 copied CLI boot blocks (`queue:work` included).
- [x] **M46:** `health:check`, `schedule:list`, `openapi:check` document why they skip
      `cfg.Validate()`.
- [ ] **M53/M54:** dev-module service layer — deferred (see below).

## 3. Activity log

- [x] Migration `00022_activity_log.sql`: `activity_log(id, tenant_id, actor_id, action,
      subject_type, subject_id, changes jsonb, ip, request_id, created_at)` with
      `(tenant_id, created_at DESC, id DESC)`, subject and `created_at` indexes.
- [x] `activity.Recorder.Record(ctx, Entry)`: nil-safe, never fails the caller, actor/IP/request id
      from the context (`httpx.ClientIP`), changes redacted with the telemetry redactor, insert
      detached from request cancellation.
- [x] Recorded: users (created, updated, deleted, roles_assigned, password_changed), tenant roles
      (created, updated, deleted, permissions_assigned), branches (created, updated, deleted),
      tenant settings (updated), subscriptions (created, activated, cancelled).
- [x] `GET /api/v1/activity` — cursor feed, filters `subject_type`, `subject_id`, `actor_id`;
      `activity.view` (granted to tenant Admin roles); tenant-scoped (platform accounts without an
      acting tenant see all tenants).
- [x] Retention task `activity.prune` (`ACTIVITY_RETENTION_DAYS`, default 365, 0 = keep).

## 4. Test helpers (`internal/testkit`)

- [x] `testkit.New(t)` — DSN-gated, clean schema, seeded, unlimited plan, router.
- [x] Factories: `app.User(t, Role(..), InTenant(..), Email(..))`, `app.Tenant(t, name)`,
      `app.Role(t, name, perms...)`, `app.SuperAdmin(t)`, `app.Login`.
- [x] `app.As(user)` request builder (`Get/Post/Patch/Put/Delete`, `InTenant` acting headers) and
      `Response.Status/JSON/Data`.
- [x] Used by the activity-log tests and `branches/testkit_example_test.go` (a 13-line test).

## 5. Feature-flag percentage rollout (owner request)

- [x] Migration `00021_feature_flag_rollout.sql`: `rollout smallint` (0-100, only on enabled rows).
- [x] Stable buckets: `fnv32a(flag key + subject) % 100`; raising 20% → 60% only adds subjects.
      `Flag.RolloutBy` picks the subject: user (default) or tenant.
- [x] Internal and dev `PATCH` accept `rollout: {"feature.x": 60}`; responses report `rollout`;
      100% is stored as plain "on".
- [x] Dev dashboard: rollout editor with presets on the flag drawer.

## 6. Storage disks (owner request)

- [x] Named disks like `config/filesystems.php`: built-in `private`, `public`, `s3`, plus
      `STORAGE_DISKS=r2,minio` with `STORAGE_<NAME>_*` (driver, endpoint, region, bucket, keys,
      path style, public URL, root prefix, visibility).
- [x] Privacy follows the disk's visibility, not its name; avatars go to `FILES_PUBLIC_DISK`.
- [x] `Disk.TemporaryURL` (presigned links), key prefix, `RequestChecksumCalculation=WhenRequired`
      for S3-compatible services.

## Deferred

- **M53/M54 — dev-module service layer.** `internal/modules/dev` (~4,800 lines) still calls
  repositories from handlers. It is a developer-only surface with full integration coverage, and
  the refactor is large with no behaviour change; do it when the dashboard next grows.
- **Porting existing integration tests to testkit.** The old tests keep their hand-rolled setup;
  new tests should use testkit. Port opportunistically when a test is touched.
- **Recording dev-dashboard and internal (super-admin) actions in the activity log.** Dev actions
  already have their own audit table (`dev_actions`); internal role/user writes are platform-level
  and can be added with `TenantID: 0` when needed.
- **Activity log UI** in the dev dashboard.

## Acceptance criteria

- [x] No handler in a migrated module repeats the identity/bind/validate boilerplate.
- [x] `config.go` split with no behaviour change.
- [x] Activity log: creating a user in tenant A writes one row visible to A's admin and invisible
      to B (`activity_integration_test.go`).
- [x] A new integration test using `testkit` is under 15 lines.
- [x] `make check`, the integration suite, `openapi-check` and the SPA type-check pass after each
      commit.
