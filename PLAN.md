# IMS Go API Starter — Implementation Plan

**Goal:** Rebuild the non-CMS capabilities of `ims-cms-saas-laravel-api-starter` as an idiomatic Go
starter that follows `go-backend-engineering-standard-updated.md` — keeping the Laravel developer
experience (Form Requests, API Resources, Artisan, queues, scheduler, cache drivers, multi-DB,
Service-Repository, tenancy) while using Go's strengths (compile-time SQL, single static binary,
`context` propagation, real concurrency, embedded assets, systemd deployment).

**Status:** Reviewed — core decisions confirmed (§14); per-phase specs tracked in `specs/` (§15).
Milestone 1 scaffolding starts on go-ahead; dashboards added as Phases 8–9 (§7.21).

---

## 1. Scope

### 1.1 Port from Laravel (non-CMS)

| Capability | Laravel source | Priority |
|---|---|---|
| Multi-tenancy (tenant + branch isolation, impersonation, exempt routes, public resolve) | `app/Tenant/*`, `app/Http/Middleware/Tenant*` | P0 |
| Authentication — Sanctum-style device tokens, OTP login, lockout | `app/Services/Auth/*` | P0 |
| RBAC — roles/permissions, catalog registry, super-admin bypass | `config/permission-registry.php`, `app/Policies/*` | P0 |
| Named DB connections, transactions, migrations, repositories | `config/database.php`, `app/Repositories/*` | P0 |
| Cache abstraction + drivers (memory / file / redis / database), versioned keys | `config/cache.php`, `app/Helpers/CacheHelper.php` | P0 |
| Queue (critical/default/heavy channels) + typed job handlers + progress tracking | `config/queue.php`, `app/Jobs/*`, `app/Registry/QueueRegistry.php`, `DataProcessingJob` | P0 |
| Scheduler + Artisan-style CLI | `routes/console.php`, `app/Console/Commands/*` | P0 |
| Health checks | `app/Checks/*`, `app/Providers/HealthServiceProvider.php` | P0 |
| Billing — packages, features/limits, subscriptions, payments, gateway drivers, approvals | `app/Billing/*`, `app/Services/Billing/*` | P1 |
| Notifications — DB fan-out model, unread counters, cursor feed | `app/Services/Notification/*`, `app/Repositories/Notification/*` | P1 |
| Settings (setanjo-style typed settings) | `config/setanjo.php`, `app/Enums/SettingKey.php` | P1 |
| i18n (en/bn, Laravel message keys) | `lang/*`, `LanguageMiddleware` | P1 |
| Uploads / media (without CMS) | Spatie Media Library usage on `User`/`Upload` | P1 |
| Import/export (Excel) with background jobs | `app/Imports/*`, `app/Exports/*`, `app/Jobs/ProcessUser*` | P1 |
| OpenAPI contract + docs UI | Scribe + Scalar | P1 |
| Deployment (binary + `.env` + systemd) | Dockerfiles, `deploy.sh` | P1 |
| Admin dashboard (React SPA) — **new, not in Laravel** | — | P2 (Phase 8) |
| Developer dashboard — lightweight Telescope replacement — **new** | — | P2 (Phase 9) |

### 1.2 Explicitly out of scope (v1)

- **CMS:** Blueprint, Component, Page, Post, Comment, Form, FormSubmission, Topic, Block, Cms\*.
- **Laravel's dashboard packages (v1 only):** Telescope / Pulse / Horizon are not ported as-is;
  during the API phases their role is filled by `slog` + request IDs + optional Prometheus/OTel +
  a queue web UI (§7.17). After the API phases, purpose-built React dashboards replace them —
  including a lightweight Telescope equivalent (§7.21, Phases 8–9). Dashboard work is **new
  capability not present in the Laravel starter** and starts only after Phases 0–7.
- **Laravel-owned infrastructure:** Eloquent, Blade/web UI, session cookies, Scribe annotations.
- **Not in the Laravel starter either (parity):** public register, forgot/reset password, refresh
  token endpoints. Admin-provisioned users only. (Easy to add later — §14 open decisions.)
- **Image conversions** (thumbnails) — store originals in v1; add processor later.

### 1.3 Preserve exactly (client-facing contract parity)

These were verified in the Laravel code and must look the same to existing frontends:

1. Success envelope `{"data": ...}`; `{"message": ...}` for message-only; `204` empty body.
2. Pagination: `{"data": [...], "links": {...}, "meta": {"current_page","per_page","total","last_page","from","to",...}}`.
3. Cursor feed: `{per_page, next_cursor, prev_cursor}` + links.
4. Login payload: `{"data": {"user": {...}, "status": "AUTH_SUCCESS|OTP_REQUIRED", "message": "...", "token": "id|plain"}}`.
5. Auth status strings, error codes (`UNAUTHORIZED`, `RESOURCE_NOT_FOUND`, ...), `messages.*` i18n keys.
6. Header `X-Acting-Tenant` / `X-Acting-Branch` impersonation + write-guard (403 when impersonator sends writes without acting headers).
7. `X-Tenant-Code` (+ optional `X-Branch-Id`) public tenant resolution.
8. Queue channel names `critical` / `default` / `heavy` with tries/timeout config keys.
9. Money in minor units (paisa), currency `BDT`.
10. IDs are `int64` (Laravel auto-increment). Devices: `user_{id}_{device}` token naming semantics (device stored as a column; see §7.5).

**One deliberate contract change:** every error becomes an RFC 9457 `application/problem+json`
response (per the Go standard §9). Laravel has two error universes (rich `ApiException` vs bare
`{"message": ...}` framework errors). The Go starter unifies them and keeps `error_code`,
`timestamp`, and adds `errors` for 422 — frontends that today parse `{"message"}` mostly work,
but this is documented as a migration note.

---

## 2. Laravel analysis digest (what we must faithfully reproduce)

Reference implementation: `K:\Projects\Herd\ims-cms-saas-laravel-api-starter` (Laravel 12 +
Sanctum + spatie/permission). This digest is the index; per-phase specs list the exact Laravel
files to consult when implementing each module. Condensed from the codebase audit; these
mechanics drive the design in §7.

### 2.1 HTTP

- Base controller helpers: `respond()` wraps `{"data":...}` (+ merges `meta`), `fail()` throws
  `ApiException`, `successNoContent()` → 204, `successWithMessage()` → `{"message"}`.
- `ApiException` renders `type` (`{base}/errors/{slug}`), `title`, `detail`, `instance`,
  `error_code`, `timestamp`; adds `status` + `trace_id` only for 5xx; masks 5xx detail in production.
- Framework validation errors: 422 `{message, errors:{field:[messages]}}` (dot paths + indices).
- Middleware (API group): `LanguageMiddleware` (Accept-Language → `en`/`bn`), `tenant.resolve`,
  `tenant.exempt`, `tenant.public` (unused today), `subscription` (unused today).
- Rate limit: `throttle:api` `1000/min` keyed by user id or IP; login lockout `5 attempts / 1 min`
  keyed `email|ip`; login success/registration routes absent.

### 2.2 Auth

- Login: active/email-verified checks → bcrypt verify → optional OTP (cache `OTP_{userId}`, 5 min,
  6 digits) → token name `user_{id}_{device}{id}`, never expires, plaintext `id|...` (Sanctum PAT).
- Logout deletes token rows by device; updates `last_login_at`; device is required input.
- Token last-used tracking via Sanctum; no refresh; no register/reset.

### 2.3 Tenancy

- `TenantContext` holds identity (`tenant_id`, `branch_id`) and impersonation
  (`actAsTenant/actAsBranch/actAsGlobal`) + `internalRequest` flag; `effective*()` returns
  impersonation ?? identity. Bound per-request **and per-job**.
- Global scopes `TenantScope`/`BranchScope` filter all model queries when effective ids are set;
  `forAllBranches()` drops branch filter only; `forAllTenants()` drops both.
- Traits stamp `tenant_id`/`branch_id` on create/update from effective context unless
  `skipTenantStamping` (notifications set it to own the explicit tenant).
- Resolution middleware: `X-Acting-*` for impersonators (no headers = read-only global view;
  writes → 403), otherwise user's `tenant_id` + `active_branch_id` with branch-belongs-to-tenant
  check cached 5 min (invalidated by `BranchObserver`).
- `tenant.exempt` → internal request (global data, e.g. tenants CRUD, permissions).
- Validation rules `TenantUniqueRule`/`TenantExistsRule` scope by effective tenant (+ branch
  optionally), fail closed when no tenant.
- Cache keys: `tenant:{t}:...`, `tenant:{t}:branch:{b}:...`, `global:...`; versioned cache helper
  bumps a per-namespace version to invalidate safely.

### 2.4 RBAC

- Tables: roles (with `tenant_id`, `is_system`), permissions (with `group`, `description`,
  `is_system`), pivots; catalog in `config/permission-registry.php` seeded by `permission:sync`.
- Super-Admin bypass via `Gate::before`; system roles/permissions protected from tenant mutation.
- Policies = permission strings (`user.view`, `user.createInternal`, ...), enforced by
  `Gate::authorize` in controllers; some hand-rolled rules (cannot edit super-admin, etc.).

### 2.5 Queue / scheduler / cache

- Channels `critical`(3/60s), `default`(5/120s), `heavy`(1/1800s); `QueueRegistry` reads config.
- Jobs re-resolve tenant context at `handle()` start; fan-out chunk size 500; idempotent delivery
  inserts via `insertOrIgnore` on `(notification_id, user_id)`.
- `DataProcessingJob` table tracks background import/export progress (status enum + counters +
  errors JSON + file metadata + `getProgressPercentage()`).
- Scheduler: billing expiry every 6h, renewal reminders daily 08:00, cleanup 02:00, health
  every minute, etc.
- Cache store default `database`; Redis used for unread counters (`notif:t{u}:u{u}:unread`),
  OTP, branch checks, rate limits; `CacheHelper` versioned keys.

### 2.6 Billing

- `Package` + `PackageFeature` (`feature_key`, `is_enabled`, `limit_value`); `Feature` enum
  (`users.max`, `branches.max`, `storage.mb`, `reports.*`, `api.access`, ...); per-tenant
  `overrides` JSON wins over package.
- `FeatureGuard`: `can/require/limit/withinLimit/requireWithinLimit`, 402-style exceptions
  (`feature_not_available`, `usage_limit_exceeded`); enforced at user-create and branch-create.
- Subscription lifecycle: `trial → pending → active → cancelled → expired` (+ `past_due`);
  `nextExpiry(monthly|yearly|one_time≈100y)`; `activate`, `cancel`, `renew`, `handleGatewayCallback`.
- Payments: `PaymentDriverInterface{initiate,verify,handleCallback,refund}` + result DTOs;
  `PaymentOrchestrator` registry (`bkash`, `nagad` stub, `cash`, `bank_transfer`); invoice
  `INV-XXXX-YmdHis`; manual approval workflow `PaymentApprovalRequest` (approve → activate,
  reject → cancel + mark failed).
- Amounts int minor units; gateway amounts `amount/100` formatted 2dp.

### 2.7 Notifications / settings / i18n / helpers

- `notifications` + `notification_targets` (USER/ROLE/BRANCH/TENANT/GLOBAL) + `notification_reads`
  (BranchAware) + `notification_deliveries` (`PENDING`); fan-out job; unread counters in Redis with
  `read_all_before` marker; feed is cursor-paginated; `markRead`, `markAllRead` (job).
- Settings: typed keys (`SYSTEM_NAME`, `NOTIFICATION_ENABLED`, `DATA_PROCESSING_JOB_CLEANUP_DAYS`)
  with defaults + strict tenancy + cache; synced by `settings:sync`.
- i18n keys are flat dot paths under `messages.*`, `auth.*`, `validation.*`, `http-statuses.*`.
- Helpers worth porting: tenant cache keys, versioned cache keys, `feature_guard`, `notify`,
  filter helper semantics (search / partial / select / whereIn / date ranges / unified search).

---

## 3. Laravel → Go concept mapping

| Laravel concept | Go equivalent in this starter | Notes |
|---|---|---|
| Service container bindings | Explicit constructors in `internal/app/wiring.go` composition root | No global locator; module factories |
| `scoped()` per request/job | `context.Context` values set by middleware; job wrapper restores tenancy | Context carries identity/scope, never business state |
| FormRequest | Request struct + `go-playground/validator` + shared `httpx.BindJSON` | Structural validation at transport boundary |
| Controller | Handler struct with methods; thin transport layer | Routes registered per module |
| API Resource | Response DTO + `NewXResponse(model)` mappers | No persistence rows on the wire |
| `ApiException` + framework errors | Unified `apperr.Error` → RFC 9457 `Problem` | One error model; `error_code`, `errors`, `trace_id` |
| Eloquent global scopes | Explicit `tenancy.Scope` params + SQL `WHERE tenant_id = $n` | No hidden query rewriting; enforced via repo APIs + tests |
| Model observers (stamp tenant/audit) | Service layer stamps `tenant_id`,`branch_id`,`created_by`,`updated_by` | Explicit; documented per repo method |
| `skipTenantStamping` | `Scope.AllTenants` / explicit tenant arg on repo create | Notifications keep caller-owned tenant |
| Eloquent repositories | sqlc queries + repository adapters implementing small interfaces | Interface owned by the consuming service |
| Sanctum PAT | `personal_access_tokens` table; SHA-256 hash; `id\|plain` token format | Configurable expiry (default 0 = never, parity) |
| spatie/permission | DIY tables preserved + catalog in Go + `authz` service | Casbin evaluated and rejected: schema/data model parity wins |
| Policies / `Gate::authorize` | `authz.Require(ctx, "user.update", target)` enforced in service + route middleware | Service-level checks so jobs/CLI are covered too |
| Queue jobs / Horizon | `queue.Engine` interface → **River** (Postgres/SQLite, transactional) recommended | Typed payload registry; `sync` engine for dev/tests; Asynq option |
| `QueueRegistry` channels | `queue.Registry` + typed config (`QUEUE_CRITICAL_*` ...) | Same names/tries/timeouts |
| `routes/console.php` scheduler | `schedule.Registry` + `robfig/cron/v3`, run via `schedule` mode | One scheduler per deployment (systemd unit) |
| Artisan commands | Cobra tree + module command registration (`queue:work`, `billing:expire-subscriptions`, ...) | Same command naming style |
| `RateLimiter` (cache) | Cache-backed limiter (`Increment` + TTL) | Login + global throttle |
| `Cache::remember/put` | `cache.Store` interface + `cache.Remember` helper (+singleflight) | Drivers: memory/file/redis/database |
| Redis unread counters | Same counters via `cache.Store` (Increment) with DB recompute fallback | Driver-agnostic; honest semantics documented |
| Scribe + Scalar | `api/openapi.yaml` + `oapi-codegen` + Scalar UI served at `/docs` | Spec-first; contract tests in CI |
| Laravel Excel | `excelize/v2` import/export services + queued jobs + tracking table | Same UX: template download, progress, errors |
| `lang/*.php` | `go:embed` JSON catalogs + `i18n` translator + Accept-Language middleware | Same `messages.*` keys; `:placeholder` substitution |
| Telescope/Pulse/Horizon | `slog` + request IDs + optional `/metrics` + queue admin (dev dashboard) + health checks | See §7.17 |
| Docker per role | Single binary + systemd units + `EnvironmentFile=` | Per the Go standard §18 |
| `make:crud` generators | `ims gen module --fields='...'` (Phase 7 stretch) | Port the declarative field spec idea |
| Pest tests | stdlib `testing` + `httptest` + `go-cmp`; real DB integration tests | Per the Go standard §20 |

---

## 4. Technology decisions

Versions are pinned when scaffolding. All choices reviewed against ecosystem research (Oct 2026).

| Concern | Choice | Rationale / alternatives |
|---|---|---|
| HTTP | `net/http` + `chi/v5` | Per standard; `std-http` mux considered, chi middleware ecosystem wins |
| CLI | `spf13/cobra` | Per standard; colon command names supported |
| Dotenv | `joho/godotenv` (Load, never Overload) | Per standard; systemd `EnvironmentFile` compatible |
| Validation | `go-playground/validator/v10` | Per standard; message mapping layer added |
| DB driver | `jackc/pgx/v5` (+`pgxpool`) | Per standard; first-class Postgres |
| SQL | `sqlc` (+ `internal/dbgen`) | Compile-time checked queries; reads goose migrations as schema |
| Migrations | `pressly/goose/v3` (embedded `embed.FS`) | Transactions by default, goose annotations, Go migrations for data fixes; run via `migrate` command, not on boot |
| SQLite | *deferred (v1 is PostgreSQL-only)* | Needs a second sqlc query/repository set per module; revisit in a future version (docs/architecture.md 11.22) |
| Queue | **`riverqueue/river`** (recommended) — Postgres/SQLite, transactional enqueue, unique/scheduled jobs, web UI | Matches Laravel's database queue default + removes dual-write bugs; Asynq (Redis) kept as alternate engine if Redis becomes required. Machinery rejected (maintenance mode) |
| Scheduler | `robfig/cron/v3` | Per standard; central registry, one process |
| Cache | internal interface; drivers: memory, file, `redis/go-redis/v9`, database | Full Laravel parity; versioned keys are the primary invalidation strategy (tags only later, Redis-only) |
| Password hashing | `golang.org/x/crypto/bcrypt` | `BCRYPT_ROUNDS` config parity |
| Authentication | Opaque personal access tokens (Sanctum semantics) behind the `authn.Authenticator` interface | Ships opaque; `golang-jwt/jwt/v5` reserved for a future JWT driver behind the same interface |
| Rate limiting | internal cache-backed limiter | Same semantics as Laravel RateLimiter; `x/time/rate` not suitable for distributed counters |
| Excel | `xuri/excelize/v2` | Standard Go Excel lib |
| i18n | `nicksnyder/go-i18n/v2` + embedded JSON catalogs (`internal/platform/i18n/locales/`) | CLDR plurals, BCP 47 locale matching; keys ported 1:1 from `lang/`; `goi18n merge` keeps catalogs in sync |
| OpenAPI | `oapi-codegen/v2` (types + chi strict server) + spec in `api/openapi.yaml` | Per standard; spec-first with CI verification; Huma/goa/fuego considered (code-first conflicts with canonical-spec rule) |
| Docs UI | Scalar (served static; Swagger UI fallback) | Same as Laravel starter UX |
| Admin & dev dashboards | React + Vite + TanStack (Router, Query, Table, Form) + zustand only if needed; TS types generated from OpenAPI | No Next.js; SPA embedded in the same binary (§7.21, Phases 8–9) |
| Request/query capture | pgx `QueryTracer` + HTTP middleware hooks + `slog` handler → telemetry store | Powers the developer dashboard (Phase 9) |
| Logging | `log/slog` JSON to stdout | journald-first per standard |
| Metrics/tracing | optional `prometheus/client_golang`, OTel later | Phase 6; not required by v1 |
| Contract tests | `getkin/kin-openapi` (validate live responses) | CI gate |
| Integration tests | env DSN + `docker compose` for local/CI; `testcontainers-go` optional | Windows dev-friendly (Herd/Docker) |
| Lint/format | `gofmt`, `go vet`, `golangci-lint` (pin version in CI), `staticcheck` | CI gates |
| Batch/fakes | hand-written fakes for service tests; `go-cmp` for diffs | stdlib `testing` default per standard |
| Evaluated, not adopted | `goravel` framework, Casbin, GORM/ent/bun, Fuego/Huma code-first, Machinery | Global containers/facades conflict with explicit-DI standard; ORM not needed with sqlc; RBAC schema parity wins over policy DSL |

---

## 5. Repository structure

Adapts the standard's layout (`go-backend-engineering-standard-updated.md` §4) with the Laravel
modules mapped into `internal/modules/*`.

```text
ims-go-api-starter/
├── cmd/
│   └── app/main.go                    # single binary entry point (serve/queue/schedule/migrate/…)
├── internal/
│   ├── app/
│   │   ├── wiring.go                  # composition root; per-mode dependency graphs
│   │   ├── routes.go                  # chi assembly + middleware order + module route mounting
│   │   └── lifecycle.go               # signals, graceful shutdown, server timeouts
│   ├── cli/
│   │   ├── root.go                    # cobra root, --env-file, version
│   │   ├── registry.go                # command registry (dup detection)
│   │   ├── deps.go                    # lazy typed dependency providers per command
│   │   ├── builtins.go                # version, config:show, health:check
│   │   └── commands/                  # serve, schedule, queue:work, migrate*, cache:clear, …
│   ├── config/                        # typed config structs + env loading/validation
│   ├── platform/                      # technical concerns
│   │   ├── database/                  # registry (named conns), pgx pool, tx manager
│   │   ├── cache/                     # store.go, factory.go, memory/, file/, redis/, database/
│   │   ├── queue/                     # engine.go (river/sync), registry.go (typed jobs), worker.go
│   │   ├── scheduler/                 # registry.go, runner.go (cron)
│   │   ├── httpx/                     # decode, validate, problem, response, middleware, pagination
│   │   ├── logging/                   # slog setup, request IDs
│   │   ├── health/                    # registry + checks + result store
│   │   ├── i18n/                      # translator + messages/*.json (embedded)
│   │   ├── storage/                   # disks: local, public, s3; upload helpers
│   │   ├── mail/                      # driver interface (log, smtp) — later
│   │   ├── ratelimit/                 # cache-backed limiter
│   │   └── telemetry/                 # request/query/log capture for dev dashboard (Phase 9)
│   ├── authn/                         # tokens service, bearer middleware, OTP, login throttle
│   ├── authz/                         # permission catalog, checker, middleware, super-admin rules
│   ├── tenancy/                       # Scope type, context helpers, resolve/exempt middleware
│   ├── apperr/                        # application error codes (ApiErrorCode parity) + errors
│   ├── modules/
│   │   ├── system/                    # health/version endpoints, sample of full pattern
│   │   ├── identity/                  # users (+ profile, avatar, switch-branch)
│   │   ├── access/                    # roles + permissions
│   │   ├── tenants/                   # tenants + branches
│   │   ├── billing/                   # packages, subscriptions, payments, approvals, drivers, features
│   │   ├── notifications/
│   │   ├── settings/
│   │   ├── uploads/
│   │   └── dataprocessing/            # import/export jobs + tracking
│   └── dbgen/                         # sqlc output — generated
├── web/                               # React dashboards (Phases 8–9): Vite + TanStack → go:embed
├── sql/
│   ├── queries/                        # sqlc query files
├── migrations/
│   ├── *.sql                          # goose migrations (sqlc schema source)
├── api/openapi.yaml                   # canonical contract
├── specs/                             # per-phase specs (roadmap §11, workflow §15)
├── tests/integration/, tests/contract/
├── deploy/systemd/, deploy/scripts/   # units + release script
├── docs/                              # architecture.md, runbooks (later)
├── tools/                             # tools.go pinning go:generate tool versions
├── .env.example  Makefile  .golangci.yml  .gitignore  sqlc.yaml  go.mod  README.md  PLAN.md
```

Module internal layout (convention, not empty-file mandate):

```text
internal/modules/identity/
├── routes.go        # RegisterRoutes(r chi.Router, h *Handler, auth, tenancy …)
├── handler.go       # HTTP transport
├── request.go       # request structs + validation tags
├── response.go      # response DTOs + mappers (API Resources)
├── service.go       # business rules, tx orchestration, authorization
├── repository.go    # interface (service-owned) + adapter over sqlc queries
├── model.go         # domain structs (+ enums where needed)
├── commands.go      # optional CLI commands contributed by the module
└── scheduled.go     # optional scheduled tasks
```

---

## 6. Configuration & bootstrap

- `config.Load(envFile string) (*Config, error)`: resolves env file path as
  `--env-file` → `APP_ENV_FILE` → `.env`; `godotenv.Load` (never `Overload`); OS env wins.
- Typed nested structs (`App`, `HTTP`, `Database`, `Cache`, `Queue`, `Auth`, `Tenancy`, `Billing`,
  `Mail`) with `Validate()` per mode. `help`/`version` construct no infrastructure.
- Absolute paths in production (`APP_DATA_DIR`, `APP_STORAGE_DIR`, `APP_CACHE_DIR`); no CWD inference.
- Draft `.env.example` (Phase 0 ships it):

```dotenv
# ── App ─────────────────────────────────────────────
APP_ENV=local
APP_NAME=ims-go-api
APP_DEBUG=true
APP_URL=http://localhost:8080
APP_LISTEN_ADDR=:8080
APP_TIMEZONE=Asia/Dhaka
APP_LOCALE=en
APP_FALLBACK_LOCALE=en
APP_SUPPORTED_LOCALES=en,bn
APP_DATA_DIR=./var
APP_STORAGE_DIR=./var/storage
APP_CACHE_DIR=./var/cache
# APP_LOG_DIR=                # empty → stdout (journald in production)
# APP_ENV_FILE=./.env

# ── HTTP ────────────────────────────────────────────
HTTP_READ_TIMEOUT=15
HTTP_WRITE_TIMEOUT=30
HTTP_IDLE_TIMEOUT=60
HTTP_SHUTDOWN_TIMEOUT=30
HTTP_MAX_REQUEST_BODY_BYTES=10485760
RATE_LIMIT_PER_MINUTE=1000

# ── Database ────────────────────────────────────────
DATABASE_DRIVER=pgsql                     # pgsql (v1 is PostgreSQL-only)
DATABASE_DSN=postgres://postgres:postgres@127.0.0.1:5432/ims_go_api?sslmode=disable
DATABASE_MAX_CONNS=20
DATABASE_MIN_CONNS=2
DATABASE_CONN_TIMEOUT=5
# Named extra connections (optional):
# DATABASE_CONNECTIONS=primary,analytics
# DATABASE_ANALYTICS_DRIVER=pgsql
# DATABASE_ANALYTICS_DSN=...

# ── Cache ───────────────────────────────────────────
CACHE_DRIVER=memory                       # memory | file | redis | database
CACHE_PREFIX=ims-
# CACHE_FILE_DIR=./var/cache
REDIS_ADDR=127.0.0.1:6379
REDIS_PASSWORD=
REDIS_DB=0
REDIS_CACHE_DB=1

# ── Queue ───────────────────────────────────────────
QUEUE_DRIVER=river                        # river | sync
QUEUE_CRITICAL_TRIES=3
QUEUE_CRITICAL_TIMEOUT=60
QUEUE_DEFAULT_TRIES=5
QUEUE_DEFAULT_TIMEOUT=120
QUEUE_HEAVY_TRIES=1
QUEUE_HEAVY_TIMEOUT=1800
QUEUE_WORKER_CONCURRENCY=10

# ── Scheduler ───────────────────────────────────────
SCHEDULE_TIMEZONE=Asia/Dhaka

# ── Auth ────────────────────────────────────────────
AUTH_TOKEN_EXPIRATION=0                   # minutes; 0 = never (Sanctum parity)
AUTH_LOGIN_MAX_ATTEMPTS=5
AUTH_LOGIN_DECAY_MINUTES=1
AUTH_OTP_ENABLED=false
AUTH_OTP_LENGTH=6
AUTH_OTP_EXPIRY_MINUTES=5
AUTH_DEFAULT_PASSWORD=123456
BCRYPT_ROUNDS=12

# ── Tenancy ─────────────────────────────────────────
DEFAULT_TENANT_ID=1
DEFAULT_TENANT_NAME=Main Tenant
DEFAULT_TENANT_CODE=fad58de7366495db4650cfefac2fcd61
DEFAULT_TENANT_SLUG=main-tenant
DEFAULT_TENANT_IS_ACTIVE=true
DEFAULT_BRANCH_ID=1
DEFAULT_BRANCH_NAME=Default Branch

# ── Billing ─────────────────────────────────────────
BILLING_CURRENCY=BDT
# BKASH_BASE_URL= / BKASH_APP_KEY= / BKASH_APP_SECRET= / BKASH_USERNAME= / BKASH_PASSWORD=
# NAGAD_BASE_URL= / NAGAD_MERCHANT_ID= / NAGAD_MERCHANT_NUMBER= / NAGAD_PUBLIC_KEY= / NAGAD_PRIVATE_KEY=
# BANK_TRANSFER_ACCOUNT_NAME= / BANK_TRANSFER_ACCOUNT_NUMBER= / BANK_TRANSFER_BANK_NAME= / BANK_TRANSFER_BRANCH=

# ── Mail (Phase 5) ──────────────────────────────────
MAIL_DRIVER=log                           # log | smtp
SMTP_HOST=127.0.0.1
SMTP_PORT=2525
SMTP_USERNAME=
SMTP_PASSWORD=
MAIL_FROM_ADDRESS=hello@example.com
MAIL_FROM_NAME=IMS Go API
```

---

## 7. Cross-cutting designs

### 7.1 HTTP kernel

Middleware order (outermost first), registered in `internal/app/routes.go`:

```
RequestID → RealIP → Recover → AccessLog(slog) → Timeout(60s) → CORS(optional)
→ RateLimit(global) → Locale(Accept-Language) → [route group] Authenticate → TenantResolve | TenantExempt
```

- `AppError` never escapes a handler: handlers map `*apperr.Error` → `httpx.Problem`.
- Success helpers: `httpx.Data(w, v)`, `httpx.Message(w, "msg")`, `httpx.NoContent(w)`,
  `httpx.Paginated(w, items, pageMeta, links)` — byte-compatible with §1.3 shapes.
- Request logging: method, path, route pattern, status, duration, request id, user id, tenant id.
- Panic → 500 problem + stack in logs only.

### 7.2 Errors — RFC 9457 (`application/problem+json`)

```go
type Problem struct {
    Type      string              `json:"type"`            // https://…/problems/validation-error
    Title     string              `json:"title"`
    Status    int                 `json:"status"`
    Detail    string              `json:"detail,omitempty"`
    Instance  string              `json:"instance,omitempty"`
    ErrorCode string              `json:"error_code,omitempty"` // UNAUTHORIZED, RESOURCE_NOT_FOUND, …
    Timestamp string              `json:"timestamp,omitempty"`
    TraceID   string              `json:"trace_id,omitempty"`   // 5xx only
    Errors    map[string][]string `json:"errors,omitempty"`     // 422 only, dot paths + indices
    Debug     *Debug              `json:"debug,omitempty"`      // APP_DEBUG only, masked otherwise
}
```

- `internal/apperr`: error `Code` constants matching `ApiErrorCode` (UNAUTHORIZED, FORBIDDEN,
  INVALID_CREDENTIALS, TOKEN_EXPIRED, TENANT_CONTEXT_NOT_RESOLVED, RESOURCE_NOT_FOUND,
  VALIDATION_FAILED, RATE_LIMIT_EXCEEDED, FEATURE_NOT_AVAILABLE, USAGE_LIMIT_EXCEEDED, ...),
  default HTTP status + i18n title per code, wrapping with `%w` for logs.
- Status map per Go standard §9.1 (400/401/403/404/409/413/415/422/429/500/503).
- 5xx detail is generic in production; real error logged with `trace_id`.
- Validation: `httpx.BindJSON` decodes (size limit, content type, unknown fields rejected,
  trailing garbage rejected) then validates; failures produce 422 with readable, translated
  messages mapped from validator tags (`required_without` → "The :attribute field is required
  when :other is present."). Raw tags never leak (Go standard §9.3).

### 7.3 Tenancy

```go
// internal/tenancy
type Scope struct {
    TenantID       int64 // identity
    BranchID       int64
    Acting         Acting // None | Tenant | Branch | Global (impersonation)
    ActingTenantID int64
    ActingBranchID int64
    Internal       bool // tenant.exempt → global queries allowed
}

func (s Scope) EffectiveTenantID() int64
func (s Scope) EffectiveBranchID() int64
func (s Scope) ForAllBranches() Scope // drop branch filter only
func (s Scope) RequiresWriteActing() bool
```

- Middleware chain: `Authenticate` → (route group) `TenantResolve` or `TenantExempt`.
  `TenantResolve` implements the exact header/validation behavior from §2.3 including
  5-minute cached branch-belongs-to-tenant checks with invalidation hooks on branch updates.
- `tenancy.FromContext(ctx)` for middleware/logging/jobs; **repositories take explicit typed
  params** (`tenantID, branchID int64`) — no query rewriting magic. SQL is always explicit:

```sql
-- name: ListUsers :many
SELECT * FROM users
WHERE tenant_id = @tenant_id
  AND (sqlc.narg('branch_id')::bigint IS NULL OR branch_id = @branch_id)
  AND deleted_at IS NULL
  AND (@search::text IS NULL OR name ILIKE '%' || @search || '%')
ORDER BY created_at DESC
LIMIT @lim OFFSET @off;
```

- `ForAllBranches` = pass `branch_id = NULL`; `tenant.exempt` = dedicated internal queries that
  omit tenant filter (`...Internal` query variants) — visible and reviewable, unlike global scopes.
- Safety net: integration tests per module asserting cross-tenant reads/writes fail. Postgres RLS
  was evaluated and dropped (Phase 6, `docs/architecture.md` §11.18): explicit `tenant_id` scoping
  in every query is the only isolation boundary.
- Tenant-aware validation helpers: `validation.UniqueForTenant(...)`, `ExistsForTenant(...)`
  (fail-closed when no tenant, `tenant.exempt` → `tenant_id IS NULL` semantics), called by services.
- Cache keys: `cache.Key(scope, namespace, args…)` → `tenant:1:branch:2:users:v3:<hash>`;
  version bump invalidates a namespace without scanning keys.
- Impersonation: `X-Acting-Tenant` / `X-Acting-Branch`; missing headers + write method → 403
  (`TENANT_CONTEXT_NOT_RESOLVED`); behavior identical to Laravel middleware.

### 7.4 Authentication (`internal/authn`)

- Table `personal_access_tokens` (Sanctum-compatible: tokenable_type/id, name, token, abilities,
  last_used_at, expires_at) **plus `device` column**; token stored as SHA-256 hex; wire format
  `{id}|{random40}` so lookup is O(1) by id and comparison is constant-time.
- **Engine contract:** all issuance/verification/revocation goes through `authn.Authenticator`
  (`Issue`, `Verify`, `Revoke(device)`, `RevokeAll`). Opaque DB tokens are the shipped default;
  a future JWT driver (short-lived access JWT + opaque refresh row, `golang-jwt/jwt/v5`) can
  replace it without touching middleware, handlers or services. Confirmed 2026-10-16 — see
  `specs/phase-2-identity-auth-tenancy-rbac.md`.
- Login flow (service `authn.Login`): lockout check (cache limiter `email|ip`, 5/min) → active +
  email-verified checks (configurable, `/api/v1/...` parity errors) → bcrypt compare → optional OTP
  (cache key `otp:user:{id}`, 6 digits, 5 min, never stored in DB — deviation from Laravel's
  `users.last_otp`, documented) → issue token with name `user_{id}_{device}` → `last_login_at`.
- Responses match §1.3.4 exactly (`AUTH_SUCCESS` / `OTP_REQUIRED`).
- Logout deletes the token row for the given device. Token TTL configurable; default 0 (never, parity).
- Middleware `Authenticate`: parse bearer → load token+user → check expiry/active → put
  `authn.Identity` in context → update `last_used_at` (throttled to once/minute).
- No register/forgot-password (parity, §14).

### 7.5 Authorization (`internal/authz`)

- Keep the DB schema names: `roles`, `permissions`, `model_has_roles`, `role_has_permissions`
  (spatie-compatible + `tenant_id`, `is_system`, `group`, `description`).
- Permission catalog in Go (`internal/authz/catalog.go`) as the single source, mirroring
  `config/permission-registry.php`; `permission:sync` upserts missing permissions; role seeding
  builds Super-Admin (all) + per-tenant Admin (module subset) as in `RoleService`.
- `Checker` interface: `Can(ctx, identity, perm, resource)`, `MustCan(...)` → `FORBIDDEN` problem;
  super-admin bypass; system-role protection rules ported (`cannot edit Super-Admin`, system
  roles/permissions read-only for tenants).
- Enforcement mirrors Laravel policy calls, but at the **service layer** so CLI/jobs are equally
  protected; route-level `RequirePermission("x.y")` middleware available for cheap route gating.
- Effective permission set cached per `(user, tenant)` with invalidation on role/permission changes
  (cache + versioned keys §7.8).
- `GET /permission/user` returns via roles like `getPermissionsViaRoles()`.

### 7.6 Database (`internal/platform/database`)

- **Named registry:** config lists connections (`primary`, optional `primary_read`, `analytics`,
  …); each has driver + DSN + pool settings; `Registry.Open(ctx)`, `Health(ctx)`, `Close()`.
  Repositories receive their handle explicitly at construction (no global `DB()`).
- **Transactions:** `database.TxManager.WithTx(ctx, func(q *dbgen.Queries) error {...})` using
  `sqlc`'s `WithTx`; multi-repo workflows share the tx-bound `Queries` (unit-of-work). Tx ownership
  is explicit in services — matching Laravel `DB::transaction` call sites.
- **Read/write splitting (optional, off by default):** adapter `database.Conn{Write, Read}`
  exposes `Queries(ctx)` (write) and `QueriesRO(ctx)`; only read-only list queries opt in.
  Transaction always uses the primary. Disabled unless `DATABASE_PRIMARY_READ_DSN` is set.
- **Migrations:** goose SQL files under `migrations` (embedded via `embed.FS`), applied by
  `ims migrate` (never on every replica boot). `migrate:status` reports applied/pending; the health
  check compares embedded vs applied versions. `--goose NO TRANSACTION` for concurrent indexes.
- **Soft deletes:** explicit `deleted_at IS NULL` in queries; partial unique indexes
  (`WHERE deleted_at IS NULL`) for parity with Laravel's soft-delete uniqueness.
- **Audit columns:** `created_by` / `updated_by` stamped by services from identity context.
- **sqlc config:** `sqlc.yaml` with `postgresql` engine (pgx/v5 output);
  generated code lives in `internal/dbgen` (never edited).
  Schema source = goose migrations (verify sqlc parses goose annotations in Phase 1).
- Sample sqlc naming convention: `ListUsers`, `GetUserByEmail`, `CreateUser`, ... (one queries file
  per table/module in `sql/queries/`).

### 7.7 Cache (`internal/platform/cache`)

```go
type Store interface {
    Get(ctx context.Context, key string, dest any) (found bool, err error) // JSON-decoded
    Set(ctx context.Context, key string, value any, ttl time.Duration) error
    Delete(ctx context.Context, key string) error
    Exists(ctx context.Context, key string) (bool, error)
    Clear(ctx context.Context) error
    Increment(ctx context.Context, key string, delta int64) (int64, error)
    TTL(ctx context.Context, key string) (time.Duration, error) // -1 = no expiry, -2 = missing
    Ping(ctx context.Context) error
}

func Remember[T any](ctx context.Context, s Store, key string, ttl time.Duration,
    fn func(ctx context.Context) (T, error)) (T, error) // + singleflight to prevent stampedes
```

- Drivers: `memory` (map + mutex + janitor; default for local/tests), `file` (JSON files under
  `APP_CACHE_DIR`), `redis` (go-redis v9), `database` (Laravel parity: `cache` table with
  key/value/expiration; useful for shared cache without Redis).
- Semantics documented per driver: TTL precision, `Clear` scope, concurrent `Increment`.
  No fake distributed guarantees (Go standard §14.3); failures surface to callers and are logged;
  cache is never authoritative (unread counters recompute from DB).
- **Versioned keys** (port of `CacheHelper`): primary invalidation strategy, works on every driver:
  `v = GetOrInit(namespace:version)`, key = `scope:namespace:v{n}:{hash(args)}`, `Bump(namespace)`.
- `cache:clear [--store=]` CLI uses the same factory.

### 7.8 Queue & jobs (`internal/platform/queue`)

```go
type Engine interface {
    Enqueue(ctx context.Context, kind string, payload any, opts EnqueueOptions) error
    StartWorkers(ctx context.Context, opts WorkerOptions) error // graceful: drain on ctx cancel
    EnqueueTx(ctx context.Context, tx pgx.Tx, kind string, payload any, opts EnqueueOptions) error // River bonus
}

type Definition struct {
    Kind        string        // "notification.fanout"
    Description string
    Queue       QueueName     // critical | default | heavy
    MaxRetries  int
    Timeout     time.Duration
    Validate    func([]byte) error
    Handle      func(ctx context.Context, payload []byte) error
}
```

- **Engines:** `sync` (goroutine/inline — tests, dev, CLI) and `river` (recommended default:
  PostgreSQL/SQLite, transactional enqueue, retries, unique jobs, scheduled jobs, web UI at
  `/observability/queue`). Asynq can be added later behind the same interface if Redis enters
  the stack.
- **Registry:** `queue.Registry` maps kind → Definition; duplicate/empty kinds fail at startup;
  handlers validate payloads, call services. Job handlers restore tenancy from the payload
  (jobs carry `tenant_id`/`branch_id`, like Laravel's `tenant()->resolve(...)` in `handle()`).
- Channels/tries/timeouts from `QUEUE_CRITICAL_*` etc. — same defaults as Laravel.
- Idempotency where it matters: notification deliveries use `ON CONFLICT DO NOTHING` on
  `(notification_id, user_id)` (port of `insertOrIgnore`).
- `DataProcessingJob` table ported as-is (status/counters/errors/file metadata + progress %),
  surfaced via `/v1/data-processing-jobs` and the export/import flows.
- CLI: `queue:work [--queue=critical,default,heavy] [--once]`, `queue:stats` (UI link).

### 7.9 Scheduler (`internal/platform/scheduler`)

```go
type Task struct {
    ID          string    // "billing.expire-subscriptions"
    Spec        string    // "0 */6 * * *"
    Description string
    Timezone    string
    Timeout     time.Duration
    Overlap     OverlapPolicy // skip | allow
    Enabled     func(*config.Config) bool
    Run         func(ctx context.Context) error
}
```

- Central registry assembled in the composition root from module contributions; validated at
  startup (spec parse, duplicate IDs). Runner = `robfig/cron/v3` in `ims schedule` mode
  (separate systemd unit; never in API replicas — Go standard §13.2).
- Ported schedules: `billing.expire-subscriptions` (every 6h), `billing.send-renewal-reminders`
  (daily 08:00), `dataprocessing.cleanup-completed` (daily 02:00), `health.run` (every minute),
  queue pruning. Also `schedule:list` and `schedule:run <id>` (one-shot invoke) commands.
- Handlers: context with timeout, structured start/end logs, errors → log + health heartbeat.

### 7.10 CLI (`internal/cli`)

Command tree (colon naming preserved from Artisan; `Makefile`/systemd invoke the same binary):

```text
ims serve                     # HTTP API
ims queue:work                # queue worker (separate unit)
ims schedule                  # scheduler daemon (separate unit)
ims migrate | migrate:status | migrate:rollback
ims permission:sync | settings:sync
ims billing:expire-subscriptions | billing:send-renewal-reminders
ims data-processing:cleanup-completed
ims cache:clear [--store=]
ims health:check [--fresh]
ims config:show
ims version | help
ims gen module …              # Phase 7 stretch (make:crud parity)
```

- Root flags: `--env-file`, `--verbose`; new commands register via the module `commands.go`
  files + `internal/cli/registry.go` (duplicate names rejected at startup).
- Dependencies: `cli.Deps` with lazy, typed providers (`Deps.DB(ctx)`, `Deps.Cache(ctx)`,
  `Deps.Queue(ctx)`, `Deps.Services(ctx)`) so `help`/`version` never touch infra.
- Exit codes: 0 ok, 1 error, 2 usage; errors printed to stderr (problem codes → readable text).
- Confirmation prompts for destructive commands (`migrate:rollback`, `cache:clear` production).

### 7.11 Billing module (`internal/modules/billing`)

- Entities/tables ported 1:1 (`packages`, `package_features`, `subscriptions` with `overrides`
  JSON, `payments`, `payment_approval_requests`) — int minor units, `BDT`.
- `features.go`: Go version of the `Feature` enum + `isLimit()`; `FeatureService` implements
  `Can/Require/Limit/WithinLimit/RequireWithinLimit` with package+override resolution and
  errors `FEATURE_NOT_AVAILABLE` / `USAGE_LIMIT_EXCEEDED` (402 problem type).
- Enforcement points preserved: user create (`users.max`), branch create (`branches.max`) —
  plus optional `RequireSubscription` route middleware (the Laravel `subscription` alias).
- `PaymentDriver` interface + `Result` structs; drivers: `bkash` (token cache 55 min, full HTTP
  flow), `nagad` (explicit not-implemented error), `cash`, `bank_transfer` (manual refs);
  `PaymentOrchestrator` registry; invoice numbers `INV-XXXX-YmdHis`; callbacks at
  `/v1/billing/{gateway}/callback` (public, `tenant.exempt`).
- Lifecycle service ports `startTrial/subscribe/renew/activate/cancel/handleGatewayCallback` and
  `nextExpiry` (monthly/yearly/one_time≈100 years); manual approvals: approve → activate,
  reject → cancel + fail payment, in a single DB transaction.
- `billing:expire-subscriptions` + `billing:send-renewal-reminders` (7/3/1 days) as scheduled
  tasks; reminder delivery goes through notifications (mail channel later).

### 7.12 Notifications module

- Tables ported: `notifications`, `notification_targets`, `notification_reads`,
  `notification_deliveries` (unique `(notification_id, user_id)`).
- Fan-out job: chunked (500) per-chunk transactions, `ON CONFLICT DO NOTHING`, cache counter
  increments after commit; target resolution SQL ported (USER/ROLE/BRANCH/TENANT/GLOBAL).
- Unread counters in `cache.Store` (`notif:{t}:u:{u}:unread`, `read_all_before` marker), with
  SQL recompute fallback when the cache is unavailable — counters are an optimization only.
- Feed: cursor pagination (created_at,id) merged with `read_all_before`; `markRead`,
  `markAllRead` (queued for large users), `unread_count` in `meta`.
- Admin CRUD with priority → queue routing (`CRITICAL` → `critical` queue) via `queue.Registry`.

### 7.13 Settings module

- `settings` table (`key`, `tenant_id` nullable = global, `value` JSON, timestamps) + typed
  registry in Go: key → type/default/validation/description (`system.name`,
  `notification.enabled`, `data_processing_job.cleanup_days` to start).
- `settings:sync` creates missing keys; `Get/Set` cached with versioned invalidation;
  tenant scoping follows effective scope (strict tenancy parity).

### 7.14 i18n

- **Library:** `nicksnyder/go-i18n/v2` (confirmed) — framework-independent, CLDR pluralization,
  template variables, BCP 47 locale matching. Catalogs are JSON, embedded via `go:embed` in
  `internal/platform/i18n/locales/{en,bn}.json`.
- **Keys:** Laravel dot-keys preserved as message IDs (`messages.login.success`,
  `validation.required`, `http-statuses.404`) — 1:1 mapping to `lang/en|bn`.
- **Placeholders:** ported from `:placeholder` to go-i18n template syntax (`{{.FeatureKey}}`);
  message text itself stays identical (documented deviation).
- **Plurals:** Laravel `trans_choice` / `|` forms map to go-i18n `PluralCount` CLDR entries.
- **Locale middleware:** `language.ParseAcceptLanguage` + bundle matcher → request-scoped
  `*i18n.Localizer` in context; validate against `APP_SUPPORTED_LOCALES`; fallback
  `APP_FALLBACK_LOCALE` (fixes Laravel's exact-tag `en-US` quirk via BCP 47 matching).
- **Validation messages:** one translation system — `httpx` maps validator FieldErrors to
  message IDs → go-i18n (no `universal-translator` dependency, raw tags never leak).
- **Workflow:** catalogs are maintained by porting `lang/*.php` (not extracted from Go source);
  `goi18n merge` + a `lang:check` parity command keep en/bn in sync.
- Translator used by problem titles/details, validation messages, and `messages.*` service output.

### 7.15 Files / storage

- `storage.Disk` interface (`Put/Get/Delete/Exists/URL/Size`) with `local`, `public`, `s3`
  (aws-sdk-go-v2) drivers; `APP_STORAGE_DIR` based, uploads never live inside release directories.
- Single `files` table (Spatie `media` + `uploads` merged — no package mimicry): the file
  library resource (`POST/GET/DELETE /v1/files`) plus polymorphic attachments (`owner_type` +
  `owner_id` + `collection`, enforced in services, collection registry with single/multi-file
  policies) covering the user avatar (`user`/`profile`) and future owners (supplier documents,
  product images, …). Image conversions deferred.

### 7.16 Import/export

- `excelize` services: user import (row validation, per-row error collection), user export with
  filters, import-template download — run through `queue` as jobs tracking into
  `data_processing_jobs` (PENDING → PROCESSING → COMPLETED/FAILED, counts, errors JSON).
- Files written under `APP_STORAGE_DIR/jobs/...` (private disk parity), download endpoint with
  ownership check. Cleanup scheduled task ported (`--days`, `--dry-run`).

### 7.17 Health, logging, observability

- `health.Registry` with checks: environment, debug-mode, DB (per connection), migrations current,
  cache ping, queue reachable, disk space, schedule heartbeat. `GET /api/health` returns
  `{finishedAt, checkResults:[{name,label,status,shortSummary,meta}]}` + `Cache-Control: no-store`;
  `?fresh=1` runs live. `GET /up` liveness. Result store: memory now, DB history optional.
- `slog`: JSON in production, text in dev; request/queue/schedule logs carry request id, user,
  tenant, duration, outcome; credentials/tokens never logged. `APP_LOG_DIR` optional file logs.
- Metrics (`/metrics`, Prometheus) and OTel tracing: optional Phase 6 additions.
- Queue UI: first-party queue admin in the developer dashboard (Phase 9) behind its auth gate
  (Horizon parity). No Telescope/Pulse — replaced by structured logs + queue UI + (optional)
  metrics; documented as a deliberate change.
- Forward-compat: HTTP middleware and pgx instrumentation expose capture hooks from Phase 0 so the
  developer dashboard (Phase 9, §7.21) attaches without restructuring the kernel.

### 7.18 OpenAPI & contract testing

- `api/openapi.yaml` is canonical: success envelopes, pagination shapes, problem schemas
  (with `errors` extension), security scheme (bearer), all routes.
- `oapi-codegen` generates DTO/param types (and a strict chi server interface where it doesn't
  fight the handwritten middleware chain); generated code verified by `git diff --exit-code` in CI.
- Docs UI: Scalar at `/docs`, `/docs/openapi.yaml` served from the embedded spec (Scribe parity).
- Contract tests: run representative requests against a real server and validate responses with
  `kin-openapi` (success + 422 + 401 + 404), including problem `content-type` and status match.

### 7.19 Rate limiting

- Cache-backed `ratelimit.Limiter` (`Increment` + TTL with atomic first-hit expiry) used by:
  global API throttle (`RATE_LIMIT_PER_MINUTE`, per user/IP), login lockout (5/min `email|ip`),
  OTP verify attempts. 429 problem + `Retry-After` header; limiter failures fail-open with logging.

### 7.20 Deployment (binary + systemd, no containers)

- Layout per Go standard §18: `/opt/ims/ims-go-api/{releases,current}`, `/etc/ims/ims-go-api/.env`,
  `/var/lib/ims/ims-go-api/` (uploads, exports), journald logging.
- Units: `ims-go-api.service` (serve), `-scheduler.service` (schedule), `-worker.service`
  (queue:work); templates in `deploy/systemd/`.
- `deploy/scripts/release.sh`: fetch artifact → verify checksum → new release dir → `migrate`
  (gated step) → switch symlink → restart units → health check; keeps N previous releases.
- Cross-compile (CGO_ENABLED=0) for linux/amd64 + linux/arm64 in CI.

### 7.21 Admin & Developer dashboards (new — Phases 8–9, after API completion)

Not present in the Laravel starter. Post-API phases; detail lives in `specs/phase-8-*` /
`specs/phase-9-*` — this section fixes only the architecture direction.

- **Serving:** React SPA(s) built by Vite, embedded with `go:embed`, served by the same binary.
  Layout: admin dashboard at `/`, developer dashboard at `/dev`, API at `/api/v1`, docs at
  `/docs`, health at `/health`. Hashed assets + long cache headers; SPA fallback for client
  routing. A future static/CDN split must not change the API.
- **Stack (confirmed):** pure React (no Next.js); TanStack Router / Query / Table / Form
  (+ Virtual where needed); zustand only for local UI state if TanStack patterns prove
  insufficient; TypeScript types generated from `api/openapi.yaml` so the SPA contract is
  CI-checked. API auth uses the same bearer tokens (Phase 8 spec decides token-vs-cookie mode).
- **Admin dashboard:** login/profile, users, roles & permissions, tenants & branches,
  packages/subscriptions/payments/approvals (super-admin areas), notifications, settings,
  data-processing jobs with progress, uploads.
- **Developer dashboard (lightweight Telescope parity):**
  - Request inspector: method/path/status/duration, request payload, headers (redacted),
    response body preview, user + tenant, request/correlation id.
  - SQL capture per request via pgx `QueryTracer` (statement, args, duration, connection, rows).
  - Cache operations (get/set/delete/hit-miss), dispatched/executed queue jobs, events.
  - Correlated `slog` capture per request (filterable by level), exceptions with debug detail.
  - Developer actions — whitelisted service-layer calls, never arbitrary shell: run a scheduled
    task, retry/replay a queue job, `cache:clear`, `permission:sync`, `settings:sync`, run/inspect
    health checks.
  - First-party queue admin built on the River client API (no River UI dependency); Scalar
    (API docs) and health are linked from the SPA.
- **Telemetry store:** DB tables + retention/pruning task (same pattern as `data_processing_jobs`);
  optional file/ring-buffer store; capture off by default in production with sampling and
  per-capture toggles (`DEV_DASHBOARD_*` env).
- **Security:** dedicated gate (super-admin or the seeded hidden `Developer` role/user holding
  all permissions) with the dashboard disabled by default in production; strict redaction of `Authorization`, cookies, tokens,
  passwords, OTPs and configured sensitive keys **before persistence**; CSP for the SPA; a test
  suite for the redactor.
- **Forward compat:** HTTP middleware and DB/cache instrumentation expose capture hooks from
  Phase 0 onward (§7.17) so these phases attach cleanly without kernel restructuring.

---

## 8. Data model inventory (non-CMS, ported)

| Table | Notes |
|---|---|
| `tenants` | soft delete, unique `code`/`slug`/`domain`, audit cols |
| `branches` | tenant-scoped, manager_id, soft delete, default-branch protection |
| `users` | tenant+branch, `active_branch_id`, verified at, `last_login_at`, `is_active` |
| `personal_access_tokens` | Sanctum-compatible + `device` column |
| `roles`, `permissions`, `model_has_roles`, `role_has_permissions` | spatie-compatible + `tenant_id`, `is_system`, `hidden`, `group`, `description` |
| `cache`, `cache_locks` (optional) | database cache driver parity |
| `settings` | typed registry, tenant nullable |
| `packages`, `package_features` | billing catalog |
| `subscriptions` | status lifecycle, `overrides` JSON |
| `payments` | gateway refs/status/response, invoice number, minor units |
| `payment_approval_requests` | approve/reject workflow |
| `data_processing_jobs` | import/export tracking + progress |
| `notifications`, `notification_targets`, `notification_reads`, `notification_deliveries` | fan-out model |
| `files` | file library + polymorphic attachments (avatar now; supplier docs etc. later); replaces Spatie `media` + `uploads` |
| `river_job*` | created by River migrations (queue engine) |
| `migrations` history | goose `goose_db_version` |

All tenant tables carry `tenant_id` (+ `branch_id` where BranchAware in Laravel) and index
`(tenant_id, …)`; unique constraints respect soft deletes via partial indexes.

---

## 9. API surface to port

| Group | Prefix | Middleware | Endpoints (representative) |
|---|---|---|---|
| Health | `/api/health`, `/up` | — | GET |
| Auth | `/api/v1` | public | POST `login`, `verify-otp`; (auth+tenant) POST `logout` |
| Users | `/api/v1` | auth+tenant.resolve | GET `user`, `user/profile`, `user/{id}`; POST `user`, `user/assign-role`, `user/change-password`, `user/profile/update`, `user/profile/change-password`, `user/profile/update-avatar`; PATCH `user/{id}`, `user/switch-branch/{id}`; DELETE `user/{id}` |
| Users (internal) | `/api/v1/internal` | auth+tenant.exempt | create/update internal users, import/export/template |
| Roles | `/api/v1` + `/internal` | auth+tenant.resolve / exempt | CRUD, `role/assign-permission` |
| Permissions | `/api/v1` | auth+tenant.exempt | GET/POST `permission`, DELETE `permission/{id}`, GET `permission/user` |
| Tenants | `/api/v1` | auth+tenant.exempt | CRUD (default-tenant protected) |
| Branches | `/api/v1` | auth+tenant.resolve | CRUD (default-branch protected) |
| Packages | `/api/v1` + public | auth / public | public GET `public/packages`; CRUD `packages` |
| Subscriptions | `/api/v1/billing` | auth+tenant.resolve | `subscribe`, `subscription/status`; internal admin ops |
| Payments | `/api/v1/billing` | auth+tenant.resolve / exempt | list; gateway callbacks; internal `manualRecord` |
| Approvals | `/api/v1/billing` internal | auth+tenant.exempt | list, approve, reject |
| Notifications | `/api/v1` | auth+tenant.resolve / exempt | feed, mark-read, mark-all-read, admin CRUD |
| Data processing | `/api/v1` | auth+tenant.resolve | list, show, download |
| Files | `/api/v1` | auth+tenant.exempt | CRUD (`/v1/files`; Laravel's `/v1/upload` renamed) |

Exact paths/methods will be transcribed from `routes/api/v1/*.php` during implementation; the
table above is the working inventory (verified route files map 1:1).

---

## 10. Testing strategy

| Layer | Tooling | Scope |
|---|---|---|
| Service unit | stdlib `testing`, hand fakes | business rules: auth flow, feature limits, lifecycle transitions, notification targeting |
| Handler | `httptest` + fakes | status codes, envelope, problem shapes, validation 422s |
| Tenancy isolation | integration (real Postgres) | cross-tenant read/write denial matrix per module |
| Repository | integration (real DB via `docker compose`) | SQL correctness, constraints, soft delete, tx rollback |
| Cache contract | shared suite per driver | miss/TTL/increment/clear/serialization parity |
| Queue | sync engine + River test helpers | idempotency, retry classification, tenant restore |
| CLI | golden output + temp env files | migrate, permission:sync, cache:clear, billing commands |
| Contract | `kin-openapi` against live server | success + error responses match spec |
| E2E smoke | binary + Postgres (+ River) | login → tenant ops → billing subscribe → notification feed |

CI (GitHub Actions): `gofmt -l`, `go vet ./...`, `golangci-lint run`, `go test ./...`,
`go test -race ./...`, `sqlc generate` diff check, `oapi-codegen` diff check, OpenAPI lint,
migrations up/down against Postgres, build matrix linux/amd64+arm64.
Integration tests gated on `DATABASE_TEST_DSN`; `docker compose -f tests/docker-compose.yml up`
provided for local/CI.

---

## 11. Roadmap

### Milestone 1 — Basic structure (Phase 0) ← start here

Deliverables:

1. Repo scaffolding: `go.mod` (module path per §14), `.gitignore`, `.env.example`, `Makefile`,
   `.golangci.yml`, `sqlc.yaml`, `tools/tools.go`, CI skeleton, initial `README.md`.
2. `internal/config` — typed config + env-file resolution + validation for the app section.
3. `internal/platform/logging` — slog setup (JSON prod / text local) + request IDs.
4. `internal/platform/httpx` — `BindJSON`, validator wiring + message mapping, `Problem` +
   `apperr` bridging, success/pagination helpers, middleware (RequestID, Recover, AccessLog,
   Timeout, RateLimit stub, Locale stub).
5. `internal/platform/health` — registry, `/up`, `GET /api/health` with live checks.
6. `internal/app` — wiring, routes, lifecycle (graceful shutdown, server timeouts).
7. `internal/cli` — cobra root (`--env-file`, version), registry, builtins (`version`,
   `config:show`, `health:check`), `serve` command; `schedule`/`queue:work` stubs with clear
   "not yet wired" errors.
8. `internal/modules/system` — reference module proving the whole pattern
   (route → handler → request DTO → service → response DTO → problem mapping) on
   `GET /api/v1/system/info`.
9. Container-free run: `go run ./cmd/app serve` boots and serves; `curl /api/health`.

Definition of done: `go build ./...`, `go vet ./...`, `golangci-lint run`, `go test ./...` all
green; server boots on Windows dev; unknown route → problem+json 404; panic → problem+json 500;
bad JSON → 400; oversize body → 413; sample validation → 422 `errors` map; graceful shutdown on
Ctrl-C; `help`/`version` work without DB/cache/queue.

### Phase 1 — Database foundation
DB registry (named connections, pgx pools), goose migrations embedded, sqlc wired
(verify goose-annotation parsing), `database.TxManager`, first real tables (tenants/branches/users/
RBAC, system tables), repository pattern reference (tenants module), DB + migration health checks,
integration test harness, `migrate`/`migrate:status`/`migrate:rollback` commands.

### Phase 2 — Identity: auth, tenancy, RBAC
Tenancy package (scope/context/middleware/cache keys/validation helpers), authn (tokens, login,
OTP, lockout, logout, device), authz (catalog, checker, super-admin, caching), identity module
(users CRUD + profile + avatar + switch-branch + internal variants), access module (roles,
permissions), tenants/branches modules, permission:sync, seeding (super admin, default tenant/
branch, roles), isolation integration tests.

### Phase 3 — Platform: cache, queue, scheduler, CLI depth
Cache interface + memory/file/redis/database drivers + versioned keys + contract tests;
rate limiter wired (global + login); queue engine (sync + River) + job registry + `queue:work`;
data_processing_jobs table + cleanup task; scheduler registry + `schedule` mode + `schedule:list`/
`schedule:run`; cache:clear; settings module (needed by cleanup task config).

### Phase 4 — Billing
Packages/features CRUD, feature guard + overrides, subscriptions lifecycle + routes, payments +
drivers (bkash HTTP, cash, bank transfer, nagad stub), orchestrator, approvals workflow, callbacks,
scheduled expiry/reminders, billing tests.

### Phase 5 — Notifications, i18n, uploads, import/export, mail
Notifications module (tables, fan-out, counters, feed, admin CRUD), i18n catalogs + locale
middleware + message port, files module + storage disks, import/export with excelize +
queued jobs + template, mail driver (log/smtp) for reminders/OTP.

### Phase 6 — Contract, observability, hardening
OpenAPI spec complete + oapi-codegen integration + CI diff gates + Scalar docs + contract tests;
optional Prometheus metrics + OTel; tenancy isolation test matrix (no RLS — explicit scoping is the
boundary) + security checklist pass; load smoke;
second cache driver matrix in CI.

### Phase 7 — Deployment + generators
systemd units + release script + CI cross-compile artifacts + runbook; `gen module` scaffolding
(`make:crud` parity, port the declarative `--fields` spec idea); docs/architecture.md finalized.

### Phase 8 — Admin dashboard (React)

Admin SPA in `web/admin` (Vite + TanStack + OpenAPI-generated TS types), embedded and served by
the binary at `/`. Covers the capability surface from Phases 2–5 (users, access, tenants,
billing, notifications, settings, data processing, uploads). Spec: `specs/phase-8-admin-dashboard.md`.

### Phase 9 — Developer dashboard (lightweight Telescope)

Go-side capture (HTTP middleware, pgx `QueryTracer`, cache/queue hooks, correlated logs) +
telemetry store with retention + TanStack Start SPA at `/dev` (request inspector, first-party
queue admin on the River client API, whitelisted developer actions, Scalar/health links). Access:
super-admin or the seeded hidden `Developer` role/user. Spec: `specs/phase-9-developer-dashboard.md`.

### Phase 10 — OpenAPI spec generator

Code-aware generator that writes CRUD paths + schemas into managed regions of the single committed
`api/openapi.yaml` (text splice, deterministic, never reformats hand-tuned content). `ims gen module`
emits the block on scaffold; `ims gen openapi --module|--all` analyzes an existing module
(`routes.go` + `request.go` via `go/ast`) and updates its block; `openapi:check` gains a
region-freshness gate. No fragments, no bundler, no new dependencies.
Spec: `specs/phase-10-openapi-generator.md`.

### Phase 11 — Settings API, groups & feature flags

REST surface for the code-defined settings catalog with exactly **two levels** — a global default
plus a per-tenant override (tenant admins may change values; internal users own globals) — exposed
grouped with `tenant → global → code-default` resolution. Storage uses a polymorphic
`target_type`/`target_id` so more targets can be enabled later without a migration. Settings groups
are data (`settings_groups` + `settings.group_id`) so the later developer panel can create groups
and reassign keys without a schema change. Feature flags get their own table (hot-path reads,
independent invalidation) with global/tenant/user targets, internal-only writes and a
`feature:<key>` middleware. `settings:sync` / `feature-flags:sync` seed globals; the API never
creates keys.
Spec: `specs/phase-11-settings-groups-feature-flags.md`.

---

## 12. Risks & mitigations

| Risk | Mitigation |
|---|---|
| Error contract change (problem+json everywhere) breaks existing clients | Documented migration note; shapes intentionally close (`message`→`detail`); frontends ours to update |
| Tenancy regressions in handwritten SQL | Mandatory explicit tenant params in every repo method; isolation test matrix per module; no RLS (evaluated and dropped — explicit scoping is the boundary) |
| River pre-1.0 API churn (v0.x) | Pin version; `queue.Engine` interface isolates engine; sync engine keeps tests independent |
| sqlc/goose parsing mismatch | Verified in Phase 1 before schema grows; fallback = keep `sql/schema` dump generated by goose for sqlc |
| Scope creep (CMS, generators, dashboards) | Explicit non-goals §1.2; generators deferred to Phase 7 |
| Windows dev vs Linux deploy differences (paths, signals, file locking) | Config-driven absolute paths; CI runs Linux tests; document Windows caveats (`make`, symlinks) |
| Never-expiring tokens (Sanctum parity) | Default parity with warning log; `AUTH_TOKEN_EXPIRATION` documented; recommend TTL in production |
| Cache `Increment`/`Clear` semantics differ per driver | Contract test suite per driver; documented guarantees; no distributed-lock promises |
| Large planning surface | Milestone 1 is deliberately small; each phase ships runnable value + tests |
| Developer dashboard exposure | Off by default in production, dedicated strong gate, redaction of secrets/tokens before persistence; redactor has its own test suite |
| SPA auth & XSS surface (bearer tokens in browser) | Phase 8 spec decides token vs httpOnly-cookie mode; strict CSP, no inline scripts; API stays the only data path |

---

## 13. Engineering conventions (for contributors)

1. Handlers thin; services reusable from HTTP/CLI/queue/scheduler.
2. SQL only in `sql/*/queries` + repository adapters; services never see sqlc types.
3. Request/response DTOs at boundaries; never expose generated rows.
4. All HTTP errors via `apperr` → problem; never `http.Error`.
5. Validation: structural in transport, business rules in services; no raw validator tags leaked.
6. One command registry, one scheduled-task registry, one job-handler registry; duplicate IDs fail fast.
7. Explicit tenant/actor params in repository signatures; no ambient query magic.
8. Transactions owned by services; `WithTx` wraps multi-repo units of work.
9. Cache through `cache.Store` + versioned keys; no direct redis client outside the driver.
10. Secrets only in env files outside the repo; `.env.example` always current.
11. `gofmt`/`go vet`/lint clean; sqlc/openapi generated code diff-clean in CI.
12. Document deviations from Laravel behavior in the PR (running list in `docs/architecture.md`).
13. Each roadmap phase starts with an accepted spec in `specs/` (§15); implementation follows the spec.
14. The developer dashboard is a debugging tool: never enabled in production without explicit gating, and its capture pipeline must redact credentials before persistence.
15. The Laravel starter at `K:\Projects\Herd\ims-cms-saas-laravel-api-starter` is the **reference implementation** for business logic and behavior — consult it while speccing and implementing each module (services under `app/Services/*`, `app/Billing/*`, middleware, routes, `lang/*`, `config/permission-registry.php`), and record deliberate deviations in the phase spec + `docs/architecture.md`.

---

## 14. Confirmed decisions

Confirmed 2026-10-16:

1. **Module path:** `github.com/Innovix-Matrix-Systems/ims-go-api-starter`.
2. **Queue engine v1:** **River** (Postgres/SQLite, transactional enqueue, no Redis) behind the
   `queue.Engine` interface; `sync` engine for dev/tests. Asynq remains a documented alternative
   if Redis is ever required.
3. **SQLite timing:** deferred — v1 is PostgreSQL-only; a future version may add a SQLite adapter.
4. **Milestone 1:** not started yet — PLAN.md under review first.
5. **UI (new, Phases 8–9 after the API):** pure React + Vite + TanStack (no Next.js), zustand only
   if needed, TypeScript types generated from OpenAPI; admin dashboard + developer dashboard
   (lightweight Telescope parity) served from the same binary.

Standing minor defaults (flag any disagreement, otherwise adopted during scaffolding):
int64 IDs (Laravel parity); token expiry default 0 (never, Sanctum parity); OTP cache-only
(no `users.last_otp` column); unified RFC 9457 problem+json for all errors; no public
register/forgot-password endpoints in v1; device stored as a token column instead of the
`user_{id}_{device}{id}` name quirk (behavior identical: per-device logout).

---

## 15. Specs workflow

`PLAN.md` is the architectural overview; each roadmap phase is specified in `specs/` before
implementation begins.

- Naming: `specs/phase-N-<slug>.md` (stubs exist for Phases 0–9, including the UI phases).
- A spec contains: goal, scope checklist, out of scope, contracts touched (DB/API/env), acceptance
  criteria, and open questions.
- A phase starts only when its spec is accepted; implementation PRs reference the spec file.
- Deviations discovered during implementation are recorded in the spec, then folded into
  `docs/architecture.md` once shipped.
- Phase 0 (`specs/phase-0-basic-structure.md`) is the first spec to review before scaffolding.
