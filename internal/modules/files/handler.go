package files

import (
	"context"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/authn"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/authz"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/httpx"
)

// multipartSlack leaves room for multipart framing beyond the file limit.
const multipartSlack = 1 << 20

// Handler serves the file resource endpoints.
type Handler struct {
	service   *Service
	checker   *authz.Checker
	responder *httpx.Responder
}

// NewHandler creates the file handler.
func NewHandler(service *Service, checker *authz.Checker, responder *httpx.Responder) *Handler {
	return &Handler{service: service, checker: checker, responder: responder}
}

func (h *Handler) index(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	if err := h.checker.MustCan(r.Context(), identity.UserID, authz.PermFileViewAll); err != nil {
		h.responder.Error(w, r, err)
		return
	}

	page, perPage, offset := httpx.PageParams(r)
	filter := ListFilter{
		Type:   r.URL.Query().Get("type"),
		Limit:  int32(perPage),
		Offset: int32(offset),
	}
	if raw := r.URL.Query().Get("attached"); raw != "" {
		if value, err := strconv.ParseBool(raw); err == nil {
			filter.Attached = &value
		}
	}

	items, total, err := h.service.List(r.Context(), filter)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	data := make([]Response, 0, len(items))
	for _, file := range items {
		data = append(data, NewResponse(file, h.service.URL(r.Context(), file)))
	}
	meta, links := httpx.NewPagination(r, page, perPage, total)
	h.responder.Paginated(w, r, http.StatusOK, data, meta, links)
}

func (h *Handler) store(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	if err := h.checker.MustCan(r.Context(), identity.UserID, authz.PermFileCreate); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	form, err := httpx.Multipart(w, r, MaxFileSize+multipartSlack)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	header, err := httpx.FormFile(form, "file")
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	file, err := h.service.Create(r.Context(), identity.UserID, header)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	h.responder.Data(w, r, http.StatusCreated, NewResponse(*file, h.service.URL(r.Context(), *file)))
}

func (h *Handler) show(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	file, _ := h.ownedFile(w, r, identity.UserID, viewDecision)
	if file == nil {
		return
	}
	h.responder.Data(w, r, http.StatusOK, NewResponse(*file, h.service.URL(r.Context(), *file)))
}

func (h *Handler) destroy(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	file, _ := h.ownedFile(w, r, identity.UserID, deleteDecision)
	if file == nil {
		return
	}
	if err := h.service.Delete(r.Context(), file.ID); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	h.responder.NoContent(w)
}

type decision int

const (
	viewDecision decision = iota
	deleteDecision
)

// ownedFile loads the file and applies the access policy: full-permission
// roles may access any file; otherwise the creator needs the granular
// permission (view/delete) and—for delete—also file.create.
func (h *Handler) ownedFile(w http.ResponseWriter, r *http.Request, userID int64, decision decision) (*File, error) {
	id, err := filePathID(r)
	if err != nil {
		h.responder.Error(w, r, err)
		return nil, err
	}
	file, err := h.service.Get(r.Context(), id)
	if err != nil {
		h.responder.Error(w, r, err)
		return nil, err
	}

	allowed, err := h.mayAccess(r, userID, *file, decision)
	if err != nil {
		h.responder.Error(w, r, err)
		return nil, err
	}
	if !allowed {
		forbidden := apperr.New(apperr.Forbidden, "You are not allowed to access this file.")
		h.responder.Error(w, r, forbidden)
		return nil, forbidden
	}
	return file, nil
}

func (h *Handler) mayAccess(r *http.Request, userID int64, file File, decision decision) (bool, error) {
	ctx := r.Context()
	if decision == viewDecision {
		if can, err := h.checker.Can(ctx, userID, authz.PermFileViewAll); err != nil {
			return false, err
		} else if can {
			return true, nil
		}
		return h.creatorWith(ctx, userID, file, authz.PermFileView)
	}

	if can, err := h.checker.Can(ctx, userID, authz.PermFileDelete); err != nil {
		return false, err
	} else if can {
		return true, nil
	}
	return h.creatorWith(ctx, userID, file, authz.PermFileCreate)
}

func (h *Handler) creatorWith(ctx context.Context, userID int64, file File, permission string) (bool, error) {
	if file.CreatedBy == nil || *file.CreatedBy != userID {
		return false, nil
	}
	return h.checker.Can(ctx, userID, permission)
}

func filePathID(r *http.Request) (int64, error) {
	raw := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id < 1 {
		return 0, apperr.New(apperr.InvalidInput, "The provided identifier is invalid.")
	}
	return id, nil
}
