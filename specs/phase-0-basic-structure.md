# Phase 0 — Basic structure (Milestone 1)

**Status:** Done (2026-10-16)
**Roadmap:** PLAN.md §11 "Milestone 1 — Basic structure (Phase 0)"
**Depends on:** nothing (first phase)

## Goal

Stand up the skeleton of the Go API starter: config/env loading, structured logging, the HTTP
kernel (chi + middleware + RFC 9457 problems + validation + response envelopes), health
endpoints, graceful lifecycle, a Cobra CLI with lazy dependencies, repo tooling (Makefile, lint,
CI), and one reference module proving the full `route → handler → request DTO → service →
response DTO → problem` flow.

No database, cache, queue or auth in this phase — those arrive in Phases 1–3.

## Scope checklist

- [x] Scaffolding: `go.mod` (`github.com/Innovix-Matrix-Systems/ims-go-api-starter`), `.gitignore`,
      `.env.example`, `Makefile`, `.golangci.yml`, `sqlc.yaml`, `tools/tools.go`, CI skeleton, `README.md`
- [x] `internal/config` — typed config, env-file resolution (`--env-file` → `APP_ENV_FILE` → `.env`),
      validation for the app section
- [x] `internal/platform/logging` — slog (JSON prod / text local), request IDs
- [x] `internal/platform/httpx` — `BindJSON` (size limit, content type, unknown fields, trailing JSON),
      validator wiring + human-readable message mapping, `Problem` + `apperr` bridge, response
      helpers (`Data`, `Message`, `NoContent`, `Paginated`), middleware (RequestID, Recover,
      AccessLog, Timeout, RateLimit stub, Locale stub)
- [x] `internal/platform/health` — registry, checks (env, debug, app), `GET /up`,
      `GET /api/health` (+ `?fresh=1`)
- [x] `internal/app` — wiring, routes (chi assembly + middleware order), lifecycle
      (graceful shutdown, server timeouts)
- [x] `internal/cli` — cobra root (`--env-file`, `--verbose`), registry, builtins (`version`,
      `config:show`, `health:check`), `serve` command; `schedule` / `queue:work` stubs that fail
      with clear "not wired yet" messages
- [x] `internal/modules/system` — reference module: `GET /api/v1/system/info` demonstrating the
      full pattern
- [x] `specs/` folder + README workflow (created alongside PLAN.md)

## Out of scope

- Database, cache, queue, scheduler wiring (Phases 1–3) — CLI stubs must fail clearly.
- Auth, tenancy, RBAC (Phase 2).
- OpenAPI spec + oapi-codegen (Phase 6) — handlers are handwritten now.
- Telemetry capture (Phase 9) — only hook points are reserved; nothing implemented.

## Contracts touched

- **API:** `GET /up`, `GET /api/health`, `GET /api/v1/system/info`,
  `POST /api/v1/system/echo` (reference flow incl. validation + strict JSON fields)
- **Env vars:** `APP_*`, `HTTP_*`, `RATE_LIMIT_PER_MINUTE` (see `.env.example`)
- **CLI:** `serve`, `version`, `config:show`, `health:check` (+ stubs)
- **Errors:** RFC 9457 for all errors; `errors` extension for 422; `debug` extension only outside
  production

## Acceptance criteria

- `go build ./...`, `go vet ./...`, `golangci-lint run`, `go test ./...` all green.
- `go run ./cmd/app serve` boots on Windows; Ctrl-C shuts down gracefully within
  `HTTP_SHUTDOWN_TIMEOUT`.
- Unknown route → problem+json 404; panic → problem+json 500 (stack only in logs); malformed
  JSON → 400; oversized body → 413; failed validation → 422 with dot-path `errors` map.
- `/api/health` returns `{finishedAt, checkResults:[{name,label,status,shortSummary,meta}]}`
  with `Cache-Control: no-store`.
- `version` and `help` run with no env file and no infrastructure.
- CI workflow runs fmt/vet/lint/test (+ race) and is green.
- `config:show` prints the effective typed config with secrets masked.

## Open questions (resolved 2026-10-16)

1. **Rate limit stub:** confirmed — in-memory fixed-window behind the `ratelimit.Limiter`
   interface; cache-backed limiter replaces it in Phase 3.
2. **Locale stub:** confirmed — Phase 0 parses/normalizes `Accept-Language` into context only;
   go-i18n translator arrives in Phase 5.
3. **HTTP timeouts:** confirmed — 15s read / 30s write / 60s idle / 60s request / 30s shutdown.

## Implementation notes

- **RealIP removed (deviation):** chi's `middleware.RealIP` is deprecated upstream for
  IP-spoofing advisories (GHSA-3fxj-6jh8-hvhx and related). Phase 0 keys rate limits and logs on
  the socket address. Trusted-proxy handling (`X-Forwarded-For` behind a reverse proxy) becomes
  explicit configuration in Phase 7 — recorded as an open question there.
- **`POST /api/v1/system/echo` added** beyond the original spec sketch: it is the reference flow
  for `BindJSON` → `Validator` → service → `Data` envelope and makes the 422/415/413 paths
  manually verifiable.
- Verified: build/vet/lint/test green; live server returns 200 envelopes, 404/405 problems,
  422 validation with `errors` map, 415 on wrong content type, `X-Request-ID` on every response,
  `Cache-Control: no-store` on `/api/health`, CLI `version`/`config:show`/`health:check` work,
  `queue:work` stub exits 1 with a Phase 3 message.
