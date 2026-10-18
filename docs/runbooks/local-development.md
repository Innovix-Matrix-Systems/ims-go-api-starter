# Runbook — Local development

Run the whole system locally: the HTTP API plus the queue worker and the scheduler. They are three
processes started from the same binary, so a full local stack means three shells.

| Process | Command | Purpose |
|---|---|---|
| API | `ims serve` (or `make dev`) | HTTP on `:8080` |
| Worker | `ims queue:work` | processes River jobs (`critical`/`default`/`heavy`) |
| Scheduler | `ims schedule` | runs cron tasks (heartbeat, billing, cleanup) |

Exactly **one** scheduler and **one** (or more) worker per environment.

## Prerequisites

- Go 1.26+, PostgreSQL 16+, GNU Make
- Optional: Docker (throwaway Postgres); `air` + `golangci-lint` via `make tools`

## First-time setup

```bash
cp .env.example .env      # .env is git-ignored
make tools                # install air + golangci-lint

make db-up                # start Postgres via docker compose (or use your own)
# compose DB is ims_go_api_test; either point DATABASE_DSN at it, or create the
# database the template defaults to:
#   createdb ims_go_api

make migrate              # goose migrations AND River's tables
make seed                 # permissions, Super-Admin role, default tenant+branch, tenant Admin
```

Seeded login: `superadmin@ims.com` / `123456` (`AUTH_DEFAULT_PASSWORD`), tenant `1`, branch `1`.

Set a shared cache driver so the three processes see the same counters and scheduler heartbeat:

```dotenv
CACHE_DRIVER=database     # memory | file | redis | database
QUEUE_DRIVER=river        # river (durable, needs the worker) | sync (inline, no worker)
```

`database` cache uses the `cache` table created by the migrations.

## Run the full stack

Three shells, same working directory:

```bash
# Shell A — HTTP API with hot reload on :8080
make dev                  # = migrate + air; use `make run` for a one-shot run

# Shell B — queue worker
make queue                # = go run ./cmd/app queue:work
#   go run ./cmd/app queue:work --queues=critical
#   go run ./cmd/app queue:work --once

# Shell C — scheduler daemon
make schedule             # = go run ./cmd/app schedule
```

After `make build` the same roles are `bin/ims serve`, `bin/ims queue:work`, `bin/ims schedule`.

## Verify

```bash
curl http://localhost:8080/up
curl http://localhost:8080/api/health             # DB, cache, queue and heartbeat checks
curl http://localhost:8080/api/v1/system/info
# API docs UI (enabled outside production): http://localhost:8080/docs

# log in and use the token
curl -X POST http://localhost:8080/api/v1/login \
  -H "Content-Type: application/json" \
  -d '{"email":"superadmin@ims.com","password":"123456","device":"cli"}'
curl http://localhost:8080/api/v1/user -H "Authorization: Bearer <id|secret>"

go run ./cmd/app queue:stats                      # QUEUE / TOTAL / ACTIVE / FAILED
go run ./cmd/app schedule:list
go run ./cmd/app schedule:run scheduler.heartbeat
go run ./cmd/app health:check
go run ./cmd/app config:show
```

To watch a job end-to-end, trigger a background user export (`POST /api/v1/user/export-background`),
watch Shell B process it, then check `queue:stats` and the job status.

## Scheduled tasks

| ID | Spec | Description |
|---|---|---|
| `scheduler.heartbeat` | `* * * * *` | heartbeat for the health check |
| `dataprocessing.cleanup-completed` | `0 2 * * *` | prune completed import/export jobs |
| `billing.expire-subscriptions` | `0 */6 * * *` | mark overdue subscriptions expired |
| `billing.send-renewal-reminders` | `0 8 * * *` | 7/3/1-day renewal reminders |

Times use `SCHEDULE_TIMEZONE` (falls back to `APP_TIMEZONE`).

## Everyday commands

```bash
make help
make run                  # API once (no hot reload)
make test                 # unit tests
make check                # format-check + vet + lint + tests
make migrate-status       # goose + River migration state
make generate             # regenerate sqlc + OpenAPI code
go run ./cmd/app permission:sync
go run ./cmd/app settings:sync
go run ./cmd/app cache:clear
```

## Feature knobs (`.env`)

| Need | Set |
|---|---|
| Durable jobs | `QUEUE_DRIVER=river` + run the worker |
| Inline jobs, no worker | `QUEUE_DRIVER=sync` |
| Shared cache / counters / heartbeat | `CACHE_DRIVER=database` (or `redis` + `REDIS_*`) |
| Email | `MAIL_DRIVER=log` (default) or `smtp` + `SMTP_*` |
| Uploads | `FILES_DISK=public`, `APP_STORAGE_DIR`, `STORAGE_*` |
| Metrics / docs | `METRICS_ENABLED=true` (`/metrics`), `DOCS_ENABLED=true` (`/docs`) |
| Billing gateways | `BKASH_*`, `NAGAD_*`, `BANK_TRANSFER_*` |

Use a specific file with `ims --env-file /path/to/.env serve` (flag → `APP_ENV_FILE` → `./.env`;
real environment variables always override file values).

## Troubleshooting

- `queue:work` exits immediately: it requires `QUEUE_DRIVER=river`. With `sync`, jobs run inline and
  no worker exists.
- Health shows a stale-scheduler warning: `CACHE_DRIVER=memory` is per-process — set `database`/`redis`.
- `migrate` fails: it creates the schema, not the database. Create the database first, or repoint
  `DATABASE_DSN`.
- Integration tests: set `DATABASE_TEST_DSN` (they skip, never fail, when it is unset).

Production is a separate runbook: see [`deploy.md`](deploy.md).
