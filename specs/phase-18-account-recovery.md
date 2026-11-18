# Phase 18 — Account recovery: password reset & e-mail verification

**Status:** Done (2026-11-19)
**Source:** project-owner review (missing auth flows for a reusable API starter); closes review **M3**
**Depends on:** Phases 12–13 (uniform login responses, token revocation)

## Goal

Every API built on this starter needs "forgot password" and "verify your e-mail". Ship them the
Laravel way: a one-time token mailed as a link, no account enumeration, sessions revoked after a
password reset.

## Scope checklist

- [x] `POST /api/v1/forgot-password {email}` — always the same 200 message; mails a reset link to
      active accounts only; one mail per address per minute (plus the per-IP rate limit).
- [x] `POST /api/v1/reset-password {email, token, password, password_confirmation}` — single-use
      token bound to the e-mail; sets the password, marks the e-mail verified, revokes every token.
- [x] `POST /api/v1/email/verification-notification {email}` — public (unverified users cannot log
      in), same answer for every address, one mail per minute.
- [x] `POST /api/v1/email/verify {token}` — single-use token, sets `email_verified_at`.
- [x] **M3:** `AUTH_NEW_USERS_VERIFIED` (default `true`, the old behaviour). `false` creates
      admin/import users unverified and mails them a verification link.
- [x] Config: `AUTH_PASSWORD_RESET_EXPIRY` (60m), `AUTH_EMAIL_VERIFY_EXPIRY` (24h),
      `AUTH_PASSWORD_RESET_URL` / `AUTH_EMAIL_VERIFY_URL` templates (`{token}`, `{email}`; default to
      `APP_URL` pages).
- [x] Tests: `internal/authn/recovery_test.go` (full flows, single use, no enumeration, resend
      window); route-coverage allowlist; OpenAPI paths.

## Design notes

- `authn.Recovery` owns the flows; tokens are 32 random bytes, only their SHA-256 is cached
  (`pwreset:token:<hash>`, `emailverify:token:<hash>`) — no new table, same model as OTP codes.
  Issuing a new token deletes the previous one for that user.
- `authn.AccountWriter` (`SetPassword`, `MarkEmailVerified`) is implemented by the identity
  repository, keeping `authn` free of `identity`.
- Mail goes through the `mail.send` queue job; the dev dashboard shows those payloads redacted.
- With the `memory` cache driver, tokens live in the API process only — fine locally; deployed
  environments default to the shared `database` cache (Phase 15).

## Out of scope

- HTML mail templates (plain text today; `mail.Message` can grow an HTML part later).
- Phone verification (`AUTH_CHECK_PHONE_VERIFIED` stays an admin-set flag).
