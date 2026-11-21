package app_test

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/testkit"
)

const mcpRedirect = "http://localhost:53682/callback"

// connector drives the OAuth flow and /mcp the way an MCP client and a
// browser would, through the router.
type connector struct {
	t        *testing.T
	app      *testkit.App
	clientID string
	secret   string
	cookies  []*http.Cookie
}

func (c *connector) do(req *http.Request) *httptest.ResponseRecorder {
	for _, cookie := range c.cookies {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	c.app.Router.ServeHTTP(rec, req)
	for _, cookie := range rec.Result().Cookies() {
		if cookie.MaxAge >= 0 {
			c.cookies = []*http.Cookie{cookie}
		}
	}
	return rec
}

func (c *connector) form(path string, values url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return c.do(req)
}

var csrfField = regexp.MustCompile(`name="csrf" value="([0-9a-f]+)"`)

func csrf(t *testing.T, html string) string {
	t.Helper()
	m := csrfField.FindStringSubmatch(html)
	if m == nil {
		t.Fatalf("no csrf field in page: %.300s", html)
	}
	return m[1]
}

// connect signs email in, consents and returns the token response.
func (c *connector) connect(email string, allowWrite bool, tenantID string) map[string]any {
	c.t.Helper()
	c.cookies = nil
	verifier := strings.Repeat("v", 50)
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	query := url.Values{
		"client_id": {c.clientID}, "redirect_uri": {mcpRedirect}, "response_type": {"code"},
		"code_challenge": {challenge}, "code_challenge_method": {"S256"}, "state": {"st"},
		"scope": {"mcp:read mcp:write"}, "resource": {"http://localhost:8080/mcp"},
	}
	page := c.do(httptest.NewRequest(http.MethodGet, "/oauth/authorize?"+query.Encode(), nil))
	if page.Code != http.StatusOK {
		c.t.Fatalf("authorize = %d: %.300s", page.Code, page.Body.String())
	}
	consent := c.form("/oauth/authorize/login", url.Values{
		"csrf": {csrf(c.t, page.Body.String())}, "email": {email}, "password": {testkit.Password},
	})
	if consent.Code != http.StatusOK || !strings.Contains(consent.Body.String(), "decision") {
		c.t.Fatalf("login = %d: %.300s", consent.Code, consent.Body.String())
	}
	values := url.Values{"csrf": {csrf(c.t, consent.Body.String())}, "decision": {"allow"}, "tenant_id": {tenantID}}
	if allowWrite {
		values.Set("allow_write", "1")
	}
	back := c.form("/oauth/authorize/consent", values)
	location, _ := url.Parse(back.Header().Get("Location"))
	if back.Code != http.StatusFound || location.Query().Get("code") == "" {
		c.t.Fatalf("consent = %d, Location %q", back.Code, back.Header().Get("Location"))
	}
	if location.Query().Get("state") != "st" || location.Query().Get("iss") != "http://localhost:8080" {
		c.t.Fatalf("redirect lacks state or iss: %s", location)
	}

	token := url.Values{
		"grant_type": {"authorization_code"}, "code": {location.Query().Get("code")},
		"redirect_uri": {mcpRedirect}, "client_id": {c.clientID}, "code_verifier": {verifier},
		"resource": {"http://localhost:8080/mcp"},
	}
	if c.secret != "" {
		token.Set("client_secret", c.secret)
	}
	rec := c.form("/oauth/token", token)
	if rec.Code != http.StatusOK {
		c.t.Fatalf("token = %d: %s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return out
}

// call sends one JSON-RPC message to /mcp.
func (c *connector) call(token, method string, params any) (*httptest.ResponseRecorder, map[string]any) {
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	c.app.Router.ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	result, _ := out["result"].(map[string]any)
	return rec, result
}

func mcpApp(t *testing.T) (*testkit.App, *connector, *testkit.User) {
	t.Setenv("MCP_ENABLED", "true")
	a := testkit.New(t)
	super := a.SuperAdmin(t)
	var client struct {
		ClientID string `json:"client_id"`
	}
	a.As(super).Post("/api/v1/oauth-client", map[string]any{
		"name": "Test agent", "type": "public", "redirect_uris": []string{"http://localhost/callback"},
	}).Status(t, http.StatusCreated).Data(t, &client)
	return a, &connector{t: t, app: a, clientID: client.ClientID}, super
}

func TestMCPDisabledAnswers404(t *testing.T) {
	a := testkit.New(t)
	for _, path := range []string{"/.well-known/oauth-authorization-server", "/oauth/authorize", "/.well-known/oauth-protected-resource/mcp"} {
		a.Guest().Get(path).Status(t, http.StatusNotFound)
	}
	a.Guest().Post("/mcp", map[string]any{}).Status(t, http.StatusNotFound)
}

func TestMCPConnectorFlow(t *testing.T) {
	a, conn, _ := mcpApp(t)
	tenantUser := a.User(t, testkit.Role("Admin"))

	// No token: 401 pointing at the protected resource metadata.
	rec, _ := conn.call("", "tools/list", map[string]any{})
	if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Header().Get("WWW-Authenticate"), "resource_metadata=") {
		t.Fatalf("no token: %d %q", rec.Code, rec.Header().Get("WWW-Authenticate"))
	}

	tokens := conn.connect(tenantUser.Email, true, "0")
	access := tokens["access_token"].(string)
	if tokens["scope"] != "mcp:read mcp:write" {
		t.Fatalf("scope = %v", tokens["scope"])
	}

	_, result := conn.call(access, "tools/list", map[string]any{})
	tools, _ := result["tools"].([]any)
	if len(tools) != 5 {
		t.Fatalf("tools/list = %v", result)
	}

	_, result = conn.call(access, "tools/call", map[string]any{"name": "create_branch", "arguments": map[string]any{"body": map[string]any{"name": "From MCP"}}})
	if result["isError"] == true {
		t.Fatalf("create_branch: %v", result)
	}
	branch := result["structuredContent"].(map[string]any)["data"].(map[string]any)
	if int64(branch["tenant_id"].(float64)) != tenantUser.TenantID {
		t.Fatalf("branch in tenant %v, want %d", branch["tenant_id"], tenantUser.TenantID)
	}

	// The activity entry says an agent did it.
	var changes []byte
	if err := a.Pool.QueryRow(t.Context(), `SELECT changes FROM activity_log WHERE action = 'branch.created' AND subject_id = $1`,
		int64(branch["id"].(float64))).Scan(&changes); err != nil {
		t.Fatalf("activity: %v", err)
	}
	if !strings.Contains(string(changes), `"source": "mcp"`) && !strings.Contains(string(changes), `"source":"mcp"`) {
		t.Fatalf("activity changes lack the MCP stamp: %s", changes)
	}

	// Validation errors are tool errors carrying the problem.
	_, result = conn.call(access, "tools/call", map[string]any{"name": "create_branch", "arguments": map[string]any{"body": map[string]any{}}})
	if result["isError"] != true || !strings.Contains(toJSON(result), "VALIDATION") {
		t.Fatalf("validation: %v", result)
	}

	// Token kinds stay apart.
	a.As(&testkit.User{Token: access}).Get("/api/v1/user/profile").Status(t, http.StatusUnauthorized)
	rec, _ = conn.call(tenantUser.Token, "tools/list", map[string]any{})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("session token on /mcp = %d", rec.Code)
	}

	// Refresh rotates; the old refresh token is then reuse and revokes all.
	refresh := conn.form("/oauth/token", url.Values{"grant_type": {"refresh_token"}, "refresh_token": {tokens["refresh_token"].(string)}, "client_id": {conn.clientID}})
	if refresh.Code != http.StatusOK {
		t.Fatalf("refresh = %d %s", refresh.Code, refresh.Body.String())
	}
	reuse := conn.form("/oauth/token", url.Values{"grant_type": {"refresh_token"}, "refresh_token": {tokens["refresh_token"].(string)}, "client_id": {conn.clientID}})
	if reuse.Code != http.StatusBadRequest || !strings.Contains(reuse.Body.String(), "invalid_grant") {
		t.Fatalf("reuse = %d %s", reuse.Code, reuse.Body.String())
	}
	var rotated map[string]any
	_ = json.Unmarshal(refresh.Body.Bytes(), &rotated)
	if rec, _ := conn.call(rotated["access_token"].(string), "tools/list", map[string]any{}); rec.Code != http.StatusUnauthorized {
		t.Fatalf("after reuse the connection must be revoked, got %d", rec.Code)
	}
}

func TestMCPReadOnlyAndPermissions(t *testing.T) {
	a, conn, super := mcpApp(t)
	a.Role(t, "Nobody") // a role with no permissions
	viewer := a.User(t, testkit.Role("Nobody"))

	ro := conn.connect(viewer.Email, false, "0")
	if ro["scope"] != "mcp:read" {
		t.Fatalf("scope = %v", ro["scope"])
	}
	rec, _ := conn.call(ro["access_token"].(string), "tools/call", map[string]any{"name": "create_branch", "arguments": map[string]any{"body": map[string]any{"name": "x"}}})
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Header().Get("WWW-Authenticate"), `error="insufficient_scope"`) {
		t.Fatalf("read-only write = %d %q", rec.Code, rec.Header().Get("WWW-Authenticate"))
	}
	_, result := conn.call(ro["access_token"].(string), "tools/call", map[string]any{"name": "list_users", "arguments": map[string]any{}})
	if result["isError"] != true || !strings.Contains(toJSON(result), "INSUFFICIENT_PERMISSIONS") {
		t.Fatalf("no permission: %v", result)
	}

	// A platform account that picked tenant 1 writes there; disconnecting
	// stops it.
	tokens := conn.connect(super.Email, true, "1")
	_, result = conn.call(tokens["access_token"].(string), "tools/call", map[string]any{"name": "create_branch", "arguments": map[string]any{"body": map[string]any{"name": "Platform via MCP"}}})
	if result["isError"] == true {
		t.Fatalf("platform create_branch: %v", result)
	}
	var conns []struct {
		ID int64 `json:"id"`
	}
	a.As(super).Get("/api/v1/user/connections").Status(t, http.StatusOK).Data(t, &conns)
	if len(conns) != 1 {
		t.Fatalf("connections = %v", conns)
	}
	a.As(super).Delete("/api/v1/user/connections/"+itoa(conns[0].ID)).Status(t, http.StatusNoContent)
	if rec, _ := conn.call(tokens["access_token"].(string), "tools/list", map[string]any{}); rec.Code != http.StatusUnauthorized {
		t.Fatalf("after disconnect = %d", rec.Code)
	}
}

func TestMCPAuthorizeRefusals(t *testing.T) {
	_, conn, _ := mcpApp(t)
	cases := map[string]url.Values{
		"unknown client":       {"client_id": {"ims_nope"}, "redirect_uri": {mcpRedirect}},
		"foreign redirect":     {"client_id": {conn.clientID}, "redirect_uri": {"https://evil.example/cb"}},
		"private CIMD address": {"client_id": {"https://127.0.0.1/client.json"}, "redirect_uri": {mcpRedirect}},
	}
	for name, query := range cases {
		query.Set("response_type", "code")
		query.Set("code_challenge", strings.Repeat("c", 43))
		query.Set("code_challenge_method", "S256")
		rec := conn.do(httptest.NewRequest(http.MethodGet, "/oauth/authorize?"+query.Encode(), nil))
		if rec.Code != http.StatusBadRequest || rec.Header().Get("Location") != "" {
			t.Errorf("%s: %d, Location %q (must show an error, never redirect)", name, rec.Code, rec.Header().Get("Location"))
		}
	}
	missingPKCE := url.Values{"client_id": {conn.clientID}, "redirect_uri": {mcpRedirect}, "response_type": {"code"}}
	rec := conn.do(httptest.NewRequest(http.MethodGet, "/oauth/authorize?"+missingPKCE.Encode(), nil))
	if rec.Code != http.StatusFound || !strings.Contains(rec.Header().Get("Location"), "error=invalid_request") {
		t.Errorf("missing PKCE: %d %q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestMCPRegisteredConfidentialClient(t *testing.T) {
	a, conn, super := mcpApp(t)
	var client struct {
		ID       int64  `json:"id"`
		ClientID string `json:"client_id"`
		Secret   string `json:"client_secret"`
	}
	a.As(super).Post("/api/v1/oauth-client", map[string]any{
		"name": "Ops", "type": "confidential", "redirect_uris": []string{mcpRedirect}, "scopes": []string{"mcp:read"},
	}).Status(t, http.StatusCreated).Data(t, &client)
	if client.Secret == "" {
		t.Fatal("a confidential client gets its secret on creation")
	}
	a.As(super).Get("/api/v1/oauth-client/"+itoa(client.ID)).Status(t, http.StatusOK)

	conn.clientID, conn.secret = client.ClientID, client.Secret
	tokens := conn.connect(super.Email, true, "0")
	if tokens["scope"] != "mcp:read" {
		t.Fatalf("a client capped at mcp:read got %v", tokens["scope"])
	}

	// A tenant user without oauth_client.manage cannot register clients.
	admin := a.User(t, testkit.Role("Admin"))
	a.As(admin).Get("/api/v1/oauth-client").Status(t, http.StatusForbidden)

	a.As(super).Delete("/api/v1/oauth-client/"+itoa(client.ID)).Status(t, http.StatusNoContent)
	if rec, _ := conn.call(tokens["access_token"].(string), "tools/list", map[string]any{}); rec.Code != http.StatusUnauthorized {
		t.Fatalf("deleting the client must stop its agents, got %d", rec.Code)
	}
}

func toJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func itoa(v int64) string {
	b, _ := json.Marshal(v)
	return string(b)
}
