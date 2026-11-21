# Phase 23 — Social login (Google, Microsoft, Apple, GitHub, Facebook)

**Status:** Draft
**Source:** project-owner request (2026-11-21): "as we introduce OAuth anyway, we should also add
support for normal login OAuth, like user able to log in with their Google account or Facebook
account".
**Depends on:** Phase 2 (tokens, login rules), Phase 18 (account recovery), Phase 22 (the OAuth
sign-in page, which gains the same buttons)

## Goal

A user can sign in with an account they already have at Google, Microsoft, Apple, GitHub or
Facebook instead of a password, and get the same API token a password login gives. It works for:

- **web front ends**, with a redirect through the provider;
- **mobile apps**, which already use the provider's own SDK (Google Sign-In, Sign in with Apple)
  and send the API what the SDK returned;
- **the MCP sign-in page** (phase 22), which gets "Continue with Google" and the others next to
  the password form.

Providers are switched on by configuration; each product picks its own. Adding another provider
later is one small file.

## Two different things called OAuth

| | Phase 22 (built) | Phase 23 (this spec) |
| --- | --- | --- |
| Our role | the **authorization server**: we issue tokens to AI apps | a **client** of Google, Apple…: they vouch for who the user is |
| Who signs in where | a user signs in **on our** page for an AI app | a user signs in **on Google's** page for our API |
| Result | an MCP access token for `/mcp` | our normal session token (`id|secret`) |

They share nothing but the vocabulary, plus one join: the phase-22 sign-in page can use phase-23
providers.

## Background (researched 2026-11-21)

### Go libraries

| Library | Who | Latest | What it does | Verdict |
| --- | --- | --- | --- | --- |
| [`golang.org/x/oauth2`](https://pkg.go.dev/golang.org/x/oauth2) | **the Go team** (golang.org/x is part of the Go project) | v0.38.0 | the OAuth 2.0 client: `Config.AuthCodeURL`, `Exchange`, PKCE (`GenerateVerifier`, `S256ChallengeOption`, `VerifierOption`), and `endpoints.Google`, `.Microsoft`, `.Apple`, `.GitHub`, `.Facebook`, `.LinkedIn`, `.Discord` and about 50 more | **use**: the closest thing to official support; already in `go.sum` (v0.35, indirect) |
| [`github.com/coreos/go-oidc/v3`](https://pkg.go.dev/github.com/coreos/go-oidc/v3/oidc) | CoreOS / Red Hat | v3.21.0 (Sep 2026) | OpenID Connect on top of x/oauth2: discovery, JWKS caching, `id_token` verification (signature, `iss`, `aud`, `exp`, nonce) | **use** for Google, Microsoft and Apple, which speak OIDC |
| [`github.com/markbates/goth`](https://github.com/markbates/goth) | community | v1.82.0 | 50+ providers behind one interface | **don't**: its README says it needs a new maintainer, and its `gothic` helper stores state in `gorilla/sessions` cookies, a second session system next to ours |

So: **x/oauth2 for the flow, go-oidc to verify identity tokens, and a few lines per provider** for
the non-OIDC ones (GitHub, Facebook read a profile API). No framework, nothing unmaintained.

### Providers

| Provider | Protocol | Identity comes from | Notes |
| --- | --- | --- | --- |
| **Google** | OIDC | `id_token` (`sub`, `email`, `email_verified`) | discovery at `accounts.google.com`; PKCE supported |
| **Microsoft** (personal and work accounts) | OIDC | `id_token` (`sub` or `oid`, `email`) | tenant `common`; the issuer varies per directory, so verification checks the issuer's form, not one fixed value; `email` is not always verified, so only trust it with `xms_edov` or for linking by a signed-in user |
| **Apple** | OIDC | `id_token` (`sub`, `email`, `email_verified`, maybe a private relay address) | the client secret is a short-lived **ES256 JWT** we sign (team id, key id, `.p8` key); name and email arrive **only on the first** sign-in; `response_mode=form_post` when asking for name/email, so the callback is a POST. To be confirmed against Apple's current docs (its pages 404'd during research) |
| **GitHub** | OAuth 2.0 (not OIDC) | `GET api.github.com/user` and `/user/emails` (primary + verified) | PKCE S256 supported; the code lives 10 minutes |
| **Facebook** | OAuth 2.0 (web; OIDC only for iOS Limited Login) | `GET graph.facebook.com/v27.0/me?fields=id,name,email` | `email` can be missing (phone sign-ups) or unconfirmed; versioned Graph API |

Best practice (OIDC Core, RFC 9700): authorization code with **PKCE on every flow**, `state` and
`nonce`, exact redirect URIs, `id_token` checked against the provider's JWKS (signature, `iss`,
`aud` = our client id, `exp`, nonce), never tokens in a URL.

### Our stack

- `users.email` is unique across the whole system (`users_email_unique` on `lower(email)`, live
  rows), so "this verified e-mail belongs to that user" is never ambiguous.
- There is **no self sign-up**: admins create users, inside a tenant. Social login therefore signs
  in **existing** users; it does not create them (see the decisions below).
- `authn.Service` already has the account checks (`verifyBeforeLogin`: active, e-mail/phone
  verified), lockout and the login activity; `completeLogin` issues the per-device token. Phase 22
  added `SignIn` without a token for the MCP page. Social login reuses the same checks.
- The cache holds short-lived state (OTP, recovery); the flow state (PKCE verifier, nonce) goes
  there with a 10-minute TTL, exactly like the phase-22 sign-in flow.

## Design

### Linking: who is this?

A provider identity is `(provider, subject)`: Google's `sub`, GitHub's numeric id. It is linked to
one user in a new table `social_accounts`. On sign-in:

1. **Linked already** → that user.
2. **Not linked, the provider says the e-mail is verified, and a user has that e-mail** → link it
   and sign in, when `SOCIAL_AUTO_LINK=true` (default). The link is recorded in the activity log
   and the user gets an e-mail ("Google was linked to your account").
3. **Otherwise** → refused with `SOCIAL_ACCOUNT_NOT_LINKED`: "sign in with your password, then link
   Google in your profile". Never link on an unverified e-mail: that is the classic account
   takeover.

Signed-in users manage their links: list, link (the same redirect flow, started while signed in),
unlink (refused if it would leave an account with no way in, i.e. no password set and no other
link).

### Web: the redirect flow

```
SPA ── GET /api/v1/auth/{provider}/redirect?return_to=https://app.example.com/auth/done
        └─ state + PKCE verifier + nonce in the cache ─▶ 302 to the provider
provider ── GET|POST /api/v1/auth/{provider}/callback?code&state   (POST for Apple form_post)
        └─ exchange code (PKCE) ─▶ verify id_token or read the profile ─▶ link rules ─▶ checks
        └─ 302 to return_to?login_code=… (one-time, 60 s)  or  ?error=…
SPA ── POST /api/v1/auth/social/exchange {login_code, device} ─▶ the normal login response (token)
```

- `return_to` must match `SOCIAL_RETURN_URLS` (exact origins) so the flow is never an open
  redirect. The token itself never travels in a URL: the one-time `login_code` does, and it is
  exchanged by a POST.
- OTP: if `AUTH_OTP_ENABLED`, the exchange answers `OTP_REQUIRED` like `POST /login`, and the code
  goes to `/verify-otp`. (Open question 3.)

### Mobile: the token flow

Native apps sign in with the provider's SDK and send the result:

```
POST /api/v1/auth/{provider}/token
  Google, Microsoft, Apple: {"id_token": "…", "nonce": "…", "device": "ios"}
  Facebook:                 {"access_token": "…", "device": "android"}
```

The `id_token` is verified with go-oidc against the provider's keys, with our **mobile** client
ids as accepted audiences (`SOCIAL_GOOGLE_AUDIENCES`). A Facebook access token is checked with
`debug_token` against our app id before its profile is read. Then the same link rules and checks,
and the normal login response.

### Providers in code

```go
// internal/authn/social
type Provider interface {
    Name() string                                  // "google"
    AuthURL(state, verifier, nonce string) string  // redirect flow
    Exchange(ctx, code, verifier, nonce string) (*Identity, error)
    VerifyToken(ctx, TokenInput) (*Identity, error) // mobile flow
}

type Identity struct {
    Provider, Subject, Email string
    EmailVerified bool
    Name, AvatarURL string
}
```

- `oidcProvider` covers Google, Microsoft and Apple (Apple adds its client-secret JWT and
  form_post); `githubProvider` and `facebookProvider` read their profile APIs.
- A registry built from config: a provider is on when its client id is set and it is listed in
  `SOCIAL_PROVIDERS`. Adding LinkedIn or GitLab is one file plus `endpoints.LinkedIn`.

### Data

- Migration: `social_accounts`: `id`, `user_id` (FK, cascade), `provider`, `subject`, `email`,
  `email_verified`, `name`, `avatar_url`, `created_at`, `last_used_at`; unique `(provider,
  subject)`; index on `user_id`. Provider tokens are **not** stored: we only need the identity.
- No change to `users`: a user created by an admin still gets a password (or an invitation); social
  sign-up is out of scope (decision 1).

### API

| Endpoint | Who | Does |
| --- | --- | --- |
| `GET /api/v1/auth/providers` | public | the enabled providers (name, display name), for the login screen |
| `GET /api/v1/auth/{provider}/redirect` | public | starts the web flow |
| `GET\|POST /api/v1/auth/{provider}/callback` | public (provider) | finishes it, redirects to `return_to` |
| `POST /api/v1/auth/social/exchange` | public | `login_code` → token |
| `POST /api/v1/auth/{provider}/token` | public | mobile: `id_token` / `access_token` → token |
| `GET /api/v1/user/social-accounts` | signed in | my links |
| `GET /api/v1/user/social-accounts/{provider}/link` | signed in | starts linking (redirect flow bound to me) |
| `DELETE /api/v1/user/social-accounts/{id}` | signed in | unlink (not the last way in) |

The phase-22 sign-in page (`/oauth/authorize`) shows a button per enabled provider; the callback
returns to the consent page instead of a front end.

### Config

```bash
SOCIAL_PROVIDERS=google,github              # empty = off
SOCIAL_RETURN_URLS=https://app.example.com  # allowed return_to origins (web flow)
SOCIAL_AUTO_LINK=true                       # link on a provider-verified e-mail
SOCIAL_GOOGLE_CLIENT_ID= / _CLIENT_SECRET= / _AUDIENCES=   # audiences: extra mobile client ids
SOCIAL_MICROSOFT_CLIENT_ID= / _CLIENT_SECRET= / _TENANT=common
SOCIAL_APPLE_CLIENT_ID= / _TEAM_ID= / _KEY_ID= / _PRIVATE_KEY_PATH=
SOCIAL_GITHUB_CLIENT_ID= / _CLIENT_SECRET=
SOCIAL_FACEBOOK_CLIENT_ID= / _CLIENT_SECRET=
```

The callback URL to register at each provider is `APP_URL/api/v1/auth/{provider}/callback`.

### Audit and security

- Activity: `auth.social_login` (provider, linked or new link), `social_account.linked`,
  `social_account.unlinked`. Failed attempts count toward the IP throttle.
- `state` and nonce are single use; PKCE everywhere it is supported (all five for the code flow);
  `id_token` checks as above; exact `return_to` origins; provider secrets and the Apple key never
  logged (the redactor covers `code`, `id_token`, `access_token`).
- Account checks are the same as password login: inactive users, unverified e-mail where required,
  lockout. A social login does not mark our e-mail verified unless the provider said so.

## Scope checklist

- [ ] `internal/authn/social`: provider interface, OIDC provider (Google, Microsoft, Apple),
      GitHub and Facebook providers, registry from config, Apple client-secret JWT.
- [ ] Config (`SOCIAL_*`), `.env.example`, validation (a listed provider without credentials
      fails at startup).
- [ ] Migration `social_accounts`, sqlc queries, link rules in the auth service.
- [ ] Endpoints above with OpenAPI; the providers list; the one-time `login_code`.
- [ ] The phase-22 sign-in page buttons.
- [ ] Activity entries and the "an account was linked" mail.
- [ ] Tests: a fake OIDC provider (httptest with discovery and JWKS) for the redirect and token
      flows; link rules (verified, unverified, auto-link off, unlink last way in); return_to
      allowlist; state and nonce reuse; Apple form_post.
- [ ] Docs: guide `docs/guides/social-login.md` with per-provider setup (where to create the app,
      the callback URL, the scopes), and a tutorial step "Sign in with Google locally".

## Out of scope

- Social **sign-up** (creating users from a provider). Needs a decision on which tenant a new user
  joins; a later spec, likely with tenant invitations.
- Per-tenant provider settings (a tenant brings its own Google Workspace or Entra app) and
  enterprise SSO (SAML).
- Using the provider's tokens to call its APIs (Google Drive, GitHub repos).
- The developer dashboard sign-in (it keeps the password login).

## Acceptance criteria

- With `SOCIAL_PROVIDERS` empty, every social route answers 404 and the login screen list is empty.
- Google (real, locally) signs in an existing user whose e-mail Google verified; the response is
  the same shape as `POST /login`; the activity log has `auth.social_login`.
- A user with no matching e-mail, or an unverified one, is refused with
  `SOCIAL_ACCOUNT_NOT_LINKED`, and nothing is linked.
- A signed-in user links GitHub, then signs in with it; unlinking the only way in is refused.
- The mobile flow accepts an `id_token` for a configured mobile audience and refuses one for
  another app, an expired one, and a replayed nonce.
- `return_to` outside `SOCIAL_RETURN_URLS` is refused without redirecting; the token never appears
  in a URL.
- An inactive user is refused exactly as on password login.
- The MCP sign-in page shows the enabled providers and completes consent after a Google sign-in.

## Open questions

1. **Sign-up.** Proposed: no; social login is for existing users. Is there a product case for
   "anyone can sign up with Google" (then: which tenant do they land in)?
2. **Auto-link on a verified e-mail** (proposed: on, with an e-mail to the user), or always require
   linking while signed in first?
3. **OTP after a social login.** Proposed: skip it, as the provider already authenticated the user
   (and Google, Microsoft and Apple usually did MFA). Or keep it when `AUTH_OTP_ENABLED`?
4. **Which providers in the first PR.** Proposed: Google and GitHub first (simplest, easy to test
   locally), then Microsoft, Apple and Facebook in a second PR, since Apple and Facebook need paid
   or reviewed developer accounts to test.
5. **Web redirect flow, mobile token flow, or both** in the first PR? Proposed: both for Google;
   redirect only for GitHub.
