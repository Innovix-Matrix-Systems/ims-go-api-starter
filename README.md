# IMS Go API Starter

![Version](https://img.shields.io/badge/version-0.1.2-ff5c1a)
![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white)
![PostgreSQL](https://img.shields.io/badge/PostgreSQL-16-4169E1?logo=postgresql&logoColor=white)

**Laravel's developer experience, Go's performance.** A multi-tenant API starter that ships as
**one CGO-free binary**: it serves the API, runs migrations, queue workers and the scheduler, and
embeds a Telescope-style **developer dashboard**. No containers required, Docker-ready when you want it.

| Developer dashboard (`/dev`) | API reference (`/docs`) |
| --- | --- |
| ![Developer dashboard](docs/images/dev-dashboard.png) | ![OpenAPI reference](docs/images/api-docs.png) |

## Why

- **Reads like Laravel.** Thin handlers (`h.Bind`, `h.Data`), permissions on routes
  (`r.With(h.can("product.create"))`), services, repositories, one config file per section — and
  `ims gen module` scaffolds the whole stack.
- **Multi-tenant and safe by default.** Tenant + branch isolation in SQL (fails closed), RBAC,
  impersonation, per-tenant plan limits, and a test that fails any route without a permission.
- **Everything an API needs.** Auth with OTP, password reset and e-mail verification · billing
  with feature limits (bKash, cash, bank) · settings · feature flags with percentage rollout ·
  activity log · notifications · files on local/S3/R2/MinIO · import/export · backups ·
  River queue · cron scheduler · RFC 9457 errors · OpenAPI.
- **A dashboard you will actually use.** Requests, logs, exceptions, queue, one-click commands,
  an inspector (health, routes, roles, config), tenants, settings, flags, files, sessions and an audit log —
  Ctrl+K to anything.
- **Fast feedback.** Type-safe SQL (sqlc), integration tests in ~10 lines (`testkit`), hot reload,
  and AI-agent skills (`.agents/skills`) that know the codebase.

## Quick start

<!-- rename:keep -->
Building a product on it? Press **Use this template** on GitHub to get a repository of your own
(clean history, not a fork), clone it, and give it your names in one step:

```bash
make rename MODULE=github.com/acme/shop-api BINARY=shop TITLE="Acme Shop"   # DRY=1 to preview
git add -A && git commit -m "chore: rename the starter to shop-api"
```

That rewrites the Go module path, the binary and CLI name, the deploy names and paths, the
display title and the `IMS_` deploy variables, renames the systemd units, and regenerates code and
docs. Seeded e-mails and the credits are left alone.
<!-- rename:end -->

```bash
cp .env.example .env          # local configuration (git-ignored)
make tools                    # air + golangci-lint (once)
npm --prefix web/dev ci       # dashboard dependencies (once)
make db-up                    # Postgres via docker compose — or point DATABASE_DSN at your own
make migrate seed             # schema, then permissions, tenant, roles, admin + developer accounts
make dev                      # dashboard on :8080/dev + API with hot reload
```

Open **<http://localhost:8080/dev/>** and sign in as `superadmin@ims.com` or `developer@ims.com`
(password `123456`, set by `AUTH_DEFAULT_PASSWORD`); the API reference is at **/docs**. Run
`make queue` and `make schedule` in two more shells for background jobs and cron tasks.

Prefer containers? `docker compose up -d --build` runs Postgres, the API, the worker and the
scheduler from one image (see [Docker](docs/operations/docker.md)).

## Build a feature

```bash
go run ./cmd/app gen module --name=product --fields="name:string,price:decimal,is_active:bool"
```

You get the migration, queries, model, repository, service, request DTOs, handler, routes with
permissions, a unit test and the OpenAPI entry. Wire it in `internal/app`, add the permissions to
`internal/authz/catalog.go`, and test it like a Laravel feature test:

```go
app := testkit.New(t)
admin := app.User(t)
app.As(admin).Post("/api/v1/product", `{"name":"Pen","price":"10"}`).Status(t, http.StatusCreated)
```

## Commands

```bash
make help               # every target
make check              # gofmt + vet + lint + unit tests
make test-integration   # full suite (needs DATABASE_TEST_DSN)
make generate           # sqlc + OpenAPI code + generated regions
make build-all          # dashboard + bin/ims
```

The binary is the CLI too: `ims serve | queue:work | schedule | migrate | db:seed |
permission:sync | cache:clear | backup:run | gen module | ...` — and the common maintenance
commands also run from the dashboard.

## Learn more

**📖 Documentation: [ims-go-api-starter.innovixmatrixsystem.workers.dev](https://ims-go-api-starter.innovixmatrixsystem.workers.dev)**

The pages live in [`docs/`](docs/README.md) and are published as that site (Astro Starlight,
`website/`) with every release. Run `make docs` to read them locally.

| | |
| --- | --- |
| [Quick start](docs/getting-started/quick-start.mdx) · [How it works](docs/getting-started/how-it-works.mdx) | Run it, then understand the one rule every request follows |
| [Tutorial: your first module](docs/tutorial/first-module.mdx) | Build a products module step by step |
| [Guides](docs/guides/modules.md) | Modules, handlers, database, tenancy, auth, testing, billing, queue, storage, … |
| [Operations](docs/operations/deploy.md) · [Docker](docs/operations/docker.md) | Local development, deploy (systemd or Docker), migrations, backups, rollback, releases |
| [Design decisions](docs/architecture/decisions.md) | What we chose, and why |
| [AGENTS.md](AGENTS.md) | Working guide for humans and AI agents |

Requires PostgreSQL 16+, Go 1.26.11+ and Node.js 20.19+ (dashboard). Every setting lives in `.env`
(documented in `.env.example`; `ims config:show` prints the effective values with secrets masked).

## Who's behind it

Built and maintained by **[Innovix Matrix System](https://innovixmatrixsystem.com)**, a software
studio in Dhaka, Bangladesh. We run our own products on it. More open-source starters:
[PocketBase BaaS](https://github.com/Innovix-Matrix-Systems/ims-pocketbase-baas-starter) ·
[NestJS API](https://github.com/Innovix-Matrix-Systems/ims-nest-api-starter) ·
[Laravel API](https://github.com/Innovix-Matrix-Systems/ims-laravel-api-starter) ·
[Laravel admin](https://github.com/Innovix-Matrix-Systems/ims-laravel-admin-starter).
Need a team to build on it? [Get in touch](https://innovixmatrixsystem.com).
