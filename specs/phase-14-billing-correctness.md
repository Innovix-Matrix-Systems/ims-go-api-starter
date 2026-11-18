# Phase 14 — Billing correctness & atomicity (P1)

**Status:** Draft
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

- [ ] **H8:** never derive payment failure from the callback body — `HandleCallback` must
      re-query the gateway (as the success path already does) before any state change.
- [ ] **H9:** reject callbacks for non-gateway methods (cash/bank) before touching payment
      state; manual-method references become high-entropy (`crypto/rand`) instead of
      `CASH-<unix-seconds>`.
- [ ] **M17:** `payments.gateway_ref` becomes unique (partial unique index
      `WHERE gateway_ref IS NOT NULL`); generation is collision-free.
- [ ] **H27:** index `payments (gateway_ref)` (with the unique index above) — the public
      callback resolves by it.
- [ ] **H10:** `post` fails on non-2xx; map gateway 5xx to a retryable/indeterminate result so
      a bKash 5xx can never permanently fail a payment whose money was captured.

### Subscribe idempotency (H11)

- [ ] Accept an `Idempotency-Key` header on `POST /billing/subscribe`; reject (or return the
      existing checkout for) a second in-flight checkout for the same tenant+package.

### Atomic state changes (M8–M11)

- [ ] **M8:** status-transition predicates on `ActivateSubscription` and `UpdatePaymentStatus`
      (`AND status IN (...)`) so stale callbacks cannot reactivate cancelled subscriptions.
- [ ] **M9:** subscription activation + payment completion run in one `WithTx`.
- [ ] **M11:** `recordPayment` and `Activate` are sequenced inside the same transaction.
- [ ] **M10:** `ReplaceFeatures` runs in a single transaction (delete + upserts).

### Guard & quota semantics (M14, M15)

- [ ] **M14:** decide and implement cancel semantics — immediate revocation vs access-until-
      `expires_at` — and align the code, comment and `docs/architecture.md` §150-151.
- [ ] **M15:** enforce `storage.mb` through the `LimitChecker` pattern (files module checks the
      quota; 402 problem with `meta{feature_key,limit,current}`).

### Gateway client hygiene (M16, M18)

- [ ] **M16:** `crypto/rand` failure generating invoice numbers must error out (never fall
      back to a predictable/duplicate number).
- [ ] **M18:** single-flight the bKash token cache; invalidate the cached token on an auth
      failure instead of retrying a dead token for up to 55 minutes.

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
