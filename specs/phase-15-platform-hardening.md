# Phase 15 — Platform hardening: HTTP, cache, queue, scheduler, storage, telemetry (P1/P2)

**Status:** Done (2026-11-19; deferred items listed below)
**Source:** [docs/codebase-review.md](../docs/codebase-review.md) §10 P1 items 12–16 — findings **H14–H25, M12, M20–M21, M23–M24, M26–M27, M29, M47, M49–M50, M52, M78, L2–L3**
**Depends on:** Phase 12 (env normalisation H1/L1 lands there first)

## Goal

Harden the technical platform package by package: the health gate must fail closed, the cache
layer must be bounded and honest about its multi-process limits, uploads must not pin the heap
or serve stored XSS, timeouts must compose, the scheduler must be self-enforcing about its
single-instance topology, and telemetry/dev-dashboard must not become an exfiltration path.
Everything here is platform-level — no business behaviour changes.

## Reference (review)

- [docs/codebase-review.md](../docs/codebase-review.md) §6 (H14–H25), §7 (M12, M20–M21, M23–M24,
  M26–M27, M29, M47, M49–M50, M52, M78), §7c (L2, L3), §10 items 12–16.

## Scope checklist

### Health & scheduler (H18, H25, M49)

- [x] **H18:** `/api/health` returns 503 when `results.HasFailures()` (liveness `/up` stays 200).
- [x] **H25:** the scheduler takes the migration-style Postgres advisory lock in `Runner.Start`
      so a second `ims schedule` process fails loudly instead of double-executing tasks.
- [x] **M49:** `/metrics` and `?fresh=1` on `/api/health` require authentication (or are
      loopback/internal-only); stop unauthenticated `river_job` GROUP BY amplification.

### Cache layer (H19, H20, H21, M12)

- [x] **H20:** memory driver gets an LRU cap + entry ceiling; the default driver outside local
      development becomes `redis`/`database`.
- [ ] **H21:** file driver: `O_EXCL` lockfile for `SetNX`/`Increment`, prefix-aware `Clear`,
      and a config warning when `file` is combined with queue-worker/scheduler modes.
- [x] **H19:** decide — wire `cachekeys` into the caching modules (authz, tenancy, settings,
      flags) or delete it and correct `docs/architecture.md` §110 / `AGENTS.md`. Either way,
      namespace-level invalidation stops being folklore.
- [x] **M12:** settings/flag cache read errors degrade to the database (mirror `Remember`),
      not 500.

### HTTP & transport (H22, H23, M20, M23, M24, M26–M28, M78)

- [x] **H22:** `ParseMultipartForm` uses a small `maxMemory` (≈8 MB) with `MaxBytesReader` at
      the total limit; files stream to disk.
- [x] **H23:** `HTTP_WRITE_TIMEOUT` ≥ `HTTP_REQUEST_TIMEOUT` (or `WriteTimeout=0`) and/or
      `http.ResponseController.SetWriteDeadline` for long bodies; the 503 from `Timeout`
      becomes reachable.
- [x] **M20:** 429 problem bodies carry `retry_after`.
- [x] **M23:** `Recover` uses `http.ErrAbortHandler` when the response already started.
- [x] **M24:** validate `X-Request-ID` (length + charset); reject or regenerate garbage.
- [x] **M26:** cache one `Localizer` per locale instead of per request.
- [ ] **M27:** render the `Timeout` 503 and rate-limiter 429 inside the `Locale` middleware
      (localized errors).
- [x] **M28:** pagination `links` stop echoing the client-supplied `Host` header.
- [ ] **M78:** fix the stray `n,` token in the default `Access-Control-Allow-Headers`.

### Files & storage (H16, H17, M52, L2)

- [x] **H16:** add `private` to `FILES_DISK`; serve private downloads through an authorized
      handler (or S3 presigned URLs); decide the default disk for new installs.
- [x] **H17:** drop `svg` from the upload allow-list (or serve with
      `Content-Disposition: attachment` + sandboxing CSP for user uploads).
- [x] **M52:** SMTP sets I/O deadlines after dial; S3 uploads set `Content-Type`;
      `localDisk.Put` honours the `size` argument.
- [x] **L2:** private-disk writes use `0600` files (and `0700` dirs).

### Queue (M47)

- [x] **M47:** River `prepare` calls `def.Validate` (parity with the sync engine) so malformed
      payloads are rejected before persistence.

### Telemetry & dev dashboard (H14, H15, M50)

- [x] **H14:** backup archive downloads require `dev.manage` (or a dedicated
      `backup.download` permission); queue admin redacts `mail.send` payloads (recipient/body).
- [x] **H15:** telemetry redacts SQL args by statement/column (or skips auth-table args) so
      bcrypt hashes never land in `dev_queries.args`.
- [x] **M50:** request/response body capture is gated on the sample rate, not done on every
      request.

### Small correctness (M29, L3)

- [x] **M29:** settings int coercion rejects out-of-range floats and stops truncating `1.9`.
- [ ] **L3:** pin the Scalar CDN asset with `integrity` (SRI) or self-host it.

### Polish (M21, M24, H24)

- [ ] **M21:** HMAC-sign the notification feed cursor.
- [ ] **M24/H24:** rate limiter — exempt health/metrics from the pre-auth per-IP budget; add a
  user-keyed tier after auth; consider fail-closed for auth-adjacent routes only (the global
  fail-open default stays a documented trade-off, but its blast radius shrinks).

## Implementation notes (2026-11-19)

- **H18:** `/api/health` answers 503 when a non-advisory check fails. `Registry.AddAdvisory` marks
  checks that are reported but never fail readiness (scheduler heartbeat, backups) — an API
  instance can serve traffic while they are unhealthy.
- **H25:** `ims schedule` takes `TxManager.TryHoldLock(SchedulerLockID)` (session advisory lock on a
  dedicated connection) and exits with a clear error when another scheduler holds it.
- **M49:** `/metrics` is wrapped in `httpx.ProtectOps`: `METRICS_TOKEN` bearer when set, otherwise
  loopback only (404 for everyone else). `?fresh=1` is honoured for loopback callers only.
  `/up`, `/api/health`, `/metrics` skip the per-IP rate limit.
- **H20:** memory driver bounded (`CACHE_MEMORY_MAX_ENTRIES`, default 100 000; sweep expired, then
  evict a tenth). Default `CACHE_DRIVER` is `memory` only in local envs, `database` elsewhere; a
  startup warning fires for memory/file outside local.
- **H21 (decision):** the file driver stays single-process by design; no lockfiles. The startup
  warning plus the new default steer multi-process deployments to `database`/`redis`.
- **H19 (decision):** `internal/platform/cachekeys` deleted (it had zero callers); docs now describe
  the real convention — module-owned keys, delete on write.
- **M12:** settings and feature-flag cache read errors fall through to the database.
- **H22:** multipart parsing keeps ≤ 8 MB in memory; the rest spills to temp files.
- **H23:** when `HTTP_WRITE_TIMEOUT` ≤ `HTTP_REQUEST_TIMEOUT` it is raised to request timeout + 30s
  (no startup failure for existing `.env` files); examples now ship 90s.
- **M20:** 429 problems carry `retry_after` (and the header) via `WithRetryAfter`.
- **M23:** a panic after the response started aborts the connection (`http.ErrAbortHandler`).
- **M24:** `X-Request-ID` must be 1–128 chars of `[A-Za-z0-9._:-]`, otherwise a new id is generated.
- **M26:** one go-i18n localizer per locale (sync.Map).
- **M28:** outside local envs pagination/cursor links use the `APP_URL` origin (`httpx.PublicOrigin`).
- **H16:** `FILES_DISK=private` is supported. Private files have no public URL; their `url` points to
  the new authorized `GET /api/v1/files/{id}/download` (same rule as show; `attachment`,
  sandbox CSP, `no-store`). Avatars always live on a public disk (they render in `<img>`).
  Default stays `public` for compatibility; choose `private` for tenant documents.
- **H17:** SVG removed from avatar uploads (extension and sniffed MIME); `/storage/*` responses carry
  a sandboxed `Content-Security-Policy`.
- **M52/L2:** SMTP connections get an I/O deadline (context deadline or 2 min); S3 objects get a
  `Content-Type` from the extension; private-disk files are written `0600` in `0700` dirs.
- **M47:** the River engine runs `def.Validate` before inserting.
- **H14:** `/dev/api/backups/{id}/download` and `/dev/api/files/{id}/download` need `dev.manage`;
  `mail.*` job payloads are shown as `"[redacted]"` in the queue admin.
- **H15:** telemetry SQL args: every string argument is redacted when the statement mentions a
  sensitive key (password, token, secret, ...), and bcrypt/argon hashes are redacted everywhere.
- **M50:** request/response bodies are only buffered for sampled requests.
- **M29:** integer settings reject fractional and out-of-range JSON numbers.

### Deferred (with reason)

- **M21:** cursor HMAC — the feed query is always scoped to the caller, so a tampered cursor only
  moves within the caller's own feed. Not worth a signing key to manage.
- **M27:** localizing the Timeout 503/429 needs the Locale middleware to wrap them; low value.
- **L3:** Scalar SRI — needs the exact CDN asset hash; do it when pinning the docs UI version.
- **M78:** the stray `n,` token is no longer present in the default `CORS_ALLOWED_HEADERS`.
- **H24 user-keyed limiter tier:** left as is; health probes are now exempt, which was the main
  operational pain. A per-user tier can be added with `ratelimit.Middleware(limiter, keyByUser, ...)`.

## Out of scope

- Per-request DB cost work (super-admin probe, batched role refs, plan caching — Phase 16).
- Retention/purge tasks and index work (Phase 16).
- Frontend CSP/token storage (Phase 17 — paired with H17's server side only where noted).
- CI scanners and deploy scripts (Phase 17).

## Contracts touched

- **DB tables / migrations:** none (index work belongs to Phase 16).
- **API endpoints:** `/api/health` status semantics (200/503); file download path for private
  disks (new authorized download route → OpenAPI region update); `/metrics` auth.
- **Env vars / config:** `FILES_DISK` gains `private`; `HTTP_WRITE_TIMEOUT` semantics;
  cache driver default change outside local; possible `CACHE_MEMORY_*` cap settings.
- **CLI commands:** `ims schedule` refuses a second instance while the advisory lock is held.

## Acceptance criteria

- `/api/health?fresh=1` returns 503 when the DB check fails (test with a broken DSN).
- Starting a second scheduler process exits with a clear "already running" error.
- Concurrent upload test: 10 parallel 100 MB uploads do not pin ~1 GB RSS (streamed).
- A long streaming response (sync export) completes instead of a mid-body connection break
  (H23 regression test).
- An SVG upload is rejected (or downloads force `attachment`); direct navigation cannot
  execute script (H17 test).
- Private-disk files are not servable from `/storage/*`; the authorized download route enforces
  `file.view` + tenant scope.
- `dev.view` (without `dev.manage`) cannot download a backup archive; `mail.send` job args in
  the queue admin are redacted; `dev_queries.args` contains no bcrypt hashes.
- Cache contract suite passes for the modified drivers (LRU cap, TTL semantics preserved).
- The notification cursor rejects tampered/timed-out signatures (M21).
- `make check` and the integration suite pass.

## Open questions

- H19 decision: wire `cachekeys` everywhere or delete it (docs must match whichever is chosen).
- H16: default disk for new installs (`private`?) and migration story for existing public
  uploads; presigned URLs vs proxy-download for S3.
- M49: authenticate `/metrics` with a token, or bind it to loopback + config gate only?
- M21: cursor TTL / key rotation policy for the HMAC.
