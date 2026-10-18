---
name: add-module
description: Use when adding a new feature/CRUD module to this Go starter (internal/modules/<name>) or porting a Laravel module — covers migration, sqlc queries, repository, service, handler, routes.go, wiring, tests and spec updates.
---

# Adding a module

Follow `AGENTS.md` "Module anatomy" + "Adding a new module (checklist)". This skill is the
short execution recipe.

## Order of work (never skip steps)

1. **Find the spec** — `specs/phase-N-*.md` owns the feature; read its Laravel reference paths.
   If the spec is a stub, flesh it out first (goal, scope, decisions) and set it to Accepted.
2. **Migration** — `migrations/000NN_create_<table>.sql` with goose Up/Down.
   Scoped tables carry `tenant_id` (and `branch_id` when branch-scoped), audit columns,
   `deleted_at` for soft deletes, and **partial unique indexes** (`WHERE deleted_at IS NULL`).
3. **Queries** — `sql/queries/<name>.sql`; run `make sqlc`; never edit `internal/dbgen`.
   - Use `sqlc.narg('x')` for optional filters; `@name` params otherwise.
   - `ORDER BY CASE WHEN @order_by = 'col' ...` pattern for whitelisted ordering.
   - Beware reserved words: quote `"group"`, rename params like `@group_name`.
4. **model.go** — domain struct + `mapRow` (pgtype.Timestamptz → `*time.Time`).
5. **repository.go** — interface (consumer-owned) + sqlc-backed impl:
   - **Pass the proper tenant id.** Every tenant-scoped method takes an explicit `tenantID`
     (and `branchID` when branch-scoped) and the SQL filters on it. There is no RLS — explicit
     scoping is the only isolation boundary, so a query that forgets the filter is a security bug.
   - `pgx.ErrNoRows` → `apperr.Newf(apperr.ResourceNotFound, ...)`.
   - All other driver errors → `database.MapError("module.action", err)`.
6. **service.go** — business rules, scope/tenant stamping, `authz.Checker.MustCan` permission
   checks, `TxManager` for multi-write operations. Big modules use a `service/` subpackage.
7. **request.go** — request structs (`validate:` tags), response DTOs + `NewXResponse` mappers.
8. **handler.go** — bind (`binder.JSON`) → validate → service → `responder.Data/Paginated/NoContent`.
9. **routes.go** — ONLY route registration; mount in `internal/app/routes.go` in the right group
   (public / tenant-resolved / tenant-exempt).
10. **Wiring** — construct repos/services/handlers in `internal/app/wiring.go` (or
    `internal/app/platform.go` for CLI/scheduler-only services).
11. **Permissions** — add new permission names to `internal/authz/catalog.go`; run
    `permission:sync`.
12. **Tests** — unit tests with fakes; integration tests via `dbtest` (skip without
    `DATABASE_TEST_DSN`). Every tenant-scoped module needs a **cross-tenant isolation test**
    (reads, updates and deletes denied) modeled on
    `internal/modules/branches/isolation_integration_test.go`. Add the new tables to any
    `dbtest.Clean(...)` lists in tests you touch.
13. **Verify** — `gofmt -l .`, `golangci-lint run ./...`, `go test -count=1 ./...` with the DSN.
14. **Record** — tick the spec checklist, add implementation notes for deviations.

## Skeleton reminders

- Handler signature: `func (h *Handler) show(w http.ResponseWriter, r *http.Request)`; read
  identity with `authn.IdentityFromContext(r.Context())`, scope with `tenancy.FromContext`.
- Pagination: `page, perPage, offset := httpx.PageParams(r)`; reply with
  `meta, links := httpx.NewPagination(r, page, perPage, total)`.
- Reference implementations: `internal/modules/tenants` (simple), `internal/modules/identity`
  (rich), `internal/modules/billing` (big-module layout: HTTP + gateway adapters at the module
  root, shared types in `domain/`, services in `service/`).
