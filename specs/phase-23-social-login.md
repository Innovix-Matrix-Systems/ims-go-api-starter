# Phase 23 — Social login (Google, Microsoft, Apple, GitHub, Facebook)

**Status:** Accepted, built (2026-11-21; simplified to "verify, look up, issue")
**Source:** project-owner request (2026-11-21): "as we introduce OAuth anyway, we should also add
support for normal login OAuth, like user able to log in with their Google account or Facebook
account".
**Depends on:** Phase 2 (tokens, login rules). Nothing else: no table, no tenancy change, no
phase-22 change.

## Goal

A user can sign in with an account they already have at Google, Microsoft, Apple, GitHub or
Facebook instead of a password, and gets **exactly the token a password login gives**.

## The idea in one paragraph

The provider only answers one question: *who is this person?* It gives us a verified e-mail. From
there everything is ours: we find our user by that e-mail, run the same checks a password login
runs (active, verified, throttle), and issue our normal token. Tenant, branch, roles and
permissions all come from **our** user record, exactly as after a password login, so social login
knows nothing about tenancy. The API stays stateless: no session, no stored provider tokens, no
flow state on the server.

```
client ── does the provider part (SDK, or redirect + PKCE) ──▶ gets an id_token or a code
client ── POST /api/v1/social/{provider}/login {id_token | code…, device}
API    ── verify with the provider ─▶ verified e-mail
       ── users.FindByEmail ─▶ same checks as POST /login ─▶ issue token
       ◀── the normal login response (same shape as POST /login)
```

That replaces a "password check" with a "provider check"; nothing after it changes.

## Two different things called OAuth

| | Phase 22 (built) | Phase 23 (this spec) |
| --- | --- | --- |
| Our role | the **authorization server**: we issue tokens to AI apps | a **client** of Google, Apple…: they vouch for who the user is |
| Result | an MCP access token for `/mcp` | our normal session token (`id|secret`) |

They share nothing but the vocabulary.

## Design

### Who does what

- **The client** (SPA, mobile app) talks to the provider. Mobile apps already use the provider SDK
  (Google Sign-In, Sign in with Apple, Facebook SDK). A web front end uses the provider's JS
  library (Google Identity Services, MSAL, Sign in with Apple JS) or a plain redirect with PKCE; it
  keeps the `state` and the PKCE verifier itself and checks `state` on return.
- **The API** only verifies what the client brings and maps it to a user. It keeps nothing between
  requests.

### The one endpoint

```
POST /api/v1/social/{provider}/login
  OIDC providers (Google, Microsoft, Apple):
    {"id_token": "…", "device": "web"}
  Code providers (GitHub, Facebook web), or any provider when the client prefers the code:
    {"code": "…", "code_verifier": "…", "redirect_uri": "…", "device": "web"}
  Facebook mobile SDK:
    {"access_token": "…", "device": "android"}
```

Per provider the API turns the input into an `Identity`:

| Provider | How it verifies | E-mail is trusted when |
| --- | --- | --- |
| Google | `id_token` checked with go-oidc (signature via JWKS, `iss`, `aud` ∈ our client ids, `exp`) | `email_verified` is true |
| Microsoft | `id_token`, tenant `common` (issuer checked by form, the tenant id varies) | `xms_edov` is true (Microsoft's `email` is not otherwise verified) |
| Apple | `id_token` | `email_verified` is true; a private-relay address only matches if the user has that address with us |
| GitHub | exchange `code` (+ verifier) with our client secret, then `GET /user/emails` | the address is `primary` and `verified` |
| Facebook | exchange `code`, or check `access_token` with `debug_token` against our app id, then `GET /me?fields=id,email` | Facebook returns an `email` at all (it returns only confirmed ones) |

```go
// internal/authn/social
type Provider interface {
    Name() string
    Verify(ctx context.Context, in Input) (*Identity, error)
}

type Identity struct {
    Provider, Subject, Email string
    EmailVerified bool
    Name string
}
```

### Adapters

Each provider is an adapter (a driver, in the repo's words) behind `Provider`; the handler and
`authn` never know which one ran. Two building blocks cover almost every provider:

- `newOIDCProvider(name, displayName, issuer, keysURL, clientIDs)`: `id_token` verification with
  go-oidc (keys fetched on first use, no network at startup; audience checked against every client
  id). Google is one call to it. Microsoft or Apple later reuse it with their own issuer and
  e-mail rule.
- `exchangeCode(ctx, oauth2Config, displayName, proof)`: the x/oauth2 code exchange with the PKCE
  verifier and redirect_uri the client used. GitHub is that plus a `GET /user/emails` reader.
  Facebook, GitLab or LinkedIn later are the same helper with their own endpoint and profile
  reader.

`social.New(cfg)` builds the enabled providers from config at startup (`SOCIAL_PROVIDERS`).
Nothing else in the code lists providers.

### Adding a provider (the recipe this phase ships)

1. `internal/authn/social/<name>.go`: build it on `newOIDCProvider` or `exchangeCode` (or
   implement `Provider` directly for an odd one), with the rule for when the e-mail is trusted.
2. Its settings in `config/social.go` (`SOCIAL_<NAME>_*`, checked in `loadSocial`) and a case in
   `social.New`.
3. A test against the shared fake provider (httptest discovery/JWKS, or a fake profile API).
4. A section in `docs/guides/social-login.md` (where to create the app, which ids to list).

The recipe also goes into `AGENTS.md` / a skill so it is followed the same way every time.

### After the provider: our side, unchanged

1. No verified e-mail → 403 `ACCOUNT_UNVERIFIED` (the existing code, with a provider message).
   Never sign in on an unverified e-mail: that is the classic account takeover.
2. `FindByEmail` (live users only; e-mail is unique system-wide). No user → 403 `SOCIAL_ACCOUNT_NOT_FOUND`
   ("no account uses this e-mail; ask your administrator"). This is not account enumeration: a
   caller can only test an address the provider has already proved is theirs.
3. The password-login checks: active, e-mail/phone verified where configured
   (`verifyBeforeLogin`), the per-IP throttle (failed attempts count; there is no e-mail to key the
   account counter on before the provider answers).
4. Issue the device token (`completeLogin`) and return the normal login response.

Code changes in `authn`: one exported method,
`Service.SocialLogin(ctx, provider, proof, device, ip) (*LoginResult, error)`, which calls the
provider and then does 1–4 by reusing the existing private helpers, so the handler stays
`Bind` → service → `respondLogin`. A provider rejecting the proof is 401 `INVALID_CREDENTIALS`; a
provider that cannot be reached is 503 `SERVICE_UNAVAILABLE`; a provider that is not enabled is
404.

### Tenancy

None. The token is a normal session token for our user; the authentication and tenancy middleware
resolve tenant and branch from the user record on every request, as they do after a password
login. A tenant user, a tenantless super admin and a developer sign in the same way.

### What is deliberately not here

| Dropped | Why it is safe to drop |
| --- | --- |
| `social_accounts` table, link/unlink endpoints | the verified e-mail is the link; nothing to store |
| Server-side redirect, callback, `return_to` allowlist, one-time `login_code`, cached state/nonce | the client runs the provider flow; the API is one stateless POST. `redirect_uri` for code exchange is enforced by the provider against what we registered there |
| Stored provider tokens | we only need the identity |
| OTP after social login | the provider already proved who the user is (decision 3); `AUTH_OTP_ENABLED` stays a password-login setting |
| Social sign-up | admins create users; social login signs in existing users only |

Accepted trade-offs of e-mail matching:
- If a user's e-mail with us differs from the one at the provider, social login does not work for
  them (they use the password, or an admin aligns the e-mail).
- Apple "Hide my e-mail" relay addresses will not match unless the user has that address with us.
- Changing the e-mail in our system moves the social login with it; that is the intended behaviour.

If any of these becomes a real problem, a later phase adds an optional `(provider, subject) → user`
link table in front of step 2; the endpoint does not change.

### Config

```bash
SOCIAL_PROVIDERS=google,github              # empty = off (the list is empty, login answers 404)
SOCIAL_GOOGLE_CLIENT_IDS=web-id,ios-id,android-id   # accepted id_token audiences
SOCIAL_GITHUB_CLIENT_ID= / _CLIENT_SECRET=
```

Providers added later follow the same shape (`SOCIAL_<NAME>_CLIENT_ID`, `_CLIENT_SECRET`, …).

A listed provider without its credentials fails at startup.

### API

| Endpoint | Who | Does |
| --- | --- | --- |
| `GET /api/v1/social/providers` | public | enabled providers (name, display name) for the login screen; empty when off |
| `POST /api/v1/social/{provider}/login` | public | provider proof → our token |

### Audit and security

- Activity `auth.login` for every API login, password and social alike, with `method`
  (`password`, `google`, `github`), success or failure (decision 2).
- Redactor: add `code_verifier` to the sensitive keys (`id_token`, `access_token` are already
  there). Not `code`: tenants and OTP forms use that key, and a provider code is single-use and
  spent by the time it could be read. Provider secrets are never logged.
- `id_token` checks as above; HTTPS-only provider calls with a short timeout.

## Background (researched 2026-11-21)

| Library | Verdict |
| --- | --- |
| `golang.org/x/oauth2` (Go team, v0.38) | **use** for the code exchange and `endpoints.*`; already indirect in `go.mod` |
| `github.com/coreos/go-oidc/v3` (v3.21) | **use** to verify `id_token` (JWKS caching, `iss`/`aud`/`exp`) |
| `github.com/markbates/goth` | **don't**: needs a maintainer, brings its own cookie sessions |

Apple's code exchange needs a client secret that is a short-lived ES256 JWT we sign (team id, key
id, `.p8`); the `id_token` input needs none. Confirm against Apple's current docs at build time.

## Code style (how this phase is built)

The code must read like the rest of the repo (AGENTS.md), not like an OAuth library:

- **Layering as everywhere:** the auth handler does `h.Bind` → `authn.Service` → `respondLogin`,
  nothing else. The rules (verified e-mail, user lookup, account checks, throttle, token) live in
  `authn.Service.SocialLogin`, next to `Login`, reusing its private helpers. The provider call is a
  collaborator of the service, like the OTP sender.
- **Providers are drivers,** like cache, storage and billing drivers: one small interface in
  `internal/authn/social`, one file per provider, a registry built from config. The package
  imports only `config`, `apperr` and the two libraries; it knows nothing about users, tokens or
  HTTP handlers.
- **Optional collaborators are `WithX` options** (`authn.Service.WithSocial`, `WithAudit`); with
  none set, social login is off and login activity is not written, so unit tests need nothing.
- **Activity through an interface owned by the caller,** like `tenancy.ImpersonationAudit`:
  `authn.LoginAudit`, implemented by `activity.Recorder`. No import from `authn` to a module.
- **Errors are `*apperr.Error`,** existing codes first; a new code only where a client must tell
  the case apart (`SOCIAL_ACCOUNT_NOT_FOUND`).
- **Config is one file** (`internal/config/social.go`) with load-time validation, like `mcp.go`.
- **Tests:** table-driven stdlib tests with fakes; provider HTTP against `httptest.Server`
  stubs, as billing gateways are; the integration test with `testkit`.
- No new framework, no reflection, no generic "auth manager". Comments say why, not what.

## Scope checklist

- [x] `internal/authn/social`: `Provider`, `newOIDCProvider`, `exchangeCode`, the Google and
      GitHub providers, `social.New` from config.
- [x] `authn.Service.SocialLogin` (no OTP step).
- [x] Config (`SOCIAL_PROVIDERS`, `SOCIAL_GOOGLE_*`, `SOCIAL_GITHUB_*`), `.env.example`, startup
      validation.
- [x] The two endpoints with OpenAPI; one new error code, `SOCIAL_ACCOUNT_NOT_FOUND` (403); the
      rest reuse `ACCOUNT_UNVERIFIED`, `INVALID_CREDENTIALS`, `SERVICE_UNAVAILABLE`.
- [x] Activity `auth.login` for password **and** social login (`method`); redactor keys.
- [x] Tests: a fake OIDC provider (httptest serving the JWKS) — valid token, wrong `aud`,
      expired, unverified e-mail, unknown e-mail, inactive user; a fake GitHub API for the code
      input; provider off → 404; social login with `AUTH_OTP_ENABLED=true` still issues a token;
      password login writes `auth.login`.
- [x] Docs: `docs/guides/social-login.md` (Google and GitHub app setup, which client ids to list,
      front-end snippets for Google Identity Services and the GitHub redirect, the "Adding a
      provider" recipe); the recipe in `AGENTS.md`; activity-log, authentication and errors
      guides; decisions 36–37.

## Out of scope

- Social sign-up; per-tenant provider apps; enterprise SSO (SAML).
- Provider buttons on the phase-22 MCP sign-in page: that page is server-rendered, so it would
  need the server-side redirect flow this spec drops. A later phase, if wanted.
- Calling provider APIs with the user's provider token.
- The developer dashboard sign-in.

## Acceptance criteria

- With `SOCIAL_PROVIDERS` empty, the providers list is empty and the login route answers 404.
- A Google `id_token` (real, locally) for an existing user whose e-mail Google verified returns the
  same response shape as `POST /login`, and the token works on every route a password token does.
- A GitHub `code` for an existing user whose primary GitHub e-mail is verified does the same.
- With `AUTH_OTP_ENABLED=true`, social login still returns a token directly (no `OTP_REQUIRED`).
- Every password and social login, successful or not, leaves an `auth.login` activity entry with
  its `method`.
- Adding a third provider needs only a new adapter file, its config and a registry line.
- A token for another app's client id, an expired one, or one with `email_verified=false` is
  refused; an unknown e-mail gets `SOCIAL_ACCOUNT_NOT_FOUND`.
- An inactive user is refused exactly as on password login.
- A tenant user and a tenantless super admin both sign in, and their tenant context is the same as
  after a password login.
- The server stores nothing per attempt besides the throttle counter and the activity entry.

## Decisions (project owner, 2026-11-21)

1. **Google and GitHub only.** This phase builds the pattern, not a catalogue: one OIDC provider
   (Google, `id_token`) and one code-exchange provider (GitHub, `code`), so both adapter kinds
   exist and are tested. Microsoft, Apple, Facebook and others are added by products that need
   them, following the "Adding a provider" recipe; the tables above stay as reference for them.
2. **Password login gets activity too.** `auth.login` is recorded for every API login, with
   `method: password | google | github …`, success and failure, so the log tells one story.
   (Today only the developer dashboard records sign-ins.)
3. **No OTP after a social login.** The provider already proved who the user is. `AUTH_OTP_ENABLED`
   keeps applying to password login only, unchanged.
