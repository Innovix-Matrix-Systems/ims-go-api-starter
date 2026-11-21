package oauth

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/oauthex"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/mcp"
)

// Endpoint paths.
const (
	AuthorizePath   = "/oauth/authorize"
	TokenPath       = "/oauth/token"
	RevokePath      = "/oauth/revoke"
	ASMetadataPath  = "/.well-known/oauth-authorization-server"
	maxTokenRequest = 16 << 10
)

// Metadata is RFC 8414 authorization server metadata. Claude uses the
// client's metadata document (no registration) only when both
// client_id_metadata_document_supported and "none" are advertised.
func (s *Service) Metadata(w http.ResponseWriter, _ *http.Request) {
	issuer := s.settings.Issuer
	doc := map[string]any{
		"issuer":                                         issuer,
		"authorization_endpoint":                         issuer + AuthorizePath,
		"token_endpoint":                                 issuer + TokenPath,
		"revocation_endpoint":                            issuer + RevokePath,
		"response_types_supported":                       []string{"code"},
		"response_modes_supported":                       []string{"query"},
		"grant_types_supported":                          []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":               []string{"S256"},
		"token_endpoint_auth_methods_supported":          []string{"none", "client_secret_basic", "client_secret_post"},
		"revocation_endpoint_auth_methods_supported":     []string{"none", "client_secret_basic", "client_secret_post"},
		"scopes_supported":                               []string{mcp.ScopeRead, mcp.ScopeWrite},
		"authorization_response_iss_parameter_supported": true,
	}
	if s.clients.cimd {
		doc["client_id_metadata_document_supported"] = true
	}
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cache-Control", "public, max-age=300")
	writeJSON(w, http.StatusOK, doc)
}

// ProtectedResource is RFC 9728 metadata for /mcp: which authorization
// server issues its tokens.
func (s *Service) ProtectedResource() http.Handler {
	return auth.ProtectedResourceMetadataHandler(&oauthex.ProtectedResourceMetadata{
		Resource:               s.settings.Resource,
		AuthorizationServers:   []string{s.settings.Issuer},
		ScopesSupported:        []string{mcp.ScopeRead, mcp.ScopeWrite},
		BearerMethodsSupported: []string{"header"},
	})
}

// Token is POST /oauth/token.
func (s *Service) Token(w http.ResponseWriter, r *http.Request) {
	corsOpen(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxTokenRequest)
	if err := r.ParseForm(); err != nil {
		tokenError(w, oauthErr("invalid_request", "The body must be application/x-www-form-urlencoded."))
		return
	}
	client, failure := s.authenticateClient(r)
	if failure != nil {
		tokenError(w, failure)
		return
	}
	req := TokenRequest{
		Client:       client,
		GrantType:    r.PostForm.Get("grant_type"),
		Code:         r.PostForm.Get("code"),
		RedirectURI:  r.PostForm.Get("redirect_uri"),
		CodeVerifier: r.PostForm.Get("code_verifier"),
		RefreshToken: r.PostForm.Get("refresh_token"),
		Resource:     r.PostForm.Get("resource"),
	}

	var (
		resp *TokenResponse
		err  error
	)
	switch req.GrantType {
	case "authorization_code":
		resp, err = s.Exchange(r.Context(), req)
	case "refresh_token":
		resp, err = s.Refresh(r.Context(), req)
	default:
		tokenError(w, oauthErr("unsupported_grant_type", "Use authorization_code or refresh_token."))
		return
	}
	if err != nil {
		var oe *Error
		if errors.As(err, &oe) {
			tokenError(w, oe)
			return
		}
		tokenError(w, oauthErr("server_error", "The token could not be issued."))
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	writeJSON(w, http.StatusOK, resp)
}

// Revoke is POST /oauth/revoke (RFC 7009). It answers 200 for unknown
// tokens too.
func (s *Service) RevokeEndpoint(w http.ResponseWriter, r *http.Request) {
	corsOpen(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxTokenRequest)
	if err := r.ParseForm(); err != nil {
		tokenError(w, oauthErr("invalid_request", "The body must be application/x-www-form-urlencoded."))
		return
	}
	client, failure := s.authenticateClient(r)
	if failure != nil {
		tokenError(w, failure)
		return
	}
	s.Revoke(r.Context(), client, r.PostForm.Get("token"))
	w.WriteHeader(http.StatusOK)
}

// authenticateClient finds the client of a token request: HTTP Basic or
// client_secret in the body for a confidential registered client; client_id
// alone for a public one.
func (s *Service) authenticateClient(r *http.Request) (*Client, *Error) {
	clientID, secret, basic := r.BasicAuth()
	if basic {
		clientID, _ = url.QueryUnescape(clientID)
		secret, _ = url.QueryUnescape(secret)
	} else {
		clientID = r.PostForm.Get("client_id")
		secret = r.PostForm.Get("client_secret")
	}
	if clientID == "" {
		return nil, &Error{Code: "invalid_client", Description: "client_id is required."}
	}
	client, err := s.clients.Resolve(r.Context(), clientID)
	if err != nil {
		return nil, &Error{Code: "invalid_client", Description: err.Error()}
	}
	switch {
	case client.Confidential && !client.CheckSecret(secret, s.now()):
		return nil, &Error{Code: "invalid_client", Description: "The client secret is not valid."}
	case !client.Confidential && secret != "":
		return nil, &Error{Code: "invalid_client", Description: "A public client has no secret."}
	}
	return client, nil
}

func tokenError(w http.ResponseWriter, e *Error) {
	status := http.StatusBadRequest
	if e.Code == "invalid_client" {
		status = http.StatusUnauthorized
		w.Header().Set("WWW-Authenticate", `Basic realm="oauth"`)
	}
	if e.Code == "server_error" {
		status = http.StatusInternalServerError
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, status, e)
}

// corsOpen lets browser-based clients (the MCP Inspector) call the token
// endpoints. They carry no cookies, so any origin is safe.
func corsOpen(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Access-Control-Allow-Origin", "*")
	h.Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
	h.Set("Access-Control-Max-Age", "600")
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}
