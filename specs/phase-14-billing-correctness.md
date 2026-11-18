# Phase 14 — Billing correctness & atomicity (P1)

**Status:** Done (2026-11-19)
**Source:** [docs/codebase-review.md](../docs/codebase-review.md) §10 P1 item 10 — findings **H8–H11, H27, M8–M11, M14–M18**
**Depends on:** Phase 12 (production guards land first; billing is reachable from public routes)

## Goal

Make the billing state machine trustworthy. Today the public callback can fail a pending
payment without asking the gateway, manual-method references are guessable (cross-tenant,
unauthenticated tampering with pending payments), `POST /billing/subscribe` double-charges on
retry, and multi-statement state changes (activation + payment completion, feature
replacement) run outside transactions with no status-transition guards. This phase makes every
payment state change atomic, verified against the authoritative source, and idempotent.

## Reference (review)

- [docs/codebase-review.md](../docs/codebase-review.md) §6 (H8–H11, H27), §7 (M8–M11, M14–M18),
  §7d items 3–4, §10 item 10.

## Scope checklist

### Callback integrity (H8, H9, H10, H27, M17)

- [x] **H8:** never derive payment failure from the callback body — `HandleCallback` must
      re-query the gateway (as the success path already does) before any state change.
- [x] **H9:** reject callbacks for non-gateway methods (cash/bank) before touching payment
      state; manual-method references become high-entropy (`crypto/rand`) instead of
      `CASH-<unix-seconds>`.
- [x] **M17:** `payments.gateway_ref` becomes unique (partial unique index
      `WHERE gateway_ref IS NOT NULL`); generation is collision-free.
- [x] **H27:** index `payments (gateway_ref)` (with the unique index above) — the public
      callback resolves by it.
- [x] **H10:** `post` fails on non-2xx; map gateway 5xx to a retryable/indeterminate result so
      a bKash 5xx can never permanently fail a payment whose money was captured.

### Subscribe idempotency (H11)

- [x] Accept an `Idempotency-Key` header on `POST /billing/subscribe`; reject (or return the
      existing checkout for) a second in-flight checkout for the same tenant+package.

### Atomic state changes (M8–M11)

- [x] **M8:** status-transition predicates on `ActivateSubscription` and `UpdatePaymentStatus`
      (`AND status IN (...)`) so stale callbacks cannot reactivate cancelled subscriptions.
- [x] **M9:** subscription activation + payment completion run in one `WithTx`.
- [x] **M11:** `recordPayment` and `Activate` are sequenced inside the same transaction.
- [x] **M10:** `ReplaceFeatures` runs in a single transaction (delete + upserts).

### Guard & quota semantics (M14, M15)

- [x] **M14:** decide and implement cancel semantics — immediate revocation vs access-until-
      `expires_at` — and align the code, comment and `docs/architecture.md` §150-151.
- [x] **M15:** enforce `storage.mb` through the `LimitChecker` pattern (files module checks the
      quota; 402 problem with `meta{feature_key,limit,current}`).

### Gateway client hygiene (M16, M18)

- [x] **M16:** `crypto/rand` failure generating invoice numbers must error out (never fall
      back to a predictable/duplicate number).
- [x] **M18:** single-flight the bKash token cache; invalidate the cached token on an auth
      failure instead of retrying a dead token for up to 55 minutes.

## Implementation notes (2026-11-19)

- **H8:** `CallbackResult` gained `IsFailed`; bKash `HandleCallback` always asks bKash (execute on
  "success", payment-status otherwise). Success → complete; Failed/Cancelled → failed; anything else
  leaves the payment pending.
- **H9/M17/H27:** cash/bank callbacks are rejected (handler + service + driver). References are
  `CASH-`/`BANK-` + 24 hex chars from crypto/rand; the payer's bank reference moves into the stored
  response. Migration `00019_billing_integrity.sql` de-duplicates old refs and adds the unique
  partial index `payments_gateway_ref_uidx`.
- **H10/M18:** `post` returns a 503 `ServiceUnavailable` on any non-2xx and drops the cached token on
  401/403; token grants are serialized by a mutex with a re-check.
- **H11 (decision):** no `Idempotency-Key` store. Subscribe is idempotent by business key — a pending
  checkout for the same tenant + package + method is returned instead of charging again, and
  concurrent subscribes are serialized with `WithTenantLock("billing.subscribe")`. Simpler, and it
  also covers CLI/queue callers. A header store can be added later if a client needs it.
- **M8:** `ActivateSubscription` refuses `cancelled`; `UpdatePaymentStatus` only allows
  pending → any, failed → completed, completed → refunded (or a no-op repeat); violations are
  `INVALID_STATE` (409).
- **M9/M11:** `completeAndActivate` and `Activate` run in one transaction (tx-bound repositories).
- **M10:** package create/update + feature replacement share one transaction.
- **M14 (decision):** cancel is immediate; comment and `docs/architecture.md` §7 updated.
- **M15:** `files.Service.WithStorageLimit(guard)`; `Guard.RequireStorage` applies only when the plan
  or an override defines `storage.mb`, answering 402 `USAGE_LIMIT_EXCEEDED` with `meta`.
- **M16:** `GenerateInvoiceNumber`/`ManualReference` return errors instead of ignoring crypto/rand.
- Tests: `billing/drivers_test.go` (forged failure, in-progress, confirmed failure, 5xx, 401
  re-grant, manual refs) and `billing/service/billing_integrity_integration_test.go`.

## Out of scope

- The `subscription.*`/`payment.*` permission boundaries (Phase 12 covers the exempt-route
  catalog; billing admin routes are already `IsSystem`).
- Billing plan caching and per-check round trips (Phase 16, M13).
- New payment gateways / the nagad stub.

## Contracts touched

- **DB tables / migrations:** one migration — unique partial index on `payments.gateway_ref`
  (H27/M17); backfill/deduplicate any colliding `CASH-*` refs before applying.
- **API endpoints:** `POST /billing/subscribe` gains `Idempotency-Key`; callback behaviour
  changes (no body-derived failures); update the managed OpenAPI regions via
  `ims gen openapi --module=billing` or a manual entry outside the generated markers.
- **Env vars / config:** none expected (driver config unchanged).
- **CLI commands:** none.

## Acceptance criteria

- Callback tests: a `status != success` bKash callback does **not** change payment state
  unless the gateway re-query confirms it; a cash/bank callback is rejected outright; a
  crafted/guessed reference cannot mutate another tenant's pending payment.
- Gateway 5xx test: payment is not marked `failed`; result is retryable/indeterminate.
- Subscribe idempotency test: two rapid subscribes with the same `Idempotency-Key` (or for the
  same in-flight tenant+package) yield one checkout.
- Stale-callback test: `UpdatePaymentStatus`/`ActivateSubscription` on a cancelled
  subscription is a no-op.
- Crash-point test: a failure between payment write and activation leaves no completed payment
  without an active plan (transaction rollback).
- `ReplaceFeatures` failure leaves the previous feature set intact.
- A package advertising `storage.mb` blocks uploads past the limit with a 402 + feature meta.
- `make check` and the integration suite pass (billing drivers tested against `httptest`
  stubs per repo convention).

## Open questions

- Cancel semantics (M14): grace-period-to-`expires_at` (matches docs) vs immediate (current
  code) — pick one; affects renewal reminders.
- `Idempotency-Key` scope: header-only, or also accept a client-supplied idempotency field for
  non-HTTP callers (CLI/queue)?
- For bKash 5xx: surface 503 (retryable) with `retry_after`, or keep the payment `pending`
  and let the gateway reconcile?
