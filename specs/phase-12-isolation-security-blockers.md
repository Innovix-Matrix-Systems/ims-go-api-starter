# Phase 12 — Isolation boundary & security blockers (P0)

**Status:** Done (implemented 2026-10-26; full integration suite verified 2026-11-19)
**Source:** [docs/codebase-review.md](../docs/codebase-review.md) §10 P0 — findings **C1–C4, H1–H6, L1, M43, M48, M51**
**Depends on:** — (do this before any external exposure; estimated 2–3 working days)

## Goal

Make the multi-tenant isolation boundary actually enforce the project's own rule — "explicit
scoping in SQL is the *only* isolation boundary" — and close the production-safety gaps that
let a misconfigured or defaulted deployment ship insecure. Three verified privilege-escalation
chains (C1–C3) share one root cause (C4): permissions that gate tenant-exempt routes are
grantable to ordinary tenant admins. This phase closes those chains, adds the regression test
that would have caught them, and hardens production defaults (env validation, seed guards,
token expiry, login uniformity).

## Reference (review)

- [docs/codebase-review.md](../docs/codebase-review.md) §5 (C1–C4), §6 (H1–H6), §7 (M43, M48, M51), §7c (L1), §10 items 1–6.

## Scope checklist

### Isolation (C1–C4, M43, M48, M51)

- [x] **Catalog fix (primary).** In `internal/authz/catalog.go`, ensure every permission that
      gates a tenant-exempt route cannot appear in `AdminTenantPermissions()` or
      `replacePermissions(stripSystem: true)`: either mark it `IsSystem: true` or introduce
      `*.internal.*` variants (convention exists: `user.internal.create/update/export`).
      At minimum: `user.delete`, `user.role.assign`, `notification.*`, `file.*` — with a
      route-by-route review of the `branch`/`role`/`setting` groups (see Open questions).
      *(fixes C1, C2, C3 step 1–4; M51 is the same mechanism)*
- [x] **Internal-identity gate (defence in depth).** For each route family mounted on the
      exempt group, decide system-only vs shared, and enforce it server-side per route family
      (not a blanket check inside `Exempt()`): shared profile handlers and the `files` module
      are the known exceptions. *(C4 item 2)*
- [x] **Migration:** add `tenant_id` to `files` (NOT NULL where derivable, backfill existing
      rows, index `(tenant_id, created_at)`); scope `ListFiles`/`GetFile`/file mutations by it.
      *(C3 step 6)*
- [x] **Notifications:** add `tenant_id` filters to the list/get/update/delete queries in
      `sql/queries/notifications.sql`; scope the handlers' filters accordingly. *(C3 step 5)*
- [x] **M43:** scope `ListUserIDsByRoleIDs` (and `ListUserIDsByBranch`) by tenant so a
      ROLE-targeted notification cannot fan out across all tenants.
- [x] **M48:** job envelopes must not degrade to tenant `0` ("no filter") under
      `ActingGlobal` impersonation — either persist the full scope (identity vs impersonation)
      in the envelope or refuse to enqueue unscoped jobs except from explicitly system tasks.
- [x] **Regression test:** an integration test that exercises **every** `/api/v1/internal/*`
      route with a tenant-scoped token and asserts 403 (or 404 where the route is
      identity-obscured). This test alone would have prevented C1–C3. *(C4 item 4)*

### Production safety (H1–H6, L1)

- [x] **H1/L1:** validate `APP_ENV` against an allowlist and normalise once at load
      (trim + lowercase); `logging.New` consumes the normalised value.
- [x] **H2:** ship `deploy/.env.production` (hardened defaults); add a guard that refuses to
      boot (or fails `config:show --strict`) when `APP_ENV=local` under a systemd/deploy context.
- [x] **H3:** `db:seed` refuses in production unless `--force` **and** the default password has
      been changed; otherwise generate a random password and print it once.
- [x] **H4:** non-zero `AUTH_TOKEN_EXPIRATION` default; reject `0` in production.
- [x] **H5:** one uniform `INVALID_CREDENTIALS` login response (status, message, timing);
      compare against a dummy bcrypt hash on the unknown-user path; move `verifyBeforeLogin`
      after the credential check.
- [x] **H6:** two independent login-throttle counters — per-account and per-IP — lock on either.

## Out of scope

- Moving authorization into services and SQL-level tenant filters for `users`/`roles`/
  `model_has_roles` (Phase 13 — the catalog fix above already closes the exploitable chains).
- Billing callback/idempotency hardening (Phase 14).
- Health/cache/queue platform hardening (Phase 15).

## Contracts touched

- **DB tables / migrations:** one migration — `files.tenant_id` (+ backfill + index).
  `notifications`/fan-out changes are query-level (`sql/queries/notifications.sql`,
  `sql/queries/roles.sql`) → `make sqlc`.
- **API endpoints:** no new endpoints. Behaviour changes: uniform 401 login response;
  403 on all `/internal/*` routes for tenant-scoped callers; file/notification responses
  become tenant-scoped.
- **Env vars / config:** `APP_ENV` allowlist validation; `AUTH_TOKEN_EXPIRATION` default
  change; `deploy/.env.production` added; permission catalog additions (`user.internal.*`,
  `notification.*`, `file.*` system flags) → run `permission:sync`.
- **CLI commands:** `db:seed` flags (`--force`, password generation).

## Acceptance criteria

- New integration test: tenant-scoped token → 403 on **every** `/api/v1/internal/*` route.
- A tenant Admin (default seeded role) cannot grant `notification.view.all`, `file.view.all`,
  `user.delete` or `user.role.assign` via `POST /role/assign-permission` (system permissions
  stripped); existing tenants' role assignments are unaffected where permissions remain
  tenant-grantable.
- Cross-tenant file and notification reads/updates/deletes return 404/403 (isolation tests
  modeled on `internal/modules/branches/isolation_integration_test.go`).
- A ROLE-targeted notification never fans out beyond the sender's tenant.
- A job enqueued under global impersonation either carries a real tenant scope or is rejected;
  a queue handler test proves no unscoped execution.
- `APP_ENV=" production "` and `APP_ENV=Production` behave identically to `production`; an
  unknown value fails config load with a clear error.
- `db:seed` in production without `--force` exits non-zero with guidance.
- `AUTH_TOKEN_EXPIRATION=0` in production fails `config.Validate()`.
- Login: identical status/body for unknown email vs wrong password; a test proves per-account
  lockout holds while the source IP rotates.
- `make check` and the integration suite (`make test-integration`) pass.

## Open questions

- **Route-by-route exempt review (C4):** for each exempt-mounted family — `user.internal.*`,
  notifications, files, shared profile handlers, tenants, packages/billing admin — decide
  system-only vs shared, and the exact permission shape (`IsSystem` flag vs `*.internal.*`).
  Note `branch`/`role`/`setting` permissions are *also* used by tenant-resolved routes, so
  blanket `IsSystem` on those groups would break tenant self-management — prefer internal
  variants for the exempt handlers.
- Backfill policy for existing `files` rows that cannot be attributed to a tenant (system
  uploads?): leave tenant-less and treat as global, or purge?
- Default `AUTH_TOKEN_EXPIRATION` value (e.g. `24h`? `720h`?) — pick one and document.
- Do existing tenants need a one-time re-sync of role permissions after the catalog change
  (`permission:sync` semantics for already-granted rows)?

## Implementation notes (resolved at implementation, 2026-10-26)

1. **Catalog decisions.** Added `user.internal.delete` and `user.internal.role.assign`
   (`IsSystem`); marked `role.internal.create`, `role.internal.update`, `file.view.all`,
   `file.delete` and all five `notification.*` permissions `IsSystem: true`. `user.delete`,
   `user.role.assign`, `user.import/export`, `file.view`, `file.create` stay tenant-grantable
   (they gate tenant-resolved features). `permission:sync` seeds the new names; already-granted
   rows are unaffected (stripping only matters for future `assign-permission` calls, which
   refuse system permissions).
2. **C2 service hardening** went in with the catalog fix: `AssignRoles` rejects an empty
   `roleIDs` list (no more "clear all roles" via the empty list) and refuses to modify a
   super-admin unless the actor is one (mirrors `Delete`/`ChangePassword`).
3. **Exempt-family review resolved.** `tenancy.InternalOnly` (403 for tenant-bound identities,
   keyed on `identity.TenantID`) wraps: tenants CRUD, the identity exempt routes, internal
   roles, internal billing, internal notifications, internal settings/flags. Ungated shared
   surfaces: `files` (creator-scoped + tenant-filtered, see 4), `permission` metadata (reads
   are global catalogue data; writes are `MustBeSuperAdmin`).
4. **Files scoping.** `files.tenant_id` is nullable (FK → tenants ON DELETE CASCADE); backfill
   takes owner tenant first, creator tenant second; unattributable rows stay global and are
   visible only to tenantless callers. Scoped reads/deletes are fail-closed in SQL:
   `all_tenants OR tenant_id = @tenant_id OR (tenant_id IS NULL AND @tenant_id = 0)`;
   `all_tenants` is true exactly for tenantless callers. Rows are stamped at create from the
   impersonation scope, else the identity's tenant.
5. **M43.** Role- and branch-targeted fan-out resolves within the notification's tenant
   (`all_tenants` only for global notifications, whose targets cannot carry USER/ROLE/BRANCH
   rules — validated at create).
6. **M48.** `Definition.SystemJob` added; `checkScope` refuses tenant-0 envelopes for
   non-system kinds in both engines (sync + River). All current kinds are marked `SystemJob`
   with justification: global notifications, tenantless self-service reads, payload-carried
   tenants, `mail.send`, platform backups, dev playground, and bulk import/export (whose
   dataprocessing job row carries the real scope via `jobScope`).
7. **H1/L1.** `APP_ENV` is normalised (trim + lowercase) at load and validated against
   {local, development, dev, staging, production}; `logging.New` also trims.
8. **H2.** `deploy/.env.production` shipped; `serve` calls
   `cfg.ValidateDeploymentPosture()` — refuses a local env under a service manager
   (`INVOCATION_ID` present) unless `APP_ALLOW_LOCAL_UNDER_SYSTEMD=true`.
9. **H4.** `AUTH_TOKEN_EXPIRATION` default 1440 (24 h); 0 rejected in production.
10. **H3.** `db:seed --force` added; production + default password without `--force` refuses;
    with `--force` a high-entropy password is generated and printed once.
11. **Open questions closed:** route review → note 3; backfill → note 4 (unattributable rows
    stay global); token default → 24 h; existing tenants need no re-sync (non-system grants
    are untouched; the removed capabilities were never legitimately grantable).
12. **Verification:** `make check` clean, full unit + integration suite green (Postgres on
    127.0.0.1), `-race` clean, and a live smoke: tenant-scoped admin token → 403 on
    `/internal/notification` and `DELETE /internal/user/{id}`; tenantless super-admin → 200;
    production `db:seed` refuses. New tests: `TestInternalRoutesRejectTenantScopedCallers`
    (41 routes × 403), `TestFilesTenantIsolation`, `TestEnqueueRefusesUnscopedScope`,
    `TestLoginPerIPLockoutCatchesSprayAcrossAccounts`,
    `TestLoginUnknownEmailIsIndistinguishableFromWrongPassword`, `TestLoadRejectsUnknownEnv`,
    `TestEnvNormalisation`.
