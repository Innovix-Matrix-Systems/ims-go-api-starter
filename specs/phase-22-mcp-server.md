# Phase 22 — MCP server as a connector

**Status:** Done (2026-11-21; rev 3, 2026-11-21: rebuilt around connectors; manual client registration alongside CIMD; tool
coverage moved to a later spec)
**Source:** project-owner request (2026-11-20): "MCP is now a must for a lot of systems"; rev 2
(2026-11-21): "make sure that it is a connector … I do not want a registration … we are only
establishing MCP and some basic things like creating a branch or a user."
**Depends on:** Phase 2 (tokens, tenancy, RBAC), Phase 10 (OpenAPI as the contract), Phase 17
(activity log), Phase 21 (acting headers, system audit log)

## Goal

A product built on the starter can be added to an AI client **as a connector**: the user pastes
one URL (`https://api.example.com/mcp`), signs in with the account they already have, approves
access, and the agent can work with the API as that user.

The same URL works in every Claude surface (claude.ai, Claude Desktop, Claude mobile, Claude
Code, Cowork) and in any other MCP client that follows the MCP authorization spec (ChatGPT,
Cursor, VS Code, the MCP Inspector).

**Two ways in, both supported:**
- **Connector, no registration (the default).** Nobody registers an OAuth client: not the user,
  not the product's operator, not Anthropic. Clients identify themselves with a Client ID Metadata
  Document (CIMD), the method the current MCP spec recommends. This is how Claude, Claude Code,
  ChatGPT, Cursor and VS Code connect.
- **Manual registration, when someone needs it.** An internal admin registers a client (name,
  redirect URIs, public or with a secret) and gets a `client_id` (and secret) to paste into the
  client. For clients without CIMD, an organisation's own agent or automation platform, or a
  product that wants a fixed allowlist of clients. In Claude it is the connector's **Advanced
  settings → OAuth Client ID / Secret**.

Dynamic Client Registration is deprecated by the spec and is not built.

This spec **establishes the MCP server**: the endpoint, sign-in, tokens, the way a tool runs, audit,
and a handful of basic tools that prove it end to end (who am I, list and create branches, list and
create users). **Which operations the product exposes is a later spec.** The mechanism added here
(one `x-mcp` line on an OpenAPI operation) makes that later work small.

## Background (researched 2026-11-21)

### Protocol: MCP 2026-07-28

The current revision is [2026-07-28](https://modelcontextprotocol.io/specification/2026-07-28/changelog).
- **Stateless.** The `initialize` handshake and `Mcp-Session-Id` are gone. Each request carries its
  protocol version and client capabilities in `_meta`. `server/discover` is the one RPC that
  advertises the server. Any API instance can answer any request: no sticky routing, no session
  store.
- **Streamable HTTP** is the remote transport: JSON-RPC over `POST /mcp`, with the `Mcp-Method` and
  `Mcp-Name` headers. HTTP+SSE is deprecated.
- Roots, sampling and logging are deprecated. This spec uses none of them.
- Many clients still speak **2025-11-25** (with `initialize`). The server must accept both.

### Authorization: what the spec requires of us

From the [2026-07-28 authorization spec](https://modelcontextprotocol.io/specification/2026-07-28/basic/authorization)
and its [client registration](https://modelcontextprotocol.io/specification/2026-07-28/basic/authorization/client-registration)
page. The API is both the **resource server** (`/mcp`) and the **authorization server** (the spec
allows them to be the same host).

| Requirement | Level |
| --- | --- |
| RFC 9728 protected resource metadata, with `authorization_servers` | MUST |
| 401 with `WWW-Authenticate: Bearer resource_metadata="…"`, or the well-known URL | MUST (we do both) |
| RFC 8414 (or OIDC discovery) authorization server metadata | MUST |
| `code_challenge_methods_supported: ["S256"]` (clients refuse to proceed without it) | MUST |
| Accept only tokens issued for this server (audience = the `resource` sent by the client, RFC 8707) | MUST |
| Never pass a received token on to another service | MUST NOT |
| Exact redirect URI matching | MUST |
| All authorization endpoints over HTTPS | MUST |
| Rotate refresh tokens for public clients | MUST |
| Short-lived access tokens | SHOULD |
| `iss` in the authorization response (RFC 9207), advertised with `authorization_response_iss_parameter_supported` | SHOULD (expected to become MUST) |
| CIMD support (`client_id_metadata_document_supported: true`) | SHOULD |
| On fetch: `client_id` in the document equals its URL exactly; valid JSON with `client_id`, `client_name`, `redirect_uris`; redirect URI is one of them | MUST |
| Show the redirect host on the consent page; warn on localhost-only redirect URIs | MUST / SHOULD |
| 403 `error="insufficient_scope"` with the needed `scope` | SHOULD (MUST in the error table) |
| Dynamic Client Registration | MAY, **deprecated** |

### Claude's connector requirements

From [Authentication for connectors](https://claude.com/docs/connectors/building/authentication)
and [Testing](https://claude.com/docs/connectors/building/testing.md). "The same authentication
infrastructure backs claude.ai, Claude Desktop, Claude mobile, Claude Code, and Cowork."
- Claude picks **CIMD** when the authorization server metadata has **both**
  `"client_id_metadata_document_supported": true` **and** `"none"` in
  `token_endpoint_auth_methods_supported` (Claude's CIMD client is a public client). Otherwise it
  falls back to DCR, which we don't offer, so both fields are required.
- Claude also takes a **client ID and secret entered by the user** when adding a custom connector
  (`custom_connection`; Advanced settings). That is the manual path: a pre-registered client wins
  over CIMD, as the MCP spec's priority order says (pre-registered → CIMD → DCR).
- Redirect URIs:
  - hosted apps (web, Desktop, mobile, Cowork): `https://claude.ai/api/mcp/auth_callback`;
  - Claude Code: its CIMD is `https://claude.ai/oauth/claude-code-client-metadata`, with loopback
    redirects `http://localhost/callback` and `http://127.0.0.1/callback` on an ephemeral port.
    **The port must be ignored** when matching loopback redirects (RFC 8252 §7.3).
- PKCE S256 on every request. `/token` takes `application/x-www-form-urlencoded`.
- Refresh: Claude adds `offline_access` only if it is in `scopes_supported` (the MCP spec says
  servers SHOULD NOT list it; we issue refresh tokens without it). Claude refreshes on a 401 and up
  to five minutes before expiry; a dead refresh token must answer `invalid_grant`; rotation returns
  the new refresh token in the same response that invalidates the old one.
- Timeouts: discovery and token endpoints 10 s, refresh 30 s.
- Reachability: hosted apps call from Anthropic's infrastructure (egress `160.79.104.0/21`), so the
  MCP **and** authorization endpoints must be public. A local server needs a tunnel (Cloudflare
  Tunnel, ngrok). Claude Code connects from the developer's machine, so `localhost` works there.
- Tokens in query strings are not allowed. `client_credentials` is not used.

### Our stack

- **Go SDK:** [`github.com/modelcontextprotocol/go-sdk`](https://github.com/modelcontextprotocol/go-sdk)
  **v1.8.0** (latest stable; v1.9.0-pre.1 adds no new protocol revision). It needs Go 1.25; we are
  on 1.26. Supports 2026-07-28, 2025-11-25 and older. What we use:
  - `mcp.NewServer`, `Server.AddTool`, `mcp.NewStreamableHTTPHandler(getServer, &StreamableHTTPOptions{Stateless: true})`.
    In stateless mode the handler ignores `Mcp-Session-Id` and serves each request with a temporary
    session, so 2025-11-25 clients work too, and it is the only mode that accepts 2026-07-28;
  - `auth.RequireBearerToken(verifier, &RequireBearerTokenOptions{ResourceMetadataURL, Scopes})`,
    which answers 401 with the `WWW-Authenticate` header; `auth.TokenInfo`;
  - `auth.ProtectedResourceMetadataHandler`.
- **The SDK has no authorization server** and no server-side CIMD. No Go library we found
  (fosite, zitadel/oidc) offers CIMD either, and a general-purpose OAuth framework would bring far
  more than we need (implicit flows, client secrets, introspection, OIDC). We build a **small,
  purpose-built authorization server**: one grant (authorization code + PKCE S256), one client
  type (public, CIMD), refresh rotation. About the size of the `auth` module.
- **Already in place:** `personal_access_tokens` with hashed secrets (`authn.OpaqueAuthenticator`),
  `authn.Service` for password + OTP sign-in, the `/api/v1` router with every gate, `tenancy.Resolve`
  with acting headers, the activity log, telemetry and `httpx.RequestInfo`, kin-openapi with the
  embedded contract, the cache with `SetNX`.
- Neither Laravel starter has an MCP server: the design is decided here.

### Prior art in our own work: Vitaena

`K:\Projects\vitaena` (the CV desktop app) ships an MCP server on the **same SDK, v1.8.0**. It is
local: `vitaena mcp` relays stdio to the running app over a socket, so it has no OAuth. Its tools
are described by the UI and run with the editor's own code, the same idea as our "tools run
through the router". What we take from it:
- **Client name across both protocol revisions** (`clientName` in `backend/mcp/service.go`): read
  `InitializeParams().ClientInfo` for 2025-11-25, else `_meta[mcp.MetaKeyClientInfo]` for
  2026-07-28; prefer `Title`, then `Name`. We use it for the activity entry's client when the grant
  has none to offer, and record both.
- **Server `Instructions`**: one short paragraph telling the model what the product is and where to
  start ("call `whoami` first"). Cheap, and it improves the first call.
- **Refusals the model can relay**: an `isError` result whose text says what is off and where the
  user changes it ("this connection can read but not change; reconnect and tick *Allow
  changes*"), not a bare code. We add this text next to the problem document.
- **Read / edit access as a user-facing choice**, mapping to our `mcp:read` / `mcp:write`, with
  `ReadOnly`, `Destructive` and `Idempotent` annotations on every tool.
- **Per-client setup instructions** (`frontend/src/features/mcp/connect.ts`): where to paste what,
  in each client's own words. Our guide does the same for the remote URL.
- Not taken: the stdio relay (we are remote), prompts (later spec), and its local undo copies
  (our activity log is the record).

## Design

### What the user does

1. In Claude: **Customize → Connectors → Add custom connector**, paste `https://api.example.com/mcp`.
   In Claude Code: `claude mcp add --transport http shop https://api.example.com/mcp`, then `/mcp`.
2. A browser window opens on **the API's own sign-in page**: e-mail and password (and the OTP step
   when the account has one), the same rules as `POST /api/v1/login`.
3. A consent page: "**Claude** wants to access your account at *Acme Shop*", the redirect host
   (`claude.ai`, or "this computer" for loopback), what it may do (read / read and change), and for
   a platform account, which tenant it works in (below). Allow or Deny.
4. Back in Claude, the connector is connected. Later they can see and revoke it.

### Discovery flow (what the client does)

```
client ──POST /mcp (no token)──▶ 401  WWW-Authenticate: Bearer resource_metadata=".../.well-known/oauth-protected-resource/mcp", scope="mcp:read"
client ──GET /.well-known/oauth-protected-resource/mcp──▶ { resource: APP_URL/mcp, authorization_servers: [APP_URL], scopes_supported: [mcp:read, mcp:write] }
client ──GET /.well-known/oauth-authorization-server──▶ { issuer, authorization_endpoint, token_endpoint, revocation_endpoint,
                                                          code_challenge_methods_supported: [S256],
                                                          token_endpoint_auth_methods_supported: [none, client_secret_basic, client_secret_post],
                                                          client_id_metadata_document_supported: true,
                                                          authorization_response_iss_parameter_supported: true,
                                                          grant_types_supported: [authorization_code, refresh_token],
                                                          response_types_supported: [code], scopes_supported }
browser ──GET /oauth/authorize?client_id=https://claude.ai/…&redirect_uri=…&code_challenge=…&resource=APP_URL/mcp&state=…
        └─ fetch + validate the client's CIMD ─▶ sign-in page ─▶ consent page ─▶ 302 redirect_uri?code=…&state=…&iss=APP_URL
client ──POST /oauth/token (code + code_verifier + resource)──▶ { access_token, token_type: Bearer, expires_in: 3600, refresh_token, scope }
client ──POST /mcp  Authorization: Bearer …──▶ tools
```

### The authorization server (`internal/modules/oauth`)

**Endpoints** (outside `/api/v1`, mounted only when `MCP_ENABLED`; served as public routes):

| Endpoint | Does |
| --- | --- |
| `GET /.well-known/oauth-protected-resource`, `…/mcp` | RFC 9728 metadata (SDK handler) |
| `GET /.well-known/oauth-authorization-server` | RFC 8414 metadata (above) |
| `GET /oauth/authorize` | validates the request, renders sign-in or consent |
| `POST /oauth/authorize/login` | checks the password (and OTP), sets a short sign-in cookie |
| `POST /oauth/authorize/consent` | Allow / Deny; issues the code and redirects |
| `POST /oauth/token` | `authorization_code` and `refresh_token` grants |
| `POST /oauth/revoke` | RFC 7009 revocation (token or refresh token) |

**Authorization request rules.** Errors that make the redirect unsafe (bad `client_id`, unknown
`redirect_uri`) render an error page and **never redirect**. Every other error redirects with
`error=…&state=…&iss=…`.
- `response_type=code` only; `code_challenge` with `code_challenge_method=S256` required (`plain`
  refused);
- `resource` required and equal to `APP_URL/mcp` (canonical form, no trailing slash);
- `scope` ⊆ `mcp:read mcp:write`; missing means `mcp:read`.

**CIMD fetch** (`client_id` is an `https://` URL with a path):
- SSRF guards: `https` only, port 443, resolves to public addresses only (no loopback, private,
  link-local or metadata ranges; checked after DNS, at dial time), no redirects, 5 s timeout,
  64 KiB cap, `application/json`.
- Validation: JSON with `client_id` (equal to the URL, byte for byte), `client_name`,
  `redirect_uris`; `token_endpoint_auth_method` absent or `none`. Optional `logo_uri`/`client_uri`
  are shown only if they share the client_id's host.
- Cache: `Cache-Control`, clamped to 5 min – 24 h, in the cache store (`oauth_cimd:<sha256>`). A
  fetch failure with a cached copy uses the copy.
- Optional `MCP_OAUTH_ALLOWED_CLIENT_HOSTS` (empty = any host): a product that wants only Claude
  sets `claude.ai`.

**Manually registered clients.** A `client_id` that is not an `https://` URL is looked up in
`oauth_clients` instead of fetched. Everything after that (sign-in, consent, codes, PKCE, tokens,
audit) is the same flow; only where the client's name and redirect URIs come from differs.
- **Who registers:** internal accounts with the new system permission `oauth_client.manage`
  (Super Admin has it; not Developer, not tenant roles). Endpoints, tenant-exempt and
  `InternalOnly`: `GET/POST /api/v1/oauth-client`, `GET/PUT/DELETE /api/v1/oauth-client/{id}`,
  `POST /api/v1/oauth-client/{id}/rotate-secret`. Also a CLI for ops and scripts:
  `ims oauth:client create --name … --redirect … [--confidential]`.
- **Fields:** `name`, `redirect_uris` (exact; loopback allowed and port-agnostic, as above),
  `type` (`public` or `confidential`), `scopes` (the most it may get, default both),
  `is_active`, optional `tenant_id` (a client only that tenant's users may authorize, for a
  tenant's own integration).
- **Ids and secrets:** `client_id` is a random id with a prefix (`ims_…`), never a URL, so it
  can't collide with CIMD. A confidential client gets a secret shown **once**, stored hashed;
  rotation keeps the old one valid for `MCP_OAUTH_SECRET_GRACE` (default 24 h). Confidential
  clients authenticate at `/oauth/token` with `client_secret_basic` or `client_secret_post` and
  still use PKCE (OAuth 2.1). Public ones use `none` + PKCE.
- **Lifecycle:** deactivating or deleting a client revokes every grant made with it. Each change
  writes a system audit entry (`oauth_client.created`, `.updated`, `.secret_rotated`, `.deleted`).
- **Policy switch:** `MCP_OAUTH_CIMD_ENABLED` (default `true`). A product that wants only
  registered clients turns CIMD off; metadata then omits `client_id_metadata_document_supported`
  and URL `client_id`s are refused. With both on, a product gets "anyone's agent" and "our
  vetted integrations" side by side.

**Redirect URI matching:** exact string match against the document, except loopback
(`http://localhost` / `http://127.0.0.1`), where the port is ignored and the path must match. A
loopback-only client gets a warning on the consent page ("an app on this computer").

**Sign-in page:** server-rendered `html/template`, no JavaScript framework, translated (en/bn), the
product's title from config. It calls `authn.Service` exactly like `POST /api/v1/login`: lockout,
rate limits, inactive users, OTP and the `auth.login` activity entry all apply. Password reset is a
link to the product's own reset flow (`APP_FRONTEND_URL`). The sign-in cookie is `HttpOnly`,
`Secure`, `SameSite=Lax`, path `/oauth`, 10 minutes, and holds only a signed reference. The forms
carry a CSRF token. No account creation here.

**Consent page:** client name and redirect host, the scopes in words, and for **platform accounts**
a tenant (and branch) picker: the connector then acts like `X-Acting-Tenant`/`X-Acting-Branch` on
every call (below). "No tenant: system work only" is an option. Tenant users get no picker: their
tenant comes from the account. Consent is remembered per (user, client): a later authorize with the
same or fewer scopes skips the consent page.

**Codes:** 32 random bytes, stored hashed, single use, 60 s, bound to client, redirect URI, PKCE
challenge, resource, scopes, user and chosen tenant/branch. A second use of a code revokes the
grant made from it (OAuth 2.1 §4.1.3).

**Tokens:**
- **Access token:** a row in `personal_access_tokens` (the same opaque `<id>|<secret>` format and
  hashing), `name = 'mcp'`, `abilities` = the granted scopes plus `aud:<resource>`, `expires_at` =
  1 hour (`MCP_ACCESS_TOKEN_TTL`), linked to its grant. Expiry is mandatory.
- **Refresh token:** in `oauth_refresh_tokens`, hashed, 30 days sliding (`MCP_REFRESH_TOKEN_TTL`),
  rotated on every use. Reusing a rotated refresh token revokes the whole grant (reuse detection).
  Dead or revoked: `invalid_grant`.
- **Separation:** `/mcp` accepts only tokens with `mcp:*` abilities and the right audience;
  `authn.Authenticate` rejects tokens with `mcp:*` abilities on `/api/v1` and `/dev` (401). A leaked
  session token cannot drive an agent, and a leaked agent token cannot call the REST API.
- Password change, user deactivation and "log out all devices" revoke the user's grants too.

**Tables** (one migration):
- `oauth_clients`: `id`, `client_id` (unique), `name`, `type`, `secret_hash`,
  `previous_secret_hash`, `previous_secret_expires_at`, `redirect_uris`, `scopes`, `tenant_id`
  (nullable), `is_active`, `created_by`, timestamps (manual clients only; CIMD clients are cached,
  not stored);
- `oauth_grants`: `id`, `user_id`, `client_id` (the CIMD URL or the registered id), `client_name`, `scopes`,
  `tenant_id`, `branch_id` (the consent's choice, nullable), `created_at`, `last_used_at`,
  `revoked_at`;
- `oauth_codes`: `code_hash`, `grant_id`, `redirect_uri`, `code_challenge`, `expires_at`, `used_at`;
- `oauth_refresh_tokens`: `token_hash`, `grant_id`, `parent_id`, `expires_at`, `rotated_at`,
  `revoked_at`;
- `personal_access_tokens` gains a nullable `oauth_grant_id` (FK, cascade delete).

**Managing connections:** `GET /api/v1/user/connections` (the user's grants: client, scopes,
tenant, last used) and `DELETE /api/v1/user/connections/{id}` (revokes the grant, its refresh
tokens and access tokens). Self-service, no catalog permission. The admin UI is Phase 8; the API is
enough here.

### The MCP endpoint

- `POST /mcp` (and `GET`/`DELETE`, which the stateless handler answers 405), outside `/api/v1`,
  mounted when `MCP_ENABLED` (default `false`; 404 when off, like `/dev`).
- Chain: rate limit → access log → `auth.RequireBearerToken` (our verifier; `ResourceMetadataURL`
  set, so a 401 carries the header) → identity middleware → `mcp.NewStreamableHTTPHandler` with
  `Stateless: true`, one shared `mcp.Server`.
- **Verifier:** `Authenticator.Verify`, then: `mcp:*` ability present, audience equals
  `APP_URL/mcp`, not expired, grant not revoked. Returns `TokenInfo{UserID, Scopes, Expiration}`
  and puts the full `authn.Identity` plus the grant's tenant/branch in the context.
- **Origin:** a request with an `Origin` header not in `CORS_ALLOWED_ORIGINS` gets 403
  (DNS-rebinding protection; hosted Claude and native clients send none or an allowed one).
- **Limits:** request body `MCP_MAX_REQUEST_BYTES` (1 MiB), result `MCP_MAX_RESULT_BYTES`
  (256 KiB), the global timeout, the per-IP limiter and a per-token limiter `MCP_RATE_LIMIT`
  (120 calls a minute).
- `serverInfo`: the product's title and `VERSION`.

### How a tool runs: through the router

A tool does **not** call a service. It builds an in-process `http.Request` for one existing
operation and serves it through the `/api/v1` sub-router. The response body is the tool result.

```
agent ─POST /mcp─▶ bearer + identity ─▶ mcp.Server ─tool "create_branch"─▶ dispatcher
        in-process POST /api/v1/branch (identity in the context, acting headers from the grant)
        └─▶ tenancy.Resolve ─▶ authz.Gate(branch.create) ─▶ quota ─▶ handler ─▶ service ─▶ SQL
```

- **No second authorization path.** Permission gates, tenancy, plan limits, validation and
  RFC 9457 errors apply unchanged. An agent can never do more than its user can with `curl`.
- **Identity hand-off:** the identity goes into the sub-request context with an unexported dispatch
  marker. `authn.Authenticate` sees the marker and does not read `Authorization`. The token never
  leaves `/mcp`.
- **Tenant:** for a platform account, the grant's tenant/branch become `X-Acting-Tenant` /
  `X-Acting-Branch` on every sub-request (writes need them, as for the super admin today). No other
  header is forwarded except `Accept-Language`. Act-as-user (`X-Acting-User`) is **not** available
  through a connector in this spec.
- The `/mcp` request ID is reused, so logs, telemetry and the activity log tie the tool call to its
  sub-request. Global middleware runs once, on `/mcp`.
- **Results:** 2xx JSON → `structuredContent` plus a text copy in `content`. 4xx → `isError: true`
  with the problem document, so the model can correct itself. 5xx → `isError: true` with only the
  title, code and request ID. Oversized → a tool error asking for a smaller page.
- **Scopes:** a write tool with an `mcp:read` token → HTTP 403 with
  `WWW-Authenticate: Bearer error="insufficient_scope", scope="mcp:read mcp:write", resource_metadata="…"`,
  so the client can step up. RBAC runs after: scopes cap what a token may attempt, permissions
  decide what the user may do.

### Tools come from the OpenAPI document

An operation becomes a tool with an `x-mcp` extension:

```yaml
/api/v1/branch:
  post:
    operationId: createBranch
    x-mcp:
      tool: create_branch      # snake_case, unique; defaults to operationId in snake_case
      scope: mcp:write         # default: mcp:read for GET, mcp:write otherwise
      # title/description default to the operation's summary/description;
      # readOnly/destructive/idempotent hints default from the method
```

At startup `internal/mcp` reads the embedded contract with kin-openapi: input schema = path and
query parameters plus the JSON body under `body`; output schema = the 2xx schema with `$ref`s
resolved; annotations from the method; tools sorted by name. A test fails when an `x-mcp`
operation has no route, two tools share a name, a tool is multipart, a `DELETE` is not marked
destructive, or a schema is invalid. `ims gen module` emits a commented-out `x-mcp` block.

**The basic tools in this spec** (enough to prove read, write, tenancy and audit; nothing more):

| Tool | Operation | Scope |
| --- | --- | --- |
| `whoami` | `GET /api/v1/user/profile` | read |
| `list_branches` | `GET /api/v1/branch` | read |
| `create_branch` | `POST /api/v1/branch` | write |
| `list_users` | `GET /api/v1/user` | read |
| `create_user` | `POST /api/v1/user` | write |

Everything else (which modules, updates, settings, billing reads, whether deletes are ever
allowed) is **a later spec**.

### Audit and observability

- Every entry written during a tool call carries `source=mcp`, the tool name and the client
  (`client_name` from the grant) in its properties. No new activity columns.
- `oauth.authorized` (consent given, with client and scopes) and `oauth.revoked` are activity
  entries on the user's tenant (or the system log for platform accounts). Sign-in through the
  authorize page writes the usual `auth.login`.
- Request telemetry and the access log tag MCP sub-requests with `via=mcp` and the tool; the
  dashboard's request list shows an "MCP" chip.
- Metrics: `ims_mcp_tool_calls_total{tool,outcome}`, `ims_mcp_tool_duration_seconds{tool}`,
  `ims_oauth_tokens_issued_total{grant_type}`.

## Security notes

- **Prompt injection.** Tool results contain user-entered data. The protections are structural:
  RBAC and tenant scoping bound what an agent can do; write tools carry hints so clients confirm
  them; read-only connections are the consent page's default; no delete tools.
- **Consent phishing.** The consent page always shows the redirect host and the client's own name
  from its document, and warns for loopback-only clients. CIMD URLs are shown in full on hover.
- **SSRF** on CIMD fetch, as above. **Open redirect:** never redirect on a bad client or redirect
  URI.
- **Token handling:** hashed at rest, never logged (the redactor covers `code`, `code_verifier`,
  `refresh_token`, `access_token`), never in URLs, never forwarded.
- **CSRF and clickjacking:** form tokens on the sign-in and consent pages; `X-Frame-Options: DENY`
  and `frame-ancestors 'none'` on `/oauth/*`.
- **Brute force:** the sign-in page uses the login limiter and lockout; `/oauth/token` has its own
  per-IP limiter.

## Scope checklist

### A. Authorization server
- [x] Migration: `oauth_grants`, `oauth_codes`, `oauth_refresh_tokens`, `personal_access_tokens.oauth_grant_id`.
- [x] `internal/modules/oauth`: metadata, authorize (sign-in, OTP, consent with the tenant picker),
      token (code + refresh with rotation and reuse detection), revoke.
- [x] CIMD fetcher with SSRF guards, validation, cache and the optional host allowlist.
- [x] Manual clients: `oauth_clients`, the `oauth_client.manage` permission, the `/api/v1/oauth-client`
      endpoints with OpenAPI, `ims oauth:client`, secret hashing and rotation, `MCP_OAUTH_CIMD_ENABLED`.
- [x] Loopback redirect matching that ignores the port.
- [x] Templates (en/bn), CSRF, cookie, security headers.
- [x] `GET/DELETE /api/v1/user/connections`; revoke on password change, deactivation, logout-all.

### B. MCP endpoint and dispatcher
- [x] Add `github.com/modelcontextprotocol/go-sdk` v1.8.x.
- [x] `internal/mcp`: server (stateless), catalog from `x-mcp`, dispatcher, result mapping, scope
      check with `insufficient_scope`.
- [x] `authn`: the dispatch marker; refuse `mcp:*` tokens on `/api/v1` and `/dev`.
- [x] `internal/app`: keep the `/api/v1` sub-router; mount `/mcp`, `/oauth/*` and the well-known
      routes when `MCP_ENABLED`; Origin check; per-token limiter.
- [x] `x-mcp` on the five basic operations; catalog tests.

### C. Audit and observability
- [x] `source=mcp`, tool and client on activity entries; `oauth.authorized` / `oauth.revoked`.
- [x] Telemetry and access log `via=mcp`; dashboard chip; metrics.

### Docs
- [x] `docs/guides/mcp.md`, "Connect an AI agent": enabling it; adding the connector in claude.ai /
      Desktop, Claude Code, ChatGPT, Cursor and VS Code; testing locally (Claude Code on localhost,
      hosted Claude through a tunnel); platform accounts and the tenant picker; exposing an
      operation with `x-mcp`; security.
- [x] Configuration reference, HTTP API reference (new routes), home page feature list,
      architecture diagram, CHANGELOG.

## Out of scope (later specs)

- **Which operations are tools** beyond the five above: the MCP coverage spec.
- Dynamic Client Registration (deprecated; add only if a client we need has neither CIMD nor a
  field for a client ID).
- Static, pasted MCP tokens (Claude's `static_headers` is beta) and the `ims mcp` stdio mode.
- Act-as-user through a connector.
- An external IdP as the authorization server (the design keeps it possible: the protected
  resource metadata points at any issuer, and the verifier can become a JWT check).
- Prompts, resources, sampling, elicitation, subscriptions, tasks; delete, bulk, upload and
  import/export tools.
- Listing in Claude's connector directory.

## Contracts touched

- **DB:** `oauth_clients`, `oauth_grants`, `oauth_codes`, `oauth_refresh_tokens`; `personal_access_tokens.oauth_grant_id`.
- **API:** `POST /mcp`; `GET /.well-known/oauth-protected-resource[/mcp]`,
  `GET /.well-known/oauth-authorization-server`; `/oauth/authorize` (+ `/login`, `/consent`),
  `/oauth/token`, `/oauth/revoke`; `GET/DELETE /api/v1/user/connections`;
  `/api/v1/oauth-client` (CRUD + `rotate-secret`).
- **OpenAPI:** the `x-mcp` vendor extension, documented in `docs/guides/handlers-and-routes.md`.
- **Config:** `MCP_ENABLED` (false), `MCP_ACCESS_TOKEN_TTL` (1h), `MCP_REFRESH_TOKEN_TTL` (720h),
  `MCP_OAUTH_CIMD_ENABLED` (true), `MCP_OAUTH_SECRET_GRACE` (24h), `MCP_OAUTH_ALLOWED_CLIENT_HOSTS` (empty), `MCP_MAX_REQUEST_BYTES` (1 MiB),
  `MCP_MAX_RESULT_BYTES` (256 KiB), `MCP_RATE_LIMIT` (120/min). `APP_URL` must be the public HTTPS
  URL (it is the issuer and the resource).
- **Permissions:** `oauth_client.manage` (system). Connections stay self-service and capped by
  the user's permissions.
- **CLI:** `ims oauth:client create|list|rotate|delete`.

## Acceptance criteria

**Connector flow**
- With `MCP_ENABLED=false`, `/mcp`, `/oauth/*` and the well-known routes answer 404.
- `POST /mcp` without a token: 401 with `WWW-Authenticate: Bearer resource_metadata="…"`; the
  metadata documents contain every field listed above, `resource` = `APP_URL/mcp`, `issuer` = `APP_URL`.
- **claude.ai custom connector** (through a tunnel) and **Claude Code** (`claude mcp add
  --transport http`, on localhost) both connect with no registration step, sign in, consent, list
  the five tools and call `create_branch` successfully.
- The MCP Inspector completes the same flow.

**Authorization server**
- Missing PKCE, `plain`, a wrong `code_verifier`, a wrong `resource`, a reused code and an expired
  code are each refused; a reused code revokes its grant.
- A CIMD whose `client_id` differs from its URL, a redirect URI not in the document, a private-IP
  `client_id` host and an oversized document are refused without redirecting.
- Loopback redirects match on any port; non-loopback redirects need an exact match.
- A manually registered client completes the same flow in claude.ai (Advanced settings → client
  ID and secret) and with the MCP Inspector; a wrong secret, an inactive client, a redirect URI
  not on the client, or a user outside the client's tenant is refused; deleting the client stops
  its agents; a rotated secret keeps working for the grace period only.
- With `MCP_OAUTH_CIMD_ENABLED=false`, URL client ids are refused and the metadata stops
  advertising CIMD.
- Refresh rotates; reusing the old refresh token revokes the grant; a dead one gets `invalid_grant`.
- Revoking a connection, changing the password or deactivating the user stops the agent (401).

**Tokens and scopes**
- An MCP access token gets 401 on `/api/v1/*` and `/dev/api/*`; a session token gets 401 on `/mcp`.
- A token for another audience gets 401.
- A read-only connection calling `create_branch` gets 403 `insufficient_scope` naming `mcp:write`.

**Permissions and tenancy**
- A tenant user without `user.create` calling `create_user` gets a tool error with
  `INSUFFICIENT_PERMISSIONS`, the same as the REST call; a plan limit gives the 402 problem.
- A tenant-A user's agent never sees tenant-B rows (an MCP variant in the isolation suite).
- A platform account that picked tenant 1 creates a branch in tenant 1; one that picked "no tenant"
  gets the same 403 the super admin gets without acting headers.

**Results and audit**
- A validation failure is `isError: true` with the 422 problem; a 5xx has no internals.
- The branch created by `create_branch` has an activity entry with `source=mcp`, the tool and the
  client name; consent wrote `oauth.authorized`; the request shows the MCP chip; metrics count it.
- Adding `x-mcp` to a generated module's list operation exposes it with no Go change.

## Decisions (2026-11-21, accepted)

1. **Sign-in page:** plain and neutral, titled with the product's name (`APP_NAME`).
2. **Platform accounts** pick a tenant (and branch) on the consent page, or "no tenant: system
   work only".
3. **Lifetimes:** access token 1 h, refresh token 30 days, sliding.
4. **Read-only by default:** the consent page asks for changes separately ("Allow changes"),
   unticked even when the client asks for `mcp:write`: the user decides.

## Deviations (as built, 2026-11-21)

1. **CLI names** are `ims oauth:client:create|list|rotate|delete`, in the style of the other
   commands (`backup:run`), not `ims oauth:client create`.
2. **Consent is not remembered.** Every authorization shows the consent page (one click). A
   remembered consent would skip the read/write choice and, for platform accounts, the tenant pick.
3. **Platform connections work in the picked tenant's first branch.** There is no branch picker.
4. **No `auth.login` entry** for the authorize page's sign-in (the REST login writes none either);
   `oauth.authorized` is the record. Platform accounts' `oauth.authorized` / `oauth.revoked` go to
   the system log, tenant users' to their tenant.
5. **Telemetry needs no new column.** MCP calls are the requests on route `/mcp`; the access log
   adds `via=mcp mcp_tool=…`, and the dashboard shows the MCP chip from the route. Metrics:
   `ims_mcp_tool_calls_total{tool,outcome}` only, since the metrics registry has counters, not
   custom histograms.
6. **`/oauth/token` has no limiter of its own.** The global per-IP limit applies, and the sign-in
   page uses the login throttles and lockout.
7. **No password-reset link** on the sign-in page: the config has no single front-end reset page
   (`AUTH_PASSWORD_RESET_URL` is a per-token template). `NewPages` takes a URL for products that
   have one.
8. **The 401 challenge carries no `scope`.** The SDK's `RequireBearerToken` writes the header;
   clients take the scopes from the protected resource metadata (`scopes_supported`), which the
   spec allows.
9. **Activity stamp key** is `_via` in an entry's changes: `{"source": "mcp", "tool", "client"}`.
10. **Loopback redirects match any loopback host**, not only any port: `localhost`, `127.0.0.1`
    and `::1` are the same computer, and tools differ in which one they use (`mcp-remote` returns
    to `127.0.0.1` even for a client registered with `localhost`). Found connecting Claude Desktop.
11. **`make dev` forwards `/mcp`, `/oauth` and `/.well-known`** from the dashboard dev server
    (`web/dev/vite.config.ts`), so the connector works on `localhost:8080` like the rest of the
    API. Found connecting Claude Desktop.
12. **`ims gen module`** writes the hint as a YAML comment (`# x-mcp: {tool: list_…}`) on the list
    operation, and `ims gen openapi` keeps a mark someone enabled inside the generated region.

Verified live against a local instance with Claude Code's real client metadata document
(`https://claude.ai/oauth/claude-code-client-metadata`): 39 of 39 checks, covering discovery, sign-in,
consent, code exchange and reuse, the five tools, 2025-11-25 and 2026-07-28 requests, token separation,
read-only scope challenge, refresh rotation and reuse, disconnect, a registered confidential client
(HTTP Basic, deactivation), refusals that never redirect, and the audit entries. claude.ai through a
tunnel was not exercised in this session.

Then connected for real from Claude Desktop (through `mcp-remote` with a registered public client)
against `make dev`: sign-in, consent with the tenant picker, `whoami`, `list_branches`, `list_users`,
`create_branch` (activity entry with `_via` naming Claude Desktop) and a validation refusal. The
steps are the [Connect an AI agent](../docs/tutorial/connect-an-ai-agent.mdx) tutorial.
