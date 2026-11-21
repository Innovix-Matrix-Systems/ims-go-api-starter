package oauth

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/authn"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/authz"
	dbgen "github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/httpx"
)

// Handler serves the JSON API side: a user's connections and the registered
// clients.
type Handler struct {
	service *Service
	pages   *Pages
	checker *authz.Checker
	httpx.Kit
}

// NewHandler creates the handler. pages may be nil when MCP is disabled.
func NewHandler(service *Service, pages *Pages, checker *authz.Checker, kit httpx.Kit) *Handler {
	return &Handler{service: service, pages: pages, checker: checker, Kit: kit}
}

// RegisterEndpoints mounts the authorization server at the root:
// /.well-known/*, /oauth/*. enabled=false answers 404 (MCP_ENABLED off), so
// the route table stays the same either way.
func (h *Handler) RegisterEndpoints(r chi.Router, enabled bool) {
	r.Group(func(r chi.Router) {
		r.Use(gate(enabled, h.Responder))
		r.Get(ASMetadataPath, h.service.Metadata)
		r.Get("/.well-known/oauth-protected-resource", h.service.ProtectedResource().ServeHTTP)
		r.Get("/.well-known/oauth-protected-resource/mcp", h.service.ProtectedResource().ServeHTTP)
		r.Get(AuthorizePath, h.pages.Authorize)
		r.Post(AuthorizePath+"/login", h.pages.Login)
		r.Post(AuthorizePath+"/consent", h.pages.Consent)
		r.Post(TokenPath, h.service.Token)
		r.Options(TokenPath, h.service.Token)
		r.Post(RevokePath, h.service.RevokeEndpoint)
		r.Options(RevokePath, h.service.RevokeEndpoint)
	})
}

// RegisterRoutes mounts the user's own connections (tenant-exempt, any
// signed-in user).
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Get("/user/connections", h.connections)
	r.Delete("/user/connections/{id}", h.disconnect)
}

// RegisterInternalRoutes mounts registered-client management (internal
// accounts with oauth_client.manage).
func (h *Handler) RegisterInternalRoutes(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(authz.Require(h.checker, authz.PermOAuthClientManage, h.Responder))
		r.Get("/oauth-client", h.index)
		r.Post("/oauth-client", h.store)
		r.Get("/oauth-client/{id}", h.show)
		r.Put("/oauth-client/{id}", h.update)
		r.Delete("/oauth-client/{id}", h.destroy)
		r.Post("/oauth-client/{id}/rotate-secret", h.rotate)
	})
}

// ConnectionResponse is one connection as the user sees it.
type ConnectionResponse struct {
	ID         int64      `json:"id"`
	ClientID   string     `json:"client_id"`
	ClientName string     `json:"client_name"`
	Scopes     []string   `json:"scopes"`
	TenantID   *int64     `json:"tenant_id"`
	BranchID   *int64     `json:"branch_id"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
}

func newConnection(g dbgen.OauthGrant) ConnectionResponse {
	out := ConnectionResponse{
		ID: g.ID, ClientID: g.ClientID, ClientName: g.ClientName, Scopes: g.Scopes,
		TenantID: g.TenantID, BranchID: g.BranchID, CreatedAt: g.CreatedAt.Time,
	}
	if g.LastUsedAt.Valid {
		t := g.LastUsedAt.Time
		out.LastUsedAt = &t
	}
	return out
}

func (h *Handler) connections(w http.ResponseWriter, r *http.Request) {
	grants, err := h.service.Connections(r.Context(), authn.UserID(r))
	if err != nil {
		h.Error(w, r, err)
		return
	}
	data := make([]ConnectionResponse, 0, len(grants))
	for _, g := range grants {
		data = append(data, newConnection(g))
	}
	h.Data(w, r, http.StatusOK, data)
}

func (h *Handler) disconnect(w http.ResponseWriter, r *http.Request) {
	id, ok := h.PathID(w, r, "id")
	if !ok {
		return
	}
	if err := h.service.Disconnect(r.Context(), authn.UserID(r), id); err != nil {
		h.Error(w, r, err)
		return
	}
	h.NoContent(w)
}

// ClientResponse is a registered client; ClientSecret is set only in the
// response that created or rotated it.
type ClientResponse struct {
	ID           int64     `json:"id"`
	ClientID     string    `json:"client_id"`
	ClientSecret string    `json:"client_secret,omitempty"`
	Name         string    `json:"name"`
	Type         string    `json:"type"`
	RedirectURIs []string  `json:"redirect_uris"`
	Scopes       []string  `json:"scopes"`
	TenantID     *int64    `json:"tenant_id"`
	IsActive     bool      `json:"is_active"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func newClientResponse(row dbgen.OauthClient, secret string) ClientResponse {
	return ClientResponse{
		ID: row.ID, ClientID: row.ClientID, ClientSecret: secret, Name: row.Name, Type: row.Type,
		RedirectURIs: row.RedirectUris, Scopes: row.Scopes, TenantID: row.TenantID, IsActive: row.IsActive,
		CreatedAt: row.CreatedAt.Time, UpdatedAt: row.UpdatedAt.Time,
	}
}

// ClientRequest creates or updates a registered client.
type ClientRequest struct {
	Name         string   `json:"name" validate:"required,max=255"`
	Type         string   `json:"type" validate:"omitempty,oneof=public confidential"`
	RedirectURIs []string `json:"redirect_uris" validate:"required,min=1,max=20,dive,required,max=2048"`
	Scopes       []string `json:"scopes" validate:"omitempty,max=2,dive,oneof=mcp:read mcp:write"`
	TenantID     *int64   `json:"tenant_id" validate:"omitempty,min=1"`
	IsActive     *bool    `json:"is_active"`
}

func (req ClientRequest) input() ClientInput {
	in := ClientInput{Name: req.Name, Type: req.Type, RedirectURIs: req.RedirectURIs, Scopes: req.Scopes, IsActive: true}
	if req.TenantID != nil {
		in.TenantID = *req.TenantID
	}
	if req.IsActive != nil {
		in.IsActive = *req.IsActive
	}
	return in
}

func (h *Handler) index(w http.ResponseWriter, r *http.Request) {
	page, perPage, offset := httpx.PageParams(r)
	rows, total, err := h.service.ListClients(r.Context(), r.URL.Query().Get("search"), int32(perPage), int32(offset))
	if err != nil {
		h.Error(w, r, err)
		return
	}
	data := make([]ClientResponse, 0, len(rows))
	for _, row := range rows {
		data = append(data, newClientResponse(row, ""))
	}
	meta, links := httpx.NewPagination(r, page, perPage, total)
	h.Paginated(w, r, http.StatusOK, data, meta, links)
}

func (h *Handler) store(w http.ResponseWriter, r *http.Request) {
	var req ClientRequest
	if !h.Bind(w, r, &req) {
		return
	}
	in := req.input()
	if in.Type == "" {
		in.Type = TypePublic
	}
	created, err := h.service.CreateClient(r.Context(), authn.UserID(r), in)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusCreated, newClientResponse(created.OauthClient, created.Secret))
}

func (h *Handler) show(w http.ResponseWriter, r *http.Request) {
	id, ok := h.PathID(w, r, "id")
	if !ok {
		return
	}
	row, err := h.service.GetClient(r.Context(), id)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, newClientResponse(*row, ""))
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	id, ok := h.PathID(w, r, "id")
	if !ok {
		return
	}
	var req ClientRequest
	if !h.Bind(w, r, &req) {
		return
	}
	row, err := h.service.UpdateClient(r.Context(), authn.UserID(r), id, req.input())
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, newClientResponse(*row, ""))
}

func (h *Handler) destroy(w http.ResponseWriter, r *http.Request) {
	id, ok := h.PathID(w, r, "id")
	if !ok {
		return
	}
	if err := h.service.DeleteClient(r.Context(), authn.UserID(r), id); err != nil {
		h.Error(w, r, err)
		return
	}
	h.NoContent(w)
}

func (h *Handler) rotate(w http.ResponseWriter, r *http.Request) {
	id, ok := h.PathID(w, r, "id")
	if !ok {
		return
	}
	rotated, err := h.service.RotateSecret(r.Context(), authn.UserID(r), id)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, newClientResponse(rotated.OauthClient, rotated.Secret))
}

// gate answers 404 when the MCP server is disabled, like /dev.
func gate(enabled bool, responder *httpx.Responder) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if enabled {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			responder.Error(w, r, apperr.New(apperr.ResourceNotFound, "The requested resource was not found."))
		})
	}
}

// Gate is the same switch for the /mcp endpoint.
func Gate(enabled bool, responder *httpx.Responder) func(http.Handler) http.Handler {
	return gate(enabled, responder)
}
