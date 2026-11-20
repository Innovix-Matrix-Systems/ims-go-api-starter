---
title: Authentication
description: Opaque per-device tokens, login throttling, OTP, password reset and e-mail verification.
---

## Tokens

The API issues **opaque personal access tokens**, like Laravel Sanctum. A token looks like
`{id}|{secret}`; only a SHA-256 hash of the secret is stored, one row per device in
`personal_access_tokens`.

```http
POST /api/v1/login
Content-Type: application/json

{ "email": "admin@acme.test", "password": "secret123", "device": "iPhone 16" }
```

```json
{ "data": { "status": "AUTH_SUCCESS", "message": "…", "token": "42|q9…", "user": { "id": 7, "name": "…" } } }
```

With OTP enabled, `status` is `OTP_REQUIRED`, `token` is `null`, and the client completes the
login with `POST /api/v1/verify-otp`.

Send it as `Authorization: Bearer 42|q9…`. Tokens expire after `AUTH_TOKEN_EXPIRATION` minutes
(`0` = never, refused in production). `last_used_at` is touched at most once a minute.

| Endpoint | Does |
| --- | --- |
| `POST /api/v1/login` | e-mail + password (+ device) → token, or an OTP challenge |
| `POST /api/v1/verify-otp` | completes an OTP login |
| `POST /api/v1/logout` | revokes the current token |
| `GET /api/v1/user/profile` | the signed-in user |

The engine sits behind an `authn.Authenticator` interface (`Issue/Verify/Revoke/RevokeAll`), so a
JWT driver can be added without touching handlers.

## Login checks

- **Throttling**: `AUTH_LOGIN_MAX_ATTEMPTS` failures per e-mail + IP within
  `AUTH_LOGIN_DECAY_MINUTES` lock the login (cache-backed).
- **Active and verified**: `AUTH_CHECK_IS_ACTIVE`, `AUTH_CHECK_EMAIL_VERIFIED` (and phone) refuse
  inactive or unverified accounts.
- **OTP** (`AUTH_OTP_ENABLED`): a 6-digit code (`AUTH_OTP_LENGTH`) valid for
  `AUTH_OTP_EXPIRY_MINUTES`, stored only in the cache and delivered by mail through the queue.

## Password reset

```http
POST /api/v1/forgot-password   { "email": "admin@acme.test" }
POST /api/v1/reset-password    { "email": "…", "token": "…", "password": "…", "password_confirmation": "…" }
```

The mail links to `AUTH_PASSWORD_RESET_URL` (your frontend) with `{token}` and `{email}`
placeholders. The token is random, only its SHA-256 lives in the cache, it works **once** and
expires after `AUTH_PASSWORD_RESET_EXPIRY`. The new password needs at least 8 characters. Responses never reveal whether an e-mail exists. A
successful reset signs the user out **everywhere**.

## E-mail verification

```http
POST /api/v1/email/verification-notification   (re)sends the link
POST /api/v1/email/verify                      { "token": "…" }
```

With `AUTH_NEW_USERS_VERIFIED=false`, admin-created and imported users must verify before they
can sign in. Links use `AUTH_EMAIL_VERIFY_URL` and expire after `AUTH_EMAIL_VERIFY_EXPIRY`.

## Changing passwords

| Who | Endpoint | Effect |
| --- | --- | --- |
| the user | `POST /api/v1/user/profile/change-password` | needs the current password; signs out the user's **other** devices |
| a tenant admin | `POST /api/v1/user/change-password` | `user.update` |
| platform staff | `PATCH /api/v1/internal/user/change-password` | internal permission |

## Acting as a user

A holder of `user.impersonate` can send `X-Acting-User: <user id>` on tenant routes to make that
one request run as the user: their permissions, tenant and branch. No token is issued and the
user's sessions and `last_login_at` are untouched; stop by not sending the header. Profile,
password, avatar and branch-switch endpoints refuse with 403 `IMPERSONATION_FORBIDDEN` while
acting, and every request is written to the system audit log. Rules and refusals are in the
[tenancy guide](tenancy.md#how-a-request-is-resolved).

In code, `authn.IdentityFromContext(ctx)` is the target; `identity.Impersonated()` and
`identity.ImpersonatorID` tell you someone else is calling.

## In code

```go
userID := authn.UserID(r)                       // 0 when anonymous
identity, ok := authn.IdentityFromContext(ctx)  // id, name, email, tenant, branch
```
