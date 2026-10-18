# Phase 1 — Database foundation

**Status:** Done (2026-10-16)
**Roadmap:** PLAN.md §11 Phase 1
**Depends on:** Phase 0

## Goal

Named-connection Postgres registry (pgx/v5), embedded goose migrations applied through the single
`ims` binary, the sqlc toolchain reading the same migrations as its schema source, an explicit
transaction manager, the genesis schema (tenants/branches/users/tokens/RBAC), the reference
repository pattern via the tenants module, and the integration test harness.

## Reference (Laravel)

- `config/database.php` — connection definitions, Redis block, naming conventions
- `database/migrations/*` — genesis schema (tenants, branches, users, RBAC, tokens)
- `app/Repositories/*` + `app/Providers/RepositoryServiceProvider.php` — repository conventions
  and interface bindings to mirror

## Decisions (locked)

1. **Genesis schema scope:** identity/tenancy/RBAC only — `tenants`, `branches`, `users`,
   `personal_access_tokens`, `roles`, `permissions`, `model_has_roles`, `role_has_permissions`.
   Billing/notifications/settings tables arrive with their phases.
2. **Soft deletes + uniqueness:** partial unique indexes (`WHERE deleted_at IS NULL`) so deleted
   rows do not block re-use of codes/slugs/emails — a planned deviation from Laravel's plain
   unique constraints (documented improvement).
3. **Connection naming/env:** `primary` from `DATABASE_*`; extra connections declared with
   `DATABASE_CONNECTIONS=primary,analytics` plus `DATABASE_<NAME>_DSN` / `_DRIVER` / `_MAX_CONNS`
   / `_MIN_CONNS` / `_CONN_TIMEOUT` (uppercase, dashes → underscores). Only `pgsql` is supported
   in this phase; `sqlite` returns a clear unsupported-driver error.
4. **Migrations never run on boot:** `ims migrate` is a gated deploy step. `serve` opens lazy
   pools (no startup ping) and reports DB state through health checks. A Postgres advisory lock
   serializes concurrent migration runs.
5. **Fixtures:** no factory framework. Integration tests use `dbtest` helpers with
   truncate-between-tests against a dedicated test database (`DATABASE_TEST_DSN`), cleanly
   skipped when the env var is absent (CI provides Postgres; compose file provided for local dev).
6. **goose integration:** classic goose API over `pgx stdlib` (`stdlib.OpenDBFromPool`); embedded
   migrations via a root `migrations` package (`go:embed postgres/*.sql`) shared with sqlc as its
   schema source. If sqlc cannot parse a construct, a checked-in schema dump is added as fallback
   (recorded here).

## Scope checklist

- [x] `config.Database` section (driver, DSN, pool limits, named connections) + validation + tests
- [x] `database.Registry`: named pgx pools, lazy connect, per-connection health, close
- [x] `database.TxManager`: `WithTx` sharing sqlc `Queries` across repositories
- [x] goose migrations embedded; `migrate`, `migrate:status`, `migrate:rollback [--steps]` CLI
- [x] `sqlc.yaml` verified against goose files; tenants queries; generated code committed
- [x] `modules/tenants` reference repository + domain model (interface owned by the module)
- [x] integration harness: `dbtest` helper, `tests/docker-compose.yml`, `DATABASE_TEST_DSN` gating
- [x] health: `Database` check (per-connection ping) + migrations-current check
- [x] CI: integration job with a Postgres service container

## Out of scope

- Business endpoints (Phase 2+); read/write splitting; `database` cache driver (Phase 3).

## Contracts touched

- DB: `migrations/*.sql`, `sql/queries/*.sql`, `internal/dbgen/*.go`
- Env: `DATABASE_DRIVER`, `DATABASE_DSN`, `DATABASE_MAX_CONNS`, `DATABASE_MIN_CONNS`,
  `DATABASE_CONN_TIMEOUT`, `DATABASE_CONNECTIONS`, `DATABASE_<NAME>_*`, `DATABASE_TEST_DSN`
- CLI: `migrate`, `migrate:status`, `migrate:rollback [--steps]`

## Acceptance criteria

- `sqlc generate` produces compiling code directly from the goose migrations (no duplicated schema)
- `gofmt` / `go vet` / `golangci-lint` / `go test ./...` green on a machine without Postgres
  (integration tests skip cleanly)
- With Postgres: `ims migrate` applies and is idempotent; `migrate:status` reports applied
  versions; `migrate:rollback` reverses; repository integration tests green; `/api/health` shows
  Database `ok`
- Two concurrent `ims migrate` runs serialize on the advisory lock without errors
- Repository errors are `apperr`-typed (not-found → `RESOURCE_NOT_FOUND`) before reaching HTTP

## Implementation notes

- Decision 6 verified: sqlc v1.31.1 parses the goose migration files directly as its schema
  source — no schema-dump fallback needed (`internal/dbgen` is generated from
  `migrations` and CI enforces a diff gate).
- goose's classic API is process-global: embedded FS + dialect setup happen once via
  `sync.Once`; goose's default stderr logger is silenced because the CLI prints its own summary.
- Verified live against PostgreSQL 16 (local Herd): `migrate` applied 5 migrations and is
  idempotent, `migrate:status`, `migrate:rollback --steps=2` + re-apply, `health:check`
  (Database + Migrations `ok`), `/api/health` payload, `config:show` with the DSN password
  masked.
- Integration tests pass with `DATABASE_TEST_DSN` and skip cleanly without it; CI runs them
  with a Postgres service container plus the sqlc diff gate.

## Open questions

None outstanding — decisions 1–6 resolved; new questions discovered during implementation were
recorded above before merging.
