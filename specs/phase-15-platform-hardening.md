# Phase 15 — Platform hardening: HTTP, cache, queue, scheduler, storage, telemetry (P1/P2)

**Status:** Draft
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

- [ ] **H18:** `/api/health` returns 503 when `results.HasFailures()` (liveness `/up` stays 200).
- [ ] **H25:** the scheduler takes the migration-style Postgres advisory lock in `Runner.Start`
      so a second `ims schedule` process fails loudly instead of double-executing tasks.
- [ ] **M49:** `/metrics` and `?fresh=1` on `/api/health` require authentication (or are
      loopback/internal-only); stop unauthenticated `river_job` GROUP BY amplification.

### Cache layer (H19, H20, H21, M12)

- [ ] **H20:** memory driver gets an LRU cap + entry ceiling; the default driver outside local
      development becomes `redis`/`database`.
- [ ] **H21:** file driver: `O_EXCL` lockfile for `SetNX`/`Increment`, prefix-aware `Clear`,
      and a config warning when `file` is combined with queue-worker/scheduler modes.
- [ ] **H19:** decide — wire `cachekeys` into the caching modules (authz, tenancy, settings,
      flags) or delete it and correct `docs/architecture.md` §110 / `AGENTS.md`. Either way,
      namespace-level invalidation stops being folklore.
- [ ] **M12:** settings/flag cache read errors degrade to the database (mirror `Remember`),
      not 500.

### HTTP & transport (H22, H23, M20, M23, M24, M26–M28, M78)

- [ ] **H22:** `ParseMultipartForm` uses a small `maxMemory` (≈8 MB) with `MaxBytesReader` at
      the total limit; files stream to disk.
- [ ] **H23:** `HTTP_WRITE_TIMEOUT` ≥ `HTTP_REQUEST_TIMEOUT` (or `WriteTimeout=0`) and/or
      `http.ResponseController.SetWriteDeadline` for long bodies; the 503 from `Timeout`
      becomes reachable.
- [ ] **M20:** 429 problem bodies carry `retry_after`.
- [ ] **M23:** `Recover` uses `http.ErrAbortHandler` when the response already started.
- [ ] **M24:** validate `X-Request-ID` (length + charset); reject or regenerate garbage.
- [ ] **M26:** cache one `Localizer` per locale instead of per request.
- [ ] **M27:** render the `Timeout` 503 and rate-limiter 429 inside the `Locale` middleware
      (localized errors).
- [ ] **M28:** pagination `links` stop echoing the client-supplied `Host` header.
- [ ] **M78:** fix the stray `n,` token in the default `Access-Control-Allow-Headers`.

### Files & storage (H16, H17, M52, L2)

- [ ] **H16:** add `private` to `FILES_DISK`; serve private downloads through an authorized
      handler (or S3 presigned URLs); decide the default disk for new installs.
- [ ] **H17:** drop `svg` from the upload allow-list (or serve with
      `Content-Disposition: attachment` + sandboxing CSP for user uploads).
- [ ] **M52:** SMTP sets I/O deadlines after dial; S3 uploads set `Content-Type`;
      `localDisk.Put` honours the `size` argument.
- [ ] **L2:** private-disk writes use `0600` files (and `0700` dirs).

### Queue (M47)

- [ ] **M47:** River `prepare` calls `def.Validate` (parity with the sync engine) so malformed
      payloads are rejected before persistence.

### Telemetry & dev dashboard (H14, H15, M50)

- [ ] **H14:** backup archive downloads require `dev.manage` (or a dedicated
      `backup.download` permission); queue admin redacts `mail.send` payloads (recipient/body).
- [ ] **H15:** telemetry redacts SQL args by statement/column (or skips auth-table args) so
      bcrypt hashes never land in `dev_queries.args`.
- [ ] **M50:** request/response body capture is gated on the sample rate, not done on every
      request.

### Small correctness (M29, L3)

- [ ] **M29:** settings int coercion rejects out-of-range floats and stops truncating `1.9`.
- [ ] **L3:** pin the Scalar CDN asset with `integrity` (SRI) or self-host it.

### Polish (M21, M24, H24)

- [ ] **M21:** HMAC-sign the notification feed cursor.
- [ ] **M24/H24:** rate limiter — exempt health/metrics from the pre-auth per-IP budget; add a
  user-keyed tier after auth; consider fail-closed for auth-adjacent routes only (the global
  fail-open default stays a documented trade-off, but its blast radius shrinks).

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
