# Phase 13 — Authorization & data-integrity in the identity/access model (P1)

**Status:** Done (2026-11-19)
**Source:** [docs/codebase-review.md](../docs/codebase-review.md) §10 P1 items 7–9, 11 — findings **M1–M7, H7, H12, H13, M80**
**Depends on:** Phase 12 (the exempt-boundary/catalog fix; this phase rebuilds on top of it)

## Goal

Finish the authorization and tenancy model for identity/access. Permission checks stay in
handlers (Laravel controller style — see the M1 decision below); this phase puts tenant scoping
into the SQL itself (several `users`/`roles` queries have no `tenant_id` filter today and rely on
post-fetch Go checks), closes identity write-path gaps (role assignment under `user.update`,
non-revoking self password change, quota TOCTOU, lost updates) and adds the route-coverage test
that makes a forgotten handler check fail CI.

## Reference (review)

- [docs/codebase-review.md](../docs/codebase-review.md) §7 (M1–M7, M80), §6 (H7, H12, H13),
  §7d items 1, 2, 8, §10 items 7–9, 11.

## Scope checklist

### Authorization in the right layer (M1) — decision: handlers own permissions

**Decision (2026-11-19, project owner):** permission checks stay in handlers / `authz.Require`
route middleware, Laravel-controller style. CLI, jobs and scheduled tasks are trusted callers
(like artisan) and do not re-check permissions. Services opt in to the Gate (`authz.Checker`) only
when they need a record-level decision. The review's M1 is resolved by documenting this, not by
moving checks. Recorded in `docs/architecture.md` §11.23.

- [x] Fix doc drift: `docs/developer-guide.md`, `AGENTS.md`, `authz.Require` comment say services own
      permission checks — reword to the handler model.
- [x] Remove `branches.Service.checker` (injected, never used).
- [x] Route-coverage test: every protected `/api/v1/*` route returns 403 for an authenticated
      token with zero permissions (catches a forgotten handler check).
- [x] Business invariants that must hold for *any* caller live in services (Super-Admin
      protection, no self role escalation, non-empty role sets where required).

### Tenant scoping in SQL (M2, M7)

- [x] Add `tenant_id` filters to `GetUser`, `UpdateUser`, `SoftDeleteUser`, `UpdateUserPassword`
      (`sql/queries/users.sql`) — the cross-tenant row fetch today is a documented violation.
- [x] Add `tenant_id` filters to role mutations and `model_has_roles` writes
      (`sql/queries/roles.sql`), removing the fetch-then-mutate guard pattern.
- [x] Delete the post-fetch Go tenant checks the filters replace; keep/extend per-module
      isolation integration tests for identity and access.

### Identity write-path correctness (H7, M3, M4, M5)

- [x] **H7:** stop performing role assignment under `PATCH /user/{id}` with `user.update`
      only — either strip `roles` from the update path or require `user.role.assign` and
      forbid self-targeting (mirror `Delete`'s self-check).
- [ ] **M3 (moved to Phase 18 — auth flows):** stop creating API users pre-verified (`EmailVerifiedAt: time.Now()`
      unconditionally) so `AUTH_CHECK_EMAIL_VERIFIED` is meaningful; decide the intended
      verified-at-creation policy (config or explicit flag) and implement it.
- [x] **M4:** imported users must not share the default password `123456` — generate random
      passwords (print-once or force-reset-on-first-login).
- [x] **M5:** self-service password change revokes existing tokens (parity with the admin flow).

### Concurrency & lost updates (M6, H12, M80)

- [x] **H12:** make feature-limit enforcement atomic — conditional insert
      (`INSERT … WHERE (SELECT count(*)) < @limit`) or a serializable transaction holding a
      tenant row lock — for `users.max` and `branches.max`.
- [x] **M80:** add optimistic concurrency for normal updates: a `version int` column or
      `WHERE updated_at = @expected` predicates, returning 409/422 on mismatch. Start with
      `users` and `roles` (the M6/M7 surfaces), then apply the convention to other modules'
      update paths.
- [x] **M6:** the `PATCH /user/{id}` fetch-then-update window is covered by the same mechanism.

### Design review (H13)

- [x] **H13:** decide the Developer role's real shape — "hidden Developer access" vs
      "all 67 catalog permissions". Recommended direction: grant `dev.view`/`dev.manage` plus
      read-only admin views, or split dashboard access from data administration. Record the
      decision in `docs/architecture.md`; adjust `internal/seeder/seeder.go` accordingly.

## Implementation notes (2026-11-19)

- **M1:** decision recorded (handlers own permission checks). `internal/app/route_permissions_integration_test.go`
  calls every `/api/v1` route with a zero-permission tenant user and expects 403 unless the route
  is in the explicit `routesWithoutPermission` allowlist (public + self-service/owner-scoped).
- **M2/M7:** `tenancy.Filter` (`Scope.Filter()`, `InTenant`, `Unscoped`) is the single-row tenant
  predicate. `GetUser`/`UpdateUser`/`SoftDeleteUser`/`UpdateUserPassword` take
  `@all_tenants OR tenant_id = @tenant_id`; a zero filter matches nothing (fails closed). Tenant role
  update/delete use `UpdateTenantRole`/`DeleteTenantRole` (`tenant_id` + `is_system = false` in SQL).
  `model_has_roles` writes stay keyed by user id after a tenant-scoped fetch of the user (a user's
  tenant never changes).
- **H7:** `PATCH /user/{id}` with `roles` additionally needs `user.role.assign`
  (`user.internal.role.assign` on the internal route); services refuse any self role change
  (update and assign-role).
- **M4:** imported rows without a password get a random 24-char password (reset flow sets a real one).
- **M5:** self password change calls `Authenticator.RevokeOthers` — other devices are signed out,
  the current token keeps working (Laravel `logoutOtherDevices`).
- **H12:** `TxManager.WithTenantLock` (advisory xact lock on `resource:tenant`) wraps
  count → limit → insert for `users.max` and `branches.max`. Concurrent test: 10 parallel creates
  against a limit of +3 create exactly 3.
- **M80/M6:** opt-in optimistic locking on `PATCH /user/{id}`: send the `updated_at` you read; a stale
  value answers 409. Omitted → last-write-wins (Laravel default). Roles keep last-write-wins.
- **H13:** the Developer role holds `authz.DeveloperPermissions()` only (`dev.view`, `dev.manage`).
- **M3:** deferred to the auth-flows work (email verification + password reset), because making
  admin-created users unverified without a verification endpoint would lock them out.

## Out of scope

- The exempt-route boundary and permission catalog system flags (Phase 12).
- Billing state machines and idempotency (Phase 14).
- Batching the per-user role lookups on list endpoints (Phase 16, M79).

## Contracts touched

- **DB tables / migrations:** `tenant_id` filter changes are query-level (`make sqlc`);
  optimistic concurrency adds a `version` column (or relies on `updated_at`) — one migration
  for the chosen mechanism on `users`/`roles` (+ others as decided).
- **API endpoints:** `PATCH /user/{id}` role-assignment semantics change (H7); update responses
  may carry a version/`updated_at` expectation (409 on conflict — new problem response usage,
  add to `api/openapi.yaml` regions via the generator or a manual entry outside the managed
  region).
- **Env vars / config:** possibly a config flag for the user-creation verification policy (M3).
- **CLI commands:** none new. CLI paths are trusted callers (M1 decision); they still go through service-level tenancy and invariants.

## Acceptance criteria

- Every protected route returns 403 for a token with no permissions (route-coverage test).
- Cross-tenant `GetUser`/`UpdateUser`/`SoftDeleteUser`/`UpdateUserPassword` and role mutations
  return no rows / affect nothing (isolation integration tests in `identity` and `access`).
- An account holding only `user.update` cannot change `roles` on `PATCH /user/{id}` (403), and
  no caller can target themselves for role escalation.
- Imported users' password hashes never equal the default password's hash.
- Self password change invalidates existing tokens (test: old token 401 after change).
- Concurrent quota test: N parallel creates against `users.max = X` never exceed X.
- Concurrent update test: stale writer gets 409/422, not silent last-write-win.
- Developer-role decision recorded; seeder grants match the decision.
- `make check` and the integration suite pass.

## Open questions

- Optimistic concurrency mechanism: `version` column vs `updated_at` predicate — pick one
  convention for the whole codebase (affects all future modules and the generator templates).
- H13: final Developer-role permission set.
- M3: should API-created users be verified by default in non-production/dev (seed/demo parity)?
