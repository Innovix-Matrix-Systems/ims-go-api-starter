# Phase 19 — Simplicity pass, audit log and test helpers

**Status:** Draft (proposed 2026-11-19 — needs owner sign-off before work starts)
**Source:** project-owner goal ("Laravel-simple code with Go performance"); deferred items from
Phases 15–17 (M44–M46, M53–M54, M73)
**Depends on:** Phases 12–18

## Goal

Make every module read like a small Laravel controller + service, so a new developer can copy one
module and be productive in an afternoon. Add the two things almost every API built on this starter
will need next: an audit/activity log and test helpers that make an integration test a few lines.

## 1. Handler helpers (biggest readability win)

Today every handler repeats ~6 lines (identity → `MustCan` → bind → validate → scope → respond),
about 190 times. Two small, explicit helpers remove most of it without any framework magic:

```go
// routes.go — permissions declared once per route, like ->middleware('can:...')
r.With(h.can(authz.PermBranchCreate)).Post("/branch", h.store)

// handler.go
func (h *Handler) store(w http.ResponseWriter, r *http.Request) {
    var req InsertUpdateRequest
    if !h.bind(w, r, &req) {          // JSON + validate, writes the 422 itself
        return
    }
    branch, err := h.service.Create(r.Context(), httpx.Scope(r), httpx.ActorID(r), req.toInput())
    h.respond(w, r, http.StatusCreated, branch, err)   // error → problem, else {data}
}
```

- [ ] `httpx.Bind(w, r, binder, validator, dst) bool`, `httpx.ActorID(r)`, `tenancy.From(r)`.
- [ ] Route-level permissions (`r.With(h.can(perm))`) for single-permission routes; `MustCan` stays
      in handlers only where the permission depends on the payload (e.g. roles on user update).
- [ ] Migrate modules one at a time (branches → tenants → access → identity → billing → …); the
      route-coverage test (`route_permissions_integration_test.go`) proves nothing lost a check.
- [ ] Generator templates already follow this shape (Phase 17).

## 2. Consistent module layout

- [ ] Every module: `routes.go`, `handler.go`, `request.go`, `service.go`, `repository.go`,
      `model.go` (branches fixed in Phase 13; check the rest).
- [ ] Split `internal/config/config.go` (1,300 lines) into `config/app.go`, `database.go`,
      `auth.go`, `billing.go`, … — one file per section, like Laravel's `config/*.php`.
- [ ] `identity/handler.go` (~800 lines) and `dev` (~4,800 lines): split by resource
      (`users_handler.go`, `profile_handler.go`, `import_export_handler.go`).
- [ ] **M53/M54:** give `internal/modules/dev` a service layer; `requireManage` becomes route
      middleware; audit inserts use a detached context.
- [ ] **M44–M46:** one `withPlatform(cmd, func(p *app.Platform) error)` helper for CLI commands;
      single cleanup owner in `NewPlatform`.

## 3. Audit / activity log

A tenant-scoped record of who did what, readable by tenant admins (like spatie/laravel-activitylog).

- [ ] Migration: `activity_log(id, tenant_id, actor_id, action, subject_type, subject_id,
      changes jsonb, ip, request_id, created_at)` + `(tenant_id, created_at DESC, id DESC)` index.
- [ ] `audit.Recorder` service: `Record(ctx, scope, actorID, action, subject, changes)`; changes
      are redacted with the telemetry redactor.
- [ ] Record from services on create/update/delete for users, roles, branches, settings, billing.
- [ ] `GET /api/v1/activity` (cursor feed, `activity.view` permission, tenant-scoped);
      retention task (`ACTIVITY_RETENTION_DAYS`).

## 4. Test helpers (`internal/testkit`)

Integration tests repeat ~40 lines of setup (config, clean tables, deps, seed, login). Target:

```go
func TestBranchCreate(t *testing.T) {
    app := testkit.New(t)                                   // migrated schema, seeded, router
    admin := app.User(t, testkit.TenantAdmin(1))             // factory with sane defaults
    rec := app.As(admin).Post("/api/v1/branch", `{"name":"Dhaka"}`)
    testkit.Status(t, rec, http.StatusCreated)
}
```

- [ ] `testkit.New(t)` — DSN-gated (skips without `DATABASE_TEST_DSN`), own schema, seeded.
- [ ] Factories: `User`, `Tenant`, `Branch`, `Role(perms...)`, `Package(features...)`.
- [ ] `app.As(user)` request builder (`Get/Post/Patch/Delete`, acting headers).
- [ ] Port two existing integration tests to prove the API, then use it for new tests.

## Acceptance criteria

- No handler in a migrated module repeats the identity/bind/validate boilerplate.
- `config.go` split with no behaviour change (`config_test.go` unchanged and green).
- Activity log: creating a user in tenant A writes one row visible to A's admin and invisible to B.
- A new integration test using `testkit` is under 15 lines.
- `make check` and the integration suite pass after each module migration (one commit each).

## Open questions

- Activity log scope: which modules/actions beyond identity/access/billing?
- Should `testkit` also offer a fake mailer that captures messages (for recovery-flow e2e tests)?
