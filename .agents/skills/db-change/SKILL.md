---
name: db-change
description: Use when changing the database schema or SQL in this repo — new goose migration, new or edited sqlc query, regenerating internal/dbgen, bigserial sequence sync, soft-delete indexes, or when CI's generated-code diff check fails.
---

# Database changes

Goose migrations in `migrations/` are the **only** schema source (sqlc parses them).
Queries live in `sql/queries/`. `internal/dbgen` is generated — never edit it.

## Adding a migration

1. Next number: `migrations/000NN_<snake_name>.sql`.
2. Structure:

```sql
-- +goose Up
CREATE TABLE example (
    id         bigserial PRIMARY KEY,
    tenant_id  bigint NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
    name       text NOT NULL,
    created_by bigint,
    updated_by bigint,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz
);
CREATE INDEX example_tenant_idx ON example (tenant_id);
CREATE UNIQUE INDEX example_tenant_name_unique ON example (tenant_id, name) WHERE deleted_at IS NULL;

-- +goose Down
DROP TABLE example;
```

3. Rules: `tenant_id`/`branch_id` on scoped tables; audit columns; `deleted_at` for soft
   deletes; partial unique indexes; JSON columns as `jsonb`; timestamps `timestamptz`.
4. Apply locally: `go run ./cmd/app migrate` (also runs River migrations). Check:
   `go run ./cmd/app migrate:status`.

## Adding or editing queries

1. `sql/queries/<module>.sql` with `-- name: QueryName :one|:many|:exec|:execrows`.
2. Optional params: `sqlc.narg('search')`. Whitelisted ordering: the
   `CASE WHEN @order_by::text = 'name' AND @order_dir::text = 'asc' THEN name END` pattern.
3. Run `make sqlc`; commit the regenerated `internal/dbgen` (CI enforces the diff).
4. Map the generated row in `model.go`; wrap driver errors with `database.MapError`.

## Gotchas

- Reserved words: quote them (`"group"`) and rename params (`@group_name`).
- **Sequences**: after inserting explicit ids (seeders), call the module's `SyncIDSequence`
  (see `internal/platform/database/migrate.go` helpers and `internal/seeder`). Otherwise the
  next auto id collides with `pkey`.
- `ON CONFLICT ... DO NOTHING RETURNING *` returns `pgx.ErrNoRows` on conflict — treat as
  "already exists" (see `CreateWithID`).
- River tables are managed by `rivermigrate` through `ims migrate` — never create them by hand.
- Test schemas: `dbtest` runs migrations into a per-package schema; new tables should be added
  to `dbtest.Clean(...)` calls in tests that need a fresh state.
- Verify with the DSN set: `$env:DATABASE_TEST_DSN=...; go test -count=1 ./...`.
