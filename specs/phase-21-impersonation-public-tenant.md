# Phase 21 — Login-as impersonation & public tenant resolution

**Status:** Accepted (2026-11-20)
**Source:** project-owner review (2026-11-20); closes review **M61** (`X-Tenant-Code` documented, never read)
**Depends on:** Phase 2 (tokens, tenancy), Phase 12 (`InternalOnly`), Phase 17 (activity log)

## Goal

Two gaps that every product built on the starter hits early:

1. **Login as a user.** Support staff need to see exactly what one user sees. Today they can only
   *view as a tenant* (`X-Acting-Tenant` / `X-Acting-Branch`), and they keep their own identity and
   permissions while doing it. Login-as gives a platform admin a short-lived session **as the target
   user**: the same identity, roles, tenant and branch. It is fully audited and easy to leave.
2. **Public tenant-scoped endpoints.** Pages shown before sign-in (tenant branding, a public
   catalogue, a booking form) need a tenant without a logged-in user. The resolver reads
   `X-Tenant-Code`, plus an optional `X-Branch-Id`. CORS already allows those headers, but no code
   reads them yet.

## Reference

- **Public tenant:**
  - `K:\Projects\Herd\ims-cms-saas-laravel-api-starter\app\Http\Middleware\ResolvePublicTenantMiddleware.php`
  - `K:\Projects\Herd\frc-backend\app\Http\Middleware\ResolvePublicTenantMiddleware.php`
  - Behavior parity: `X-Tenant-Code` is required; an unknown or inactive tenant gets 422
    `TENANT_CONTEXT_NOT_RESOLVED`; `X-Branch-Id` is optional and must belong to the tenant; without
    it, the tenant's first branch is used.
- **Login-as:** neither Laravel app has it. Their `canImpersonate` plus `X-Acting-*` is the header
  view-as this repo already implements (`internal/tenancy/middleware.go`). Login-as is new design;
  the decisions are listed below.

## Two kinds of impersonation

|  | View-as (exists) | Login-as (this phase) |
| --- | --- | --- |
| How | `X-Acting-Tenant` / `X-Acting-Branch` on each request | a separate token, issued once |
| Identity | still the admin | the target user |
| Permissions | the admin's | the target's |
| Use it for | cross-tenant administration, support reads | "show me what Jane sees", reproducing a user's bug |
| Writes | allowed with both acting headers | allowed, as the target, attributed to both |

## Scope checklist

### A. Login-as impersonation

- [ ] **Permission:** add `user.impersonate` to the catalog. It is `IsSystem`, held by Super-Admin
      only, and not seeded to any role.
- [ ] **Migration:**
  - add `impersonator_id bigint NULL REFERENCES users(id)` to `personal_access_tokens`;
  - add an index on `impersonator_id` (partial, `WHERE impersonator_id IS NOT NULL`).
- [ ] **Config:** `AUTH_IMPERSONATION_TTL` (default `1h`, max `8h`). Validation rejects `0`:
      impersonation tokens always expire, in every environment.
- [ ] **`POST /api/v1/impersonate/{user_id}`:**
  - Mounted in the exempt group behind `InternalOnly` and `authz.Require(user.impersonate)`.
  - Issues a token for the target with `impersonator_id` set, `expires_at = now + TTL` and
    device `impersonation`.
  - The response matches the login response, plus an `impersonator` object (`{id, name}`).
  - Re-impersonating the same user replaces the previous impersonation token.
- [ ] **Refusals:** each case returns **403** with a clear message.
  - the target is the caller;
  - the target is inactive or soft-deleted;
  - the target holds `user.impersonate` or is a Super-Admin (no escalation sideways);
  - the target is a tenantless platform account (developer);
  - the caller is already impersonating (no chains).
- [ ] **Identity:**
  - `authn.Identity` gains `ImpersonatorID int64`, loaded with the token.
  - Expired impersonation tokens are rejected like any expired token.
- [ ] **`GET /api/v1/user/profile`** returns `impersonator: {id, name} | null`, so a client can show the
      "You are logged in as Jane — Stop" banner.
- [ ] **Stop:** `DELETE /api/v1/impersonate` deletes the current impersonation token and returns
      204. The admin's own token was never touched; the client switches back to it.
- [ ] **Blocked while impersonating** (wherever the endpoint exists): these return 403 `IMPERSONATION_FORBIDDEN` through a
      `authn.DenyImpersonated` middleware.
  - change password (`/user/profile/change-password`); change e-mail;
  - two-factor / OTP settings;
  - log out other sessions; delete the account;
  - starting another impersonation;
  - impersonation tokens never refresh `last_login_at` / `last_login_device`.
- [ ] **Audit:**
  - Activity entries `impersonation.started` and `impersonation.stopped`. The causer is the
    admin, the subject is the target, and the properties hold the token id and TTL.
  - Every activity entry recorded while impersonating is caused by the target and carries
    `properties.impersonator_id`.
  - The access log and the dashboard's request entries carry `impersonator_id`.
- [ ] **Dev dashboard:**
  - The Sessions panel marks impersonation tokens with an "impersonated by …" badge, and the
    existing revoke action ends them.
  - No "log in as" button in `/dev`: starting impersonation belongs in the product's admin UI.
- [ ] **OpenAPI:** both endpoints, the `impersonator` field and the new error code.
- [ ] **Docs:**
  - the authentication guide gets a "Login as a user" section;
  - the tenancy guide gets the view-as vs login-as table;
  - permissions reference: regenerate it.

### B. Public tenant resolution

- [ ] **`tenancy.Public(opts)` middleware:**
  - `X-Tenant-Code` is required. Missing, unknown, soft-deleted or inactive (`is_active = false`)
    all give 422 `TENANT_CONTEXT_NOT_RESOLVED` with the same message, so codes cannot be
    probed by error text.
  - `X-Branch-Id` is optional and must belong to the tenant (422 otherwise).
  - Without it, the tenant's first branch (lowest id, not deleted) is used. A tenant without
    branches gets 422.
  - It produces `Scope{TenantID, BranchID, Public: true}`.
- [ ] **`Scope.Public` (new field):** services can refuse writes that must not happen
      anonymously. `EffectiveTenantID`, `EffectiveBranchID` and `Filter` behave exactly as for a
      tenant user.
- [ ] **Cache:**
  - code → `{id, is_active}` under `tenant_code:<code>` for `CheckTTL` (5 min);
  - invalidated by tenant update and delete (`tenancy.InvalidateTenantCode`), next to the existing
    `InvalidateTenantCheck`;
  - the branch check reuses `branch_check:*`.
- [ ] **SQL:** `FirstBranchOfTenant :one`. `GetTenantByCode` already exists.
- [ ] **Routes:**
  - a new `/api/v1/public` group in `internal/app/routes.go` with `tenancy.Public`, below the
    existing global public routes (`/public/packages` stays tenant-free);
  - modules get a `RegisterPublicTenantRoutes(r)` hook for their own public endpoints.
- [ ] **First endpoint:** `GET /api/v1/public/tenant` returns `{name, code, branch: {id, name}}`,
      for branding the sign-in page.
- [ ] **Rate limiting:** the existing per-IP limiter covers the group. Nothing new is needed.
- [ ] **OpenAPI:** reuse the existing `X-Tenant-Code` parameter (M61) and add `X-Branch-Id`. The
      route-coverage test lists `/api/v1/public/tenant` as public.
- [ ] **Docs:** the tenancy guide gets a "Public routes" section with a `RegisterPublicTenantRoutes`
      example. Restore the `X-Tenant-Code` line removed in 2026-11-20 once it is true.

## Out of scope

- Tenant admins impersonating users of their own tenant (only `user.impersonate` holders, i.e.
  platform staff). It can come later as a tenant-scoped permission.
- An impersonation UI in the developer dashboard (see A, Dev dashboard).
- Enforcing `tenants.is_active` for **authenticated** requests (still the known gap). This phase
  enforces it only on public routes.
- Resolving the tenant from the host name (`acme.example.com`). The code header covers it today;
  subdomain resolution can wrap the same lookup later.

## Contracts touched

- **DB:**
  - migration `00025_token_impersonator.sql`;
  - query `FirstBranchOfTenant`;
  - token queries gain `impersonator_id`.
- **API:**
  - `POST /api/v1/impersonate/{user_id}`
  - `DELETE /api/v1/impersonate`
  - `GET /api/v1/user/profile` (adds `impersonator`)
  - `GET /api/v1/public/tenant`
- **Errors:** `IMPERSONATION_FORBIDDEN` (403).
- **Config:** `AUTH_IMPERSONATION_TTL`.
- **Permissions:** `user.impersonate` (then run `ims permission:sync`).
- **CLI:** none.

## Acceptance criteria

1. Super-admin impersonates a tenant admin, and the following holds:
   - `GET /user/profile` returns the target with `impersonator` set;
   - a list call returns the target tenant's rows only;
   - a write is recorded in the activity log with the target as the causer and
     `impersonator_id` set.
2. Each refusal case in A answers 403. A tenant admin with every tenant permission cannot call
   `POST /impersonate` (403 from `InternalOnly`).
3. While impersonating, `POST /user/profile/change-password` answers 403 `IMPERSONATION_FORBIDDEN`.
4. Expiry and stopping:
   - after the TTL, the impersonation token answers 401;
   - `DELETE /impersonate` makes it answer 401 at once;
   - the admin's own token still works.
5. Impersonation never changes the target's `last_login_at`. The target's own sessions stay valid.
6. Public tenant resolution:
   - `GET /public/tenant` with a valid code returns the tenant and its first branch;
   - with `X-Branch-Id` of another tenant's branch it returns 422;
   - with no code, an unknown code or an inactive tenant it returns the same 422 body.
7. Deactivating a tenant makes `/public/tenant` answer 422 within the cache TTL, and immediately
   when the change goes through the tenants service (invalidation).
8. Tests and checks: `make gen-check`, `openapi:check`, the route-permission coverage test and the
   tenancy isolation matrix pass. Each criterion above maps to a testkit integration test.

## Open questions

1. **TTL:** is 1 h right? It is long enough for a support call and short enough that a forgotten
   tab dies.
2. **Developers:** should the Developer role also get `user.impersonate`? Proposed no: it touches
   real users' data, and developers have the dashboard.
3. **Notification:** should the impersonated user be notified (e-mail or in-app) when someone logs
   in as them? Proposed: off by default, with a setting `security.notify_impersonation`.
4. **Code matching:** should the public code match case-insensitively? Codes are generated hex
   today, so proposed exact match.
