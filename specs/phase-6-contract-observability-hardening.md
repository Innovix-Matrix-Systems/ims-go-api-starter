# Phase 6 — Contract, observability, hardening

**Status:** Done (2026-10-17)
**Roadmap:** PLAN.md §11 Phase 6
**Depends on:** Phases 0–5
**Started:** 2026-10-17

## Goal

Freeze the HTTP contract of Phases 2–5, prove it in CI, and harden the edges. Concretely: a
complete canonical `api/openapi.yaml` covering every route with the starter's envelopes and
RFC 9457 problem shapes; oapi-codegen types generated from it and gated by a `git diff --exit-code`
check; an embedded Scalar docs UI; contract tests that validate real responses against the spec;
optional, config-gated metrics/tracing hook points; a completed tenancy-isolation test matrix that
makes explicit `tenant_id` scoping the *only* isolation boundary (Postgres RLS was evaluated and
dropped); and a security pass (headers, CORS policy, secret/redaction audit, load smoke). No new
business behaviour is added — this phase locks what already exists.

## Reference (Laravel)

- `.scribe/`, `config/scribe.php`, controller/request annotations — endpoint + field coverage
  checklist for the OpenAPI spec
- `routes/web.php` docs routes + `config/observability.php` — Scalar/Swagger serving parity
- `app/Exceptions/ApiException.php` — problem-details shapes and status mapping to match
- `app/Http/Middleware/*` — the security/CORS posture to mirror

## Decisions (resolved at spec review)

1. **oapi-codegen mode: types-only.** Strict-server generation fights the handwritten chi
   middleware chain, the custom `{data}` envelope, and the `Responder`-owned error rendering.
   We generate DTOs + parameter types + embedded path info, but keep our handlers. The spec is
   embedded (`internal/contract/openapi.gen.go`) so contract tests/CLI can reference route metadata.
   A future change to strict-server is possible without moving the spec (it stays canonical).
2. **Embedded, generated spec surface:** `api/openapi.yaml` is the source of truth and is
   `go:embed`-ed. The served bytes at `/docs/openapi.yaml` are the raw file; the checked-in
   `openapi.gen.go` is derived from it and diff-gated.
3. **Postgres RLS: evaluated and dropped.** Investigated during this phase and rejected for v1:
   a correct setup needs a dedicated non-owner application role, a fail-closed policy, and the
   tenant GUC set on every connection (not just transactions) — and it would only backstop the
   explicit filters already required. Instead, **explicit `tenant_id` scoping in SQL is the only
   isolation boundary**, enforced by per-module isolation tests (see `docs/architecture.md` §11.18).
   `DATABASE_RLS_ENABLED` and the RLS migration were removed.
4. **Metrics: config-gated, disabled by default** (`METRICS_ENABLED=false`). Exposed at `/metrics`;
   OTel tracing stays a hook point (no exporter wired). Keep cardinality low — no tenant/user
   labels: `http_server_requests_total{method,route,status}`, `http_server_request_duration_seconds`
   (histogram buckets 0.005…10), `ims_queue_jobs_total`, `ims_scheduler_runs_total`.
5. **Docs UI: Scalar via CDN** with an offline note; when `DOCS_ENABLED=false` (default in
   production) both `/docs` and `/docs/openapi.yaml` 404. Swagger UI remains a documented fallback
   (same embedded spec), not a separate bundled asset.
6. **Problem `type` URLs** stay `{APP_URL}/errors/{slug}`; when `APP_URL` is empty the builder emits
   `about:blank` (RFC 9457 default). The spec documents both as `format: uri`.

## Scope checklist

- [x] `api/openapi.yaml` complete: envelopes, pagination shapes, problem schemas (incl. `errors`,
      `meta`, `retry_after`), bearer security scheme, every route from Phases 2–5
- [x] Route inventory matches `internal/app/routes.go` (a test asserts no drift between the spec's
      paths and the live chi routes)
- [x] oapi-codegen integration: `tools/` pin, `make openapi` target, generated
      `internal/contract/*.gen.go`, `git diff --exit-code` CI gate
- [x] OpenAPI lint (`kin-openapi` load + validation) run in CI and unit tests
- [x] Scalar docs at `/docs` + raw spec at `/docs/openapi.yaml` (embedded, config-gated)
- [x] Contract tests: success + 401/403/404/422 + 402/409/429 shapes, asserting
      `application/problem+json` content type on every error
- [x] Optional `/metrics` (Prometheus) + OTel hook points (config-gated, disabled by default)
- [x] Tenancy isolation matrix completed across all modules (cross-tenant read/write denied)
- [x] Postgres RLS evaluated and dropped; explicit `tenant_id` scoping documented as the only
      isolation boundary (`AGENTS.md`, `docs/architecture.md`, `PLAN.md`)
- [x] Security checklist: security headers middleware, CORS policy (config-gated), redaction audit
      (tokens/OTP/passwords never logged), secrets review
- [x] `ims openapi:check` (offline spec validation + route drift)
- [x] Load smoke test (bounded concurrency against `/api/health` + an authenticated read) in CI
- [x] `docs/architecture.md` + specs README status updated

## Out of scope

- Dashboards (Phases 8–9); read/write splitting enablement; wiring a live OTel exporter
  (hook points only); image conversions; any new business endpoints.

## Contracts touched

- **DB tables / migrations:** none (no schema change; RLS migration was evaluated and dropped).
- **API endpoints (see api/openapi.yaml):** no new business endpoints. New operational surfaces:
  `GET /docs`, `GET /docs/openapi.yaml` (config-gated), `GET /metrics` (config-gated).
- **Env vars / config:** `DOCS_ENABLED` (default false in production, true otherwise),
  `METRICS_ENABLED` (default false), `CORS_ALLOWED_ORIGINS` (default empty = CORS off),
  `CORS_ALLOWED_METHODS`/`CORS_ALLOWED_HEADERS` (sane defaults), `SECURITY_HEADERS_ENABLED`.
- **CLI commands:** `ims openapi:check` (validates the embedded spec + asserts no route drift;
  builds the full route table, so it needs database connectivity like `health:check`).

## Acceptance criteria

- Every live API response (success and error) validates against `api/openapi.yaml` via
  `kin-openapi`, including `application/problem+json` content type and status match.
- `go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@<pin> ...` regenerates
  `internal/contract` with no diff (`git diff --exit-code`).
- A unit test fails if a chi route in `internal/app/routes.go` is missing from the spec (and
  vice-versa), so the contract cannot silently drift.
- 5xx responses leak no internals: a spot test forces a panic and asserts `detail` is generic and
  `trace_id` present, with no stack outside debug.
- Tenancy matrix: for each tenant-scoped module, a cross-tenant read and write is denied (404/403).
- Security audit produces a checklist with no unexplained findings; tokens/OTP/passwords absent
  from logs in the e2e flow.
- `gofmt -l .` empty, `golangci-lint run ./...` exit 0, unit + integration (with DSN) + `-race`
  suites green; load smoke stays within bounds.

## Open questions

- ~~oapi-codegen mode~~ → types-only (Decision 1).
- ~~Metric naming/buckets~~ → low-cardinality names as above (Decision 4).
- Whether `DOCS_ENABLED` should default true outside production (proposed: yes) — revisit if the
  deployment phase (7) prefers a single explicit switch.

## Implementation notes (rolling)

Shipped (verified: `gofmt` clean, `go vet`, `golangci-lint` exit 0, unit suite + `-race`, and the
full integration suite against a live Postgres with the new migration):

- `api/openapi.yaml` — the full contract (~65 operations), including the two tenant-exempt
  internal user import/export routes discovered by the drift test.
- `api/embed.go` (`api.Spec`), `internal/platform/docs` (Scalar UI + raw spec), `/docs` +
  `/docs/openapi.yaml` registered only when `DOCS_ENABLED`.
- `internal/contract/openapi.gen.go` generated types-only with oapi-codegen **v2.4.1**; `make
  openapi` / `make openapi-check`; CI diff gate added to `.github/workflows/ci.yml`.
- `internal/app/contract_test.go`: spec loads + validates (kin-openapi), spec↔chi route drift
  (both directions), response schema + content-type validation, and a 5xx panic-masking spot test.
- `internal/app/ops_test.go` + `load_test.go`: docs on/off, metrics on/off, security headers,
  CORS (off by default; allow/deny preflight), bounded-concurrency load smoke.
- `internal/platform/metrics`: dependency-free Prometheus text exporter (low cardinality),
  config-gated by `METRICS_ENABLED`.
- `internal/platform/httpx`: `SecurityHeaders` (HSTS only over TLS; no CSP so docs load) and
  `CORS` middleware.
- Tenant isolation: RLS evaluated and dropped (see Decision 3). `AGENTS.md` now carries the rule
  that every tenant-scoped query must filter an explicit `tenant_id`, with a per-module isolation
  test to enforce it. The matrix is enforced by:
  - `internal/app/tenancy_isolation_integration_test.go` — end-to-end (branches, users, roles,
    billing payments, data-processing) against two tenants; reads/updates/deletes of the other
    tenant's resources return 404 and lists are scoped.
  - `internal/modules/branches/isolation_integration_test.go` — repository-level cross-tenant
    read/update/delete.
  - `internal/modules/notifications/isolation_integration_test.go` — a tenant-1 notification is
    invisible/absent from a tenant-2 user's visibility check and feed.
  - `tenants` (global, tenant-exempt CRUD) and `files` (owner-scoped, no `tenant_id`) are not
    tenant-scoped by design; their access rules are covered by their existing integration tests.
- Security checklist (audit complete, no unexplained findings):
  - [x] Passwords: bcrypt at rest; never logged (redaction test).
  - [x] Tokens: opaque `id|secret`, SHA-256 at rest, per-device; never logged (redaction test).
  - [x] OTP: cache-only, constant-time compare; the code/mail body is logged only in debug outside
    production (`authn/otp.go`, `mail/log.go`).
  - [x] Errors: every error is RFC 9457 problem+json; 5xx `detail` is generic in production with a
    `trace_id` (panic-masking test).
  - [x] Response headers: `nosniff`, `X-Frame-Options: DENY`, `Referrer-Policy: no-referrer`,
    HSTS over TLS; CSP intentionally omitted so the docs UI loads.
  - [x] CORS: disabled unless `CORS_ALLOWED_ORIGINS` is set; credentials only for explicit origins.
  - [x] Request limits: body size cap, strict JSON fields, content-type checks, trailing-garbage
    rejection.
  - [x] Rate limiting: global throttle + login lockout + OTP attempts (fail-open with logging).
  - [x] Tenancy isolation: explicit `tenant_id` scoping + per-module isolation tests.
  - [x] Secrets: supplied via `.env`/environment only (`.env` gitignored, not committed);
    `config:show` masks DSN passwords; no secrets in code or example files.
- Config: `Ops{DocsEnabled,MetricsEnabled,CORS}`, `Security{HeadersEnabled}`; `.env.example` and
  `docs/architecture.md` updated.
- Bug fixed (found by the isolation matrix): `ListDataProcessingJobs` / `CountDataProcessingJobs`
  referenced a non-existent column `original_file_name` (schema has `file_name`), which made the
  jobs list fail at runtime; corrected in `sql/queries/dataprocessing.sql` + regenerated
  `internal/dbgen`.
