package tenants

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/authn"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/authz"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/httpx"
)

// InsertUpdateRequest is the tenant create/update payload.
type InsertUpdateRequest struct {
	Name     string  `json:"name" validate:"required,min=2,max=255"`
	Slug     *string `json:"slug" validate:"omitempty,max=255"`
	Domain   *string `json:"domain" validate:"omitempty,max=255"`
	Phone    *string `json:"phone" validate:"omitempty,max=50"`
	Email    *string `json:"email" validate:"omitempty,email,max=255"`
	IsActive *bool   `json:"is_active"`
}

// Response is the public tenant representation.
type Response struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Code      string    `json:"code"`
	Slug      *string   `json:"slug"`
	Domain    *string   `json:"domain"`
	Phone     *string   `json:"phone"`
	Email     *string   `json:"email"`
	IsActive  bool      `json:"is_active"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// NewResponse maps a tenant to its public representation.
func NewResponse(tenant Tenant) Response {
	return Response{
		ID:        tenant.ID,
		Name:      tenant.Name,
		Code:      tenant.Code,
		Slug:      tenant.Slug,
		Domain:    tenant.Domain,
		Phone:     tenant.Phone,
		Email:     tenant.Email,
		IsActive:  tenant.IsActive,
		CreatedAt: tenant.CreatedAt,
		UpdatedAt: tenant.UpdatedAt,
	}
}

// Handler serves tenant endpoints (tenant-exempt group).
type Handler struct {
	service   *Service
	checker   *authz.Checker
	binder    *httpx.Binder
	validator *httpx.Validator
	responder *httpx.Responder
}

// NewHandler creates the tenant handler.
func NewHandler(service *Service, checker *authz.Checker, binder *httpx.Binder, validator *httpx.Validator, responder *httpx.Responder) *Handler {
	return &Handler{service: service, checker: checker, binder: binder, validator: validator, responder: responder}
}

func (h *Handler) index(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	if err := h.checker.MustCan(r.Context(), identity.UserID, authz.PermTenantViewAll); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	page, perPage, offset := httpx.PageParams(r)
	items, total, err := h.service.List(r.Context(), ListFilter{
		Search: r.URL.Query().Get("search"),
		Limit:  int32(perPage),
		Offset: int32(offset),
	})
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	data := make([]Response, 0, len(items))
	for _, item := range items {
		data = append(data, NewResponse(item))
	}
	meta, links := httpx.NewPagination(r, page, perPage, total)
	h.responder.Paginated(w, r, http.StatusOK, data, meta, links)
}

func (h *Handler) show(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	if err := h.checker.MustCan(r.Context(), identity.UserID, authz.PermTenantView); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	id, err := tenantPathID(r)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	tenant, err := h.service.Get(r.Context(), id)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	h.responder.Data(w, r, http.StatusOK, NewResponse(*tenant))
}

func (h *Handler) store(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	if err := h.checker.MustCan(r.Context(), identity.UserID, authz.PermTenantCreate); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	var req InsertUpdateRequest
	if err := h.binder.JSON(w, r, &req); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	if err := h.validator.Struct(r.Context(), &req); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	isActive := true
	if req.IsActive != nil {
		isActive = *req.IsActive
	}
	tenant, err := h.service.Create(r.Context(), identity.UserID, CreateInput{
		Name:     strings.TrimSpace(req.Name),
		Slug:     req.Slug,
		Domain:   req.Domain,
		Phone:    req.Phone,
		Email:    req.Email,
		IsActive: isActive,
	})
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	h.responder.Data(w, r, http.StatusCreated, NewResponse(*tenant))
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	if err := h.checker.MustCan(r.Context(), identity.UserID, authz.PermTenantUpdate); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	id, err := tenantPathID(r)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	var req InsertUpdateRequest
	if err := h.binder.JSON(w, r, &req); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	if err := h.validator.Struct(r.Context(), &req); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	current, err := h.service.Get(r.Context(), id)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	isActive := current.IsActive
	if req.IsActive != nil {
		isActive = *req.IsActive
	}
	tenant, err := h.service.Update(r.Context(), identity.UserID, id, UpdateInput{
		Name:     strings.TrimSpace(req.Name),
		Slug:     req.Slug,
		Domain:   req.Domain,
		Phone:    req.Phone,
		Email:    req.Email,
		IsActive: isActive,
	})
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	h.responder.Data(w, r, http.StatusOK, NewResponse(*tenant))
}

func (h *Handler) destroy(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	if err := h.checker.MustCan(r.Context(), identity.UserID, authz.PermTenantDelete); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	id, err := tenantPathID(r)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	if err := h.service.Delete(r.Context(), identity.UserID, id); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	h.responder.NoContent(w)
}

func tenantPathID(r *http.Request) (int64, error) {
	raw := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id < 1 {
		return 0, apperr.New(apperr.InvalidInput, "The provided identifier is invalid.")
	}
	return id, nil
}
