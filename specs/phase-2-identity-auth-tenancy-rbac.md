# Phase 2 — Identity: auth, tenancy, RBAC

**Status:** Done (2026-10-17)
**Roadmap:** PLAN.md §11 Phase 2
**Depends on:** Phase 1

## Goal

The heart of the platform: tenant/branch scoping enforced by construction, Sanctum-style device
tokens with OTP login, and role/permission authorization using the Laravel permission catalog —
plus the users/roles/permissions/tenants/branches modules and seed data.

## Reference (Laravel)

- `app/Tenant/*`, `app/Models/Scopes/*` — tenancy context, traits, global scopes, stamping
- `app/Http/Middleware/Tenant*.php` — resolve / exempt / public middleware behavior
- `app/Services/Auth/*`, `app/Traits/RateLimiterTrait.php` — login, OTP, token naming, lockout
- `config/permission-registry.php` + `app/Registry/PermissionRegistry.php` — permission catalog
- `app/Policies/*` (esp. `UserPolicy.php`) — policy semantics and system-role protections
- `app/Services/Role/RoleService.php` — tenant default-role composition
- `routes/api/v1/{Auth,User,Role,Permission,Tenant,Branch}Routes.php` — route inventory

## Decisions (locked)

1. **Auth engine — opaque tokens behind an interface.** Ship opaque personal access tokens
   (Sanctum semantics: random token, SHA-256 stored, per-device rows, server-side revocation).
   All token operations run through `authn.Authenticator` so a JWT driver (short-lived access
   JWT + opaque refresh row, `golang-jwt/jwt/v5`) can be added later without touching middleware,
   handlers or services. JWT is explicitly out of scope for this phase.
2. **Cache pulled forward.** Implement `cache.Store` + the memory driver in this phase (OTP,
   branch-check caching, permission caching, login throttling need it). File/Redis/database
   drivers, versioned keys and driver contract suites stay in Phase 3.
3. **Permission catalog.** Ported from `config/permission-registry.php`, non-CMS modules only:
   `role`, `tenant`, `branch`, `user`, `upload`, `data_processing_job`, `package`,
   `subscription`, `payment`, `payment_approval`, `notification` (~52 entries incl. `is_system`
   flags). Seeded/upserted by `permission:sync`.
4. **Seeding (`db:seed`)**: idempotent — permission sync, default tenant/branch from `DEFAULT_*`
   env, `Super-Admin` system role (all permissions, tenant_id NULL) + per-tenant `Admin` role
   (composition verified against `RoleService::createDefaultRolesForTenant`: role perms minus
   `role.internal.*` + branch perms + user perms minus `role.internal.*`), super-admin user
   (`superadmin@ims.com` / `AUTH_DEFAULT_PASSWORD`).
5. **RBAC pivot model_type** is the constant `user` (deviation from Laravel's FQCN morph type).
6. **Impersonation.** `X-Acting-Tenant` / `X-Acting-Branch` for users with `user.tenant.switch`
   or Super-Admin; no headers = global read view; writes without acting headers → 403.
   Branch-belongs-to-tenant checks cached 5 minutes (`branch_check:{branch}:{tenant}`).
7. **Avatar deferred** to Phase 5 (uploads/storage module); profile update covers name/phone.
8. **Feature-guard enforcement points** (users.max/branches.max) deferred to Phase 4.
9. **Order-by whitelisting** for list endpoints (name/email/created_at/id; asc|desc) — Laravel
   accepted arbitrary columns; ours fails closed with 422.

## Scope checklist

- [x] `cache.Store` interface + memory driver (TTL, Increment preserving TTL, Ping, Remember helper)
- [x] `tenancy`: Scope + context + Resolve/Exempt middleware + cached branch checks
- [x] `authn`: `Authenticator` interface + opaque driver (`id|secret`, SHA-256, device rows),
      OTP (cache, 6 digits, 5 min), login throttling, bearer middleware, `last_login_at`
- [x] `authz`: Go permission catalog, Checker (Can/MustCan, Super-Admin bypass), permission
      cache + invalidation, route `Require` middleware
- [x] Modules: `auth` (login/OTP/logout), `identity` (users CRUD/profile/change-password/
      switch-branch/internal), `access` (roles/permissions), `tenants`, `branches`
- [x] `permission:sync` + `db:seed` commands; default tenant/branch/roles/super-admin
- [x] tenant-scoped validation in services (email uniqueness, role/branch ownership)
- [x] wiring + routes: auth/tenant.resolve/tenant.exempt groups per Laravel route inventory

## Out of scope

- Avatar/upload endpoints (Phase 5); feature-limit guards (Phase 4); notifications feed (Phase 5);
  JWT driver; SQLite; read/write splitting.

## Contracts touched

- DB: `sql/queries/{users,branches,roles,permissions,tokens,tenants}.sql` + `dbgen`
- API: `/api/v1` auth login/verify-otp/logout; user/role/permission/tenant/branch routes
- Env: `AUTH_*`, `BCRYPT_ROUNDS`, `CACHE_DRIVER` (memory), `CACHE_PREFIX`, `DEFAULT_*`
- CLI: `permission:sync`, `db:seed`

## Acceptance criteria

- Login → token → protected request → logout verified end-to-end on the test database
- Cross-tenant access denied (users/branches), impersonation write-guard enforced
- Super-Admin bypass + system-role protection (`role.internal.*`) verified by tests
- Seeding is idempotent; `db:seed` twice produces no duplicates
- `/api/health` unchanged; `go test -race ./...` green with and without `DATABASE_TEST_DSN`

## Implementation notes

- **Sequence sync discovered in e2e:** explicit-id seeding (`CreateTenantWithID`,
  `CreateBranchWithID`) does not advance bigserial sequences, so the next auto-generated id
  collided with seeded rows. Fixed with `SyncTenantIDSequence` / `SyncBranchIDSequence`
  (`setval(max(id))`) run by the seeder; `CreateDefaultForTenant` now uses auto ids and the
  fixed `DEFAULT_BRANCH_ID` is reserved for the seeded default tenant only.
- **ActingGlobal simplification:** an explicit global acting scope yields tenant `0` (no tenant
  filter) instead of Laravel's effective-id null-fallback quirk; behavior for super admins is
  identical.
- **Write guard scope:** the impersonation write-guard (403 without `X-Acting-*`) applies to
  tenant-resolved routes; tenant-exempt management routes (tenants, permissions, internal
  users/roles) accept direct super-admin writes — matching the Laravel route inventory.
- **Deviations recorded:** user email is immutable via `PATCH /user/{id}` in this phase; avatar
  endpoints deferred to Phase 5; `model_has_roles.model_type = 'user'`.
- Verified: full e2e (login → profile → impersonation guard → tenant/branch/user creation →
  tenant-admin login + listing → permission denial → logout + revoked token 401), seeder
  idempotency, `go test -race` with and without `DATABASE_TEST_DSN`, live curl smoke with the
  Laravel-compatible login/pagination envelopes.

## Open questions

None outstanding — decisions 1–9 resolved; the deviations above are recorded for
`docs/architecture.md`.
