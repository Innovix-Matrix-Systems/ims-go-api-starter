package branches

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/authn"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/authz"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/httpx"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/tenancy"
)

// Handler serves branch HTTP endpoints.
type Handler struct {
	service   *Service
	checker   *authz.Checker
	binder    *httpx.Binder
	validator *httpx.Validator
	responder *httpx.Responder
}

// NewHandler creates the branch handler.
func NewHandler(service *Service, checker *authz.Checker, binder *httpx.Binder, validator *httpx.Validator, responder *httpx.Responder) *Handler {
	return &Handler{service: service, checker: checker, binder: binder, validator: validator, responder: responder}
}

func (h *Handler) index(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	if err := h.checker.MustCan(r.Context(), identity.UserID, authz.PermBranchViewAll); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	scope, _ := tenancy.FromContext(r.Context())

	page, perPage, offset := httpx.PageParams(r)
	orderBy, orderDir := httpx.OrderParams(r, "id", "name", "created_at")
	filter := ListFilter{
		Search:   r.URL.Query().Get("search"),
		OrderBy:  orderBy,
		OrderDir: orderDir,
		Limit:    int32(perPage),
		Offset:   int32(offset),
	}

	items, total, err := h.service.List(r.Context(), scope, filter)
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
	if err := h.checker.MustCan(r.Context(), identity.UserID, authz.PermBranchView); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	id, err := pathID(r)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	scope, _ := tenancy.FromContext(r.Context())
	branch, err := h.service.Get(r.Context(), scope, id)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	h.responder.Data(w, r, http.StatusOK, NewResponse(*branch))
}

func (h *Handler) store(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	if err := h.checker.MustCan(r.Context(), identity.UserID, authz.PermBranchCreate); err != nil {
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
	scope, _ := tenancy.FromContext(r.Context())
	branch, err := h.service.Create(r.Context(), scope, identity.UserID, req.toInput())
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	h.responder.Data(w, r, http.StatusCreated, NewResponse(*branch))
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	if err := h.checker.MustCan(r.Context(), identity.UserID, authz.PermBranchUpdate); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	id, err := pathID(r)
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
	scope, _ := tenancy.FromContext(r.Context())
	branch, err := h.service.Update(r.Context(), scope, identity.UserID, id, req.toInput())
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	h.responder.Data(w, r, http.StatusOK, NewResponse(*branch))
}

func (h *Handler) destroy(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	if err := h.checker.MustCan(r.Context(), identity.UserID, authz.PermBranchDelete); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	id, err := pathID(r)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	scope, _ := tenancy.FromContext(r.Context())
	if err := h.service.Delete(r.Context(), scope, identity.UserID, id); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	h.responder.NoContent(w)
}

func pathID(r *http.Request) (int64, error) {
	raw := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id < 1 {
		return 0, apperr.New(apperr.InvalidInput, "The provided identifier is invalid.")
	}
	return id, nil
}
