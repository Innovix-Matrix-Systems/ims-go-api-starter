# Phase 7 — Deployment & generators

**Status:** Done (2026-10-17)
**Roadmap:** PLAN.md §11 Phase 7
**Depends on:** Phases 0–6
**Started:** 2026-10-17

## Goal

Ship it: systemd units + a build-on-server release script + runbooks; the `gen module` scaffolding
command (`make:crud` parity); explicit trusted-proxy handling; and a finalized
`docs/architecture.md`.

> **SQLite is out of scope for v1.** It was originally bundled into this phase and has been
> deliberately deferred to a future version (see Decisions). v1 is PostgreSQL-only; the SQLite
> adapter, its second query/repository set and its backup runbook are removed from this spec and
> tracked in `PLAN.md` as a later deliverable.

## Reference (Laravel)

- `Dockerfile*` + `deploy.sh` — deploy steps to translate into systemd + release script
- `app/Console/Commands/Crud/*` + `stubs/*` — `make:crud` behavior, field types/options, and the
  declarative `--fields` spec format to port (`FieldType.php`, `FieldDefinition.php`,
  `CrudBlueprint.php`)

## Decisions (resolved at spec review)

1. **Release artifacts: build on the server.** `deploy/scripts/release.sh` runs on the target VPS:
   it fetches the source (git tag/ref or a provided `--source` dir), builds a static binary with
   the on-host Go toolchain, runs the gated migration step, switches the `current` symlink,
   restarts units, health-checks, and prunes old releases. No external artifact host/registry. CI
   still compiles `CGO_ENABLED=0` linux/amd64 + linux/arm64 (verification only) so the static build
   is guaranteed to work on the target.
2. **Trusted proxies: explicit `HTTP_TRUSTED_PROXIES`.** Comma-separated IPs/CIDRs. Forwarded
   headers (`X-Forwarded-For` / `X-Real-IP`) are honoured **only** when the immediate peer is a
   trusted proxy; the client IP is resolved right-to-left skipping trusted hops. The resolved IP
   feeds the login throttle, the rate limiter and access logs.
3. **Migration gating.** Migrations are an explicit release step (`ims migrate`), never on boot or
   replica start. Multi-instance convention: expand/contract; `migrate:status` reports pending
   versions; the health check flags drift.
4. **Generator scope.** `ims gen module` is non-interactive and CI-friendly:
   `ims gen module --name=product --fields="name:string,price:decimal,is_active:bool"`. It writes a
   goose migration, a sqlc query file, and a full module package
   (`model/repository/service/request/handler/routes`) plus a service unit test, then runs
   `sqlc generate` (unless `--no-sqlc`) so the output compiles, and prints the manual wiring steps.
   Field types mirror `FieldType.php`: `string`, `text`, `int`, `bigint`, `bool`, `decimal`,
   `date`, `datetime`, `json`.
5. **SQLite: deferred to a future version.** v1 is PostgreSQL-only. With sqlc, SQLite needs a
   second, permanently-duplicated query/repository set per module; an ORM-style alternative would
   mean rewriting the whole data layer and losing compile-time SQL + explicit tenant scoping. The
   SQLite schema, queries, config and runbook added during the investigation were removed. See
   `docs/architecture.md` §11.22.

## Scope checklist

- [x] `deploy/systemd/` units: api, scheduler, worker (`EnvironmentFile=/etc/ims/.../.env`)
- [x] `deploy/scripts/release.sh`: fetch source → build → gated `migrate` → symlink switch →
      restart → health check; release retention (+ `install.sh` for first-time provisioning)
- [x] CI: `CGO_ENABLED=0` static builds for linux/amd64 + linux/arm64 (verification)
- [x] runbooks: deploy, rollback, migration policy (expand/contract)
- [x] `ims gen module` with declarative `--fields` spec (non-interactive) + generated test
- [x] `HTTP_TRUSTED_PROXIES` config + client-IP resolution middleware wired into throttle/logging
- [x] `docs/architecture.md` finalized + deviations-from-Laravel list

## Out of scope

- Dashboards (Phases 8–9).
- **SQLite** (deferred to a future version — see Decision 5).

## Contracts touched

- `deploy/systemd/*`, `deploy/scripts/{install,release}.sh`; `docs/runbooks/*`.
- CLI: `ims gen module …`.
- Env/config: `HTTP_TRUSTED_PROXIES`.

## Acceptance criteria

- `deploy/scripts/release.sh` performs an offline dry run (`--dry-run`) that validates its steps;
  a fresh install + upgrade + rollback is documented step-by-step in the runbooks.
- `gen module` output compiles (`go build ./...`) after its automatic `sqlc generate`, and the
  generated service test passes.
- Trusted-proxy resolution: a forwarded client IP is honoured only from a trusted peer (unit tests
  cover trusted, untrusted, and multi-hop `X-Forwarded-For`).
- Static `CGO_ENABLED=0` builds succeed for linux/amd64 and linux/arm64.
- No writable state inside release directories (data/storage/cache live under `/var/lib/...`).
- `gofmt` clean, `golangci-lint` exit 0, unit + integration + `-race` suites green.

## Open questions

- ~~Release artifact hosting~~ → build on the server (Decision 1).
- ~~Trusted proxy handling~~ → `HTTP_TRUSTED_PROXIES` middleware (Decision 2).
- ~~Migration gating policy~~ → explicit release step + expand/contract (Decision 3).
- ~~Generator surface scope~~ → CRUD + test, auto-`sqlc` (Decision 4).
- ~~SQLite timing~~ → deferred to a future version (Decision 5).

## Implementation notes

- **Deployment:** `deploy/systemd/{ims-go-api,ims-go-api-scheduler,ims-go-api-worker}.service`
  (systemd hardening: `ProtectSystem=strict`, `NoNewPrivileges`, `ReadWritePaths=/var/lib/...`);
  `deploy/scripts/install.sh` (user/dirs/units/.env) and `deploy/scripts/release.sh`
  (build-on-server, gated `migrate`, atomic symlink, restart, health check, retention,
  `--rollback`, `--dry-run`). CI gained a `cross-compile` job (CGO_ENABLED=0, linux/amd64+arm64).
- **Runbooks:** `docs/runbooks/{deploy,rollback,migrations}.md`.
- **Trusted proxies:** `HTTP_TRUSTED_PROXIES` → `httpx.RealIP` (trusted-peer check + right-to-left
  resolution), wired first in the middleware chain; unit tests cover trusted/untrusted/multi-hop/
  `X-Real-IP`/no-op.
- **Generator:** `internal/scaffold` (parse + templates + `go/format`) and `ims gen module`
  (auto-`sqlc generate`, `--dry-run`, `--force`, `--no-sqlc`, `--table`, `--package`). Verified
  end-to-end: a sample module generated, `sqlc` run, `go build ./...` green, the generated
  migration applied by the integration migrator, and the generated service test passed.
- **SQLite:** investigated, then deferred (Decision 5); all SQLite artifacts removed.
