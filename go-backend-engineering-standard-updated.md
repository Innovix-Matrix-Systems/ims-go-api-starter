# Go Backend Engineering Standard

## Laravel-Inspired Architecture, Registries, Configuration, and Non-Container Deployment

**Status:** Proposed baseline  
**Architecture:** Feature-first modular monolith with explicit dependency injection  
**Databases:** PostgreSQL by default; SQLite for applications requiring offline/embedded operation  
**Persistence:** Explicit SQL, `pgx` and `sqlc`; ORM not required  
**HTTP:** `net/http` + Chi  
**CLI:** Cobra, with application-level command registration  
**API errors:** RFC 9457 Problem Details, including Laravel-style field validation errors  
**API contract:** OpenAPI 3  
**Cache:** Driver-independent application contract; optional in-memory, file and Redis implementations  
**Scheduling:** Central registry backed by `robfig/cron/v3`  
**Durable queue:** Optional; adopt when reliability requirements justify it  
**Deployment target:** Linux binary + `.env` + systemd, without requiring containers

---

# 1. Purpose and Design Goals

This document defines a consistent architecture and development workflow for Go backend applications. It is designed for engineers familiar with Laravel conventions: Form Requests, Controllers, Services, Repositories, DTOs, API Resources, service-container wiring, Artisan commands, database connections, queues, scheduled tasks, cache drivers, exception handling and automated testing.

The goal is to preserve the useful developer experience of Laravel while implementing it with idiomatic Go. The architecture should make a Go codebase immediately navigable to an engineer who already understands the Laravel application lifecycle.

The standard must provide:

- Predictable feature organization and an explicit request lifecycle.
- Shared request binding, validation, response mapping and API error formatting.
- A powerful extensible CLI with user-defined commands and access to application services.
- Shared services that can be invoked from HTTP, CLI, scheduler and job handlers.
- Multiple named database connections, with optional read/write pools.
- PostgreSQL support and SQLite support where offline deployment is required.
- Explicit SQL and typed generated query code without requiring an ORM.
- A replaceable cache backend behind a stable interface.
- One central registry for scheduled tasks and one registry for typed job handlers.
- OpenAPI documentation and contract verification.
- Unit, handler, repository integration, CLI, cache and end-to-end tests.
- Repeatable binary deployments using `.env`, stable data directories and systemd.

## 1.1 Non-goals

The architecture does not aim to reimplement Laravel as a framework, build a custom ORM, require Redis or a durable queue in every application, make every SQL query portable across all databases, or introduce microservices where a modular monolith is sufficient.

# 2. Architectural Principles

## 2.1 Standard API flow

The default API workflow is:

**Route → Handler → Request binding and validation → Service → Repository interface → Database adapter → Response DTO → JSON response.**

| Component | Responsibility |
|---|---|
| Router | HTTP methods, paths and middleware registration |
| Handler | HTTP transport, binding, service invocation and response writing |
| Request DTO | Request input shape and transport-level validation |
| Service | Business rules, workflow orchestration and transaction coordination |
| Repository interface | Persistence contract required by the service |
| Repository implementation | SQL execution and persistence mapping |
| Model/domain type | Application or persistence data representation |
| Response DTO | Public API representation |
| Problem mapper | Application errors to HTTP Problem Details |
| Application wiring | Dependency construction and lifecycle |

Not every small endpoint needs a separate file for each layer. Boundaries and responsibilities matter more than file count.

## 2.2 Dependency direction

Business logic must not depend directly on `http.Request`, `http.ResponseWriter`, Cobra command types, SQL driver types, Redis client types or queue-library task objects. Concrete implementations are constructed in the composition root and injected into consumers.

- Handlers depend on services.
- Services depend on application-level contracts.
- Repository implementations depend on SQL drivers and generated queries.
- CLI commands and scheduled/job handlers call services.
- Platform packages own technical concerns such as database connection pools, cache drivers, HTTP decoding and logging.

## 2.3 Explicitness over magic

Prefer typed structs, explicit constructors, small interfaces, explicit error returns, visible transaction boundaries and documented conventions. Avoid global service locators, hidden connection selection, unnecessary reflection, oversized interfaces and abstractions that obscure the call graph.

# 3. Technology Defaults

| Concern | Recommended default | Policy |
|---|---|---|
| HTTP server | `net/http` | Standard library |
| Routing | Chi | Lightweight router and middleware |
| CLI | `github.com/spf13/cobra` | Recommended for shared starter conventions |
| `.env` loading | `github.com/joho/godotenv` | Load an explicitly selected file without overriding existing environment variables |
| Request validation | `go-playground/validator` | Use through shared binder |
| Request binder | Internal `httpx` package | Shared JSON and validation behavior |
| API errors | RFC 9457 Problem Details | Required for HTTP API errors |
| Response mapping | Typed response DTOs | Avoid returning database rows/models by accident |
| API contract | OpenAPI 3 | Canonical public contract |
| API generation | `oapi-codegen` where useful | Generate selected pieces; avoid duplicated sources of truth |
| PostgreSQL | `pgx/v5` | Connection pool and driver |
| SQL generation | `sqlc` | Typed code generated from explicit SQL |
| SQLite | Compatible SQLite driver + SQL/query configuration | For offline or embedded applications |
| Migrations | Versioned SQL migration tool | Separate migration sets where schemas differ |
| Cache | Internal interface and factory | Driver optional by application |
| Logging | `log/slog` | Structured logs to stdout/stderr by default |
| Scheduler | `robfig/cron/v3` | One central registry |
| Durable queue | Optional, e.g. Asynq + Redis | Only when requirements need durable work |
| Testing | Standard `testing` package | Default test framework |
| HTTP tests | `net/http/httptest` | Handler and API integration tests |
| Format/static checks | `gofmt`, `go vet`, `staticcheck` or `golangci-lint` | CI quality gates |

Dependencies and versions are selected and pinned when implementing the starter repository. Do not add a dependency just to mimic the existence of a Laravel component if a small internal convention is sufficient.

# 4. Reference Repository Structure

```text
go-api/
├── cmd/
│   ├── api/                         # Optional dedicated API executable
│   │   └── main.go
│   └── app/                         # Default single-binary CLI entry point
│       └── main.go
├── internal/
│   ├── app/
│   │   ├── wiring.go                # Composition root
│   │   ├── routes.go
│   │   └── lifecycle.go
│   ├── cli/
│   │   ├── root.go
│   │   ├── dependencies.go
│   │   ├── registry.go
│   │   ├── builtins.go
│   │   └── commands/
│   ├── config/
│   │   └── config.go
│   ├── platform/
│   │   ├── database/
│   │   │   ├── config.go
│   │   │   ├── registry.go
│   │   │   ├── postgres.go
│   │   │   ├── sqlite.go
│   │   │   ├── transaction.go
│   │   │   └── health.go
│   │   ├── cache/
│   │   │   ├── cache.go
│   │   │   ├── config.go
│   │   │   ├── factory.go
│   │   │   ├── memory/
│   │   │   ├── file/
│   │   │   └── redis/
│   │   ├── httpx/
│   │   │   ├── decode.go
│   │   │   ├── validate.go
│   │   │   ├── problem.go
│   │   │   ├── errors.go
│   │   │   ├── response.go
│   │   │   └── middleware.go
│   │   └── logging/
│   ├── modules/
│   │   ├── orders/
│   │   │   ├── routes.go
│   │   │   ├── handler.go
│   │   │   ├── request.go
│   │   │   ├── response.go
│   │   │   ├── dto.go
│   │   │   ├── model.go
│   │   │   ├── service.go
│   │   │   ├── repository.go
│   │   │   ├── authorization.go
│   │   │   ├── commands.go              # Optional custom CLI commands
│   │   │   ├── scheduled_tasks.go        # Optional scheduled tasks
│   │   │   ├── job_handlers.go           # Optional typed job handlers
│   │   │   ├── handler_test.go
│   │   │   └── service_test.go
│   │   └── inventory/
│   ├── adapters/
│   │   ├── postgres/
│   │   └── sqlite/
│   └── dbgen/
│       ├── postgres/
│       └── sqlite/
├── sql/
│   ├── postgres/
│   │   ├── schema/
│   │   └── queries/
│   └── sqlite/
│       ├── schema/
│       └── queries/
├── migrations/
│   ├── postgres/
│   └── sqlite/
├── api/
│   └── openapi.yaml
├── tests/
│   ├── integration/
│   └── contract/
├── deploy/
│   ├── systemd/
│   └── scripts/
├── docs/
│   └── architecture.md
├── Makefile
├── .env.example
├── .gitignore
├── go.mod
├── go.sum
├── README.md
└── CONTRIBUTING.md
```

This is a reference layout, not a mandate to create empty files. Keep feature files together and add optional files only when the feature needs them.

## 4.1 Why `cmd/` and `internal/cli/` are distinct

`cmd/` contains Go executable entry points. `internal/cli/` contains the command tree, command registration and custom commands.

- `cmd/app/main.go` starts the CLI executable.
- `internal/cli/root.go` constructs the Cobra root command.
- `internal/cli/registry.go` registers built-in and feature-provided commands.
- `internal/app/wiring.go` constructs application dependencies.

This separation allows API, CLI, scheduler and worker entry points to reuse the same services and composition logic without making the application depend on Cobra.

# 5. Configuration and `.env`

The project prefers `.env` files for local development and for deployments where that operational model is desired. Production must not depend on the `.env` file being committed to the repository.

## 5.1 Loading policy

Use a typed configuration struct and validate it after loading. Use `github.com/joho/godotenv` to load `.env` in local development and for CLI commands invoked outside systemd. Use `github.com/spf13/cobra` for the command tree and command registration.

Use one production file outside the release directory, for example `/etc/ims/my-api/.env`. The same file is used in two ways:

- **systemd service:** systemd reads it through `EnvironmentFile=` and passes its values to the process.
- **Manual CLI invocation:** the application loads that same file through `godotenv.Load()` before reading application configuration.

Set `APP_ENV_FILE=/etc/ims/my-api/.env` in the systemd unit and allow the CLI to accept an explicit `--env-file` option if useful. Resolve the file path in this order: explicit CLI flag, `APP_ENV_FILE`, then a documented local-development default. Production commands should not depend on the current working directory.

Recommended precedence:

1. Environment variables already supplied by the operating system, systemd or the invoking shell win.
2. `.env` values fill variables that are not already set. `godotenv.Load()` has this behavior; do not use `godotenv.Overload()` for normal application startup.
3. Defaults apply only to optional settings.

Load dotenv values before constructing the typed configuration. Validate configuration for the selected command rather than requiring a database for commands such as `help` or `version`. Required production secrets, database settings and security-sensitive values must fail validation when absent. Never silently use development credentials in production. Never commit `.env`; commit `.env.example` with non-secret placeholders.

Keep the file syntax compatible with both the dotenv loader and systemd's `EnvironmentFile=` parser: use simple `KEY=value` assignments and do not rely on shell expansion, `export` statements or complex dotenv interpolation.

## 5.2 Path configuration

All writable paths must be explicit configuration, not inferred from the current working directory. Examples include:

- `APP_ENV`
- `APP_LISTEN_ADDR`
- `APP_DATA_DIR`
- `APP_STORAGE_DIR`
- `APP_CACHE_DIR`
- `APP_LOG_DIR` (only if file logs are enabled)
- `DATABASE_DRIVER`
- `DATABASE_URL` or named connection DSNs
- `DATABASE_PATH` for SQLite
- `CACHE_DRIVER`
- queue and scheduler settings where enabled

Prefer absolute paths in production. Use the process working directory only for developer convenience, not as the implicit location of the SQLite database, uploads, logs or secrets.

# 6. HTTP Routing and Handlers

Use Chi and register routes explicitly. Each module can expose `RegisterRoutes(router, handler)` or a similar function. Attach authentication, authorization and request middleware to the appropriate group.

Handlers must:

1. Decode and validate transport input.
2. Obtain authenticated identity/request context.
3. Map request DTOs to service input.
4. Call the relevant service.
5. Map application errors to HTTP responses.
6. Map success results to response DTOs.
7. Serialize the response.

Handlers must not perform SQL directly or duplicate business workflows.

# 7. Form Requests and Validation

Laravel Form Requests map to typed Go request structs and a shared binding/validation package.

```go
type CreateOrderRequest struct {
    CustomerID int64              `json:"customer_id" validate:"required,gt=0"`
    Items      []CreateOrderItem  `json:"items" validate:"required,min=1,dive"`
}

type CreateOrderItem struct {
    ProductID int64 `json:"product_id" validate:"required,gt=0"`
    Quantity  int   `json:"quantity" validate:"required,gt=0"`
}
```

`httpx.BindJSON` is a project-defined helper, not a standard Go API. It should handle JSON decoding, body-size limits, unsupported content types as appropriate, malformed/trailing JSON, struct validation and stable error mapping.

Structural validation belongs at the transport boundary. Business validation—tenant ownership, product state, inventory availability, workflow rules—belongs in services or domain-level validators. Database constraints remain necessary to protect against concurrent conflicts.

Go zero values do not always distinguish omitted from explicitly supplied zero values. Use pointer/nullable fields or a custom decoder where PATCH semantics require that distinction.

# 8. API Resources and Response DTOs

Use typed response DTOs and explicit mapping functions rather than exposing persistence models or generated SQL rows directly.

```go
type OrderResponse struct {
    ID         int64  `json:"id"`
    CustomerID int64  `json:"customer_id"`
    Status     string `json:"status"`
}

func NewOrderResponse(order *Order) OrderResponse {
    return OrderResponse{
        ID:         order.ID,
        CustomerID: order.CustomerID,
        Status:     order.Status,
    }
}
```

Define consistent response forms for resource collections, pagination, validation errors and other documented API behavior. Do not create a reflection-based resource framework without a demonstrated need.

# 9. RFC 9457 API Error Contract

All API error responses must use RFC 9457 Problem Details with `Content-Type: application/problem+json`. The standard fields are `type`, `title`, `status`, `detail` and, when available, `instance`. The `status` field must match the actual HTTP status.

## 9.1 Standard status mapping

| Condition | Status |
|---|---:|
| Malformed request JSON / invalid syntax | 400 |
| Missing or invalid authentication | 401 |
| Authenticated but not authorized | 403 |
| Resource not found | 404 |
| Conflict with current state or uniqueness | 409 |
| Semantically invalid fields | 422 |
| Unsupported media type | 415 |
| Request body too large | 413 |
| Rate limit exceeded | 429 |
| Unexpected internal error | 500 |
| Temporarily unavailable service/dependency | 503 |

Other HTTP statuses can be used when appropriate. Do not map every error to 400 or 500.

## 9.2 Example: internal server error

```json
{
  "type": "https://api.example.com/problems/internal-error",
  "title": "Internal Server Error",
  "status": 500,
  "detail": "An unexpected error occurred.",
  "instance": "/api/v1/orders"
}
```

For 500 responses, do not reveal SQL statements, driver errors, internal paths, stack traces, secrets or implementation details. Log the real error internally with correlation/request identifiers.

## 9.3 Example: validation problem

Validation errors use an application-defined `errors` extension while preserving the Problem Details envelope:

```json
{
  "type": "https://api.example.com/problems/validation-error",
  "title": "Validation Failed",
  "status": 422,
  "detail": "One or more fields failed validation.",
  "instance": "/api/v1/orders",
  "errors": {
    "customer_id": ["The customer id field is required."],
    "items.0.product_id": ["The product id must be greater than zero."],
    "items.1.quantity": ["The quantity must be at least 1."]
  }
}
```

The `errors` mapping uses field paths as keys and arrays of readable messages as values. Use dot notation for nested objects and array indices. Do not expose raw validator tags such as `gt`, `dive` or `required_without`.

Create reusable Problem Details and validation-problem schemas in OpenAPI. Test content type, status consistency, required fields and information-leak prevention.

## 9.4 Application errors versus HTTP errors

Services return application errors, not HTTP status codes. The HTTP boundary maps recognized errors to Problem Details. CLI commands map them to readable terminal errors and exit codes. Job handlers classify errors for retry decisions. One business error model is shared across entry points; each entry point renders it appropriately.

# 10. Service and Repository Layers

Services own business rules and workflow orchestration. They must be callable from HTTP, CLI, scheduler and queue handlers.

Repositories own persistence operations. Define small interfaces around service needs rather than generic repositories that expose arbitrary SQL construction.

Use explicit constructors and pass `context.Context` to operations that need cancellation or deadlines. Wrap errors with useful context. Do not store request context in long-lived service structs.

# 11. SQL, PostgreSQL, SQLite and Migrations

The default persistence strategy is explicit SQL, `pgx/v5` and `sqlc` for PostgreSQL. SQLite applications use a compatible driver and their own query/migration configuration where necessary.

The application/business layer should be database-agnostic; the SQL and migration layers may be database-specific. Avoid an ORM requirement solely to support multiple databases.

## 11.1 Multiple named connections

Support configured connection names such as `primary`, `analytics`, `reporting`, `external`, `primary_read` and `local`. Each connection has its own driver, DSN, credentials, pool limits and lifecycle.

The database registry opens and closes configured pools, performs startup checks and exposes handles by name to the composition root. Repositories receive the connection or generated query implementation they need; services must not look up arbitrary connections from global state.

## 11.2 Read/write splitting

Read/write routing is optional and disabled by default. Writes use the primary. Eligible reads may use a replica. Read-after-write operations and transactions use an explicit consistency policy. Do not infer all semantics from SQL text. Transactions remain bound to one connection/transaction handle.

## 11.3 Transactions and cross-database workflows

Transactions need explicit ownership. When several repositories participate, they must use the same transaction handle or unit-of-work boundary.

A local transaction does not make operations across separate databases atomic. For important secondary effects, use an outbox or other delivery strategy and idempotent consumers. Do not claim cross-database atomicity without implementing a mechanism that provides it.

## 11.4 PostgreSQL and SQLite differences

Portable SQL can be shared when it truly behaves equivalently. PostgreSQL-specific queries, types, indexes or features should remain explicit and need not be forced into SQLite. Keep separate migration sets when required. Run repository integration tests against the actual database engine being targeted.

SQLite files must reside in a stable data directory outside versioned releases. Give the process write permissions on the parent directory because SQLite may create WAL/journal/SHM files beside the database. Document SQLite concurrency, backup and migration behavior. For a single-file offline application, SQLite is a good fit; it is not automatically an appropriate shared database for distributed server instances.

# 12. CLI Architecture: Artisan-Like Extensibility

The CLI is a first-class application interface. It should support built-in and user-defined commands, arguments, flags, subcommands, generated help, interactive prompts, tables, progress reporting, database/cache access, logging, configuration, cancellation and predictable exit codes.

Cobra provides command structure, flags and help. The starter supplies application wiring, registration conventions and optional terminal presentation helpers.

## 12.1 One binary by default

Prefer one compiled application binary with subcommands:

```bash
my-api serve
my-api schedule
my-api queue:work
my-api migrate
my-api migrate:status
my-api inventory:reconcile
my-api reports:generate --date=2026-10-16
my-api cache:clear
```

The same binary contains the command tree and can be invoked by systemd, deployment scripts or an administrator. Separate API/worker executables can be introduced if there is a concrete packaging or dependency reason.

## 12.2 `cmd/` versus `internal/cli/`

`cmd/app/main.go` is the executable entry point. `internal/cli/` implements the Cobra root command, command registration and built-ins. `internal/app/wiring.go` constructs services and infrastructure. This lets other entry points reuse application services without depending on Cobra.

The root command should support an `--env-file` option (or an equivalent documented mechanism) and load configuration before constructing dependencies for the selected command. Keep dotenv loading in the configuration/bootstrap layer rather than duplicating it in every command. A command should receive typed configuration and dependencies, not read `.env` independently.

Recommended packages:

```bash
go get github.com/spf13/cobra github.com/joho/godotenv
```

Pin and maintain dependency versions through `go.mod` and `go.sum`.

## 12.3 Explicit dependencies

Custom commands must receive a dependency set or the specific services they need. The dependency set may expose configured databases/repositories, cache, application services, configuration, logger and external clients. Avoid a global service locator. A `help` command should not require a reachable database; construct only the dependencies needed by the selected command when practical.

## 12.4 Registry pattern

Use a registry or explicit module registration functions so feature modules can contribute commands. Validate duplicate names and command definitions during startup. Feature registration should happen in one visible composition point.

Conceptually, an application command definition can include name/use, short and long descriptions, arguments/flags, enabled state, and a constructor that receives dependencies and returns a `*cobra.Command`. Use `RunE`, return errors, and avoid calling `os.Exit` deep inside handlers so deferred cleanup can run.

## 12.5 CLI user experience

Standardize command conventions for:

- Argument and flag validation
- Generated help and examples
- Confirmation for destructive operations
- Machine-readable output where automation needs it
- Human-readable tables and progress for interactive tasks
- Logging and error display
- Exit codes
- Cancellation and graceful cleanup

Cobra does not provide every interactive UI feature itself. Add a small terminal UI dependency or internal helper only when needed; do not build a large framework without requirements.

## 12.6 Database and cache access

CLI commands use the same configured database registry, repositories, cache implementation and services as the API. They must not create unmanaged connections or duplicate business logic. Commands should open only what they need and close resources when done.

# 13. Registry Pattern for Scheduled Tasks and Jobs

The central registry pattern used in the IMS PocketBase BaaS Starter should be retained: each entry has a stable identifier, description, configuration/enabled state and handler registration. The Go implementation should use explicit dependencies and context-aware handlers.

## 13.1 Scheduled task registry

A scheduled task definition should contain:

- Stable ID
- Cron expression
- Human-readable description
- Enabled state derived from typed configuration/environment
- Handler or handler constructor receiving application dependencies
- Optional timeout and overlap policy

Feature modules can expose task registrations. The application composition root combines them and registers them in one scheduler registry. Validate expressions and duplicate IDs during startup; do not panic for ordinary configuration errors.

## 13.2 Scheduler process

Use one central scheduler backed by `robfig/cron/v3`. Keep scheduled function registration centralized while allowing the task implementation to live alongside its feature.

Recommended modes:

- `my-api serve`: HTTP only
- `my-api schedule`: run the central scheduler
- `my-api <task-name>`: invoke an explicitly registered task once, if that is a supported command convention

Run the scheduler as a separate systemd service when it is needed. Avoid starting the same scheduler inside every API replica. If high availability requires multiple schedulers, add leader election or distributed coordination. Define time zone, missed-run, overlap and shutdown behavior.

Scheduled handlers should accept a context, log start/end/duration, return errors, and use shared services. Avoid hidden global dependencies. Recovering a panic may prevent one task from crashing the scheduler, but panic recovery is not a substitute for error handling.

## 13.3 Job handler registry

Use a registry keyed by stable job type, inspired by the PocketBase starter's `JobRegistry`. Each handler validates its payload and calls an application service. Registration should reject duplicate or empty types.

Keep two concepts separate:

1. **Handler registry:** maps job types to code that can execute them.
2. **Queue backend/producer/worker:** defines how tasks are submitted, stored, reserved, retried and dispatched.

A registry alone does not make a queue durable. In-process goroutines or bounded worker pools are suitable when losing work on process exit is acceptable. A durable queue such as Asynq is optional when tasks must survive restarts, retry automatically or be processed by multiple instances. If using a database-backed queue, reservation, lease expiry, atomic claiming, retry/backoff and duplicate execution need deliberate design.

Jobs should be idempotent where possible, have timeouts, carry small serializable payloads, and avoid holding transactions over unrelated network calls. Use an outbox for critical after-commit delivery gaps.

# 14. Cache Abstraction

Provide a driver-independent cache interface to make switching between in-memory, file and Redis implementations possible without rewriting services. The interface must define behavior, not pretend all drivers have identical operational guarantees.

## 14.1 Suggested structure

```text
internal/platform/cache/
├── cache.go
├── config.go
├── factory.go
├── memory/
├── file/
└── redis/
```

## 14.2 Initial contract

Start with the smallest needed contract, for example:

```go
type Store interface {
    Get(ctx context.Context, key string, dest any) (found bool, err error)
    Set(ctx context.Context, key string, value any, ttl time.Duration) error
    Delete(ctx context.Context, key string) error
    Exists(ctx context.Context, key string) (bool, error)
    Clear(ctx context.Context) error
}
```

This is a proposed project contract, not a Go standard. Define serialization, key naming, TTL behavior, miss-vs-error semantics, clear scope and concurrency guarantees before implementation.

Add `Remember`, atomic increment, cache tags or distributed locks only when a concrete use case requires them. Distributed locks must not be emulated with a naive file cache. A file cache is not a cross-instance cache if instances do not share the same filesystem and locking semantics.

## 14.3 Cache failure policy

Cache must not be authoritative for business state. Optional cache read failure may fall back to the source of truth if that is safe, but errors should be observable. Do not silently discard cache errors in every case.

## 14.4 CLI integration

Commands such as `cache:clear` should use the same cache factory/registry as the API and support a selected store only if the configured implementation supports that operation.

# 15. API Documentation

OpenAPI 3 is the canonical public API contract. Define routes, request/response schemas, authentication, error responses and pagination. Use `oapi-codegen` for code that reduces duplication, and check generation/validation in CI.

The specification must document RFC 9457 Problem Details and the `errors` validation extension. Contract tests verify that actual responses match the documented schema. Documentation is not a substitute for runtime request validation.

# 16. Authentication and Authorization

Authentication middleware establishes the caller identity and relevant request context. Authorization must be enforced in a way that is shared by HTTP and non-HTTP workflows where applicable. Multi-tenant ownership must not be enforced only in the UI or middleware; service and repository behavior must prevent cross-tenant access.

Select authentication technology based on application needs rather than mechanically reproducing a Laravel package's internal implementation.

# 17. Logging and Observability

Use structured logging (`log/slog` or the selected project logger). Include request/correlation ID, module/operation, task/job ID, connection name where appropriate, duration, outcome and error category. Never log credentials, access tokens or sensitive request payloads.

Default production logging should write to stdout/stderr so systemd can capture it in journald. Application-managed files are optional and require explicit retention/rotation. Metrics/tracing can use OpenTelemetry when justified, but observability dependencies should remain configurable.

# 18. Non-Container Linux Deployment

## 18.1 Deployment model

Build a Linux binary in CI, upload it as a versioned release, apply migrations in a controlled step, switch the current release, and restart systemd. Do not require Docker or a container runtime.

Use a dedicated non-root service account. Keep executable releases immutable, configuration outside releases and writable state in stable directories.

## 18.2 Filesystem layout

Recommended per-application layout:

```text
/opt/ims/my-api/
├── releases/
│   ├── 20261016-120000/
│   │   └── my-api
│   └── 20261016-143000/
│       └── my-api
└── current -> releases/20261016-143000/

/etc/ims/my-api/
└── .env

/var/lib/ims/my-api/
├── app.db              # Only if SQLite is enabled
├── uploads/
├── exports/
└── state/

/var/cache/ims/my-api/  # Optional cache files, if this is the chosen policy

/var/log/ims/my-api/    # Optional application-managed file logs only

/etc/systemd/system/
├── ims-my-api.service
├── ims-my-api-scheduler.service
└── ims-my-api-worker.service  # Only if a worker is used
```

`/opt` is a suitable location for add-on application software. `/etc` is for host-specific configuration, `/var/lib` for persistent variable state and `/var/log` for logs. Exact path choices may be adapted to the host's packaging standards.

Do not place an application's active SQLite database, uploads or mutable state inside `releases/`. Do not deploy the whole application into `/usr/bin`. A stable launcher under `/usr/local/bin` is optional, but systemd and scripts can invoke `/opt/ims/my-api/current/my-api` directly.

## 18.3 Binary layout

Prefer a single compiled binary with Cobra subcommands:

```bash
/opt/ims/my-api/current/my-api serve
/opt/ims/my-api/current/my-api schedule
/opt/ims/my-api/current/my-api queue:work
/opt/ims/my-api/current/my-api migrate
/opt/ims/my-api/current/my-api orders:reconcile
```

The API, scheduler, optional worker and administrative commands use the same codebase and command definitions. Different systemd units invoke different commands. Separate executables can be introduced if there is a concrete reason.

## 18.4 `.env` deployment and CLI configuration

Store production `.env` outside the release, at `/etc/ims/my-api/.env`. Set restrictive permissions and give the application account only the required read access. Never overwrite `.env` during a binary deployment; configuration changes are a separate, deliberate operation.

Use the same file for the systemd-managed API and for manually executed CLI commands:

- systemd loads the file with `EnvironmentFile=/etc/ims/my-api/.env`.
- The unit sets `Environment=APP_ENV_FILE=/etc/ims/my-api/.env`.
- The application bootstrap uses `godotenv.Load()` with that path when a command is run manually. Existing environment variables are preserved, so systemd/shell values take precedence.
- CLI commands read typed configuration through the shared config package. They must not each load their own dotenv file.

For example, a manual command can run as the service account:

```bash
sudo -u ims-api \
  /opt/ims/my-api/current/my-api migrate
```

The CLI bootstrap loads the configured `.env` before validating the configuration required by `migrate`. `help` and `version` should not need a live database connection. An explicit `--env-file` flag is useful for staging, tests or multiple installations; document the precedence between that flag and `APP_ENV_FILE`.

Use `github.com/joho/godotenv` for application-side loading and `github.com/spf13/cobra` for the CLI. Do not use `godotenv.Overload()` for normal startup because it replaces existing process environment values.

Keep values compatible with both dotenv and systemd `EnvironmentFile=` parsing: simple `KEY=value` assignments, without shell expansion, `export` statements or complex interpolation. Validate this behavior in the target Linux environment. The service and production CLI commands must fail clearly when required configuration is missing.

## 18.5 Example systemd API unit

```ini
[Unit]
Description=IMS My API
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=ims-api
Group=ims-api
WorkingDirectory=/var/lib/ims/my-api
EnvironmentFile=/etc/ims/my-api/.env
Environment=APP_ENV_FILE=/etc/ims/my-api/.env
ExecStart=/opt/ims/my-api/current/my-api serve
Restart=on-failure
RestartSec=3
TimeoutStopSec=30

NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/lib/ims/my-api

[Install]
WantedBy=multi-user.target
```

This is a starting example, not a universal hardened unit. Adapt writable paths for file cache, exports, uploads and any other legitimate writes. Validate behavior with the actual application and distribution. `WorkingDirectory` must not be used as an implicit database/config path; use absolute configured paths.

## 18.6 Scheduler and worker units

If recurring jobs are enabled, install a separate `ims-my-api-scheduler.service` that runs:

```ini
EnvironmentFile=/etc/ims/my-api/.env
Environment=APP_ENV_FILE=/etc/ims/my-api/.env
ExecStart=/opt/ims/my-api/current/my-api schedule
```

If a durable queue worker is used, install a separate worker unit that runs the chosen worker command. Do not enable a scheduler/worker unit if the application does not need it.

One scheduler must coordinate each set of schedules unless duplicates are intentional. If running multiple scheduler replicas, implement leader election or distributed locking. Do not simply start the same in-process cron registry inside every API replica.

## 18.7 SQLite specifics

Configure SQLite with an absolute persistent path:

```dotenv
DATABASE_DRIVER=sqlite
DATABASE_PATH=/var/lib/ims/my-api/app.db
```

The service user needs write permission to the parent directory because SQLite may create journal, WAL and shared-memory files. Backups must account for the chosen SQLite journal mode; copying only the main `.db` file while writes are active may not produce a valid backup.

For SQLite-backed deployments, start with a controlled release procedure: stop the service, back up the database, apply compatible migrations, switch the release and restart. Avoid running incompatible old and new application versions concurrently against the same SQLite file.

## 18.8 Logging

Default to structured logs on stdout/stderr and use `journalctl`:

```bash
journalctl -u ims-my-api.service -f
journalctl -u ims-my-api.service --since today
```

This avoids creating an application-specific file-rotation problem. Use `/var/log/ims/my-api/` only if there is a specific need for files, and document the rotation, retention and permissions.

# 19. Deployment Workflow and Rollback

A deployment script should be deterministic and fail fast.

Recommended sequence:

1. Download the versioned artifact from CI.
2. Verify the checksum/signature where supported.
3. Create a new release directory.
4. Install the binary and immutable assets with correct ownership/permissions.
5. Run non-destructive preflight checks.
6. Stop the API and any required scheduler/worker processes if the migration or schema change requires it.
7. Back up data where required, especially for SQLite.
8. Apply migrations once, through the same CLI binary and the intended database configuration.
9. Atomically switch the `current` symlink to the new release.
10. Restart the relevant systemd units.
11. Check service health and recent logs.
12. Retain a small number of previous releases for rollback.

The exact ordering depends on backward compatibility between the new code and schema. For PostgreSQL, prefer expand/contract migrations if old and new versions could overlap. For SQLite, use a more conservative stop-backup-migrate-restart workflow when schema compatibility is uncertain.

Rollback must distinguish code rollback from database rollback. Switching the binary back does not safely reverse a migration. Prefer forward-compatible migrations and documented recovery procedures; restore a database backup only under an explicit recovery plan.

# 20. Testing Strategy

| Test type | Purpose | Dependencies |
|---|---|---|
| Service unit test | Business rules and error paths | Fake repositories/services |
| Handler test | HTTP status, validation and JSON contract | `httptest` and controlled dependencies |
| CLI command test | Arguments, output and error handling | Injected dependencies and output writers |
| Cache contract test | Miss, TTL, serialization, errors | Each supported cache adapter |
| Repository integration test | SQL, constraints and transactions | Actual target database |
| API contract test | Compliance with OpenAPI | Integrated API or response samples |
| Queue integration test | Dispatch/retry/idempotency | Queue test environment where applicable |
| End-to-end test | Full application workflow | Representative services and database |

PostgreSQL-backed applications must run integration tests against PostgreSQL. SQLite is suitable for applications that actually use SQLite and for fast tests only when the behavior under test is compatible. It is not a universal substitute for PostgreSQL tests.

The CI baseline includes:

```bash
gofmt -w .
go vet ./...
go test ./...
go test -race ./...
```

Add lint, OpenAPI validation, generated-code verification and database integration jobs as separate checks.

# 21. Application Lifecycle and Shutdown

The composition root constructs only the dependencies required by the selected mode. For example, `help` should not require a database connection; a migration command requires a database but does not need to start the HTTP server; the scheduler does not need to bind the API listener.

Long-running modes must handle termination signals and shut down gracefully:

1. Stop accepting new HTTP requests or scheduled tasks.
2. Cancel or drain in-flight operations according to policy.
3. Stop queue workers and release resources.
4. Flush logs/telemetry where applicable.
5. Close database pools and external clients.

Avoid `os.Exit` deep inside handlers or command implementations because it prevents deferred cleanup. Let the executable entry point determine the final exit code.

# 22. Engineering Conventions

1. Keep handlers thin and services reusable.
2. Keep SQL in repository adapters.
3. Use explicit request/response DTOs at public API boundaries.
4. Format all HTTP errors according to RFC 9457.
5. Use a stable Laravel-style `errors` mapping for validation failures.
6. Keep CLI commands declarative, testable and dependent on injected services.
7. Use one visible command registry and one visible scheduled-task registry.
8. Keep the job-handler registry separate from the queue backend.
9. Use named database connections and explicit repository injection.
10. Keep transaction ownership visible.
11. Expose cache through a small documented interface; do not promise unsupported distributed semantics.
12. Keep secrets/configuration outside versioned releases.
13. Keep SQLite databases and all mutable state outside release directories.
14. Prefer journald to application-managed log files by default under systemd.
15. Make deployment and rollback steps explicit and automated.
16. Test each supported database/cache backend against its stated contract.
17. Use `godotenv.Load()` for standalone CLI configuration and preserve existing process environment values.
18. Use one shared production `.env` path for systemd and CLI; keep dotenv loading in bootstrap/configuration code.
19. Document architectural exceptions in the pull request or module documentation.

# 23. Implementation Roadmap

## Phase 1 — Application foundation

- Create the Go module and folder structure.
- Add Chi, Cobra, typed configuration and application wiring.
- Define lifecycle and shutdown handling.
- Implement request binding, validation and RFC 9457 error handling.
- Establish structured logging and response DTO conventions.

## Phase 2 — PostgreSQL persistence

- Implement named connection registry using `pgx`.
- Configure `sqlc` and versioned SQL migrations.
- Define repository interfaces and one reference repository.
- Implement transaction coordination.
- Add PostgreSQL integration tests.

## Phase 3 — Developer CLI

- Add command registry and module registration.
- Add typed flags, help, output and progress conventions.
- Implement representative commands for migration, health and a real feature.
- Ensure commands use the same database/cache/services as the API.
- Add command tests.

## Phase 4 — API contract and reliability

- Add OpenAPI 3 as the canonical contract.
- Define Problem Details and validation schemas.
- Add contract tests and CI generation checks.

## Phase 5 — Cache and scheduling

- Define cache semantics and interface.
- Add only the cache drivers the application needs.
- Implement central schedule registration and a scheduler command.
- Define registration, timeout, overlap, timezone and shutdown policies.

## Phase 6 — SQLite and optional queue

- Add SQLite adapter and migrations for offline applications.
- Add SQLite integration tests and backup/concurrency documentation.
- Add a queue backend only for durable work requirements.
- Reuse typed job-handler registration regardless of queue backend.

## Phase 7 — Deployment automation

- Build platform-specific binaries in CI.
- Add systemd unit templates and deployment scripts.
- Deploy releases under `/opt`, configuration under `/etc`, state under `/var/lib` and optional file logs under `/var/log`.
- Add health checks, migration policy, release retention and rollback procedure.
- Test fresh installation, upgrade and rollback on a disposable VPS/VM.

# 24. Acceptance Criteria

The starter architecture is ready when:

- A developer can add a feature using the documented request/handler/service/repository/response pattern.
- API errors and validation errors are consistent and tested.
- OpenAPI describes success, validation and operational errors.
- Custom CLI commands can be registered without duplicating business logic.
- CLI commands can access configured services, database and cache dependencies.
- Help/version commands do not require unnecessary infrastructure.
- Multiple named database connections can be wired explicitly.
- PostgreSQL and SQLite adapters can implement shared application contracts where required.
- Cache drivers can be selected through configuration without rewriting service code.
- Scheduled tasks are registered centrally and do not accidentally execute once per API replica.
- Queue handler registration is separate from the persistence/delivery mechanism.
- Unit, HTTP, CLI, cache and database tests have clearly defined responsibilities.
- A release can be deployed without overwriting `.env`, SQLite files, uploads or durable state.
- systemd runs the application under a dedicated unprivileged account and logs are accessible through journald.
- Shutdown and rollback behavior are documented and tested.

# Final Architectural Decision

Adopt a Laravel-inspired Go architecture with a feature-first module layout, explicit dependency injection, typed request validation, response DTOs, RFC 9457 Problem Details, OpenAPI, Cobra, central registries for commands and scheduled tasks, and optional typed job handlers.

Use explicit SQL and repository adapters instead of requiring an ORM. Support named connections, PostgreSQL by default and SQLite for real offline use cases. Provide a small cache interface with honest guarantees across implementations.

Deploy as a versioned Linux binary managed by systemd. Keep configuration outside releases, durable data outside releases, use journald by default and allow the same binary to serve HTTP traffic or run administrative commands. Add scheduler and worker systemd units only when the application uses those modes.

The guiding principle is to preserve the useful Laravel developer experience while making dependencies, data flow, failure handling and deployment behavior explicit.
