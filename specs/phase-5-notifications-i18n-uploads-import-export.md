# Phase 5 — Notifications, i18n, uploads, import/export, mail

**Status:** Accepted (2026-10-17)
**Roadmap:** PLAN.md §11 Phase 5
**Depends on:** Phases 2–4 (identity/users, tenancy, RBAC, cache, queue engine, settings, billing)

## Goal

User-facing platform features: DB fan-out notifications with unread counters and a cursor feed;
embedded i18n catalogs (en/bn) that translate validation/problem/service messages; file uploads
with a storage disk abstraction; background Excel import/export with real progress tracking; and a
log/smtp mail driver used for OTP delivery. Billing renewal reminders (log-only in Phase 4) start
creating real in-app notifications.

## Reference (Laravel)

- `app/Services/Notification/*`, `app/Repositories/Notification/*`, `app/Jobs/{FanOutNotification,MarkAllNotificationsRead,DecrementUnreadCounters}Job.php`,
  `app/Http/Controllers/Api/V1/Notification/*`, `routes/api/v1/NotificationRoutes.php`,
  migrations `2026_07_08_1048*_create_notification*` — fan-out, counters, targets, feed
- `app/Helpers/NotificationHelper.php`, `app/DTOs/Notification/*`, `app/Rules/NotificationTargetIdRule.php`
- `lang/en|bn/*` — catalogs to port key-for-key (scope in Decisions 13)
- `app/Services/Upload/UploadService.php`, `app/Http/Controllers/Api/V1/Upload/UploadController.php`,
  `app/Policies/UploadPolicy.php`, `routes/api/v1/UploadRoutes.php`, `app/Models/Upload.php`,
  Spatie media usage on `User`/`Upload` — uploads/media
- `app/Jobs/ProcessUser{Import,Export}.php`, `app/Imports/UserImport.php`, `app/Exports/UserExport.php`,
  `app/Services/DataProcessingJob/*`, `app/DTOs/DataProcessingJob/*`,
  `app/Http/Controllers/Api/V1/DataProcessingJob/*`, `routes/api/v1/{User,DataProcessingJob}Routes.php`,
  `app/Console/Commands/CleanupCompletedDataProcessingJobsCommand.php` — import/export + tracking
- `config/mail.php`, `app/Services/Auth/OtpService.php` — mail driver + OTP intent
- `app/Console/Commands/SendRenewalRemindersCommand.php`, `app/Jobs/SendRenewalReminderJob.php` — reminder TODO
- `config/filesystems.php`, `config/media-library.php` — disk/path/URL conventions

## Decisions (locked)

1. **Notifications schema — ported 1:1** (four tables, no soft deletes): `notifications`
   (tenant_id nullable = global; event_type, priority INFO|WARNING|CRITICAL, title, body, data
   jsonb, group_key, created_by, updated_by, expires_at), `notification_targets` (target_type
   USER|ROLE|BRANCH|TENANT|GLOBAL, target_id nullable; **bigint**, not Laravel's varchar — the
   model cast it to int anyway), `notification_reads` (unique `(notification_id, user_id)`;
   `tenant_id`/`branch_id` nullable), `notification_deliveries` (unique
   `(notification_id, user_id)`; status `pending`; Laravel never advances it — keep the column,
   don't build on it). Children carry `ON DELETE CASCADE` on `notification_id`, so deleting a
   notification removes targets/reads/deliveries atomically.
2. **Fan-out** (`notification.fanout` job; CRITICAL → `critical` queue, else `default`; mirror of
   `FanOutNotificationJob`): resolve target users (USER by id; ROLE by role id via role
   assignments; BRANCH by `users.branch_id` — not `active_branch_id`, parity; TENANT by tenant_id;
   GLOBAL by `users.tenant_id IS NULL`), dedupe, chunk 500, then per chunk
   `INSERT ... ON CONFLICT (notification_id, user_id) DO NOTHING RETURNING user_id` and increment
   unread counters **only for returned (newly inserted) users** — fixes the reference's
   retry double-count bug (delivery rows were idempotent, counters were not). Increments happen
   after each chunk commits. Missing notification → job exits quietly (parity). Empty targets
   normalize to TENANT (tenant context) or GLOBAL (internal) at create time only. Fan-out skips
   dispatch entirely when the `notification.enabled` setting is off.
3. **Unread counters + recompute fallback**: counters live in `cache.Store` under
   `notif:t{tenant}:u{user}:unread` (TTL 7d) and the read-all marker under
   `notif:t{tenant}:u{user}:read_all_before` (TTL 90d, Unix seconds). **Improvement over the
   reference**: when the unread key is missing (flushed/expired) the service recomputes it from
   the DB and re-caches:
   `count(deliveries d ⋈ notifications n) where d.user_id = u, matching tenant, n not expired,
   no read row for (n, u)` — counters are an optimization, never authoritative. This is the
   acceptance case "cache flush mid-flight".
4. **Reads are user-scoped, not branch-scoped** (deviation/fix): read state and the unique key are
   per `(notification_id, user_id)`; `tenant_id`/`branch_id` on `notification_reads` are stamped
   for reporting only and never filter reads. Fixes the reference's BranchAware bugs (duplicate-key
   errors across branches, mark-all-read rows invisible to branch-scoped lookups).
5. **markRead** (`POST /v1/notification/{id}/read` → 204): 404 when the notification is not
   target-addressable to the caller, `INSERT ... ON CONFLICT DO NOTHING`, decrement the counter
   only when a row was actually inserted. Expired notifications can still be marked read by id
   (parity).
6. **markAllRead** (`POST /v1/notification/read-all` → 204): enqueues `notification.mark-all-read`
   (`default` queue); the handler marks only target-addressable, non-expired notifications
   (idempotent inserts), then sets the `read_all_before` marker and count `0` after commit.
   Fixes the reference's unscoped mark-all (which could touch every tenant's rows).
7. **Delete + counter repairs**: the service collects unread delivery user ids before deleting,
   deletes in one transaction, then dispatches `notification.decrement-unread` with user ids
   chunked to 500 per job payload (bounds payload size; the reference serialized the whole
   audience into one job).
8. **Feed** (`GET /v1/notification/feed`): cursor pagination ordered `(created_at, id) DESC`;
   cursor is base64url JSON `{created_at, id}` (documented format; Laravel's own clients use it);
   params `limit` (default 20, **max 100** — deviation: the reference hard-capped at 20),
   `unread_only`, `priority`; response `{data, links, meta{per_page, next_cursor, prev_cursor,
   unread_count}}`; items carry `is_read` from the caller's read row; expired rows excluded;
   visibility is target-based (a broadcast is visible before fan-out finishes — parity).
   `unread_only` also excludes rows at/below the caller's `read_all_before` marker.
9. **Admin CRUD is internal/tenant-exempt** under `/v1/internal/notification*` with
   `notification.view.all|view|create|update|delete` permission gates; feed/mark-read routes
   require only authentication (parity) — tenant variants under `/v1/notification*`, internal
   variants under `/v1/internal/notification*` (for super-admin identities without a tenant).
10. **Update = full replacement** (parity): `event_type`, `priority`, `title`, `targets` required;
    targets replaced wholesale; `tenant_id` immutable (accepted-but-ignored, parity); delta
    fan-out runs with `excludeExistingDeliveries=true`; users dropped by a target change keep
    their reads/deliveries and unread counts (parity, documented).
11. **`event_type` is a validated string catalog** in Go (ships `renewal.reminder`, the reference's
    only enum value) instead of the reference's single-value enum — extension point, same
    validation behavior for today's value.
12. **Billing renewal reminders produce notifications** now: `billing.send-renewal-reminders`
    creates one `renewal.reminder` notification per subscription at 7/3/1 days, targeted TENANT,
    `group_key = billing.renewal.{subscription_id}`, `data = {subscription_id, expires_at,
    days_before}`. Billing depends on a small `Notifier` interface implemented by the
    notifications service (no import cycle). Reminder mail stays out of scope (in-app only).
13. **i18n — go-i18n v2, API-relevant catalogs only**: embedded JSON in
    `internal/platform/i18n/locales/{en,bn}.json`; Laravel dot-keys are message IDs (flat
    `messages.*` keys stay flat; `validation.*`, `http-statuses.*` nested); ported scope:
    `messages.php` (46), `http-statuses.php` (78), `validation.php` (262 incl. `attributes.*`) +
    the few framework strings our responses use (e.g. `The given data was invalid.` for 422
    detail). Dormant files (`en/bn.json` sentence keys, `actions`, `auth`, `passwords`,
    `pagination`) are **not** ported (deviation; catalogs can be added later). Placeholders
    `:snake_case` become go-i18n params (`{{.Attribute}}`); message text stays identical (fix bn's
    lost `:name` placeholder in "Duplicate :name"). The single plural key
    (`(and :count more errors)`) uses CLDR `PluralCount`. Locale middleware puts the request
    localizer in context (BCP 47 matching, existing `ParseLocale` semantics); CLI/jobs fall back
    to English.
14. **Validation + problem translation through the same system**: `httpx` maps validator failures
    to `validation.<tag>[.kind]` keys with `:attribute` from `validation.attributes.<field>`
    (fallback: humanized field) and rule params (`:min`, `:max`, `:size`, `:other`, `:value`);
    problem titles come from `http-statuses.<status>`; service errors use `messages.*` ids
    (`apperr.Error` gains an optional message-key + params, rendered by the responder via the
    request localizer). Raw validator tags never leak.
15. **Files — one table for the library and all attachments; Spatie `media` dropped.** Research
    finding: the Laravel two-table split exists only because the package keeps a generic registry
    apart from your own `uploads`; in Go the storage layer is ours, so one table covers the
    library resource and every future attachment (the Spatie model without the package). `files`
    columns: `uuid` (public id, unique), `name` (original filename), `file_name` (sanitized
    stored name), `disk`, `path`, `mime_type`, `size`, `type` (`image|video|document|other`,
    derived from mime, parity mapping), `owner_type` + `owner_id` + `collection` (nullable
    **attachment descriptor**: `owner_type` is a short app enum — `user` today, `supplier` etc.
    later; library files have all three NULL), `created_by`, `updated_by`, timestamps.
    - Constraints: `CHECK` that owner columns are all NULL or all set; index
      `(owner_type, owner_id, collection)`; index `type`. Owner references are enforced in
      services (a polymorphic id cannot carry a FK; the alternative — one FK column per owner
      type — needs a migration for every new owner and is rejected as unscalable; the
      reference's morphs had the same property). Modules delete their files when hard-deleting
      an owner; users soft-delete, so avatar files persist (documented).
    - **Collection registry in Go** per owner (`user: [profile(single)]`,
      `supplier: [documents(multi), …]`): single-file collections are replaced atomically in the
      service; `profile` additionally gets a dedicated partial unique index
      (`WHERE owner_type='user' AND collection='profile'`) so concurrent avatar replacements can
      never leave two profile files. Future single-file collections add their own named partial
      index (explicit, reviewable).
    - **No `media` table** (deviation): no conversions/responsive-image/manipulation/order
      columns, no `custom_properties` (all unused in the reference). Per-attachment metadata
      (e.g. supplier document type/expiry) belongs to the owning module in its own table
      referencing `files.id` — `files` stays generic. Reuse without ownership is also a FK from
      the owning table (or an extra upload).
    - Library/API (**deviation** — `upload` is the action, `file` is the resource): `GET
      /v1/files` lists **all** files with optional `type` and `attached=true|false` filters
      (nothing hidden; responses carry `attached`); `POST /v1/files` (multipart, 201);
      `GET/DELETE /v1/files/{id}` (204; hard delete removes the disk file; deleting an attached
      file frees its owner). Catalog permissions renamed `upload.*` → `file.*` with the
      reference's policy logic (`view.all`, or `view` + creator; `delete`, or creator +
      `create`); `permission:sync` prunes catalog-absent system permissions so old `upload.*`
      rows disappear. Create rules parity: `pdf,jpg,jpeg,png,docx,doc,mp4,mov,mkv` ≤ 100 MB.
    - `POST /v1/user/profile/update-avatar` (parity path; identity action): image ≤ 2 MB,
      `user.update` or self; replaces the previous profile file (old row + disk file deleted).
      User responses gain an additive `photo` field (computed URL, null when none).
    - Files are platform-global: no tenant columns, tenant-exempt routes (parity).
16. **Multipart binding**: new `httpx` multipart helper with per-route byte limits (avatar 2 MB,
    import 10 MB, file 100 MB). The global JSON binder limit is untouched; multipart routes
    parse with their own limits.
17. **Storage disks** (`internal/platform/storage`): `Disk` interface
    (`Put/Get/Delete/Exists/URL/Size`), drivers `local` (private root `APP_STORAGE_DIR/private`),
    `public` (root `APP_STORAGE_DIR/public`, URL `{APP_URL}/storage/...`), and `s3`
    (**aws-sdk-go-v2**, `S3_ENDPOINT/REGION/BUCKET/ACCESS_KEY/SECRET_KEY/PATH_STYLE` for
    MinIO compatibility; `S3_PUBLIC_URL` base when set, else a presigned GET). File
    resource/avatar paths `files/{uuid}/{file_name}` on `FILES_DISK` (default `public`; `local`
    is rejected for file resources — they must be publicly addressable). The app serves
    `GET /storage/*` from the public dir. Data-processing files use `DATA_PROCESSING_DISK`
    (default `private`), base path `jobs/`.
18. **Import/export — background only** (deviation: the reference's synchronous import/export
    endpoints are deferred; background covers the UX):
    - `POST /v1/user/import-background` (xlsx/xls/csv ≤ 10 MB, `user.import`),
      `POST /v1/user/export-background` (filter body, `user.export`), `POST /v1/user/import-template`
      (CSV attachment: heading + 3 sample rows, auth only — parity); internal variants under
      `/v1/internal/user/*` (same permissions, create global users, parity).
    - Job kinds `user.import` / `user.export` on the **heavy** queue with **MaxRetries 1**
      (deviation: imports are not idempotent — retries would duplicate users — and large files
      need the 1800s timeout; the reference left them on `default`).
    - Import: excelize streaming read; heading row slug-normalized (`name, email, phone, password,
      status, roles`), roles matched by **name**; per-row validation mirrors identity rules;
      blank password → `AUTH_DEFAULT_PASSWORD`; `status` "active" → true, else false; per-row
      errors in the Laravel shape `[{row, errors:{field:[...]}}]` (+ `general` string for row
      exceptions); rows are created through the identity service (users.max enforced per row).
    - Export: excelize streamed write; columns parity `ID, Name, Email, Phone, Status, Roles,
      Branch, Created At`; filters snapshot in the job `filters` JSON (search/is_active/
      role_name/order_by/order_direction, whitelisted ordering); rows read in chunks (no
      load-all, deviation) preserving order.
    - **Real progress** (fix): `total_items` = parsed data rows; counters updated as chunks
      process so `progress_percentage` is meaningful mid-run (the reference never updated it).
    - **Status lifecycle** (fix of the reference's overwrite bug): fatal error → `FAILED` +
      `error_message`; partial row failures → `COMPLETED` with counts. Import source file is
      deleted after the run; exports keep the generated file.
19. **Data-processing API** (module gains handler/request/routes):
    `job_id` is a UUID assigned at creation (public identifier, parity) and used in routes:
    `GET /v1/data-processing/jobs` (paginated; `data_processing_job.view.all`; search/type/status
    filters), `GET /v1/data-processing/jobs/user` (own jobs), `GET /v1/data-processing/job/{jobId}`
    (own or `view.all` — the reference had no gate), `GET /v1/data-processing/job/{jobId}/download`
    (own or `view.all`; internal/global jobs require `view.all`; 404 unless `COMPLETED` with an
    existing file — the reference skipped the status check),
    `GET|POST /v1/data-processing/settings/cleanup` (`view.all`) backed by the
    `data_processing_job.cleanup_days` setting (tenant override → global, default 30).
    Cleanup task (existing) deletes records **and files** older than the retention window.
20. **Mail** (`internal/platform/mail`): `Driver` interface `Send(ctx, Message{To, Subject, Text})`;
    drivers `log` (slog; body logged only in local/debug, never OTPs in production) and `smtp`
    (`net/smtp`, STARTTLS + auth when configured). Config `MAIL_DRIVER=log|smtp`, `MAIL_HOST`,
    `MAIL_PORT`, `MAIL_USERNAME`, `MAIL_PASSWORD`, `MAIL_ENCRYPTION`, `MAIL_FROM_ADDRESS`,
    `MAIL_FROM_NAME`; validation requires host/from when smtp. Delivery happens through the queue:
    job kind `mail.send` (`default`), so OTP login (`AUTH_OTP_ENABLED=true`) generates the code,
    enqueues the mail (subject "Your OTP code", body "{code} is your OTP", matching the
    reference's intended text), and returns `OTP_REQUIRED`; the sync engine executes inline in
    dev/tests (log driver prints the code only when debug).
21. **Queue wiring**: all Phase 5 job definitions (`notification.fanout`,
    `notification.mark-all-read`, `notification.decrement-unread`, `user.import`, `user.export`,
    `mail.send`) are registered in the composition root before the engine is constructed; services
    receive the `queue.Engine` interface explicitly. Envelope tenant restoration (existing River
    worker) supplies scope to handlers; payloads stay small JSON structs.
22. **Config/env additions**: `Mail`, `Storage` (disks + S3), `Files` (disk selection),
    `Processing` (disk, base path) sections + `.env.example` entries. Existing
    `HTTP_MAX_REQUEST_BODY_BYTES` (10 MB JSON) stays; multipart limits are per-route (Decision 16).

## Scope checklist

- [x] migrations + sqlc: `notifications` (4 tables), `files` (+ query files, dbgen)
- [x] notifications module: model, repository (target resolution, feed, reads, counters),
      service (create/update/delete, markRead, markAllRead, fan-out, recompute), jobs
- [x] notifications HTTP: feed + mark-read + internal admin CRUD (parity route table), target
      validation rules (tenant required unless all targets GLOBAL)
- [x] billing reminders wired through the notifications `Notifier` (replaces log-only)
- [x] i18n: locales/{en,bn}.json catalogs + translator + middleware localizer; httpx validation
      message mapping + problem titles + apperr message keys; en/bn parity test
- [x] storage platform package (private/public/s3 disks) + `/storage/*` static serving
- [x] files module: `files` CRUD, avatar attachment (single-file replacement), type mapping,
      user `photo` field
- [x] dataprocessing: handler/request/routes (list/show-by-jobId/own/download/cleanup settings),
      GetByJobID + user queries, download ownership checks, file cleanup on retention
- [x] identity: excelize import/export services + `user.import`/`user.export` jobs + template CSV
      download + internal/tenant routes; user `photo` in responses
- [x] mail platform package (log/smtp) + `mail.send` job + OTP enqueue path
- [x] CLI: cleanup task deletes files too; any new commands if needed (`queue:stats` untouched)
- [x] tests: fan-out idempotency, counter recompute/flush, mark-all-read, feed cursor stability,
      i18n parity + localized 422, file typing/limits/delete/avatar replacement, import per-row
      errors + status lifecycle, export file + download ownership, mail job/OTP enqueue, tenant
      isolation for feed/jobs/downloads

## Out of scope

- Image conversions/thumbnails, responsive images, S3 lifecycle policies.
- Direct (synchronous) import/export endpoints — deferred (Decision 18).
- Notification channels beyond in-app (mail/SMS/push delivery of notifications); reminder mail.
- Dormant i18n catalogs (Decision 13); `messages.serial.conflict` (feature absent).
- CMS component-file references (`component_media`); image dimension validation; per-attachment
  metadata columns on `files` (owning modules keep their own metadata tables); a separate
  `file_attachments` table (only if files ever need shared/simultaneous attachments).
- Public register/reset flows — still out (parity).

## Contracts touched

- DB: `migrations/00010_create_notifications.sql`,
  `00011_create_files.sql`; `sql/queries/{notifications,files}.sql`;
  `internal/dbgen` (regenerate); dataprocessing queries gain job_id/user lookups
- API: `/v1/notification*`, `/v1/internal/notification*`, `/v1/files*`,
  `/v1/user/profile/update-avatar`, `/v1/user/{import-background,export-background,import-template}`,
  `/v1/internal/user/{import-background,export-background}`, `/v1/data-processing/*`, `/storage/*`
- Permissions: catalog rename `upload.*` → `file.*`; `permission:sync` prunes catalog-absent
  system permissions (removes the old keys)
- Env: `MAIL_*`, `FILES_DISK`, `DATA_PROCESSING_DISK`, `S3_*`
- CLI: `data-processing:cleanup-completed` (now also deletes files)

## Acceptance criteria

- Fan-out is idempotent: re-running `notification.fanout` inserts no duplicate deliveries and does
  not double unread counters; target resolution matches the USER/ROLE/BRANCH/TENANT/GLOBAL matrix
- Unread counters: increment on new deliveries, decrement on first read and on delete, recompute
  from DB when the cache key is missing (cache flush mid-flight), `read_all_before` respected by
  `unread_only`; `markAllRead` marks only addressable rows and is idempotent
- Feed cursor pagination is stable under concurrent inserts (no skipped/duplicated rows across
  pages); invalid cursors return the first page
- Admin create validates `tenant_id` unless all targets are GLOBAL, and target ids exist in the
  tenant (roles: tenant or system); update replaces targets and delta-fans-out only new users
- i18n: en/bn key parity test passes; a 422 renders `validation.*` messages, problem titles render
  from `http-statuses.*`, and both switch with `Accept-Language: bn`; values spot-checked against
  `lang/en`
- Files: mime→type mapping (image/video/pdf/other), > 100 MB and disallowed mimes rejected,
  delete removes the disk file and frees its owner, list filters `type`/`attached`, avatar
  replacement is atomic under concurrency (exactly one `profile` file) and `photo` updates;
  `permission:sync` retires `upload.*` system permissions
- Import: xlsx/csv with valid and invalid rows yields per-row `errors` and exact counters; job row
  goes pending → processing → completed, fatal errors → failed (no overwrite); export produces a
  parity-columns xlsx on the private disk, downloadable only by owner/`view.all`
- OTP-enabled login enqueues `mail.send` (sync engine sends via log driver in tests) and returns
  `OTP_REQUIRED`; smtp driver verified against an in-test SMTP listener; smtp config validated
- Reminders: expiring subscriptions at 7/3/1 days create `renewal.reminder` notifications
- Tenant isolation: notification feed, job show/download and files denial matrix passes
- `gofmt -l` empty, `golangci-lint run` exit 0, `go test -race ./...` green with and without
  `DATABASE_TEST_DSN`; `make sqlc` diff-clean

## Implementation notes

- **Status:** shipped 2026-10-17. Verified live: migrations applied, `health:check` green,
  `permission:sync` synced 54 permissions (rename applied), a bn request renders the localized
  422 problem (`title: আনপ্রসেস এন্টিটি`, translated field errors), `/api/v1/files` requires auth,
  `/storage/*` serves the public disk.
- **Tests:** unit suites for i18n (en/bn parity + rendering), storage (disk contract, traversal,
  private-URL), mail (log driver, smtp against an in-test SMTP listener), OTP mail enqueue, and
  the notifications service (fan-out counter idempotency, recompute fallback, feed page assembly,
  target validation matrix, read idempotency). Integration suites (real Postgres) cover fan-out
  idempotency + visibility matrix + mark-all-read + delete counter repair + cursor stability
  (notifications), library files + avatar replacement (files), and import per-row errors +
  export workbook + download guards (identity).
- **Laravel quirks fixed (recorded deviations):** idempotent fan-out counters (retries safe);
  user-scoped read state; scoped/idempotent mark-all-read; counter recompute fallback; real
  progress + honest FAILED status in import/export; download requires COMPLETED + existing file;
  feed `limit` up to 100; mark-all job payload chunking; import/export on the heavy queue
  (MaxRetries 1 — imports are not idempotent).
- **Implementation deviations found during the build:**
  - Legacy `.xls` import is rejected with a clear message (excelize reads xlsx + csv only;
    reference accepted xls via PhpSpreadsheet).
  - `DATA_PROCESSING_DISK`/`FILES_DISK` disk names are `private|public|s3` (the private local
    root is registered as `private`, not `local`).
  - Admin notification list filters: `search`, `priority`, whitelisted ordering — the reference's
    `user_id`/date-range/delivery filters are deferred (its delivery filter was dead anyway).
  - Admin notification responses include `targets` but not nested tenant/creator objects.
  - Data-processing responses omit `file_disk`/`file_path` (internal details) and the per-user job
    list is paginated (the reference returned unpaginated collections).
  - `user` export filter snapshot uses `order_direction` in the job row JSON.
  - Import source files are deleted after successful runs; the reader is closed before deletion
    (Windows blocks deleting open files).
- **Data model deviations:** `notification_targets.target_id` is bigint; `notification` children
  cascade on delete; `files` replaces the Spatie `media` + `uploads` pair — one polymorphic table
  serving the file library and all attachments, with a collection registry in Go, no
  conversions/custom-properties columns. `notification_reads`/`deliveries` keep branch/tenant
  columns for reporting but read state never branches.
- **Storage consolidation (research):** the Laravel two-table split is a package artifact.
  Owner references are polymorphic (`owner_type` + `owner_id`) because a FK column per owner
  type does not scale (one migration per future owner) — the same trade-off the reference's
  morphs made; integrity is enforced by services + integration tests (owner-delete hooks remove
  files). If files ever need to be shared/simultaneously attached, extract a `file_attachments`
  table later (mechanical migration).
- **API naming deviation:** `/v1/files` + `file.*` permissions instead of Laravel's
  `/v1/upload` + `upload.*` (resource-oriented naming; `upload` is the action). No Go client
  depends on the old paths yet; migration `00012` retires the old rows and `permission:sync`
  prunes catalog-absent system permissions.
- **User soft-delete caveat:** users are soft-deleted, so avatar `files` rows are retained when
  a user is deleted; a future hard-delete/cleanup path must remove attached files.
- **Open questions (resolved):** S3 client = aws-sdk-go-v2; OTP delivery = direct mail driver via
  the queue; import/export = background only; i18n = API-relevant subset; storage = one
  polymorphic `files` table (library + attachments) replacing Spatie `media` + `uploads`,
  exposed as `/v1/files`.
