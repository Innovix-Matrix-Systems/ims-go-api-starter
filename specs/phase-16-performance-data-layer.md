# Phase 16 — Performance, scalability & data-layer hygiene (P2)

**Status:** Done (2026-11-19; deferred items listed below)
**Source:** [docs/codebase-review.md](../docs/codebase-review.md) §10 P2 items 17–21, 30–31 — findings **H26, H28, M19, M22, M25, M30–M42, M75–M77, M79**, §9 (per-request DB cost + §9.3 ceilings)
**Depends on:** Phase 15 (cache-driver defaults and health/scheduler fixes land there first)

## Goal

Remove the per-request database tax and the scalability ceilings the review identified, and
bring the schema up to the standard the codebase already uses elsewhere. The single most
actionable item: the uncached `IsSuperAdmin` probe runs on every `Can`/`MustCan` call (2–4
queries per request before business logic). Then: batched role lookups, keyset-first
pagination for large tables, streaming import/export, retention tasks for the tables that
grow forever, and the missing/constrained indexes.

## Reference (review)

- [docs/codebase-review.md](../docs/codebase-review.md) §9 (9.1–9.4), §6 (H28), §7 (M19, M22,
  M25, M30–M43, M75–M77, M79), §7d items 6–7, §10 items 17–21, 30–31.

## Scope checklist

### Per-request database cost (§9.1, M13, M79)

- [x] Cache the super-admin probe alongside the cached permission set in `authz.Checker`
      (invalidates with the permission cache) — removes ~2–4 queries per request.
- [x] **M79:** batched `RoleRefsForUsers(ids []int64)` called once per page in the user list
      (kills the N+1 — 101 round trips on a 100-row page).
- [ ] **M13:** the billing guard resolves the plan once per request/tenant (cache or
      request-scoped memoization) instead of twice per check.

### Timeouts & pool (H28)

- [x] **H28:** `statement_timeout` + `idle_in_transaction_session_timeout` in the pool's
      `RuntimeParams` as a backstop for deadline-less code paths (CLI, ad-hoc calls).

### Pagination strategy (M19, M22, M25, §7d item 7)

- [x] **M22:** cap `page` (or the derived offset) to a sane maximum; document the keyset
      alternative for deep traversal.
- [ ] **M25:** make keyset/cursor the default for large tables (the notification feed pattern
      already exists) — at minimum for notifications, telemetry/dev lists and file listings;
      offset+COUNT stays for small admin tables.
- [ ] **M19:** role and permission list endpoints get `links`/`meta` (paginated) like every
      other list.

### Retention & unbounded growth (M30, M31)

- [x] **M30:** scheduled task purging expired/deleted notifications (four tables).
- [x] **M31:** scheduled task calling `CacheDeleteExpired` for the database cache driver
      (or wire it into the driver's own GC).

### Indexes & schema hygiene (M32–M42)

- [x] **M33:** `(tenant_id, deleted_at)` composite indexes on `branches`, `users`, `roles`.
- [ ] **M34:** trigram/GIN indexes for `ILIKE '%…%'` search (users, branches, notifications,
      settings_groups, dataprocessing) — requires `pg_trgm` extension in a migration.
- [x] **M35:** feed index matches the feed order: `(tenant_id, created_at DESC, id DESC)`.
- [x] **M36:** add `subscriptions(tenant_id,status)`, `dev_requests(tenant_id)`,
      `dev_requests(user_id)`, `personal_access_tokens(expires_at)`, `files(created_at)`.
- [ ] **M32:** fix FK behaviour — `users.branch_id`/`active_branch_id` should not silently
      `SET NULL` on branch delete (RESTRICT + explicit reassignment, per decision); add
      `branches.manager_id` FK.
- [ ] **M37:** restore the tenant FK/cascade semantics dropped by migration 00013 for
      settings/flags.
- [ ] **M38:** add audit columns (`created_by`/`updated_by`) where they are missing
      (settings, data_processing_jobs, backup_runs, dev_* — as decided).
- [x] **M39:** CHECK constraints (or enums) on free-text status columns
      (`subscriptions.status`, `payments.status`, `payment_approval_requests.status`,
      `notifications.priority`, `data_processing_jobs.status`) — follow the `backup_runs`
      model.
- [ ] **M40:** `users.is_active` → `boolean`; `payments.currency` gets a scale column or a
      fixed convention (documented).
- [x] **M41:** convention + `lock_timeout` guard for concurrent-sensitive migrations
      (`CREATE INDEX CONCURRENTLY` where applicable); fix the 00013-style ACCESS EXCLUSIVE
      drop pattern going forward.
- [x] **M42:** give migration 00012 a real Down (or document it as deliberately irreversible
      in the migration header).

### Import/export (M75–M77)

- [ ] **M75:** export streams (keyset paging + row-by-row write, no full workbook × 2 in RAM).
- [x] **M76:** import caps the unzip budget (excelize size limit) and streams rows; a crafted
      small xlsx cannot OOM a worker.
- [x] **M77:** CSV/XLSX formula-injection guard (prefix `=`,`+`,`-`,`@` cells).

### Misc (H26)

- [ ] **H26:** notification fan-out — compute unread counters in SQL instead of per-recipient
  cache ops, make the fan-out/decrement job timeout configurable, and add a large-tenant scale
  test to locate the recipient ceiling (linear work under a fixed 5-minute timeout today).
- [ ] Batch the telemetry sink's inserts (`dev/telemetry_sink.go` — one multi-row INSERT per
      flush instead of one INSERT per captured entry).
- [ ] Cross-check: list endpoints' COUNT queries are excluded where keyset pagination replaces
      them (no orphaned COUNTs).

## Implementation notes (2026-11-19)

- **§9.1:** `authz.Checker` caches `{super, permissions}` per user under `authz:access:user:{id}`;
  `IsSuperAdmin` and `Can` share it, so a warm request does no permission queries. Cache read
  errors fall through to the database.
- **M79:** `Checker.RoleRefsForUsers` (one `ANY($1)` query) feeds the user list page.
- **H28:** `DATABASE_STATEMENT_TIMEOUT` (default 5m) and `DATABASE_IDLE_IN_TX_TIMEOUT` (default 1m)
  are set as pool runtime params; `0` disables. Raise the statement timeout for very long migrations.
- **M22:** already capped (`httpx.MaxPage`).
- **M30/M31:** scheduled `notifications.purge-expired` (daily, expired > 7 days; children cascade)
  and `cache.purge-expired` (hourly, database driver only). Feed queries already hide expired rows,
  so purging changes nothing users can see.
- **Migration 00020:** `(tenant_id, id) WHERE deleted_at IS NULL` on users/branches, feed index
  `(tenant_id, created_at DESC, id DESC)`, `subscriptions(tenant_id, status)`,
  `personal_access_tokens(expires_at)`, `files(created_at)`, `dev_requests(tenant_id|user_id,
  created_at)`, plus `NOT VALID` CHECK constraints on subscription/payment/approval/job statuses
  and notification priority (new writes are enforced; legacy rows can be validated later). The
  migration header documents the M41 convention (`lock_timeout`, CONCURRENTLY for hot tables).
- **M42:** migration 00012's Down is documented as deliberately irreversible.
- **M76:** xlsx imports open with `UnzipSizeLimit` 64 MB / `UnzipXMLSizeLimit` 16 MB.
- **M77:** export cells starting with `= + - @` (or tab/CR) are prefixed with `'`.

### Deferred (with reason)

- **M34 (decision):** no trigram indexes in core — `pg_trgm` needs extension privileges and lives in
  one schema; the recipe is in the migration header for projects that need it.
- **M19 / M25:** paginating role/permission lists and keyset for files/dev lists change response
  shapes; do it when a client needs it (the notification feed shows the keyset pattern).
- **M13:** guard plan resolution per request — two cheap indexed queries; revisit with metrics.
- **M32 / M37 / M38 / M40:** FK behaviour on branch delete, settings tenant FK, extra audit columns,
  `is_active` → boolean and currency scale are schema/product decisions with migration risk on
  existing data; left for a dedicated data-model pass.
- **M75 / H26 / telemetry sink batching:** streaming export, fan-out counter rewrite and multi-row
  telemetry inserts are throughput work best done against real load numbers.

## Out of scope

- Frontend pagination UI changes (Phase 17 tracks the SPA side).
- Cache driver defaults and LRU caps (Phase 15).
- Queue/scheduler topology changes (Phase 15).

## Contracts touched

- **DB tables / migrations:** several migrations — new columns (`version` if chosen in Phase 13
  lands first), `is_active` type change, currency scale, CHECK constraints, FK fixes, many
  indexes (CONCURRENTLY where the table is hot). Each migration keeps `-- +goose Down` (fix
  M42's while touching it).
- **API endpoints:** paginated role/permission lists (`links`/`meta` added); keyset endpoints
  where introduced; export/import behaviour unchanged externally except limits.
- **Env vars / config:** `statement_timeout`/`idle_in_transaction_session_timeout` settings
  (sane defaults, overridable); pagination caps.
- **CLI commands:** possibly `ims db:prune` style commands if retention runs via CLI rather
  than the scheduler registry.

## Acceptance criteria

- An authenticated request performs ≤ 2 DB round trips before business logic (verified with
  query counting in a test).
- A 100-row user page issues exactly 1 role-refs query (batched), not 101.
- `EXPLAIN` (integration test or manual runbook) shows index usage for the hot list searches
  and the notification feed order (no filesort on ties).
- Retention tasks appear in the scheduler registry with specs, and integration tests prove
  expired rows are removed.
- Export of a large tenant completes with bounded memory (streaming test with a synthetic
  large dataset).
- An import file with a zip bomb header is rejected by size/budget (422), and formula-injection
  cells are neutralised on export.
- `users.is_active` reads as boolean in the domain layer; status columns reject invalid values
  at the DB level.
- `make check`, `make sqlc` (no diff) and the integration suite pass.

## Open questions

- Keyset pagination: expose cursor tokens for admin lists too, or only for the known-large
  tables (notifications, files, telemetry/dev)?
- M32: on branch delete — RESTRICT, or reassign users to a fallback branch (needs product
  input)?
- M38: which of the dev_* tables actually need audit columns (telemetry-style tables may be
  exempt by design)?
- M40 currency: store `numeric(18,4)` + currency code, or minor units everywhere (matches the
  integer-minor-units decision for amounts)?
