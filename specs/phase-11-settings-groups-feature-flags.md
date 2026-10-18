# Phase 11 — Settings API, groups & feature flags

**Status:** Done (2026-10-18)
**Roadmap:** PLAN.md §11 (new phase; API work that can ship before the dashboards)
**Depends on:** Phase 1 (settings table), Phase 3 (settings module, cache), Phase 5 (consumers)

## Goal

Expose the code-defined settings catalog over REST with exactly **two levels**: a **global** default
(applies to every tenant) and an optional **tenant override** (a tenant admin changes the value for
their tenant only). Reads are grouped so the UI can render settings by section. Add first-class
**feature flags** (global → tenant → user) with the same catalog/sync model.

Keys are never created through the API — `settings:sync` / `feature-flags:sync` own the catalog and
its globals. Groups are data (seeded from the catalog) so a later developer panel can create groups
and reassign keys without a schema change. Storage keeps a **polymorphic target** (`target_type`,
`target_id`) so more target types (branch/user) can be enabled later without a migration — but
settings themselves only use global + tenant in this phase.

## Reference (Laravel / mem-backend)

Read these before implementing (behavior parity where it applies; this project is multi-tenant, the
reference is single-company):

- `K:\Projects\Herd\mem-backend\app\Http\Controllers\Api\V1\Settings\SettingController.php`
  — global settings list/update, `/settings/feature-flags` (+ per-user override).
- `K:\Projects\Herd\mem-backend\app\Http\Controllers\Api\V1\SettingsGroup\SettingsGroupController.php`
  — group CRUD (the future dev-panel surface).
- `K:\Projects\Herd\mem-backend\app\Http\Services\Settings\SettingsService.php`
  — global/user flag resolution + cache invalidation.
- `K:\Projects\Herd\mem-backend\app\Enums\SettingKey.php`
  — `allSettings()`, `featureFlags()`, `group()`, `type()`, `isFeatureFlag()`.
- `K:\Projects\Herd\mem-backend\app\Models\User.php` — `getAllFeatureFlags()` (user → global → default).
- `K:\Projects\Herd\mem-backend\vendor\ahs12\laravel-setanjo\src\Repositories\DatabaseSettingsRepository.php`
  — the cache/query pattern (one blob per target), and the polymorphic `tenantable` columns we mirror.
- Migrations: `2025_06_12_131350_create_setanjo_settings_table.php`,
  `2026_08_04_113208_add_settings_group_table.php`, `2026_08_04_113228_add_settings_group_id_in_settings_table.php`.
- Policies: `SettingPolicy`, `SettingsGroupPolicy`; middleware `EnsureFeatureEnabled`.

## Decisions (resolved at spec review)

1. **Polymorphic target storage** (`target_type text`, `target_id bigint`): global = both NULL.
   This mirrors the reference's `tenantable_*` columns and lets branch/user targets be enabled later
   with **no migration**. There is no FK on the target (app-level validation); tenant/user rows are
   soft-deleted, so stale overrides are harmless and can be pruned later.
2. **Settings have exactly two levels: global + tenant.** Precedence
   `tenant → global → catalog default`. Branch/user overrides for settings are **not** enabled
   (the column supports them; the API rejects them for now).
3. **Feature flags are separate and support three levels: global, tenant, user.** Precedence
   `user → tenant → global → default`. Separate table so flag reads never load settings and settings
   writes never evict the flag cache (see performance analysis).
4. **No create through the API.** Writes only accept catalog keys; unknown key → 404. Group
   assignment is catalog/sync-owned now, dev-panel-editable later.
5. **Groups are data from day one** (`settings_groups` + `settings.group_id`) so the future dev
   panel is CRUD-only, not a migration.
6. **Cache is per target, as one blob** (like setanjo), replacing the current per-key cache for bulk
   reads. Writes invalidate only the affected target.
7. **Write permissions:**
   - **Tenant admin** may change **settings values** for their tenant only
     (`PATCH /settings/tenant/{key}`, `setting.update`). Tenant admins **never** control flags.
   - **Internal users** own everything else: global settings, tenant overrides for any tenant, and
     all feature-flag targets. Internal writes put the target in the payload/query — one route per
     resource, no per-target routes.
   - Effective reads (`GET /settings`, `GET /feature-flags`) require only authentication.
8. `feature-flags:sync` is a separate command from `settings:sync`. Catalog settings keys default to
   `Scope: GlobalOnly`; tenant override must be declared per key. The settings API is not gated by a
   feature flag.

## Performance analysis (why separate flags)

| Aspect | Same `settings` table (reference) | Separate `feature_flags` table (chosen) |
|---|---|---|
| Cache shape | One blob per target holds settings **and** flags | Two blobs per target; flags blob is small |
| Flag check cost | Loads the whole settings blob (all settings) | Loads only the small flags blob |
| Invalidation | Any setting write evicts the flags too | Settings writes never evict flags |
| Write frequency | Coupled | Independent (flags toggle often, settings rarely) |
| Evolution | Flags must stay in the settings type system | Flags can add targeting rules/rollouts later |
| Cost | Fewer tables | +1 table, +1 repo/service/cache namespace |

Both are one indexed query + one cache read per target on a miss, so the difference is payload size
and invalidation isolation. Flags are read on every authenticated request (route/UI gating), while
settings are read on settings screens, so the separate table wins on the hot path. Keep flags
boolean-only in v1; add `rules jsonb` later if rollout targeting is needed.

## Data model

New migration `migrations/000NN_settings_targets_and_groups.sql` (goose up/down). It migrates the
phase-3 `tenant_id` column to the polymorphic target and adds groups.

```sql
CREATE TABLE settings_groups (
    id          bigserial PRIMARY KEY,
    slug        text NOT NULL,
    name        text NOT NULL,
    description text,
    sort_order  integer NOT NULL DEFAULT 0,
    is_system   boolean NOT NULL DEFAULT false,
    created_by  bigint,
    updated_by  bigint,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX settings_groups_slug_unique ON settings_groups (slug);
```

`settings` — replace `tenant_id` with the polymorphic target:

```sql
ALTER TABLE settings
    ADD COLUMN target_type text,
    ADD COLUMN target_id   bigint,
    ADD COLUMN group_id    bigint REFERENCES settings_groups (id) ON DELETE SET NULL;

-- Backfill the phase-3 rows: tenant overrides become target_type = 'tenant'.
UPDATE settings SET target_type = 'tenant', target_id = tenant_id WHERE tenant_id IS NOT NULL;

-- Existing indexes reference tenant_id; replace them.
DROP INDEX settings_global_key_unique;
DROP INDEX settings_tenant_key_unique;
ALTER TABLE settings DROP COLUMN tenant_id;

CREATE UNIQUE INDEX settings_global_key_unique ON settings (key)
    WHERE target_type IS NULL;
CREATE UNIQUE INDEX settings_target_key_unique ON settings (target_type, target_id, key)
    WHERE target_type IS NOT NULL;
```

Rules: global = `target_type IS NULL AND target_id IS NULL`; tenant =
`target_type = 'tenant' AND target_id = <tenant id>`. `group_id` is meaningful on the **global**
row (key metadata); override rows leave it NULL. `value` stays `jsonb NOT NULL DEFAULT 'null'`.
Down migration reverses this (recreate `tenant_id`, backfill `target_type = 'tenant'`, restore the
phase-3 indexes).

`feature_flags` (new; polymorphic targets so `user` is supported now and future types later):

```sql
CREATE TABLE feature_flags (
    id          bigserial PRIMARY KEY,
    key         text NOT NULL,
    target_type text,          -- NULL = global; 'tenant'; 'user'
    target_id   bigint,
    value       boolean NOT NULL,
    created_by  bigint,
    updated_by  bigint,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX feature_flags_global_key_unique ON feature_flags (key)
    WHERE target_type IS NULL;
CREATE UNIQUE INDEX feature_flags_target_key_unique ON feature_flags (target_type, target_id, key)
    WHERE target_type IS NOT NULL;
```

## Catalog changes (`internal/modules/settings/registry.go`)

```go
type Scope string

const (
    // ScopeGlobalOnly is the zero value: no tenant override is legal.
    ScopeGlobalOnly Scope = ""
    ScopeTenant     Scope = "tenant"
)

type Key struct {
    Name        string
    Description string
    Type        ValueType // string | int | bool | json
    Default     any
    Group       string    // settings_groups.slug
    SortOrder   int
    Scope       Scope     // which targets may override the value (default GlobalOnly)
}
```

- **Default scope is `ScopeGlobalOnly`** (the zero value). Keep the three existing keys:
  `system.name` (`GlobalOnly`), `notification.enabled` (`Tenant`),
  `data_processing_job.cleanup_days` (`Tenant`); assign groups `general`, `notifications`,
  `data-processing`.
- Add `TypeJSON` (`value jsonb`, any JSON value) for structured settings.
- Feature-flag catalog (separate file/type): `{Key, Description, Default, Group}`; start with the
  flags the code actually gates on (e.g. `feature.audit`, `feature.data_processing`). Same dotted
  naming convention.

## Resolution

- **Settings** (two levels): tenant row → global row → catalog default. Return the effective value
  and `source` (`tenant | global | default`).
- **Flags** (three levels): user row → tenant row → global row → default.
- Writes upsert the target row. A key's `Scope` decides whether a tenant override is legal
  (`GlobalOnly` rejects it).

### Target values

| Resource | Allowed `target_type` | Notes |
|---|---|---|
| settings | `global` (default) | `target_id` forbidden |
| settings | `tenant` | `target_id` = tenant id |
| feature_flags | `global` (default) | `target_id` forbidden |
| feature_flags | `tenant` | `target_id` = tenant id, must exist |
| feature_flags | `user` | user must exist; tenant derived server-side |

`user`/`branch` for **settings** are rejected with 422 until enabled; enabling them later is a
validation change only (the polymorphic column already stores any type).

## Caching

Replace per-key caching for bulk reads with **one blob per target**:

| Key | Contents | TTL |
|---|---|---|
| `settings:blob:g` | global key → value | 1 h |
| `settings:blob:t:<tenantID>` | tenant overrides | 1 h |
| `settings:groups:v2` | groups + key→group assignment + per-key global row id | 1 h |
| `flags:blob:g` / `flags:blob:t:<id>` / `flags:blob:u:<id>` | key → bool | 5 min |

- Settings read path: load at most two blobs (tenant + global), merge, coerce with the catalog.
  One SQL query per blob on a miss
  (`WHERE target_type = $1 AND target_id = $2` or `WHERE target_type IS NULL`).
- Flags read path: load up to three blobs (user + tenant + global), merge.
- Write path: upsert the row, then `Delete` only the affected blob (and `settings:groups` for group
  edits). This replaces the current `cachekeys.Bump("settings")` that invalidates **every** tenant's
  settings on any write.
- Multi-replica: a shared driver (Redis/database) is required; memory cache is per process (same
  caveat as rate limiting). Documented in the developer guide.
- Optional later: an "effective settings/flags per tenant/user" blob (one GET) with versioned keys
  if profiling shows the merge cost matters.

## API

All responses follow the existing `{data}` envelope; problems are RFC 9457.

### Settings

**Read — any authenticated user** (the SPA needs effective values to render the UI):

`GET /api/v1/settings` (tenant-resolved) — effective (tenant → global → default), grouped:

```json
{ "data": [
  { "group": { "id": 2, "slug": "notifications", "name": "Notifications", "sort_order": 2 },
    "settings": [
      { "id": 3, "key": "notification.enabled", "type": "bool", "value": true, "default": true,
        "description": "Toggles outbound notification fan-out.",
        "source": "tenant", "scope": "tenant", "editable": true, "sort_order": 10 } ] } ] }
```

Each setting also carries `id` — the global `settings` row id (0 when a key has not been
materialized by `settings:sync`); the effective value may still come from a tenant override, as
indicated by `source`.

- Optional `?group=<slug>` filter.
- `editable` = the caller may write at their scope (tenant admin for tenant-scoped keys; internal
  for any).

**Write — tenant admin (settings only, their own tenant):**

- `PATCH /api/v1/settings/tenant/{key}` — upserts the caller's **tenant override**, requires
  `setting.update`; the key's `Scope` must allow tenant overrides. Body `{"value": …}`.

**Writes — internal (one route, target in body/query):**

- `PATCH /api/v1/internal/settings/{key}`:

  ```json
  { "value": false, "target_type": "tenant", "target_id": 1 }
  ```

  `target_type`: `global | tenant`, default **global** when omitted; `target_id` required for
  `tenant` and forbidden for `global`. Requires `setting.internal.update`. (`user`/`branch` → 422,
  not enabled for settings.)
- `DELETE /api/v1/internal/settings/{key}?target_type=tenant&target_id=1` — removes the override so
  the value falls back (`tenant` → global → default); omitting `target_type` deletes the global row
  (falls back to the catalog default). Same permission.
- `GET /api/v1/internal/settings[?target_type=tenant&target_id=1]` — admin view: every catalog key
  with the effective value for the chosen target (default global) + `source`, group metadata and
  `editable`; requires `setting.internal.view`.

Validation: unknown key → 404; type mismatch → 422; `global` + `target_id` → 422; `tenant` without
`target_id` → 422; tenant must exist. Return the effective setting after a write.

### Feature flags

Targets: **global → tenant → user** (resolution: user beats tenant beats global beats default).
**All writes are internal-only** — tenant admins cannot toggle flags.

**Read — any authenticated user:**

- `GET /api/v1/feature-flags` — effective flags for the caller:
  `{"data": {"feature.audit": false, "feature.data_processing": true}}`.

**Writes — internal only (target in the payload/query):**

- `PATCH /api/v1/internal/feature-flags` — batch update:

  ```json
  { "target_type": "tenant", "target_id": 1,
    "flags": { "feature.audit": true, "feature.data_processing": false } }
  ```

  `target_type`: `global | tenant | user` (default global); `target_id` required for
  `tenant`/`user`; a `null` flag value clears that override (inherit). Requires
  `feature_flag.internal.update`.
- `DELETE /api/v1/internal/feature-flags/{key}?target_type=tenant&target_id=1` — clear one override.
- `GET /api/v1/internal/feature-flags[?target_type=user&target_id=7]` — admin view: effective flags
  for the chosen target (default global).
- Middleware `feature:<key>` (new, `internal/platform/featureflags`): resolves the flag for the
  caller and returns `403` `{"feature_flag": "<key>"}` when disabled — same shape as the reference.

### Groups

Group metadata is embedded in `GET /settings` (above) **and** exposed through an internal CRUD
surface so the developer panel can create groups and assign keys without a schema change:

- `GET /api/v1/internal/settings-groups` — paginated list (`search`, `order_by` ∈
  `sort_order|name|created_at`, `order_direction`, `per_page`); `?is_light_weight=1` returns the
  unpaginated `{id,name,sort_order}` list. Requires `settings_group.view.all`.
- `POST /api/v1/internal/settings-groups` — create (`name` required; slug derived `Str::slug`
  parity; duplicate name → 422). Requires `settings_group.create`.
- `GET|PATCH|DELETE /api/v1/internal/settings-groups/{id}` — show / update (slug re-derived) /
  delete. Requires `settings_group.view|update|delete`. Deleting a group clears `settings.group_id`
  (FK `ON DELETE SET NULL`); affected keys fall back to their catalog group.
- `PUT /api/v1/internal/settings/{key}/group` — assign a catalog key's global row to a group
  (body `{"group_id": <id|null>}`; null clears). Requires `setting.internal.update`.

All group endpoints are internal (tenant-exempt). Group edits invalidate the `settings:groups`
cache. `settings:sync` still owns catalog groups and never overwrites a dev-panel `group_id`
assignment (only `NULL` rows are stamped).

## Permissions (`internal/authz/catalog.go` + `permission:sync`)

`setting.update` (tenant admin — settings values only), `setting.internal.view` +
`setting.internal.update` (internal settings admin), `feature_flag.internal.update` (internal flag
admin — global/tenant/user), and `settings_group.view.all` / `settings_group.view` /
`settings_group.create` / `settings_group.update` / `settings_group.delete` (internal group admin).
Effective reads (`GET /settings`, `GET /feature-flags`) require only authentication. **Tenant admins
have no feature-flag or settings-group permission.** Super-admin bypasses.

## Sync

Two commands, both idempotent:

**`settings:sync`**

1. Upsert catalog-owned groups (`is_system = true`) by slug — create or update
   name/description/sort_order. Never delete groups.
2. Insert global setting rows (`target_type IS NULL`) when absent with catalog defaults.
3. Set `group_id` on global rows **only when NULL** (a dev-panel reassignment wins).
4. Never create tenant rows.

**`feature-flags:sync`**

1. Insert global feature-flag rows (`target_type IS NULL`) when absent with catalog defaults.
2. Never touch tenant/user overrides.

`db:seed` invokes both syncs after the base seed, so a plain seed yields a complete settings
surface (groups + globals + flag globals). Existing installs upgrading to this phase must run
`settings:sync` once to materialize the catalog groups and stamp `group_id` on the global rows
(global rows created before this phase have `group_id = NULL`).

## Contracts touched

- **DB:** new `settings_groups`, `feature_flags`; `settings` swaps `tenant_id` for
  `target_type`/`target_id` + adds `group_id` and new unique indexes.
- **API:** `GET /settings`, `PATCH /settings/tenant/{key}`; `GET /internal/settings` (+ target
  query), `PATCH /internal/settings/{key}` (target in body), `DELETE /internal/settings/{key}`,
  `PUT /internal/settings/{key}/group`; `GET|POST /internal/settings-groups`,
  `GET|PATCH|DELETE /internal/settings-groups/{id}`;
  `GET /feature-flags`, `GET /internal/feature-flags` (+ target query),
  `PATCH /internal/feature-flags` (target + `flags` map), `DELETE /internal/feature-flags/{key}`;
  `api/openapi.yaml` + regenerated `internal/contract`.
- **CLI:** `settings:sync` extended (groups + global settings); new `feature-flags:sync`.
- **Config/env:** none.

## Out of scope

- Dev-panel group CRUD UI (the endpoints and schema are implemented; only the React screens are
  later).
- Branch/user **settings** overrides (the polymorphic column supports them; not enabled in the API).
- Percentage/segment rollout, allowlists, or per-request flag context (schema can grow `rules`).
- File/image settings (mem-backend's `company_logo`); use the files module later if needed.
- Audit logging of setting changes (the reference audits; this repo has no audit module yet).
- Encrypted settings/secrets.

## Acceptance criteria

- `settings:sync` seeds groups + global settings idempotently and updates catalog-owned group
  metadata; `feature-flags:sync` seeds global flags idempotently (two runs → no change).
- `GET /settings` returns groups ordered by `sort_order`, settings ordered by `sort_order`, with
  effective `value` + `source`; a tenant row beats a global row beats the catalog default.
- `PATCH /settings/tenant/{key}` writes only the caller's tenant override (tenant admin,
  `setting.update`). Internal `PATCH /internal/settings/{key}` writes the target named in the body
  (`global`/`tenant`). A `GlobalOnly` key (the default) rejects tenant writes; `user`/`branch`
  targets → 422; unknown keys → 404; wrong value type → 422; `global` with a `target_id` → 422.
- `GET /feature-flags` resolves user → tenant → global → default for any authenticated user; every
  flag write is internal (`feature_flag.internal.update`) with the target in the body — tenant admins
  cannot toggle flags. `feature:<key>` middleware returns 403 with `feature_flag` when disabled.
- Tenant isolation: a tenant admin cannot write another tenant's override; overrides are only read
  for the caller's tenant/user. Covered by integration tests.
- Cache: one blob per target (no per-key N+1), writes invalidate only the affected blob, shared-cache
  behavior verified with Redis in the cache contract suite.
- Migration up/down is clean (including the `tenant_id` → polymorphic backfill); `make sqlc` /
  `make openapi` regenerated; `make check` green.

## Resolved questions (2026-10-18)

1. Settings have **two levels only**: global + tenant override. No branch/user settings in this
   phase; the polymorphic `target_type`/`target_id` columns keep the door open for later. ✔
2. Feature flags keep three targets: global, tenant, user (user most specific), internal-write-only. ✔
3. Tenant admins may change **settings values** only (`PATCH /settings/tenant/{key}`); they hold no
   feature-flag permission. ✔
4. Group sync upserts catalog-owned (`is_system = true`) groups; key→group assignment only when NULL. ✔
5. Flag seeding is a separate `feature-flags:sync`. ✔
6. Catalog `Scope` defaults to `GlobalOnly`; tenant override must be declared per key. ✔
7. The settings API is not gated by a feature flag. ✔

## Implementation notes (2026-10-18)

- **Migration** `00013_settings_targets_and_groups.sql` creates `settings_groups` and
  `feature_flags`, backfills the phase-3 `tenant_id` into `target_type='tenant'`/`target_id`, and
  swaps the unique indexes. The `settings` table has no `created_by`/`updated_by` (only
  `settings_groups` and `feature_flags` do, matching the spec's SQL).
- **Cache blobs**: `settings:blob:g`, `settings:blob:t:<id>`, `settings:groups` (groups + key→group
  assignment), `flags:blob:g|t:<id>|u:<id>`. Writes delete only the affected blob; `settings:sync`
  deletes the global + groups blobs; `feature-flags:sync` deletes the global flag blob.
- **Resolution**: settings use two levels (tenant → global → default); flags three
  (user → tenant → global → default). `EffectiveSettings` returns group metadata per key; the
  public `GET /settings` nests items under their group, the internal views return a flat list with
  a `group` object on each item.
- **Writes**: `PATCH /settings/tenant/{key}` requires `setting.update` and rejects `GlobalOnly`
  keys with 422. Internal settings/flags writes take the target in the body (settings) or
  query (delete/read); `user`/`branch` targets for **settings** are 422 (not enabled).
- **Catalog**: `system.name` is `GlobalOnly`; `notification.enabled` and
  `data_processing_job.cleanup_days` are `Tenant`. Feature flags: `feature.audit` (default false),
  `feature.data_processing` (default true).
- **Permissions**: `setting.update` (group `setting`, granted to tenant admins),
  `setting.internal.view`, `setting.internal.update`, `feature_flag.internal.update` (system).
- **`feature:<key>` middleware** (`internal/platform/featureflags`) returns 403 with the flag key
  in the problem `meta` (`{"feature_flag": "<key>"}`); it depends on a local `Resolver` interface
  so the platform package does not import the module.
- **`settings:sync`** upserts catalog groups, then resolves slug→id via `ListGroups` (no dedicated
  lookup query) to stamp `group_id` on new global rows and backfill `group_id IS NULL`.
- **Unrelated bug fixed while verifying**: `ProcessImport` only wrote progress when
  `processed % importProgressAt (100) == 0`, so imports smaller than 100 rows completed with
  zeroed totals/counters and lost per-row errors. `ProcessImport` now persists the final
  `ImportResult` counters before `MarkCompleted` (`internal/modules/identity/exchange.go`),
  which makes `TestUserImportReportsPerRowErrors` pass.
- **Grouped response shape**: `GET /settings` returns `{group:{...}, settings:[...]}` per the
  spec example (`GroupedSettings`), while the flat internal views carry a `group` object on each
  item. The OpenAPI schema is `SettingsGroupSection` (not `SettingsGroup`, which is the group
  entity used by the CRUD endpoints).
- **Path parameters in the OpenAPI spec** are declared at the path-item level (via shared
  `PathID`/`PathKey`/`PathJobID` components) so Scalar substitutes them in generated requests.
