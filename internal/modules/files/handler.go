package files

import (
	"context"
	"io"
	"mime"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/authn"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/authz"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/httpx"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/tenancy"
)

// multipartSlack leaves room for multipart framing beyond the file limit.
const multipartSlack = 1 << 20

// Handler serves the file resource endpoints.
type Handler struct {
	service *Service
	checker *authz.Checker
	httpx.Kit
}

// NewHandler creates the file handler.
func NewHandler(service *Service, checker *authz.Checker, responder *httpx.Responder) *Handler {
	return &Handler{service: service, checker: checker, Kit: httpx.NewKit(nil, nil, responder)}
}

// tenantFilter resolves the data-access tenant for the caller: impersonation
// wins, then the identity's own tenant. A tenantless (platform) caller sees
// all files — reachable only with the system-only file.view.all permission.
func tenantFilter(r *http.Request) (allTenants bool, tenantID int64) {
	scope := tenancy.From(r)
	identity, _ := authn.IdentityFromContext(r.Context())
	tenantID = scope.EffectiveTenantID()
	if tenantID == 0 {
		tenantID = identity.TenantID
	}
	return tenantID == 0, tenantID
}

func (h *Handler) index(w http.ResponseWriter, r *http.Request) {

	allTenants, tenantID := tenantFilter(r)
	page, perPage, offset := httpx.PageParams(r)
	filter := ListFilter{
		AllTenants: allTenants,
		TenantID:   tenantID,
		Type:       r.URL.Query().Get("type"),
		Limit:      int32(perPage),
		Offset:     int32(offset),
	}
	if raw := r.URL.Query().Get("attached"); raw != "" {
		if value, err := strconv.ParseBool(raw); err == nil {
			filter.Attached = &value
		}
	}

	items, total, err := h.service.List(r.Context(), filter)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	data := make([]Response, 0, len(items))
	for _, file := range items {
		data = append(data, NewResponse(file, h.fileURL(r, file)))
	}
	meta, links := httpx.NewPagination(r, page, perPage, total)
	h.Paginated(w, r, http.StatusOK, data, meta, links)
}

func (h *Handler) store(w http.ResponseWriter, r *http.Request) {
	form, err := httpx.Multipart(w, r, MaxFileSize+multipartSlack)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	header, err := httpx.FormFile(form, "file")
	if err != nil {
		h.Error(w, r, err)
		return
	}
	_, tenantID := tenantFilter(r)
	file, err := h.service.Create(r.Context(), authn.UserID(r), tenantID, header)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusCreated, NewResponse(*file, h.fileURL(r, *file)))
}

func (h *Handler) show(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	file, _ := h.ownedFile(w, r, identity.UserID, viewDecision)
	if file == nil {
		return
	}
	h.Data(w, r, http.StatusOK, NewResponse(*file, h.fileURL(r, *file)))
}

func (h *Handler) destroy(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	file, _ := h.ownedFile(w, r, identity.UserID, deleteDecision)
	if file == nil {
		return
	}
	allTenants, tenantID := tenantFilter(r)
	if err := h.service.Delete(r.Context(), file.ID, allTenants, tenantID); err != nil {
		h.Error(w, r, err)
		return
	}
	h.NoContent(w)
}

// download streams the file through an authorized route (the only way to
// read files on the private disk). Same access rule as show.
func (h *Handler) download(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	file, _ := h.ownedFile(w, r, identity.UserID, viewDecision)
	if file == nil {
		return
	}
	reader, _, err := h.service.Open(r.Context(), file.ID)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	defer reader.Close()
	w.Header().Set("Content-Type", file.MimeType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": file.FileName}))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	w.Header().Set("Cache-Control", "private, no-store")
	if file.Size > 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(file.Size, 10))
	}
	_, _ = io.Copy(w, reader)
}

// fileURL is the public URL, or the authorized download route for private
// files.
func (h *Handler) fileURL(r *http.Request, file File) string {
	if url := h.service.URL(r.Context(), file); url != "" {
		return url
	}
	return httpx.AbsoluteURL(r, "/api/v1/files/"+strconv.FormatInt(file.ID, 10)+"/download")
}

type decision int

const (
	viewDecision decision = iota
	deleteDecision
)

// ownedFile loads the file (tenant-scoped) and applies the access policy:
// full-permission roles may access any file; otherwise the creator needs the
// granular permission (view/delete) and—for delete—also file.create.
func (h *Handler) ownedFile(w http.ResponseWriter, r *http.Request, userID int64, decision decision) (*File, error) {
	id, err := filePathID(r)
	if err != nil {
		h.Error(w, r, err)
		return nil, err
	}
	allTenants, tenantID := tenantFilter(r)
	file, err := h.service.Get(r.Context(), id, allTenants, tenantID)
	if err != nil {
		h.Error(w, r, err)
		return nil, err
	}

	allowed, err := h.mayAccess(r, userID, *file, decision)
	if err != nil {
		h.Error(w, r, err)
		return nil, err
	}
	if !allowed {
		forbidden := apperr.New(apperr.Forbidden, "You are not allowed to access this file.")
		h.Error(w, r, forbidden)
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

// can is route middleware requiring a permission (Laravel's can:...).
func (h *Handler) can(permission string) func(http.Handler) http.Handler {
	return authz.Require(h.checker, permission, h.Responder)
}
