# Phase 4 — Billing

**Status:** Done (2026-10-17)
**Roadmap:** PLAN.md §11 Phase 4
**Depends on:** Phases 2–3

## Goal

Full SaaS billing parity: packages with feature flags/limits and per-tenant overrides,
subscription lifecycle, payments with pluggable gateway drivers (bkash/cash/bank_transfer/nagad
stub), gateway callbacks, the manual approval workflow, feature-limit enforcement on tenant
resources, and scheduled expiry/reminders.

## Reference (Laravel)

- `app/Billing/*` — `Feature`, `FeatureGuard`, `Contracts/PaymentDriverInterface`, `Drivers/*`,
  `Results/*`, `PaymentOrchestrator`
- `app/Services/Billing/*` — subscription lifecycle, payments, approvals
- `routes/api/v1/{Package,Subscription,Payment,PaymentApproval}Routes.php` — route inventory
- `app/Console/Commands/{ExpireSubscriptions,SendRenewalReminders}Command.php`

## Decisions (locked)

1. **Money** is `int64` minor units (poisha), currency `BDT`; gateway amounts formatted 2dp.
2. **Feature catalog** in Go (`users.max`, `branches.max`, `patients.manage`, `appointments.manage`,
   `invoices.manage`, `reports.basic`, `reports.advanced`, `reports.export`, `sms.notifications`,
   `api.access`, `audit.log`, `custom.branding`, `storage.mb`); limit features: users.max,
   branches.max, storage.mb. Resolution: tenant overrides > package features; missing feature =
   denied; `limit` = -1 unlimited, 0 blocked.
3. **402 problem extensions:** feature failures map to `FEATURE_NOT_AVAILABLE` /
   `USAGE_LIMIT_EXCEEDED` / `SUBSCRIPTION_REQUIRED` (all HTTP 402) with `feature_key`, `limit`,
   `current` exposed via the problem `meta` extension.
4. **Enforcement points** wired now: user creation (`users.max`) and branch creation
   (`branches.max`) via a small `LimitChecker` interface implemented by the billing guard.
5. **Drivers:** `bkash` (token cache 55 min, checkout create/status/execute/refund),
   `nagad` (explicit not-implemented), `cash` + `bank_transfer` (manual refs + notes, awaiting
   approval). Registration in the orchestrator; missing gateway config surfaces as
   `SERVICE_UNAVAILABLE` when that driver is used.
6. **Approval workflow** is transactional: approve → subscription active + payment completed;
   reject → subscription cancelled + payment failed.
7. **Subscriptions:** `trial|pending|active|past_due|cancelled|expired`; `nextExpiry` monthly +
   1 month, yearly + 1 year, one_time + 100 years; `subscribe` deactivates previous actives only
   after a successful new subscription creation is validated (same transaction semantics as the
   reference: validation, then create, then charge, then manual-approval row when needed).
8. **Routes:** public `GET /v1/public/packages`; callbacks `GET /v1/billing/{gateway}/callback`
   (tenant-exempt); tenant group: `GET /v1/billing/subscription/status`,
   `GET /v1/billing/payments`; system management under `/v1/packages` and `/v1/billing/internal/*`
   (tenant-exempt, super-admin permissions). Tenant members may read their own subscription
   status; everything else requires the system billing permissions.
9. **Scheduled tasks:** `billing.expire-subscriptions` (`0 */6 * * *`),
   `billing.send-renewal-reminders` (`0 8 * * *`, logs reminders at 7/3/1 days — delivery channel
   arrives with notifications in Phase 5). CLI equivalents exist for one-off runs.
10. **Per-tenant overrides** are a JSON object on subscriptions; super-admin only
    (`subscription.override`).

## Scope checklist

- [x] config: billing section (currency, bkash/nagad/bank settings) + validation
- [x] migrations: packages, package_features, subscriptions, payments,
      payment_approval_requests + sqlc queries
- [x] feature guard (catalog, resolution, 402 errors) + enforcement in users/branches creation
- [x] payment orchestrator + drivers (bkash HTTP, cash, bank transfer, nagad stub)
- [x] subscription service (trial, subscribe, renew, activate, cancel, expiry, reminders,
      overrides, status)
- [x] payment service (manual record, list, mark failed) + callback handling
- [x] payment approval service (list, approve, reject)
- [x] package service + public endpoint + CRUD with feature definitions
- [x] routes/handlers per route inventory + permission gates
- [x] scheduled tasks + CLI commands (`billing:expire-subscriptions`,
      `billing:send-renewal-reminders`)
- [x] integration tests: lifecycle (subscribe→approve→active→expire), guard limits/overrides,
      enforcement 402s

## Out of scope

- Real Nagad integration; refunds UI; multi-currency; invoice PDFs; mail/SMS delivery
  (Phase 5 notifications); per-tenant billing dashboards (Phase 8).

## Contracts touched

- DB: `migrations/00009_create_billing_tables.sql`,
  `sql/queries/billing.sql`, `internal/dbgen`
- Env: `BILLING_CURRENCY`, `BKASH_*`, `NAGAD_*`, `BANK_TRANSFER_*`
- API: `/v1/public/packages`, `/v1/billing/*`, `/v1/packages*`
- CLI: `billing:expire-subscriptions`, `billing:send-renewal-reminders`

## Acceptance criteria

- Subscribing with cash/bank transfer creates a pending payment + pending approval; approving
  activates the subscription; rejecting cancels it and fails the payment
- Subscription status/expiry transitions are correct; expiry task marks overdue subscriptions
- Feature guard: limits and overrides enforced; exceeding `users.max` returns 402 with
  `feature_key`/`limit`/`current`
- bKash driver is exercised against a stub HTTP server (token caching + initiate + callback)
- `go test -race ./...` green with and without `DATABASE_TEST_DSN`; lint/gofmt clean

## Implementation notes

- **Live verification:** super admin created a package, cash-subscribe produced a pending
  subscription + pending payment + pending approval; approving activated the subscription;
  enforcement returned `402 USAGE_LIMIT_EXCEEDED` with `meta {feature_key, limit, current}` when
  creating a second user under a `users.max=1` plan; the public packages endpoint served the
  active package unauthenticated.
- **Subscribe ordering (improvement over the reference):** the new pending subscription is created
  and charged first; on charge failure it is cancelled, and previously active subscriptions are
  deactivated only after a successful charge. This avoids leaving a tenant without a plan when a
  gateway fails, while activation still replaces old subscriptions.
- **Gateway failure semantics:** a driver returning `success=false` marks the payment `failed` and
  returns `503 SERVICE_UNAVAILABLE`; unconfigured bKash returns 503 without creating requests.
- **Approval workflow is transactional** via `TxManager`: approve → payment completed + old
  subscriptions deactivated + new one activated; reject → payment failed + subscription cancelled.
- **Enforcement wiring:** `users.max` (identity create) and `branches.max` (branch create) call the
  billing guard through small `LimitChecker` interfaces (no module coupling); system/internal
  creates without a tenant skip limits.
- **Test-suite note (local machine only):** `go test -race ./...` needed `-p 2` because the Go
  compiler crashed under the memory pressure of ~20 parallel race builds; individual packages and
  CI are unaffected.
- Covered by tests: lifecycle (subscribe → approve → active → expiry), reject flow, overrides,
  limits/402 metadata, renewal reminders, bKash driver against a stub HTTP server (token caching +
  initiate + verify + callback), and the identity e2e with a trial subscription.

## Open questions

None outstanding — decisions 1–10 resolved; the notes above are recorded for
`docs/architecture.md`.
