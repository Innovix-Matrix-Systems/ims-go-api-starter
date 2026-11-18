# Phase 17 — Product & ops maturity: frontend, CI, deploy, devx (P2/P3)

**Status:** Done (2026-11-19; deferred items listed below)
**Source:** [docs/codebase-review.md](../docs/codebase-review.md) §10 P2 items 22–29, P3 items 32–35 — findings **H29–H31, M44–M46, M53–M74, M62–M63, M55–M58, M59–M61, §8 documentation drift**
**Depends on:** Phases 12–15 (their contract changes — health semantics, file downloads,
problem bodies — should be stable before the frontend/CI work pins them)

## Goal

Finish the production story around the backend: a tested developer dashboard that speaks the
real error contract, CI that scans dependencies and secrets, a deploy path that writes to
writable paths and rolls itself back on a failed check, generators that are safe to point at a
repo, and documentation that matches the code again. Each area is small on its own; together
they close the "ops maturity" gap the review scored.

## Reference (review)

- [docs/codebase-review.md](../docs/codebase-review.md) §6 (H29–H31), §7 (M44–M46, M53–M74),
  §8 (documentation drift table), §10 items 22–29, 32–35.

## Scope checklist

### Frontend — correctness (M65–M69, M71–M72, M74)

- [x] **M65:** sign-out calls `POST /api/v1/logout` (server-side token invalidated).
- [x] **M66:** parse full RFC 9457 problem bodies — surface `errors`, `meta`, `retry_after`,
  `error_code`; stop flattening to one sentence.
- [x] **M67:** `retry: 1` never retries 4xx (401/403/404/429).
- [ ] **M68:** add a CSP to the SPA shell; move the bearer token out of `localStorage`
  (in-memory + refresh cookie, or document the accepted trade-off).
- [ ] **M69:** `DataTable` stops client-side sorting of server-paginated lists (server sort
  params or remove the affordance).
- [ ] **M71:** live polling re-fetches only the visible page, not every accumulated page.
- [ ] **M72:** sandbox or remove the `<iframe src={blobUrl}>` fallback (latent).
- [x] **M74:** untrack `web/dev/.tanstack/tmp/*`; unify the dev-port defaults (5173 vs
  8080/8081).

### Frontend — accessibility & structure (M70, M73, M64)

- [ ] **M70:** modals/drawers/confirms get `role="dialog"` + `aria-modal`, focus trap, Escape
  handling, focus restoration.
- [ ] **M73:** split the megascreens (`settings.tsx` 1,036 lines, `files.tsx` 833, `flags.tsx`
  610, `backups.tsx` 602, `sessions.tsx` 507) into per-screen hooks/components.
- [ ] **M64:** tests — vitest for the API client/token lifecycle/problem parser; Playwright
  for login + one panel; add `make web-check` and wire it into CI.

### CI (H30)

- [x] Add `govulncheck`, `dependency-review-action`, CodeQL (or equivalent SAST), `gitleaks`.
- [ ] Add a `concurrency` group; move to golangci-lint v2; add a coverage floor (start at the
  current measured value and ratchet).
- [ ] Broaden `.golangci.yml` (P3 item 32): `gosec`, `bodyclose`, `errorlint`, `unparam`,
  `gocritic` — fix or `nolint`-with-reason what they surface.

### Deploy & release (H29, H31, M62)

- [x] **H29:** systemd units get `StateDirectory=ims-go-api`; `APP_DATA_DIR`/`APP_STORAGE_DIR`/
  `APP_CACHE_DIR`/`BACKUP_DIR` point at it; `install.sh` writes the correct paths
  (parity with `docs/runbooks/deploy.md` §13).
- [x] **H31:** `release.sh` health-checks `/api/health?fresh=1` by default and auto-reverts
  the `current` symlink when the post-switch check fails.
- [x] **M62:** release tree (`$BASE_DIR`, `releases/`, `current`) owned by root/admin, not the
  service user; build as an unprivileged user.

### Scaffold & generators (M55–M58)

- [x] **M55:** validate `--name`, `--table`, `--package` (identifiers only — no paths, no SQL
  injection into the migration template).
- [ ] **M56:** generated `PATCH` uses `sqlc.narg` + `COALESCE` (partial-update semantics —
  omitting a field no longer nulls it).
- [ ] **M57:** `ims gen openapi --all` fails loudly (or skips the whole region write) when a
  module cannot be parsed — never silently drop endpoints.
- [x] **M58:** the generated service stops importing `authn` (accept identity via parameters,
  matching the developer guide's layering rules).

### Contract & spec hygiene (M59–M61)

- [ ] **M59:** include `/dev/api/*` in route-drift checking.
- [ ] **M60:** decide — give `internal/contract` a consumer (handler request/response types)
  or remove it from the build; document the choice.
- [x] **M61:** remove the phantom `X-Tenant-Code` parameter from `api/openapi.yaml`.

### Documentation drift (§8, M63)

- [ ] Fix all 13 verified drift items in §8 — including the `cachekeys` claim (aligned with the
  Phase 15 decision), the middleware chain list, the scheduled-task count (docs list 4, seven
  exist), the `openapi:check` "needs a DB" claim (it runs DB-less in CI), `make dev` port,
  `PLAN.md` (decide: create or remove the references), the copilot-instructions `api/` package
  claim, the cancel-until-`expires_at` claim (aligned with the Phase 14 decision), and the
  Makefile `clean` Windows bug.
- [x] **M63:** add the six missing keys to `.env.example` (`AUTH_CHECK_IS_ACTIVE`,
  `AUTH_CHECK_EMAIL_VERIFIED`, `AUTH_CHECK_PHONE_VERIFIED`, `DEFAULT_TENANT_EMAIL`,
  `DEFAULT_TENANT_PHONE`, `BKASH_CALLBACK_URL`).

### CLI architecture (M44–M46)

- [ ] **M44:** close the telemetry collector on every `NewPlatform` error path (one `defer`/
  cleanup owner, not 17 duplicated blocks).
- [ ] **M45:** a single `withPlatform` helper replaces the 16 near-identical setup blocks;
  `queue:work` reuses the platform's River engine; `cache:clear` reuses the platform cache.
- [ ] **M46:** `health:check`, `schedule:list`, `openapi:check` either run `cfg.Validate()` or
  document why they skip it.

### Dev module architecture (M53–M54)

- [ ] **M53:** introduce a service layer in `internal/modules/dev` (handlers stop talking to
  repositories; business logic leaves handlers); replace the per-handler `requireManage`
  helper with route-level middleware so a forgotten check fails closed.
- [ ] **M54:** the dev audit insert uses a detached context with retries/backoff — never the
  (possibly cancelled) request context, and insert failures are logged, not swallowed.

## Implementation notes (2026-11-19)

- **M65/M66/M67 (SPA):** sign-out posts `/api/v1/logout` (device `dev-dashboard`) before clearing the
  session; `ApiError` keeps the whole RFC 9457 problem (`errors`, `meta`, `retry_after`,
  `trace_id`); React Query never retries 4xx.
- **M74:** `web/dev/.tanstack/` untracked and ignored.
- **H30 (CI):** new `security` job — `govulncheck`, `gitleaks` (history; `.gitleaks.toml` allowlists
  the redaction-test fixture), `dependency-review` on PRs; workflow-level `concurrency` and
  read-only `permissions`. Go jobs build with the latest 1.26 patch (`check-latest`), because
  1.26.1 has standard-library CVEs (fixed in ≤ 1.26.11). excelize upgraded to v2.11.1 (10 CVEs).
- **H29/M62:** `install.sh` seeds `/etc/ims/ims-go-api/.env` from `deploy/.env.production` (state
  under `/var/lib/ims/ims-go-api`, matching `ReadWritePaths`); the release tree is root-owned.
- **H31:** `release.sh` health-checks `/api/health?fresh=1` and restores + restarts the previous
  release when the new one is unhealthy.
- **M55:** `--name/--package/--table` must match `^[a-z][a-z0-9_]{0,62}$`.
- **M58:** generated services no longer import `authn` or check permissions; generated routes use
  `r.With(h.can(Perm...))` (Laravel `can:` middleware) and pass the actor id into the service.
  Verified by generating a throwaway module and building/vetting/testing it.
- **M61:** phantom `X-Tenant-Code` parameter and description removed from `api/openapi.yaml`.
- **M63:** missing auth/tenant/bKash keys added to `.env.example`.

### Deferred (with reason)

- **M68–M73, M64 (SPA):** CSP on the SPA shell, token storage, DataTable sorting, polling, modal a11y,
  splitting the large screens and frontend tests are a dedicated dashboard pass (Phase 9
  hardening).
- **golangci-lint v2 + broader linters + coverage floor:** a config migration that can surface
  many findings; do it as its own change.
- **M56/M57/M59/M60:** generator PATCH semantics, `gen openapi --all` strictness, `/dev/api` drift
  checking and the `internal/contract` decision.
- **M44–M46, M53–M54:** CLI `withPlatform` refactor and a service layer for the dev module are
  structural refactors with no behaviour change — part of the simplicity pass.

## Out of scope

- New dashboard screens or admin features (Phase 8 / Phase 9 remaining hardening owns those —
  coordinate M53/M54 with phase-9's outstanding items).
- Backend performance work (Phase 16).
- Any change to the public API error contract (Phases 12–15 own those; this phase only makes
  the frontend consume them).

## Contracts touched

- **DB tables / migrations:** none expected.
- **API endpoints:** none new; `/dev/api/*` enters drift checking (M59); `X-Tenant-Code`
  removed from the spec (M61).
- **Env vars / config:** `.env.example` completions (M63); deploy env path changes (H29).
- **CLI commands:** `web-check` make target; generator flags validation.

## Acceptance criteria

- `make web-check` runs vitest + Playwright (headless) locally and in CI; the API client and
  problem parser are covered.
- Signing out invalidates the server-side token (e2e: old token → 401).
- CI runs vulnerability, secret and SAST scans; a deliberately added vulnerable dependency or
  committed secret fails the build.
- A fresh deploy following `docs/runbooks/deploy.md` writes all state under the systemd
  state directory; `release.sh` rolls back automatically when `/api/health?fresh=1` fails
  after a switch.
- `ims gen module --name=../../evil` and `--table` with SQL fail with a validation error.
- Generated PATCH preserves omitted optional fields (integration test on a scaffolded module).
- `ims openapi:check` fails when a `/dev/api/*` route is missing from the spec.
- Every §8 drift row is fixed (grep-able evidence per row); `.env.example` documents every key
  the config reads.
- `go vet`/lint clean with the new scanners enabled; `make check` passes.

## Open questions

- M68 token storage: in-memory + silent-refresh pattern vs accepted localStorage risk with a
  strict CSP — decide and document.
- M60: adopt `internal/contract` types in handlers (big change, real payoff) vs delete them
  (cheap, loses compile-time linkage) — needs a decision before the scaffold work (M56/M58)
  so the generator emits the right style.
- Coverage floor value: start at 22 % (current measured) and ratchet per-phase?
- Coordinate M53/M54 with phase-9's remaining hardening to avoid double-touching the dev
  module in the same window.
