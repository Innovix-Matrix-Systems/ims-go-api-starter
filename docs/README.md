# Documentation

Start with the [README](../README.md) for the feature tour and quick start. This index links every
guide, runbook and reference in the repository.

## Understand the system

| Document | Use it for |
| --- | --- |
| [Architecture](architecture.md) | Request lifecycle, module layering, tenancy, data layer, platform services and deliberate deviations |
| [Developer guide](developer-guide.md) | Module anatomy, conventions and recipes: add a CRUD module, endpoint or field |
| [AGENTS.md](../AGENTS.md) | Working agreement for humans and AI agents: commands, patterns, database workflow |
| [OpenAPI contract](../api/openapi.yaml) | Canonical HTTP contract (paths, schemas, errors, security) |
| [Phase specs](../specs/README.md) | Accepted scope and implementation notes for each development phase |

## Run it

| Document | Use it for |
| --- | --- |
| [Local development](runbooks/local-development.md) | First run, three-process stack, hot reload, seeded accounts, troubleshooting |
| [Migrations](runbooks/migrations.md) | Creating/applying goose migrations and sqlc regeneration |
| [Rollback](runbooks/rollback.md) | Reverting migrations safely |
| [Deploy](runbooks/deploy.md) | systemd installation, release, configuration and operations |
| [Backups](runbooks/backups.md) | `pg_dump` archives, local + S3/R2 destinations, retention, monitoring and restore notes |

## Reference

| Resource | Where |
| --- | --- |
| Interactive API reference | `http://localhost:8080/docs` while the API is running |
| Liveness / health | `GET /up`, `GET /api/health` |
| Prometheus metrics | `GET /metrics` when `METRICS_ENABLED=true` |
| Developer dashboard | `http://localhost:8080/dev/` when `DEV_DASHBOARD_ENABLED=true` |
| Dashboard screenshot | [images/dev-dashboard.png](images/dev-dashboard.png) |
| API docs screenshot | [images/api-docs.png](images/api-docs.png) |
| Agent skills | [`.agents/skills/`](../.agents/skills) (`add-module`, `db-change`, `verify-changes`) |

## Documentation conventions

- Commands in guides assume the repository root.
- `make` targets are the supported entry points; the CLI equivalents are shown where useful.
- Operational runbooks are written for a single deployment (one scheduler, one or more queue
  workers) and use `bin/ims` after `make build-all`.
- When a change alters behavior described here, update the relevant guide (and the phase spec's
  implementation notes) in the same change.
