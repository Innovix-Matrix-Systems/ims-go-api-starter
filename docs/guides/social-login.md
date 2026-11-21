---
title: Social login
description: Sign existing users in with Google or GitHub, with the same token a password login gives.
---

A user can sign in with their Google or GitHub account instead of a password. The provider only
answers *who is this?* with an e-mail address it has verified. Everything after that is ours: we
find the user with that address, run the same checks as a password login and issue the same
device token. Tenant, branch, roles and permissions come from our user record, exactly as after a
password login, so social login knows nothing about tenancy.

The API stays stateless: no session, no stored provider tokens, nothing kept between requests.
The client (web or mobile app) talks to the provider; the API only verifies what it brings back.

```
client ── signs in at Google / GitHub ──▶ gets an id_token or a code
client ── POST /api/v1/social/{provider}/login ──▶ API verifies it with the provider
API    ── verified e-mail → our user → account checks → token (same response as POST /login)
```

Social login **signs in existing users**; it does not create them. Admins create users as today.

## Turn it on

```bash
SOCIAL_PROVIDERS=google,github                 # empty (the default) = off; order = login screen order
SOCIAL_GOOGLE_CLIENT_IDS=web-id,ios-id,android-id
SOCIAL_GITHUB_CLIENT_ID=Iv1.…
SOCIAL_GITHUB_CLIENT_SECRET=…
```

A listed provider without its credentials stops the app at startup. Nothing else is needed: no
migration, no callback URL on the API.

| Endpoint | Does |
| --- | --- |
| `GET /api/v1/social/providers` | the enabled providers (`name`, `display_name`) for the login screen; empty when off |
| `POST /api/v1/social/{provider}/login` | the provider's proof (+ `device`) → the normal login response |

## Google

1. In the [Google Cloud console](https://console.cloud.google.com/apis/credentials), create an
   **OAuth client ID** for each app: *Web application* (add your front end's origin under
   *Authorized JavaScript origins*), and *iOS* / *Android* for mobile apps.
2. List every client id in `SOCIAL_GOOGLE_CLIENT_IDS`. An id_token issued to any other app is
   refused.
3. The client signs in with Google and sends the id_token:

```html
<script src="https://accounts.google.com/gsi/client" async></script>
<div id="g_id_onload" data-client_id="WEB_CLIENT_ID" data-callback="onGoogle"></div>
<div class="g_id_signin"></div>
<script>
  async function onGoogle({ credential }) {
    const res = await fetch('/api/v1/social/google/login', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ id_token: credential, device: 'web' }),
    })
    const { data } = await res.json() // data.token, as from POST /login
  }
</script>
```

On iOS and Android, Google Sign-In returns the same `idToken`; send it the same way.

The API checks the token's signature against Google's published keys, the issuer, the expiry and
that it was issued to one of your client ids, then reads `email` and `email_verified`.

## GitHub

1. Create an **OAuth app** under *GitHub → Settings → Developer settings*. Its *Authorization
   callback URL* is a page of **your front end** (for example
   `https://app.example.com/auth/github`), not the API.
2. Set `SOCIAL_GITHUB_CLIENT_ID` and `SOCIAL_GITHUB_CLIENT_SECRET`. The secret stays on the API.
3. The front end runs the redirect with PKCE and the `user:email` scope, checks `state` when
   GitHub sends the user back, then posts the code:

```js
// Before redirecting: keep state and the PKCE verifier in sessionStorage.
location = 'https://github.com/login/oauth/authorize?' + new URLSearchParams({
  client_id: 'GITHUB_CLIENT_ID', redirect_uri, scope: 'user:email', state,
  code_challenge, code_challenge_method: 'S256',
})

// On the callback page, after checking state:
await fetch('/api/v1/social/github/login', {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({ code, code_verifier, redirect_uri, device: 'web' }),
})
```

The API exchanges the code with the client secret (GitHub checks the verifier and the redirect
URI) and reads the account's **primary** e-mail address from `/user/emails`.

## Who gets in

| Case | Answer |
| --- | --- |
| The provider verified the e-mail and a user has it | 200, the same body as `POST /login`, always with a token |
| The provider rejects the proof (expired, another app, bad code) | 401 `INVALID_CREDENTIALS` |
| The provider has not verified the e-mail | 403 `ACCOUNT_UNVERIFIED` (never signed in on an unverified address) |
| No user has the e-mail | 403 `SOCIAL_ACCOUNT_NOT_FOUND` |
| The user is inactive or unverified with us | the same 403 as on password login |
| The provider is not enabled | 404 |
| The provider cannot be reached | 503 `SERVICE_UNAVAILABLE` |

- **No OTP.** `AUTH_OTP_ENABLED` applies to password login only: the provider has already
  authenticated the user.
- **Throttling.** Failed attempts count against the IP (`AUTH_LOGIN_MAX_ATTEMPTS` per
  `AUTH_LOGIN_DECAY_MINUTES`).
- **Matching is by e-mail.** A user whose e-mail with us differs from the one at the provider
  cannot use social login; changing a user's e-mail moves their social login with it.
- **Activity.** Every login, password or social, is an `auth.login` entry with its `method`
  (see the [activity log](activity-log.md)).

## Adding a provider

Providers are drivers in `internal/authn/social`, one file each, behind one interface:

```go
type Provider interface {
	Name() string        // "gitlab": the {provider} in the URL
	DisplayName() string // "GitLab": for the login screen
	Verify(ctx context.Context, proof Proof) (*Identity, error)
}
```

Two building blocks cover almost every provider:

- **OpenID Connect** (Microsoft, Apple, GitLab…): `newOIDCProvider(name, displayName, issuer,
  keysURL, clientIDs)` verifies an id_token. `google.go` is the example.
- **Code exchange** (Facebook, LinkedIn…): `exchangeCode` trades the code for an access token;
  then read the profile API for the e-mail. `github.go` is the example.

The recipe:

1. `internal/authn/social/<name>.go`: build the provider and decide when the e-mail counts as
   verified. When unsure, it does not: an unverified address is how accounts get taken over.
   (Microsoft's `email` claim, for example, is only verified when `xms_edov` is true.)
2. Its settings in `internal/config/social.go` (`SOCIAL_<NAME>_*`, the check in `loadSocial`),
   a case in `social.New`, and the lines in `.env.example`.
3. A test against a fake provider (`httptest`), like `oidc_test.go` or `github_test.go`.
4. A section in this guide: where to create the app and what the client sends.
