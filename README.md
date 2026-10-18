# IMS Go API Starter

![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white)
![PostgreSQL](https://img.shields.io/badge/PostgreSQL-16-4169E1?logo=postgresql&logoColor=white)

A batteries-included, multi-tenant Go API starter that ships as **one CGO-free binary** — it serves
the HTTP API and runs migrations, queue workers, the scheduler and code generators. Feature-first
and convention-driven: explicit dependency injection, typed validation, RFC 9457 `problem+json`
errors, `{data}` envelopes, tenant/branch isolation, RBAC, billing with feature limits,
notifications, settings, feature flags, i18n, file storage and background jobs.

## Documentation

| Guide | What's inside |
| --- | --- |
| [Architecture](docs/architecture.md) | System shape, request lifecycle, data layer, platform services, deviations |
| [Developer guide](docs/developer-guide.md) | Module anatomy, conventions, recipes (add module / endpoint / field) |
| [Local development](docs/runbooks/local-development.md) | First run, hot reload, troubleshooting |
| [Migrations](docs/runbooks/migrations.md) · [Rollback](docs/runbooks/rollback.md) · [Deploy](docs/runbooks/deploy.md) | Database and systemd operations |
| [AGENTS.md](AGENTS.md) | Working guide for humans and AI agents |
| [API reference](http://localhost:8080/docs) | Interactive Scalar UI over `api/openapi.yaml` (while the server is running) |

## Features

**⚙️ Runtime & configuration**
- 📦 Single binary: `serve`, `migrate`, `queue:work`, `schedule`, `gen`, … — no containers required.
- 🔧 Typed, 12-factor config with `--env-file` → `APP_ENV_FILE` → `.env` → built-in defaults
  precedence; `ims config:show` prints the effective config with secrets masked.

**🌐 HTTP API**
- 🧭 chi router with a full middleware chain: request ID, real IP (trusted proxies), security
  headers, CORS, access log, panic recovery, metrics, timeout, rate limit and locale negotiation.
- 🧾 RFC 9457 `application/problem+json` for every error; `{data}` success envelopes; LengthAware
  pagination and cursor feeds.
- ✅ Typed request binding with human-readable validation messages; strict JSON (unknown fields
  rejected, body-size and content-type limits).
- 📜 `api/openapi.yaml` is the canonical contract: generated types, managed regions, route-drift +
  freshness checks, and an interactive reference at **`/docs`**.
- ❤️ Health (`/up`, `/api/health`) and optional Prometheus metrics (`/metrics`).

**🔐 Identity, tenancy & access**
- 🎫 Opaque bearer tokens (`id|secret`, SHA-256 at rest, per-device), OTP login, login throttling.
- 🏢 Multi-tenant + branch isolation with explicit scoping (no RLS); super-admin impersonation via
  `X-Acting-Tenant` / `X-Acting-Branch`.
- 🛡️ RBAC: permission catalog, roles (Super-Admin / Admin / User), cached checker with eager
  invalidation, `permission:sync`.
- 👤 User management: CRUD, roles, profile, avatar, password, tenant/branch switching.

**🧱 Platform services**
- 🗄️ **Database** — pgx pools, named connections, embedded goose migrations, sqlc-generated queries,
  `TxManager`.
- ⚡ **Cache** — `memory | file | redis | database` behind one interface, versioned scope-aware keys,
  per-driver contract tests.
- 🧵 **Queue** — `sync` (inline) and River (Postgres) engines, typed job registry,
  `critical/default/heavy` queues, stats + health.
- ⏰ **Scheduler** — validated cron registry with overlap-skip and timeouts, heartbeat + health.
- ✉️ **Mail** — `log` and `smtp` drivers with queued sends.
- 💾 **Storage** — local private/public roots + S3-compatible (aws-sdk-go-v2); static `/storage`.
- 🚦 **Rate limiting** — in-memory and cache-backed fixed-window limiters.

**🧩 Product features**
- ⚙️ **Settings** — typed, grouped catalog with global + per-tenant targets; settings-group CRUD and
  key→group assignment; `settings:sync`.
- 🚩 **Feature flags** — global/tenant/user targets, a `feature:<key>` route gate,
  `feature-flags:sync`.
- 💳 **Billing** — packages, subscriptions (trial/active/past-due/cancelled/expired), payments
  (bKash, Nagad, cash, bank transfer), manual approvals, and a feature-limit guard returning `402`.
- 🔔 **Notifications** — in-app feed (cursor pagination, unread counts), queue fan-out, mail.
- 📁 **Files** — uploads with private/public/S3 disks and signed URLs.
- 🔄 **Import / Export** — background data-processing jobs (CSV/XLSX) with progress and per-row
  errors.
- 🌍 **i18n** — embedded `en` / `bn` catalogs with `Accept-Language` negotiation.
- ℹ️ **System** — info + echo reference module.

**🛠️ Developer experience**
- 🏗️ Feature-first module layout; `ims gen module` scaffolds migration → sqlc queries → repository →
  service → handler → routes → tests → OpenAPI.
- 📝 `ims gen openapi` documents existing modules into managed OpenAPI regions; `make gen-check`
  fails on generated-code drift.
- 🧪 Testing: unit tests, per-package Postgres schemas (`dbtest`, parallel-safe), contract tests, an
  end-to-end flow and a cache-driver contract suite.
- 🚀 Deployment: systemd units plus install/release scripts.

## Requirements

- Go 1.26+
- PostgreSQL 16+ (v1 is PostgreSQL-only)
- Optional: Docker (local database), GNU Make, `air` + `golangci-lint` (installed by `make tools`)

## Quick start

```bash
cp .env.example .env     # local configuration (git-ignored)
make tools               # install air + golangci-lint (once)
make db-up               # start Postgres (or point DATABASE_DSN at your own)
make dev                 # apply migrations, then hot-reload on :8080
make seed                # baseline: permissions, tenant/branch/roles, admin, settings, flags
```

Smoke test:

```bash
curl http://localhost:8080/up                  # liveness
curl http://localhost:8080/api/health          # dependency checks
curl http://localhost:8080/api/v1/system/info  # app info
```

📖 Interactive API reference (Scalar): **<http://localhost:8080/docs>** — the raw OpenAPI document
is served at <http://localhost:8080/docs/openapi.yaml>. Docs are on by default outside production
(toggle with `DOCS_ENABLED`).

## Everyday commands

```bash
make help        # list every target
make dev         # hot-reload API (migrates first)
make build       # build bin/ims
make check       # format-check + vet + lint + unit tests
make test        # unit tests
make generate    # regenerate sqlc + OpenAPI code
make migrate     # apply pending migrations
make seed        # seed baseline data
```

Without `make`: `go run ./cmd/app migrate && go run ./cmd/app serve`.

## CLI

One binary, one command tree (`ims help` lists everything):

```bash
ims serve                                        # HTTP API
ims migrate | migrate:status | migrate:rollback | db:seed
ims queue:work | queue:stats                     # queue worker + stats
ims schedule | schedule:list | schedule:run <id> # scheduler daemon + one-shot tasks
ims permission:sync | settings:sync | feature-flags:sync | cache:clear
ims billing:expire-subscriptions | billing:send-renewal-reminders
ims data-processing:cleanup-completed
ims health:check | config:show | version
ims gen module | gen openapi | openapi:check     # code generators
```

`serve`, `queue:work` and `schedule` each run as their own process.

## Testing

```bash
make test                     # unit tests (integration tests skip without a DSN)
make test-race                # unit tests with the race detector

make db-up                    # local Postgres
DATABASE_TEST_DSN="postgres://postgres:postgres@127.0.0.1:5432/ims_go_api_test?sslmode=disable" make test-integration
```

Each test package gets its own Postgres schema (`dbtest`), so the integration suite is parallel-safe.

## Project layout

```text
cmd/app/            single-binary entry point
internal/app/       composition root: wiring, router, lifecycle
internal/modules/   feature modules (identity, tenants, branches, billing, notifications, settings, …)
internal/platform/  database, cache, queue, scheduler, httpx, storage, mail, i18n, health, metrics
internal/dbgen/     sqlc-generated code (do not edit)
internal/contract/  OpenAPI-generated code (do not edit)
migrations/         embedded goose migrations (also sqlc's schema source)
sql/queries/        sqlc queries
api/openapi.yaml    canonical HTTP contract
docs/               architecture, developer guide and runbooks
```

## Configuration & database workflow

- `.env` is git-ignored; start from `.env.example`, which documents every key with working defaults.
  Inspect the effective configuration with `ims config:show`.
- Migrations are embedded goose SQL and applied only via `ims migrate` (never on boot). `sqlc` reads
  the same files as its schema source, so run `make sqlc` after a schema change.
- `api/openapi.yaml` is the canonical contract; run `make generate` after API changes and
  `make gen-check` before opening a PR.
