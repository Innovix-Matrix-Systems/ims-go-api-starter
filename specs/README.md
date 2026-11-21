# Specs

Per-phase specifications for the IMS Go API Starter.

`PLAN.md` (repo root) is the architectural overview — scope, stack, cross-cutting designs,
roadmap. This folder holds the implementation detail for each roadmap phase. A phase starts
only when its spec is accepted; implementation PRs reference the spec file.

**Reference implementation:** when behavior is ambiguous, the Laravel starter at
`K:\Projects\Herd\ims-cms-saas-laravel-api-starter` is the source of truth (PLAN.md §2, §13.15).
Every spec lists the exact Laravel files to consult for its phase.

## Status

| Phase | Spec | Status |
| --- | --- | --- |
| 0 | [phase-0-basic-structure.md](phase-0-basic-structure.md) | **Done** (2026-10-16) |
| 1 | [phase-1-database-foundation.md](phase-1-database-foundation.md) | **Done** (2026-10-16) |
| 2 | [phase-2-identity-auth-tenancy-rbac.md](phase-2-identity-auth-tenancy-rbac.md) | **Done** (2026-10-17) |
| 3 | [phase-3-platform-cache-queue-scheduler-cli.md](phase-3-platform-cache-queue-scheduler-cli.md) | **Done** (2026-10-17) |
| 4 | [phase-4-billing.md](phase-4-billing.md) | **Done** (2026-10-17) |
| 5 | [phase-5-notifications-i18n-uploads-import-export.md](phase-5-notifications-i18n-uploads-import-export.md) | **Done** (2026-10-17) |
| 6 | [phase-6-contract-observability-hardening.md](phase-6-contract-observability-hardening.md) | **Done** (2026-10-17) |
| 7 | [phase-7-deployment-generators.md](phase-7-deployment-generators.md) | **Done** (2026-10-17) |
| 8 | [phase-8-admin-dashboard.md](phase-8-admin-dashboard.md) | Stub (post-API) |
| 9 | [phase-9-developer-dashboard.md](phase-9-developer-dashboard.md) | In progress (A–J done; hardening remaining) |
| 10 | [phase-10-openapi-generator.md](phase-10-openapi-generator.md) | Done (2026-10-18) |
| 11 | [phase-11-settings-groups-feature-flags.md](phase-11-settings-groups-feature-flags.md) | **Done** (2026-10-18) |
| 12 | [phase-12-isolation-security-blockers.md](phase-12-isolation-security-blockers.md) | **Done** (2026-11-19, integration-verified) |
| 13 | [phase-13-authorization-data-integrity.md](phase-13-authorization-data-integrity.md) | **Done** (2026-11-19) |
| 14 | [phase-14-billing-correctness.md](phase-14-billing-correctness.md) | **Done** (2026-11-19) |
| 15 | [phase-15-platform-hardening.md](phase-15-platform-hardening.md) | **Done** (2026-11-19, some deferred) |
| 16 | [phase-16-performance-data-layer.md](phase-16-performance-data-layer.md) | **Done** (2026-11-19, some deferred) |
| 17 | [phase-17-product-ops-maturity.md](phase-17-product-ops-maturity.md) | **Done** (2026-11-19, some deferred) |
| 18 | [phase-18-account-recovery.md](phase-18-account-recovery.md) | **Done** (2026-11-19) |
| 19 | [phase-19-simplicity-audit-testkit.md](phase-19-simplicity-audit-testkit.md) | Done — handler helpers, config split, activity log, testkit, flag rollout, storage disks |
| 20 | [phase-20-dev-dashboard-finish.md](phase-20-dev-dashboard-finish.md) | Done — inspector, commands, form kit, settings view, dev service layer, tests on testkit |
| 21 | [phase-21-impersonation-public-tenant.md](phase-21-impersonation-public-tenant.md) | **Done** (2026-11-21) — act-as-user impersonation (`X-Acting-User`), system audit log (`activity.view.all`), public tenant resolution (`X-Tenant-Code`) |
| 22 | [phase-22-mcp-server.md](phase-22-mcp-server.md) | **Done** (2026-11-21) — MCP server as a connector: `/mcp`, built-in OAuth 2.1 with CIMD (no registration) and manually registered clients, tools dispatched through the router, five basic tools |
| 23 | [phase-23-social-login.md](phase-23-social-login.md) | Draft — sign in with Google, Microsoft, Apple, GitHub, Facebook (`golang.org/x/oauth2` + `go-oidc`); web redirect and mobile token flows; linking by verified e-mail |

Remediation phases 12–17 are derived from [docs/codebase-review.md](../docs/codebase-review.md)
(the full-codebase review at commit `1f55eeb`); each spec lists the finding IDs it resolves.

## Template

````md
# Phase N — <Name>

**Status:** Stub | Draft | Accepted | In progress | Done
**Roadmap:** PLAN.md §11
**Depends on:** Phase N-1 (and any phase the deliverables require)

## Goal
One paragraph: what value this phase ships and for whom.

## Reference (Laravel)
- Path(s) in the Laravel starter to consult for behavior parity.

## Scope checklist
- [ ] Deliverable 1
- [ ] Deliverable 2

## Out of scope
- Explicit exclusions for this phase.

## Contracts touched
- DB tables / migrations:
- API endpoints (see api/openapi.yaml):
- Env vars / config:
- CLI commands:

## Acceptance criteria
- Concrete, testable statements. Each maps to a test in CI where possible.

## Open questions
- Decisions needed before/during implementation.
````

## Process

1. Fill the stub (including the Laravel reference list), confirm open questions, set
   **Status: Accepted**.
2. Implement; keep the checklist updated in the PR.
3. Record deviations here, then fold them into `docs/architecture.md` once the phase ships.
