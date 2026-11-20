# Phase 22 — MCP server

**Status:** Draft
**Source:** project-owner request (2026-11-20): "MCP is now a must for a lot of systems"
**Depends on:** Phase 2 (tokens, tenancy, RBAC), Phase 10 (OpenAPI as the contract), Phase 17
(activity log), Phase 21 (act-as-user; optional, see §D)

## Goal

Let AI agents (Claude Code, Claude Desktop and claude.ai connectors, ChatGPT, Cursor, VS Code
Copilot and others) work with a product built on the starter through the
[Model Context Protocol](https://modelcontextprotocol.io). The API gains one endpoint, `/mcp`,
that exposes a **curated set of tools** (list users, read the activity log, check a subscription,
toggle a feature flag…). Each tool runs **exactly** like the matching REST call: the caller's
identity, permissions, tenant scoping, validation, errors and audit trail are the same. Nothing
about authorization is re-implemented for agents.

Every product forked from the starter gets this for free: adding a tool to a new module is one
OpenAPI extension on an existing operation.

## Background (researched 2026-11-20)

- **Protocol.** The current revision is
  [2026-07-28](https://modelcontextprotocol.io/specification/2026-07-28/changelog). It is the
  biggest change since launch, and it fits this API well:
  - **Stateless.** The `initialize` handshake and protocol sessions (`Mcp-Session-Id`) are gone.
    Every request carries its protocol version and client capabilities in `_meta`. No sticky
    sessions, no session store: any API instance can answer any request, like the REST API today.
  - **Streamable HTTP** is the remote transport: JSON-RPC over `POST /mcp`. The `Mcp-Method` and
    `Mcp-Name` headers are required on each POST. The old HTTP+SSE transport is deprecated.
  - `server/discover` replaces the handshake for advertising capabilities.
  - Roots, Sampling and Logging are deprecated; tasks moved to an optional extension. This spec
    uses none of them.
  - Most clients in the field still speak **2025-11-25** (session-based). The server must accept
    both for now.
- **Authorization** ([spec](https://modelcontextprotocol.io/specification/2026-07-28/basic/authorization)).
  The MCP server is an OAuth 2.1 **resource server**. It MUST:
  - publish RFC 9728 Protected Resource Metadata and point to it from
    `WWW-Authenticate: Bearer resource_metadata="…"` on 401;
  - accept only tokens issued **for itself** (audience, RFC 8707) and never pass a token through to
    another service;
  - answer 403 `error="insufficient_scope"` with the needed `scope` when a token lacks a scope.

  Clients register through Client ID Metadata Documents (CIMD) or pre-registration. Dynamic Client
  Registration (RFC 7591) is deprecated but still used by many clients. Clients must check `iss`
  (RFC 9207) and send `resource` (RFC 8707).
- **Go SDK.** [`github.com/modelcontextprotocol/go-sdk`](https://github.com/modelcontextprotocol/go-sdk)
  is the official SDK. v1.8.0 is the latest stable release; it supports 2026-07-28 and every
  earlier revision. What it provides:
  - `mcp.NewServer`, `Server.AddTool`, `AddResource` and `AddPrompt`;
  - `mcp.NewStreamableHTTPHandler(getServer, opts)`;
  - `auth.RequireBearerToken(verifier, opts)`, where `TokenVerifier` is
    `func(ctx, token string, *http.Request) (*auth.TokenInfo, error)` and
    `TokenInfo{Scopes, Expiration, UserID, Extra}`;
  - `auth.ProtectedResourceMetadataHandler(*oauthex.ProtectedResourceMetadata)`;
  - `oauthex.MatchesResource` for audience checks.

  Its module needs Go 1.25; this repo is on 1.26.
- **Laravel parity.** Neither the Laravel starter nor frc-backend has an MCP server (no
  `laravel/mcp`). The design is new and is decided here.

## Design

### The one decision that matters: tools dispatch through the router

A tool does **not** call a service directly. It builds an in-process `http.Request` for one
existing operation and serves it through the API's `/api/v1` router. The response body becomes the
tool result.

```
agent ──POST /mcp──▶ authenticate (MCP token) ──▶ mcp.Server ──tool "list_users"──▶ dispatcher
                                                                                     │
        in-process GET /api/v1/users?search=…  (identity already in the context)   ◀┘
        └─▶ tenancy.Resolve ─▶ authz.Gate(user.view) ─▶ handler ─▶ service ─▶ SQL
```

Why:
- **No second authorization path.** Route gates, `tenancy.Resolve` / `Exempt` / `InternalOnly`,
  quota checks, validation and RFC 9457 errors all apply unchanged. An agent can never do more
  than the same user could do with `curl`. The isolation tests already cover it.
- **The OpenAPI document already describes every input and output.** Tool schemas come from it
  (see below), and `openapi:check` keeps them honest.
- **Every module works the same way.** A module author adds a tool without writing Go.

The cost is a JSON round trip per call, which is negligible next to the model's latency.

**Dispatcher rules:**
- The dispatcher serves the **`/api/v1` sub-router**, not the root router. Global middleware
  (rate limit, access log, metrics, timeout) runs once, on the `/mcp` request. The sub-router is
  kept in `Dependencies` next to `routes`.
- The identity from the MCP request is placed in the sub-request context together with a
  dispatch marker. `authn.Authenticate` sees the marker and passes the request through instead of
  reading `Authorization` again. This is the only change to `authn.Authenticate`, and the marker
  key is unexported, so nothing outside the dispatcher can set it.
- Headers forwarded from the MCP request: an allowlist only: `Accept-Language`,
  `X-Acting-Tenant`, `X-Acting-Branch` and, after Phase 21, `X-Acting-User`. A client sets them
  once in its server config (every MCP client supports static headers).
- The request ID of the `/mcp` call is reused, so logs, telemetry and the activity log tie the
  tool call to its sub-request.
- Responses:
  - 2xx: the JSON body is the tool's `structuredContent`. A text copy goes in `content` for
    clients that ignore structured output.
  - 4xx: a tool result with `isError: true` and the problem document as its content. The model can
    read the error and correct itself (for example a validation error). This is a tool error, not
    a JSON-RPC error, as the MCP spec recommends.
  - 5xx: the same, with only the problem's `title`, `code` and request ID. Internals never reach
    the model.
- Response size: bodies over `MCP_MAX_RESULT_BYTES` (default 256 KiB) are refused with a tool
  error that asks for a smaller page. List tools default to `per_page=20`.

### Tools come from the OpenAPI document

An operation becomes a tool when it carries an `x-mcp` extension:

```yaml
/api/v1/users:
  get:
    operationId: listUsers
    x-mcp:
      tool: list_users              # snake_case, unique; defaults to operationId in snake_case
      title: List users             # optional; defaults to summary
      scope: mcp:read               # mcp:read (default for GET) or mcp:write
      readOnly: true                # ToolAnnotations hints; derived from the method by default
      destructive: false
      # description: defaults to the operation's description
```

At startup, `internal/mcp` reads the embedded OpenAPI document (`api/embed.go`) with kin-openapi,
which is already a dependency:
- **input schema** = the path and query parameters as properties, plus the request body schema
  under `body` (JSON only; multipart operations cannot be tools);
- **output schema** = the 200/201 response schema, with `$ref`s resolved;
- **annotations** = `readOnlyHint`, `destructiveHint` and `idempotentHint` from `x-mcp` or from the
  method (GET → read-only; PUT → idempotent; DELETE → destructive);
- the tool list is sorted by name, as 2026-07-28 asks for deterministic lists.

A test fails the build when:
- an `x-mcp` operation has no route;
- two tools share a name;
- a tool would be multipart;
- a `DELETE` operation is marked without `destructive: true`;
- a schema fails JSON Schema 2020-12 validation (the SDK's input check).

`ims gen module` emits commented-out `x-mcp` blocks on the operations it scaffolds, and the
`add-module` skill explains when to enable them.

**The v1 catalog.** It is curated: an agent gets fewer, clearer tools. Deletes are left out.

| Tool | Operation | Scope |
| --- | --- | --- |
| `whoami` | `GET /api/v1/user/profile` (user, tenant, branch, roles) | read |
| `list_users`, `get_user` | users | read |
| `create_user`, `update_user` | users | write |
| `list_branches` | branches | read |
| `list_roles`, `list_permissions` | roles, permission catalog | read |
| `search_activity` | activity log, with filters | read |
| `list_notifications`, `mark_notification_read` | notifications | read / write |
| `get_subscription`, `list_packages` | billing (status, public packages) | read |
| `list_settings`, `update_setting` | settings | read / write |
| `list_feature_flags`, `check_feature_flag`, `set_feature_flag` | feature flags | read / write |
| `list_tenants`, `get_tenant` | tenants (internal accounts only, by `InternalOnly`) | read |

If an operation is missing, it is added to the REST API
first and then exposed. The MCP layer never gets endpoints of its own.

### Resources

Two read-only resources:
- `ims://openapi.yaml`: the API contract, so an agent can reason about what exists;
- `ims://me`: the same JSON as `whoami`.

No prompts in v1.

### Endpoint, transport and protocol versions

- **Route:** `POST /mcp` (Streamable HTTP), outside `/api/v1`, mounted only when `MCP_ENABLED`
  (default `false`). When disabled it answers 404, like `/dev`.
- **Handler:** one `mcp.Server` built at startup, shared by every request;
  `mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, opts)`. It runs in
  stateless mode where the SDK version offers it, so 2026-07-28 clients need no session.
  2025-11-25 clients still get the session the SDK keeps in memory. Sessions are not shared across
  instances, so multi-instance deploys of old clients need sticky routing; this is documented, not
  solved.
- **Origin:** as the transport spec requires, requests with an `Origin` that is not in
  `CORS_ALLOWED_ORIGINS` get 403 (DNS-rebinding protection). Native clients send no `Origin`.
- **Limits:**
  - request body: `MCP_MAX_REQUEST_BYTES` (default 1 MiB);
  - the global request timeout applies;
  - the per-IP limiter applies, plus a per-token limiter, `MCP_RATE_LIMIT` (default 120 calls a
    minute).

### Authentication: MCP tokens first, OAuth second

**A. MCP access tokens (v1, required).**
- Users create them in the API: `POST /api/v1/user/mcp-tokens` takes `name`, `scopes` (`mcp:read`,
  `mcp:write`) and `expires_in_days` (default 90, maximum 365). The plain text is shown once.
  `GET` lists them (no secrets) and `DELETE /{id}` revokes one.
- They are stored in `personal_access_tokens`, a table that already exists. `name = 'mcp'`, and
  `abilities` (unused so far) holds the JSON list of scopes.
- **Separate from session tokens:**
  - `/mcp` accepts only tokens whose `abilities` include an `mcp:*` scope;
  - `authn.Authenticate` rejects tokens with `mcp:*` abilities on `/api/v1` (401
    `TOKEN_INVALID`).

  A leaked mobile session cannot drive an agent, and a leaked agent token cannot call the REST
  API, the session routes or `/dev`.
- **Wiring:** `auth.RequireBearerToken` wraps the MCP handler. Its `TokenVerifier` calls
  `Authenticator.Verify`, checks the abilities and returns `TokenInfo{UserID, Scopes, Expiration}`.
  Expiry is mandatory for MCP tokens. A small middleware then places the full `authn.Identity` in
  the context.
- **Scope check per tool:** in the dispatcher. A `mcp:write` tool called with a read-only token
  gets 403 `insufficient_scope`, as the authorization spec requires. RBAC still runs after it:
  scopes cap what a token may attempt, permissions decide what the user may do.
- **Metadata:** `GET /.well-known/oauth-protected-resource/mcp` and
  `GET /.well-known/oauth-protected-resource` are served by `auth.ProtectedResourceMetadataHandler`.
  They report:
  - `resource` = `APP_URL/mcp`;
  - `scopes_supported` = `mcp:read`, `mcp:write`;
  - `bearer_methods_supported` = `header`;
  - `authorization_servers`, only when part C is enabled.
- **Who it works with:** clients that accept a static bearer header. That is Claude Code, Cursor,
  VS Code, the MCP Inspector, Claude Desktop through a config file, and server-side agents built
  on the Claude Agent SDK or the OpenAI Agents SDK. That covers developers and internal
  automation, which is the main audience of a starter.
- **Dashboard:** the profile area in the dev dashboard gets "MCP tokens" (create, copy once,
  revoke), with ready-to-paste client configs (see Docs).

**C. Built-in OAuth 2.1 authorization server (v2, behind `MCP_OAUTH_ENABLED`).**
- **Why:** consumer hosts (claude.ai custom connectors, ChatGPT apps) do not take pasted tokens.
  They run the OAuth flow from the protected-resource metadata.
- **Scope of the work:**
  - RFC 8414 metadata at `/.well-known/oauth-authorization-server`, with
    `authorization_response_iss_parameter_supported: true`;
  - `GET/POST /oauth/authorize`: authorization code with **PKCE S256 only**. A server-rendered
    sign-in page (e-mail + password + OTP, reusing `authn.Service`), then a consent page that lists
    the client, its redirect URI and the requested scopes. `iss` is returned (RFC 9207);
  - `POST /oauth/token`: `authorization_code` and `refresh_token` grants. The `resource` parameter
    is required and must equal `APP_URL/mcp` (RFC 8707). It issues the same MCP tokens as part A,
    plus refresh tokens with rotation and reuse detection;
  - **client registration:**
    - CIMD: `client_id` is an HTTPS URL; the metadata document is fetched with SSRF guards
      (public addresses only, 5 s timeout, 64 KiB cap, cached by `Cache-Control`);
    - pre-registered clients in config;
    - DCR at `/oauth/register`, behind its own flag, deprecated by the spec but still needed by
      some clients;
  - tables: `oauth_clients`, `oauth_codes`, `oauth_refresh_tokens`;
  - the users' MCP-token list shows OAuth grants too, and revoking one revokes its refresh tokens.
- **Alternative, documented:** point `authorization_servers` at an external IdP (Auth0, WorkOS,
  Keycloak, Cognito) and switch the verifier to JWT validation (issuer, audience = `APP_URL/mcp`,
  JWKS). The `Authenticator` interface already allows a JWT driver. Teams that already run an IdP
  should use it instead of part C.
- **Split:** part C is large. It ships as its own PR after A, B and D are merged and used.

### D. Tenancy, impersonation and audit

- **Tenancy:** tools run through `tenancy.Resolve`, so a tenant user's agent sees only its tenant
  and active branch. An internal account's agent uses `X-Acting-Tenant` / `X-Acting-Branch` from
  its client config, or the tenant-exempt tools.
- **Per-tenant switch:** a feature flag `mcp` (default on when `MCP_ENABLED`). A tenant that turns
  it off gets 403 `FEATURE_NOT_AVAILABLE` on every tool call. Products can sell agent access as a plan
  feature through the existing package features.
- **Impersonation:** after Phase 21, an MCP token with `X-Acting-User` acts as that user, under
  the same rules (the token's owner needs `user.impersonate`).
- **Audit:**
  - the activity log gets `source` (`api` | `mcp`) and, for MCP, the client name from
    `_meta.clientInfo` and the tool name. Both go in the entry's properties: no new columns;
  - request telemetry tags MCP sub-requests with `via=mcp` and the tool name, and the dashboard's
    request list shows an "MCP" badge;
  - metrics: `ims_mcp_tool_calls_total{tool,outcome}` and `ims_mcp_tool_duration_seconds{tool}`.

### E. Local stdio mode

`ims mcp` (new CLI command) runs the same `mcp.Server` over stdio for clients that launch a local
process:
- it authenticates with `IMS_MCP_TOKEN` (an MCP token, verified exactly as over HTTP);
- it dispatches in-process against the same router;
- it needs the database (and Redis when configured), like `ims serve`.

It is useful for local development against a dev database. Production use is HTTP.

## Security notes

- **Prompt injection.** Tool results contain user-entered data (names, activity descriptions,
  settings). The protections are structural, not textual:
  - read-only tokens by default: the dashboard preselects `mcp:read`;
  - no delete tools in v1;
  - write tools carry `destructiveHint` / `readOnlyHint` so clients ask the user before calling
    them;
  - RBAC and tenant scoping bound the blast radius to what the user could do anyway.
- **Data exposure.** Tool outputs pass through the same response DTOs as the REST API, so
  password hashes, tokens and secrets are never present. The security-redaction integration test
  gains an MCP case.
- **No token passthrough.** The dispatcher never forwards `Authorization` anywhere. The
  sub-request carries the identity in the context, and the token never leaves `/mcp`.
- **Origin check and body limit.** Both are described above. Neither the MCP request nor its tool
  arguments are logged verbatim: the access log records the tool name and the outcome.

## Scope checklist

### A. Server, dispatcher and tool catalog
- [ ] Add `github.com/modelcontextprotocol/go-sdk` (pin the v1.8.x line).
- [ ] `internal/mcp`:
  - `server.go`: build the `mcp.Server` and register tools and resources;
  - `catalog.go`: read `x-mcp` from the embedded OpenAPI and build the schemas and annotations;
  - `dispatch.go`: the in-process request, forwarded headers, result mapping, size cap.
- [ ] `authn`: the dispatch marker in `Authenticate`; rejecting `mcp:*` tokens on `/api/v1`.
- [ ] `internal/app`: keep the `/api/v1` sub-router; mount `/mcp` and the well-known routes when
      `MCP_ENABLED`; check `Origin`; add the per-token limiter.
- [ ] `x-mcp` on the v1 catalog operations.
- [ ] Catalog tests (route exists, unique names, no multipart, DELETE marked destructive, schemas
      valid).
- [ ] The `ims://openapi.yaml` and `ims://me` resources.

### B. MCP tokens
- [ ] `POST/GET/DELETE /api/v1/user/mcp-tokens` (self-service, no catalog permission: the token is
      capped by the user's own permissions), in the `auth` module, with OpenAPI.
- [ ] The token verifier, the `auth.RequireBearerToken` wiring and the protected-resource metadata.
- [ ] Scope enforcement per tool, answering 403 `insufficient_scope`.
- [ ] Dev dashboard: an "MCP tokens" panel with copyable client configs.

### C. OAuth 2.1 authorization server (separate PR, `MCP_OAUTH_ENABLED`)
- [ ] AS metadata, authorize (sign-in + consent pages), token, refresh rotation.
- [ ] CIMD fetch with SSRF guards; pre-registered clients; DCR behind a flag.
- [ ] Migrations: `oauth_clients`, `oauth_codes`, `oauth_refresh_tokens`.
- [ ] A conformance run with the MCP Inspector and a claude.ai custom connector.

### D. Tenancy, audit and observability
- [ ] The tenant feature flag `mcp`.
- [ ] Activity-log `source`, client and tool; telemetry `via=mcp`; the dashboard badge; metrics.

### E. stdio
- [ ] The `ims mcp` command.

### Docs
- [ ] A new guide, `docs/guides/mcp.md`, "Connect an AI agent". It covers:
  - enabling the server;
  - creating a token;
  - client configs, for example
    `claude mcp add --transport http ims https://api.example.com/mcp --header "Authorization: Bearer <token>"`,
    plus the Cursor and VS Code `mcp.json` and the Claude Desktop config;
  - acting headers;
  - exposing a module's operation (`x-mcp`);
  - security guidance.
- [ ] The tutorial's "first module" gains an optional step that exposes its list operation as a
      tool.
- [ ] Regenerate the configuration reference; add MCP to the home page's feature list, the
      architecture diagram and the CHANGELOG.

## Out of scope

- Prompts, sampling, roots, elicitation, subscriptions and the tasks extension.
- Delete tools, bulk tools, file upload tools (multipart) and import/export as tools. Agents can
  start an export job through a normal JSON operation later, if one is marked.
- An MCP **client** inside the API (calling other MCP servers).
- Exposing `/dev/api` as tools.
- Shared session state for 2025-11-25 clients across instances.

## Contracts touched

- **DB:**
  - A/B: none. MCP tokens use `personal_access_tokens.abilities`;
  - C: `oauth_clients`, `oauth_codes`, `oauth_refresh_tokens`.
- **API:**
  - `POST /mcp`;
  - `GET /.well-known/oauth-protected-resource[/mcp]`;
  - `POST/GET/DELETE /api/v1/user/mcp-tokens`;
  - C: `/.well-known/oauth-authorization-server`, `/oauth/authorize`, `/oauth/token`,
    `/oauth/register`.
- **OpenAPI:** the `x-mcp` vendor extension, documented in `docs/guides/handlers-and-routes.md`.
- **Config:**
  - `MCP_ENABLED` (false), `MCP_MAX_REQUEST_BYTES` (1 MiB), `MCP_MAX_RESULT_BYTES` (256 KiB),
    `MCP_RATE_LIMIT` (120/min), `MCP_TOKEN_MAX_DAYS` (365);
  - C: `MCP_OAUTH_ENABLED`, `MCP_OAUTH_DCR_ENABLED`, `MCP_OAUTH_CLIENTS` (pre-registered).
- **CLI:** `ims mcp`.
- **Permissions:** none new. Tokens are self-service and capped by the user's own permissions.

## Acceptance criteria

**Protocol and tools**
- With `MCP_ENABLED=false`, `/mcp` and the well-known routes answer 404.
- The MCP Inspector, connected with an MCP token:
  - lists the v1 tools in name order, each with input and output schemas;
  - calls `list_users` and gets the same JSON as `GET /api/v1/users`.
- Both protocol revisions work: a 2026-07-28 client (no handshake, `Mcp-Method` header) and a
  2025-11-25 client (initialize + session) can both call tools.
- A request with a foreign `Origin` gets 403.

**Tokens and scopes**
- Without a token, `/mcp` answers 401 with `WWW-Authenticate: Bearer resource_metadata="…"`, and
  the metadata URL returns RFC 9728 JSON with `resource` = `APP_URL/mcp`.
- A session token gets 401 on `/mcp`; an MCP token gets 401 on `/api/v1/users`.
- A read-only token calling `update_user` gets 403 `insufficient_scope` naming `mcp:write`.
- An expired or revoked MCP token gets 401.

**Permissions and tenancy**
- A tenant user without `user.view` calling `list_users` gets a tool error carrying the same
  problem code as the REST call (`INSUFFICIENT_PERMISSIONS`).
- Tenant isolation: a tenant-A user's agent never sees tenant-B rows. The isolation suite gains an
  MCP variant.
- `X-Acting-Tenant` set on the MCP request reaches the sub-request; any other header does not.
- With the tenant flag `mcp` off, tool calls fail with `FEATURE_NOT_AVAILABLE`.

**Results and audit**
- A validation failure comes back as `isError: true` with the 422 problem document. A 5xx comes
  back without internals.
- A write tool's activity entry has `source=mcp`, the tool name and the client name. The request
  shows in the dashboard with the MCP badge, and metrics count the call.
- An oversized result returns a tool error asking for a smaller page, not a truncated body.

**stdio and catalog**
- `ims mcp` with `IMS_MCP_TOKEN` serves the same tools over stdio.
- Adding `x-mcp` to a generated module's list operation exposes it with no Go change, and a
  duplicate tool name fails the catalog test.

## Open questions

1. **Stateless mode in the SDK.** v1.8.0 supports 2026-07-28, but its exact stateless option
   (`StreamableHTTPOptions`) was not confirmed. Check it during implementation; the fallback is
   the SDK's in-memory sessions plus documenting sticky routing.
2. **Static tokens for v1?** This spec ships part A/B first (static bearer tokens: developer
   tools, server-side agents) and part C later (claude.ai and ChatGPT connectors). Is consumer
   connector support needed in the first release? If so, C moves up, or an external IdP is the
   recommended path.
3. **Catalog breadth.** Is the v1 table right? Billing writes (subscribe, approve a payment) are
   deliberately out. Should agents ever move money?
4. **Write tools on by default?** The proposal is that write tools exist but tokens default to
   `mcp:read`. The alternative is `MCP_WRITE_ENABLED=false` server-wide until a product opts in.
5. **Where tokens are managed.** The dev dashboard is developer-only, and the admin dashboard
   (Phase 8) is a stub. Is the API endpoint plus the dev dashboard enough for v1?
