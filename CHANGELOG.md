# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses
[Semantic Versioning](https://semver.org) (see `VERSION`).

## [Unreleased]

## [0.1.2] - 2026-11-21

Act as a user, a system audit log, public tenant routes, request telemetry that knows who made each
request, and `make rename` to turn the template into your own app.

**Upgrading from 0.1.0:** run `ims migrate` (migrations `00025` and `00026`), then
`ims permission:sync` and `ims db:seed` (idempotent; it gives the Developer role
`user.impersonate`). Internal accounts that read `GET /api/v1/activity` now need
`activity.view.all`. Code calling `tenancy.Exempt()` passes the responder:
`tenancy.Exempt(responder)`.

### Added

<!-- rename:keep -->
- **`make rename`.** Turns a project made from the template into your own app in one step:
  `make rename MODULE=github.com/acme/shop-api [NAME=] [BINARY=] [TITLE=] [DRY=1]` rewrites the
  module path, binary and CLI name, deploy names and paths, display title and `IMS_` deploy
  variables, renames the systemd units, and regenerates code and docs. Seeded e-mails and credit
  links are kept; it refuses uncommitted changes and a project that was already renamed.
<!-- rename:end -->
- **Act as a user.** `X-Acting-User: <user id>` runs one tenant request as that user (their
  permissions, tenant and branch), for holders of the new `user.impersonate` permission
  (Super-Admin, Developer, or any internal role it is assigned to). Admins, platform accounts and
  inactive users cannot be targets; profile, password, avatar and branch-switch changes are refused
  while acting (`IMPERSONATION_FORBIDDEN`).
- **System audit log.** Activity entries without a tenant are the system log. Internal users with
  the new `activity.view.all` read every tenant and the system log at `GET /api/v1/activity`,
  filtered with `tenant_id=<id>` or `tenant_id=system`, plus `impersonator_id` and `impersonated`.
- **Act-as audit trail.** `impersonation.started` (fail-closed), `impersonation.request` for every
  request and `impersonation.denied` are system entries. Entries written while acting carry the
  staff member (`impersonator_id`, `impersonator_name`); tenants see only `by_staff: true`.
- **Public tenant routes.** `tenancy.Public` resolves the tenant from `X-Tenant-Code` (and
  optionally `X-Branch-Id`) before sign-in; first endpoint `GET /api/v1/public/tenant`. Modules add
  their own with `RegisterPublicTenantRoutes`.

### Fixed

- **Request telemetry records who made the request.** Captured requests always had an empty
  `user_id` and `tenant_id`: telemetry read them from its own outer request context, which the
  authentication and tenancy layers never write to. They now fill an `httpx.RequestInfo` holder
  that telemetry and the access log read after the handler. Requests made while acting as a user
  also record `impersonator_id` (migration `00026`), shown as an "acting" chip in the dashboard;
  the access log gains `user_id`, `tenant_id` and `impersonator_id`.

- **Dashboard date filters use your calendar day.** The audit log and files pages sent the picked
  date as a bare `YYYY-MM-DD`, which the API reads as a UTC day, so near midnight (00:00-06:00 in
  UTC+6) "today" missed that night's entries. They now send the local day as RFC 3339 bounds with
  the browser's offset. The audit-log test built "today" from local time and failed in the same
  window; it now checks both forms.

### Changed

- Internal accounts without `activity.view.all` now get 403 on `GET /api/v1/activity` instead of
  every tenant's entries.
- `tenancy.Exempt` takes the responder and refuses `X-Acting-User` with 422.
- `CORS_ALLOWED_HEADERS` default includes `X-Acting-User`.

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

[Unreleased]: https://github.com/Innovix-Matrix-Systems/ims-go-api-starter/compare/v0.1.2...HEAD
[0.1.2]: https://github.com/Innovix-Matrix-Systems/ims-go-api-starter/compare/v0.1.0...v0.1.2
[0.1.0]: https://github.com/Innovix-Matrix-Systems/ims-go-api-starter/releases/tag/v0.1.0
