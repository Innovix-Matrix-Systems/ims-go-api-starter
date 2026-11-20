# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses
[Semantic Versioning](https://semver.org) (see `VERSION`).

## [Unreleased]

### Added

- **Documentation site at [ims-go-api-starter.innovixmatrixsystem.com](https://ims-go-api-starter.innovixmatrixsystem.com).**
  - Innovix Matrix System credited in the hero, a footer on every page, the home page and the
    README.
  - The search dialog suggests pages and example searches before you type.

## [0.1.0] - 2026-11-20

The first versioned release: the full starter, its developer dashboard and its documentation site.

### Added

- **Versioning.** `VERSION` holds the release (`0.1.0`); release builds stamp it, local builds report
  `0.1.0-dev`, and a test keeps it valid semver.
- **Documentation site.** An Astro Starlight site in `website/` renders `docs/`: getting started, a
  step-by-step tutorial, guides for every feature, the dashboard tour, operations runbooks,
  architecture and design decisions. Published to Cloudflare Workers when a GitHub Release is
  published.
- **Docker support.**
  - What ships: a multi-stage `Dockerfile`, one image that runs every role, and a `compose.yaml`
    that runs PostgreSQL, the migration step, the API, the worker and the scheduler.
  - Optional: systemd stays the reference deploy.
  - CI builds the image and boots the stack on every push.
- **Generated reference pages.** `ims gen docs` renders the CLI, configuration, permissions and Make
  references from the code; `make gen-check` fails when they are stale.
- **Core API.** Multi-tenant tenants and branches with explicit SQL scoping that fails closed;
  opaque per-device tokens with OTP, throttling, password reset and e-mail verification; a
  permission catalog with roles per tenant and route-level checks; RFC 9457 errors; OpenAPI as the
  enforced contract.
- **Billing.** Packages with features and limits, subscriptions, bKash/cash/bank payments with staff
  approvals, staff plan assignment and per-tenant overrides (a `null` override resets to the plan).
- **Platform.** Settings and feature flags (with stable percentage rollout), activity log,
  notifications, files on local/S3/R2/MinIO disks, import/export jobs, backups to S3-compatible
  storage, River queue, a cron scheduler, mail, English and Bangla catalogs, health checks and
  Prometheus metrics.
- **Developer dashboard** at `/dev`: Telescope-style requests, logs and exceptions, queue, backups,
  commands, an inspector (system health, routes, roles, config, packages), audit log, tenants,
  sessions, settings, flags, files and developers, with a Ctrl+K palette.
- **Developer experience.** `ims gen module`, `testkit` integration tests, hot reload, and AI-agent
  skills in `.agents/skills`.

### Fixed

- Subscribe and the user/branch quota checks no longer deadlock under concurrent requests: inside the
  per-tenant lock every query runs on the locked transaction.
- A failed backup run reports `destinations: []` instead of `null`.

[Unreleased]: https://github.com/Innovix-Matrix-Systems/ims-go-api-starter/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/Innovix-Matrix-Systems/ims-go-api-starter/releases/tag/v0.1.0
