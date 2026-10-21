# IMS Go API Starter

![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white)
![PostgreSQL](https://img.shields.io/badge/PostgreSQL-16-4169E1?logo=postgresql&logoColor=white)

A batteries-included, multi-tenant Go API starter that ships as **one CGO-free binary**. It serves
the HTTP API, applies migrations, runs queue workers and the cron scheduler, and embeds a
Telescope-style **developer dashboard** — no containers required.

Feature-first and convention-driven: explicit dependency injection, typed validation, RFC 9457
`problem+json` errors, `{data}` envelopes, tenant/branch isolation, RBAC, billing with feature
limits, settings and runtime flags, notifications, file storage, PostgreSQL backups and background
jobs.

| Developer dashboard (`/dev`) | API reference (`/docs`) |
| --- | --- |
| ![Developer dashboard](docs/images/dev-dashboard.png) | ![OpenAPI reference](docs/images/api-docs.png) |
| Telemetry, queue, sessions, flags, files, backups and developers. | Scalar over the canonical OpenAPI contract. |

## Features

**Developer dashboard (`/dev`)** — gated to super-admins and the seeded hidden `Developer` role

- **Overview + Telescope** — traffic, queue depth, exceptions, recent commands; request detail with
  SQL, cache/queue, log and exception tabs.
- **Queue + Sessions** — retry/cancel/delete, pause/resume, River job deep links; device/token
  directory, force logout and idle pruning.
- **Settings + Feature flags** — grouped typed catalog with drag-and-drop and global/tenant
  overrides; global/tenant/user flags with source badges and resolution drawer.
- **Files + Backups** — grid/list file manager with previews and tenant filters; local-first
  `pg_dump` archives with optional S3/R2 copies, retention, download and cleanup.
- **Developers + Playground + Command center** — create/repair developer accounts, local-only
  synthetic/real job triggers, and `Ctrl/Cmd+K` navigation/actions.

**Developer experience**
- `ims gen module` scaffolds a tenant-scoped CRUD module (migration, sqlc queries, repository,
  service, handler, routes, tests, OpenAPI region); `ims gen openapi` documents existing modules.
- One binary and one CLI: `serve`, `migrate`, `queue:work`, `schedule`, `gen`, `backup:*`, `db:seed`.
- Embedded goose migrations, sqlc-generated queries, per-package Postgres test schemas (`dbtest`),
  cache-driver contract tests, and OpenAPI route-drift/freshness checks.
- systemd deployment units plus install/release scripts.

**Identity, tenancy & API**
- Opaque bearer tokens (`id|secret`, SHA-256 at rest, per-device), OTP login and login throttling.
- Multi-tenant + branch isolation with **explicit** scoping (no RLS), super-admin impersonation.
- Permission catalog, cached RBAC checker, seeded Super-Admin/Admin/User and hidden Developer roles.
- Typed request binding, strict JSON, validation messages, pagination and cursor feeds.
- Health endpoints (`/up`, `/api/health`), optional Prometheus metrics and interactive `/docs`.

**Platform**
- PostgreSQL (pgx), goose migrations, sqlc, transactions.
- Cache: `memory | file | redis | database`, versioned scope-aware keys.
- Queue: `sync` and River (`critical/default/heavy`), typed job registry, retries, health.
- Scheduler: validated cron tasks with overlap/timeout policies and heartbeat.
- Mail (`log`/`smtp`), storage (local private/public + S3-compatible), rate limiting, i18n `en`/`bn`.

**Product modules**
- Settings (typed/grouped, global + tenant), feature flags (global/tenant/user), notifications
  (feed, queue fan-out, mail), files/uploads, import/export jobs, billing (packages, subscriptions,
  bKash/Nagad/cash/bank, feature limits), system info, and PostgreSQL backups.

## Quick start

```bash
cp .env.example .env          # local configuration (git-ignored)
make tools                    # air + golangci-lint (once)
make db-up                    # Postgres 16 via docker compose (or point DATABASE_DSN elsewhere)
# compose creates ims_go_api_test: point DATABASE_DSN at it, or createdb ims_go_api
npm --prefix web/dev ci       # dashboard dependencies (once)
make migrate                  # goose + River tables
make seed                     # permissions, tenant/branch, roles, admin + developer accounts
make dev                      # dashboard :8080/dev (HMR) + API :8081
```

Open **<http://localhost:8080/dev/>** for the dashboard and
**<http://localhost:8080/docs>** for the API reference.

| Seeded account | Password | Access |
| --- | --- | --- |
| `superadmin@ims.com` | `123456` (`AUTH_DEFAULT_PASSWORD`) | Super-Admin |
| `developer@ims.com` | `123456` (`AUTH_DEFAULT_PASSWORD`) | Hidden Developer role |

The dashboard is disabled by default in production (`DEV_DASHBOARD_ENABLED`). Rotate or disable the
developer account outside local development.

Without Docker: create your database, point `DATABASE_DSN` at it, then run `make migrate seed dev`.
Without `make`: `go run ./cmd/app migrate && go run ./cmd/app serve`.

Smoke test:

```bash
curl http://localhost:8080/up
curl http://localhost:8080/api/health
curl http://localhost:8080/api/v1/system/info
```

## Documentation

| Document | What's inside |
| --- | --- |
| [Documentation index](docs/README.md) | Every guide and runbook in one place |
| [Architecture](docs/architecture.md) | Request lifecycle, layering, tenancy, platform services |
| [Developer guide](docs/developer-guide.md) | Module anatomy, conventions, add-CRUD/endpoint/field recipes |
| [Local development](docs/runbooks/local-development.md) | First run, hot reload, dev accounts, troubleshooting |
| [Backups](docs/runbooks/backups.md) | `pg_dump` archives, local + S3/R2, retention, monitoring |
| [Migrations](docs/runbooks/migrations.md) · [Rollback](docs/runbooks/rollback.md) · [Deploy](docs/runbooks/deploy.md) | Schema and systemd operations |
| [AGENTS.md](AGENTS.md) | Working guide for humans and AI agents |
| [OpenAPI contract](api/openapi.yaml) | Canonical HTTP contract (`/docs` serves it live) |

## Commands

```bash
make help               # every target
make dev                # dashboard + hot-reload API
make build              # bin/ims (use build-all to embed the SPA)
make check              # format-check + vet + lint + unit tests
make test-integration   # full suite against DATABASE_TEST_DSN
make migrate            # apply pending goose + River migrations
make seed               # idempotent baseline data
make generate           # sqlc + OpenAPI contract + generated regions
make gen-check          # fail on generated-code drift
```

CLI highlights:

```bash
ims serve | queue:work | schedule
ims migrate | migrate:status | migrate:rollback | db:seed
ims permission:sync | settings:sync | feature-flags:sync | cache:clear
ims backup:run | backup:list | backup:clean | backup:monitor
ims dev-dashboard:prune
ims gen module | gen openapi | openapi:check
```

`serve`, `queue:work` and `schedule` are separate processes. See the
[local runbook](docs/runbooks/local-development.md) for the three-shell setup.

## Testing

```bash
make test                     # unit tests; integration tests skip without a DSN
make test-race                # unit tests with the race detector
make db-up                    # throwaway Postgres on :5432 (database ims_go_api_test)
DATABASE_TEST_DSN="postgres://postgres:postgres@127.0.0.1:5432/ims_go_api_test?sslmode=disable" make test-integration
```

Every test package gets its own schema, so integration packages are parallel-safe.

### Git hooks

```bash
git config core.hooksPath .githooks
chmod +x .githooks/pre-commit .githooks/pre-push   # macOS/Linux (once)
```

`pre-commit` fails when Go files are not `gofmt`-clean; `pre-push` runs `make check`,
`make openapi-check` and the dashboard type-check (when `web/dev/node_modules` exists).

## Project layout

```text
cmd/app/            single-binary entry point
.githooks/          pre-commit (gofmt) + pre-push (check/OpenAPI/typecheck) hooks
internal/app/       composition root: platform, wiring, routes, lifecycle
internal/modules/   feature modules (identity, access, tenancy, billing, settings, files, backup, ...)
internal/platform/  database, cache, queue, scheduler, httpx, storage, mail, i18n, health, metrics
internal/scaffold/  module + OpenAPI generators
internal/dbgen/     sqlc code (generated)
internal/contract/  OpenAPI code (generated)
web/dev/            embedded TanStack Start developer dashboard
migrations/         embedded goose migrations (also sqlc's schema source)
sql/queries/        sqlc queries
api/openapi.yaml    canonical HTTP contract
docs/               architecture, guide, runbooks, screenshots
```

## Configuration

`.env` is git-ignored; start from `.env.example` (every key documented with working defaults) and
inspect the effective config with `ims config:show`. Precedence: `--env-file` → `APP_ENV_FILE` →
`.env` → code defaults; real environment variables always win.

Migrations are applied only by `ims migrate` (never on boot). After schema/query changes run
`make sqlc`; after API changes run `make generate` and `make openapi-check` before a PR.
