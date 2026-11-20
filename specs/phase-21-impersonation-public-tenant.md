# Phase 21 — Act-as-user impersonation, system audit log & public tenant resolution

**Status:** In progress (implemented on `feat/phase-21-impersonation`; telemetry item open)
**Source:** project-owner review (2026-11-20); closes review **M61** (`X-Tenant-Code` documented, never read)
**Depends on:** Phase 2 (tokens, tenancy), Phase 12 (`InternalOnly`), Phase 17 (activity log)

## Goal

Three gaps that every product built on the starter hits early:

1. **Act as a user.** Support staff need to see and do exactly what one staff member would. Today
   they can only *view as a tenant* (`X-Acting-Tenant` / `X-Acting-Branch`), and they keep their
   own identity and permissions while doing it. A new header, `X-Acting-User`, makes one request
   behave **as that user**: their roles, permissions, tenant and branch. The admin never signs in as
   them, no token is issued and nothing is stored. Every action is audited with both people.
2. **Public tenant-scoped endpoints.** Pages shown before sign-in (tenant branding, a public
   catalogue, a booking form) need a tenant without a logged-in user. The resolver reads
   `X-Tenant-Code`, plus an optional `X-Branch-Id`. CORS already allows those headers, but no code
   reads them yet.
3. **System audit log.** Internal staff can read the system audit log (`tenant_id IS NULL` rows)
   and every tenant's log, filtered by tenant or to system rows only. Act-as is always recorded
   there, and never shown to tenant users.

## Reference

- **Public tenant:**
  - `K:\Projects\Herd\ims-cms-saas-laravel-api-starter\app\Http\Middleware\ResolvePublicTenantMiddleware.php`
  - `K:\Projects\Herd\frc-backend\app\Http\Middleware\ResolvePublicTenantMiddleware.php`
  - Behavior parity: `X-Tenant-Code` is required; an unknown or inactive tenant gets 422
    `TENANT_CONTEXT_NOT_RESOLVED`; `X-Branch-Id` is optional and must belong to the tenant; without
    it, the tenant's first branch is used.
- **Act-as-user:** neither Laravel app has it. It extends the header impersonation both apps
  share (`canImpersonate` plus `X-Acting-*`), which this repo implements in
  `internal/tenancy/middleware.go` (`tenancy.Resolve`). The design is new; the decisions are
  listed below.

## Two kinds of impersonation, one middleware

|  | View-as-tenant (exists) | Act-as-user (this phase) |
| --- | --- | --- |
| Header | `X-Acting-Tenant` (+ `X-Acting-Branch`) | `X-Acting-User` |
| Who may send it | `user.tenant.switch` | `user.impersonate` (new) |
| Identity in the context | still the admin | the target user (`ImpersonatorID` = the admin) |
| Permissions checked | the admin's | the target's |
| Tenant and branch | the acting headers | the target's own tenant and active branch |
| Use it for | cross-tenant administration, support reads | "do what Jane would do", reproducing a user's bug |
| Writes | allowed with both acting headers | allowed, as the target, attributed to both |
| Session | none: per request | none: per request, stop by not sending the header |

**Why it fits.** Every layer reads the caller from the identity that `authn.Authenticate` puts in
the request context:
- route permissions: `authz.Gate` checks `identity.UserID`;
- tenancy: `tenancy.Resolve` takes the tenant and branch from the identity;
- handlers: `authn.UserID(r)` is the actor on creates and updates;
- the activity recorder falls back to the context user;
- request telemetry reads the user from the identity.

Swapping the identity once, inside `tenancy.Resolve`, makes the whole request behave as the
target. `Resolve` already runs after `Authenticate` and before every route's permission gate (gates
sit on the routes inside its group), and it already parses the acting headers.

## Scope checklist

### A. Act-as-user impersonation (`X-Acting-User`)

- [x] **Permission:** add `user.impersonate` to the catalog. The rule is simply **whoever holds
      the permission may act as a user**; there is no role check.
  - It is `IsSystem`, so it is never granted to tenant roles (`AdminTenantPermissions` skips
    system permissions). Tenant admins cannot get it.
  - Super-Admin has it through the checker's bypass.
  - The Developer role is seeded with it: `DeveloperPermissions()` returns the `dev` group plus
    `user.impersonate`. This widens what a leaked developer token can do (review H13); accepted,
    because every use is audited and the target refusals below still apply.
  - Any other internal role (support, staff) gets it by being assigned the permission through the
    normal internal role management.
- [x] **`authn.Identity` gains `ImpersonatorID int64`.** It is zero normally; when it is set,
      `UserID` and the rest of the identity are the target's.
- [x] **`tenancy.Resolve`, a new first case:** when `X-Acting-User` is present, it runs before
      today's logic.
  1. The caller must hold `user.impersonate`. Otherwise 403.
  2. `X-Acting-User` together with `X-Acting-Tenant` or `X-Acting-Branch` gets 422: the target's
     own tenant applies, so the request must pick one kind of impersonation.
  3. Load the target with `GetUser` (one indexed read per request; no cache in v1).
  4. Refuse with 403 `IMPERSONATION_FORBIDDEN`, each case with a clear message:
     - the target is the caller;
     - the target is missing, inactive or soft-deleted;
     - the target holds `user.impersonate`. That covers Super-Admins, developers and any staff
       role given the permission, so there is no sideways escalation and no chains;
     - the target is a tenantless platform account (developer, internal staff).
  5. Replace the identity in the context with the target's, setting `ImpersonatorID` to the
     caller's user ID.
  6. Continue down today's **tenant-user** path with the target. It requires a tenant and an active
     branch, uses the cached branch-belongs-to-tenant check, and produces
     `Scope{TenantID, BranchID}`. That code is reused, not copied.
- [x] **Where it applies:** only the tenant-resolved route group, because that is where
      `Resolve` is mounted. It never runs on:
  - the session routes: `/logout` deletes the token of the user in the context, so it would sign
    the *target* out;
  - `/dev`.
- [x] **Tenant-exempt routes:** `tenancy.Exempt()` answers 422 when `X-Acting-User` is present
      ("act-as is only available on tenant routes"). Otherwise an upload or other exempt call
      would silently run as the admin.
- [x] **Blocked while acting:** these return 403 `IMPERSONATION_FORBIDDEN` when
      `ImpersonatorID != 0`, through a small `authn.DenyImpersonated` middleware mounted on them.
  - profile password change (`/user/profile/change-password`);
  - profile update and avatar (the target's own account data);
  - any future e-mail, OTP or account-deletion endpoint.
- [x] **Audit:** see §C. Act-as is not shippable without it.
- [x] **CORS:** add `X-Acting-User` to the default `CORS_ALLOWED_HEADERS`.
- [x] **OpenAPI:**
  - a reusable `ActingUser` header parameter, added next to `ActingTenant`/`ActingBranch` on
    tenant-resolved paths;
  - the scaffold generator (`internal/scaffold/openapi.go`) emits it too;
  - the new error code.
- [x] **Docs:**
  - the tenancy guide replaces its impersonation table with the two-kinds table above;
  - the authentication guide gets an "Acting as a user" section;
  - permissions reference: regenerate it.

### B. Public tenant resolution

- [x] **`tenancy.Public(opts)` middleware:**
  - `X-Tenant-Code` is required. Missing, unknown, soft-deleted or inactive (`is_active = false`)
    all give 422 `TENANT_CONTEXT_NOT_RESOLVED` with the same message, so codes cannot be
    probed by error text.
  - `X-Branch-Id` is optional and must belong to the tenant (422 otherwise).
  - Without it, the tenant's first branch (lowest id, not deleted) is used. A tenant without
    branches gets 422.
  - It produces `Scope{TenantID, BranchID, Public: true}`.
- [x] **`Scope.Public` (new field):** services can refuse writes that must not happen
      anonymously. `EffectiveTenantID`, `EffectiveBranchID` and `Filter` behave exactly as for a
      tenant user.
- [x] **Cache:**
  - code → `{id, is_active}` under `tenant_code:<code>` for `CheckTTL` (5 min);
  - invalidated by tenant update and delete (`tenancy.InvalidateTenantCode`), next to the existing
    `InvalidateTenantCheck`;
  - the branch check reuses `branch_check:*`.
- [x] **SQL:** `FirstBranchOfTenant :one`. `GetTenantByCode` already exists.
- [x] **Routes:**
  - a new `/api/v1/public` group in `internal/app/routes.go` with `tenancy.Public`, below the
    existing global public routes (`/public/packages` stays tenant-free);
  - modules get a `RegisterPublicTenantRoutes(r)` hook for their own public endpoints.
- [x] **First endpoint:** `GET /api/v1/public/tenant` returns `{name, code, branch: {id, name}}`,
      for branding the sign-in page.
- [x] **Rate limiting:** the existing per-IP limiter covers the group. Nothing new is needed.
- [x] **OpenAPI:** reuse the existing `X-Tenant-Code` parameter (M61) and add `X-Branch-Id`. The
      route-coverage test lists `/api/v1/public/tenant` as public.
- [x] **Docs:** the tenancy guide gets a "Public routes" section with a `RegisterPublicTenantRoutes`
      example. Restore the `X-Tenant-Code` line removed in 2026-11-20 once it is true.

### C. Audit log: the system log, and act-as entries

`activity_log` is already split by `tenant_id`: a tenant's rows are that tenant's log, and rows with
`tenant_id IS NULL` are the **system audit log** (internal users and roles, system settings,
platform actions). This phase makes that split readable on purpose, and records act-as in it.

**Who sees what**

|  | Tenant user (`activity.view`) | Internal user (`activity.view.all`) |
| --- | --- | --- |
| Rows | their own tenant's rows only; never system rows | every tenant's rows and the system rows |
| Filter by tenant | no (always their tenant) | `tenant_id=<id>` |
| System log only | no | `tenant_id=system` |
| Done by staff | `by_staff: true` on the entry, no staff id or name | `by_staff: true` plus `impersonator {id, name}` |
| Act-as entries (`impersonation.*`) | never returned (they are system rows) | returned |

- [x] **Permission `activity.view.all`:** new, `IsSystem`, so never granted to tenant roles.
      Super-Admin has it through the bypass. It is not seeded to Developer or any other role; an
      internal role gets it by assignment.
- [x] **Feed rules** (`activity.Service.Feed`), replacing today's "platform accounts see
      everything":
  - The route stays `GET /api/v1/activity`, gated by `activity.view` **or** `activity.view.all`.
  - A caller holding `activity.view.all` and not acting as a user reads across tenants. Query
    parameter `tenant_id`: absent = every row; `<id>` = that tenant's rows; `system` = only rows
    with `tenant_id IS NULL`. Any other value is 422.
  - Any other caller (a tenant user, or anyone acting as one) reads `scope.EffectiveTenantID()`
    only. `tenant_id` is ignored for them, so a tenant user can never reach system rows.
  - An internal account without `activity.view.all` gets 403. Today it would see every tenant.
  - New filter `impersonator_id` (and `impersonated=true` for "any act-as row"), honoured only for
    `activity.view.all` holders.
- [x] **SQL:** `ListActivity` gets a three-way tenant predicate (all / one tenant / `IS NULL`) and
      the impersonation filters. Index `activity_log_system_feed_idx ON activity_log (created_at
      DESC, id DESC) WHERE tenant_id IS NULL` for the system-only view.

**Migration `00025_activity_impersonator.sql`**

- [x] `impersonator_id bigint NULL REFERENCES users(id) ON DELETE SET NULL`, plus
      `impersonator_name text NULL`, a snapshot taken when the row is written, so the record still
      names the person if their account is deleted later.
- [x] Partial index `ON activity_log (impersonator_id, created_at DESC) WHERE impersonator_id IS
      NOT NULL`.

**What gets recorded while acting**

Three layers, so that nothing done while acting can go unrecorded:

1. **Start: `impersonation.started`, a system row.** Written by `tenancy.Resolve` the first time a
   caller acts as a given target within 30 minutes (cache `SetNX` on
   `impersonation:<caller>:<target>`; if the cache is down, write it anyway). `actor_id` = the
   impersonator, `subject` = `user` / target id, `changes` = target's tenant and branch. This write
   is **fail-closed**: if the row cannot be stored, the request is refused with 503 and nothing runs
   as the target.
2. **Every request: `impersonation.request`, a system row.** A middleware right after `Resolve`
   records each request made while acting, reads included, after the response: method, route
   pattern, status, target, tenant, `request_id`. Act-as is rare support work, so the volume is
   small, and this is what answers "what did they look at". Writes are covered even in modules that
   never call `Record` (uploads, product modules added later).
3. **Existing entries are marked.** The recorder reads `ImpersonatorID` from the identity and fills
   `impersonator_id` / `impersonator_name` on every entry written while acting. The actor stays the
   target, so the tenant row reads "done by Jane, via staff"; the system view shows "done by Jane,
   on behalf of admin X". The shared `request_id` links it to the matching `impersonation.request` row.

- [x] **Refusals:** every 403 `IMPERSONATION_FORBIDDEN` writes an `impersonation.denied` system row
      (caller, requested target, reason). Best-effort, like ordinary entries.
- [x] **Tenants see that staff acted, not who.** Every entry carries `by_staff` (true when
      `impersonator_id` is set). Callers without `activity.view.all` get only that flag; the
      `impersonator` object is `null` for them. The `impersonation.*` rows are system rows, so the
      start, per-request and denied entries never reach a tenant feed. A user acting as Jane who
      opens the activity page sees Jane's view: the flag, not the details.
- [x] **Survives tenant deletion:** the `impersonation.*` rows have no tenant, so deleting the
      tenant (which cascades its own rows) keeps the record of who acted inside it.
- [x] **Retention:** one window for everything, `ACTIVITY_RETENTION_DAYS`.
- [ ] **Telemetry:** `telemetry.RequestEntry` and the access log gain `impersonator_id`; the
      developer dashboard's request list shows an "acting as" badge. This is debugging aid only; the
      audit record is the activity log.
- [x] **OpenAPI:** the `tenant_id`, `impersonator_id` and `impersonated` query parameters; the
      `by_staff` (boolean) and `impersonator` (`{id, name}`, null for tenant callers) on activity
      entries.

## Out of scope

- **Login-as tokens** (signing in as the user with a separate, expiring session). Act-as headers
  give the same behaviour without a second session. They can be added later on top of the same
  `ImpersonatorID` field.
- **Tenant admins** acting as users of their own tenant. Only `user.impersonate` holders (platform
  staff) can act as users. It can come later as a tenant-scoped permission.
- **An act-as UI.** Starting act-as belongs in the product's admin UI. The developer dashboard only
  shows the audit trail.
- Enforcing `tenants.is_active` for **authenticated** requests (still the known gap). This phase
  enforces it only on public routes.
- Resolving the tenant from the host name (`acme.example.com`). The code header covers it today;
  subdomain resolution can wrap the same lookup later.

## Contracts touched

- **DB:**
  - migration `00025_activity_impersonator.sql` (`activity_log.impersonator_id`,
    `impersonator_name`, the impersonation and system-feed indexes);
  - query `FirstBranchOfTenant`;
  - the activity insert gains the impersonator columns; `ListActivity` gains the three-way tenant
    predicate and the impersonation filters.
- **API:**
  - header `X-Acting-User` on tenant-resolved routes;
  - `GET /api/v1/activity`: query parameters `tenant_id` (`<id>` | `system`), `impersonator_id`,
    `impersonated`; `impersonator` on entries (system readers only); internal accounts now need
    `activity.view.all`;
  - activity actions `impersonation.started`, `impersonation.request`, `impersonation.denied`;
  - `GET /api/v1/public/tenant`.
- **Errors:** `IMPERSONATION_FORBIDDEN` (403); 503 when the `impersonation.started` row cannot be
  written.
- **Config:** `CORS_ALLOWED_HEADERS` default gains `X-Acting-User`.
- **Permissions:** `user.impersonate` and `activity.view.all` (then run `ims permission:sync`).
- **CLI:** none.

## Acceptance criteria

1. A super-admin sends `X-Acting-User: <tenant admin>`, and the following holds:
   - a list call returns that admin's tenant's rows only;
   - a route the admin's role lacks answers 403, even though the super-admin could call it;
   - a create stores the target as the actor, and its activity entry has
     `impersonator_id` = the super-admin and `impersonator_name` set.
   The same holds for a Developer, and for an internal user whose role was given
   `user.impersonate`.
2. Refusals:
   - Every case in A step 4 answers 403 `IMPERSONATION_FORBIDDEN`.
   - An internal user without `user.impersonate` gets 403.
   - A tenant admin holding every tenant permission gets 403 when sending `X-Acting-User`.
   - Combining it with `X-Acting-Tenant` answers 422.
3. While acting, `POST /user/profile/change-password` answers 403 `IMPERSONATION_FORBIDDEN`, and
   the target's password is unchanged.
4. Route groups:
   - `X-Acting-User` on a tenant-exempt route answers 422;
   - on `/logout` it is ignored: the admin's own token is revoked, the target's are not.
5. The next request without the header behaves as the admin again. Nothing about the target
   (tokens, `last_login_at`) changed.
6. Public tenant resolution:
   - `GET /public/tenant` with a valid code returns the tenant and its first branch;
   - with `X-Branch-Id` of another tenant's branch it returns 422;
   - with no code, an unknown code or an inactive tenant it returns the same 422 body.
7. Deactivating a tenant makes `/public/tenant` answer 422 within the cache TTL, and immediately
   when the change goes through the tenants service (invalidation).
8. System audit log:
   - an `activity.view.all` holder gets every row with no filter, one tenant's rows with
     `tenant_id=<id>`, and only `tenant_id IS NULL` rows with `tenant_id=system`;
   - a tenant admin never gets a system row, whatever `tenant_id` they send;
   - an internal account without `activity.view.all` gets 403 on `GET /activity`.
9. Act-as audit trail:
   - the first act-as request writes one `impersonation.started` system row; a second request
     within 30 minutes does not write another;
   - every request while acting, a GET included, writes one `impersonation.request` system row with
     the same `request_id` as any entry the service recorded;
   - when the `impersonation.started` insert fails, the request answers 503 and the handler does not
     run;
   - a refused act-as writes `impersonation.denied`;
   - the tenant admin's feed shows the act-as write as done by the target with `by_staff: true`
     and `impersonator: null`, and contains no `impersonation.*` rows;
   - after the tenant is deleted, its `impersonation.*` rows are still there.
10. Tests and checks: `make gen-check`, `openapi:check`, the route-permission coverage test and the
   tenancy isolation matrix pass. Each criterion above maps to a testkit integration test.

## Decisions (project owner, 2026-11-20)

1. **Who may act as a user:** whoever holds `user.impersonate`. Super-Admin (bypass) and Developer
   (seeded) have it; other internal roles get it by assignment. No role-based rule.
2. **Notifying the target:** no. No notification and no setting for it.
3. **Writes while acting:** allowed, as the target, and always logged with the impersonator. No
   read-only mode.
4. **Public code matching:** exact match.
5. **Audit log:** `tenant_id IS NULL` rows are the system audit log. Internal users holding
   `activity.view.all` read every tenant and the system log, and can filter to one tenant or to
   the system log only. Tenant users see only their tenant's rows. Starting act-as is written to the
   system log; every request while acting is logged; every existing entry written while acting is
   marked with the impersonator. Tenant users see that a change was made by staff
   (`by_staff`), but not who, and never the `impersonation.*` rows; system readers see everything
   (§C).

## Deviations (implementation, 2026-11-20)

1. **Branch switch is blocked too.** `PATCH /user/switch-branch/{id}` stores the user's active
   branch, so it joins the account changes refused while acting.
2. **A non-numeric `X-Acting-User`** answers 422 (validation), not 403: it is a malformed header,
   not a refused target.
3. **A caller without `user.impersonate`** gets 403 `IMPERSONATION_FORBIDDEN` (not
   `INSUFFICIENT_PERMISSIONS`) and an `impersonation.denied` row with reason `permission`, so
   attempts by tenant users show up in the system log.
4. **`X-Tenant-Code` had no OpenAPI parameter** to reuse (it was removed earlier); `TenantCode`
   and `BranchId` parameters were added.
5. **Telemetry `impersonator_id` is not done.** Request telemetry reads the identity from the
   outer request context, which never holds it, so `user_id`/`tenant_id` are always empty in
   captured requests today. That pre-existing bug needs fixing first (a mutable holder put in the
   context by the telemetry middleware); the audit record does not depend on it.
6. **Public branch cache:** the branch name and the tenant's first branch are cached under
   `public_branch:*` / `public_first_branch:*`, dropped by `InvalidateBranchCheck`, instead of
   reusing `branch_check:*` (which holds only a boolean).

## Open questions

None.
