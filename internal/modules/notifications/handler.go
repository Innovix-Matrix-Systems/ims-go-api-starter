package notifications

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/authn"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/authz"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/httpx"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/tenancy"
)

// Handler serves notification HTTP endpoints.
type Handler struct {
	service   *Service
	checker   *authz.Checker
	binder    *httpx.Binder
	validator *httpx.Validator
	responder *httpx.Responder
}

// NewHandler creates the notification handler.
func NewHandler(service *Service, checker *authz.Checker, binder *httpx.Binder, validator *httpx.Validator, responder *httpx.Responder) *Handler {
	return &Handler{service: service, checker: checker, binder: binder, validator: validator, responder: responder}
}

func (h *Handler) feed(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	scope, _ := tenancy.FromContext(r.Context())

	filter, err := feedFilter(r)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	page, err := h.service.Feed(r.Context(), scope, identity.UserID, filter)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}

	data := make([]FeedItemResponse, 0, len(page.Items))
	for _, item := range page.Items {
		data = append(data, NewFeedResponse(item))
	}
	meta := FeedMeta{
		Path:        r.URL.Path,
		PerPage:     page.PerPage,
		NextCursor:  page.NextCursor,
		PrevCursor:  page.PrevCursor,
		UnreadCount: page.UnreadCount,
	}
	links := FeedLinks{
		Prev: httpx.CursorURL(r, page.PrevCursor),
		Next: httpx.CursorURL(r, page.NextCursor),
	}
	h.responder.Cursor(w, r, http.StatusOK, data, meta, links)
}

func feedFilter(r *http.Request) (FeedFilter, error) {
	query := r.URL.Query()
	limit := 20
	if raw := query.Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			return FeedFilter{}, apperr.Validation(map[string][]string{
				"limit": {"The limit must be a positive integer."},
			})
		}
		limit = parsed
	}
	if limit > 100 {
		limit = 100
	}
	filter := FeedFilter{Limit: int32(limit), Cursor: DecodeCursor(query.Get("cursor"))}
	if raw := query.Get("unread_only"); raw != "" {
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			return FeedFilter{}, apperr.Validation(map[string][]string{
				"unread_only": {"The unread only field must be true or false."},
			})
		}
		filter.UnreadOnly = parsed
	}
	if priority := query.Get("priority"); priority != "" {
		if _, ok := Priorities[priority]; !ok {
			return FeedFilter{}, apperr.Validation(map[string][]string{
				"priority": {"The selected priority is invalid."},
			})
		}
		filter.Priority = priority
	}
	return filter, nil
}

func (h *Handler) markRead(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	scope, _ := tenancy.FromContext(r.Context())
	id, err := notificationPathID(r)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	if err := h.service.MarkRead(r.Context(), scope, identity.UserID, id); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	h.responder.NoContent(w)
}

func (h *Handler) markAllRead(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	scope, _ := tenancy.FromContext(r.Context())
	if err := h.service.MarkAllRead(r.Context(), scope, identity.UserID); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	h.responder.NoContent(w)
}

func (h *Handler) index(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	if err := h.checker.MustCan(r.Context(), identity.UserID, authz.PermNotificationViewAll); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	page, perPage, offset := httpx.PageParams(r)
	orderBy, orderDir := httpx.OrderParams(r, "created_at", "created_at", "title", "priority")
	filter := ListFilter{
		Search:   r.URL.Query().Get("search"),
		Priority: r.URL.Query().Get("priority"),
		OrderBy:  orderBy,
		OrderDir: orderDir,
		Limit:    int32(perPage),
		Offset:   int32(offset),
	}
	if filter.Priority != "" {
		if _, ok := Priorities[filter.Priority]; !ok {
			h.responder.Error(w, r, apperr.Validation(map[string][]string{
				"priority": {"The selected priority is invalid."},
			}))
			return
		}
	}

	items, total, err := h.service.List(r.Context(), filter)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	data := make([]Response, 0, len(items))
	for _, item := range items {
		targets, err := h.service.Targets(r.Context(), item.ID)
		if err != nil {
			h.responder.Error(w, r, err)
			return
		}
		data = append(data, NewResponse(item, targets))
	}
	meta, links := httpx.NewPagination(r, page, perPage, total)
	h.responder.Paginated(w, r, http.StatusOK, data, meta, links)
}

func (h *Handler) show(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	if err := h.checker.MustCan(r.Context(), identity.UserID, authz.PermNotificationView); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	id, err := notificationPathID(r)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	scope, _ := tenancy.FromContext(r.Context())
	notification, err := h.service.Get(r.Context(), scope, id)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	targets, err := h.service.Targets(r.Context(), id)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	h.responder.Data(w, r, http.StatusOK, NewResponse(*notification, targets))
}

func (h *Handler) store(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	if err := h.checker.MustCan(r.Context(), identity.UserID, authz.PermNotificationCreate); err != nil {
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
	notification, targets, err := h.service.Create(r.Context(), scope, identity.UserID, req.toCreateInput())
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	h.responder.Data(w, r, http.StatusCreated, NewResponse(*notification, targets))
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	if err := h.checker.MustCan(r.Context(), identity.UserID, authz.PermNotificationUpdate); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	id, err := notificationPathID(r)
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
	notification, targets, err := h.service.Update(r.Context(), scope, identity.UserID, id, req.toUpdateInput())
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	h.responder.Data(w, r, http.StatusOK, NewResponse(*notification, targets))
}

func (h *Handler) destroy(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	if err := h.checker.MustCan(r.Context(), identity.UserID, authz.PermNotificationDelete); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	id, err := notificationPathID(r)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	scope, _ := tenancy.FromContext(r.Context())
	if err := h.service.Delete(r.Context(), scope, id); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	h.responder.NoContent(w)
}

func (req InsertUpdateRequest) toCreateInput() CreateInput {
	return CreateInput{
		TenantID:  req.TenantID,
		EventType: req.EventType,
		Priority:  req.Priority,
		Title:     req.Title,
		Body:      req.Body,
		Data:      dataJSON(req.Data),
		GroupKey:  req.GroupKey,
		ExpiresAt: req.ExpiresAt,
		Targets:   targetsInput(req.Targets),
	}
}

func (req InsertUpdateRequest) toUpdateInput() UpdateInput {
	return UpdateInput{
		EventType: req.EventType,
		Priority:  req.Priority,
		Title:     req.Title,
		Body:      req.Body,
		Data:      dataJSON(req.Data),
		GroupKey:  req.GroupKey,
		ExpiresAt: req.ExpiresAt,
		Targets:   targetsInput(req.Targets),
	}
}

func targetsInput(requests []TargetRequest) []TargetInput {
	targets := make([]TargetInput, 0, len(requests))
	for _, request := range requests {
		targets = append(targets, TargetInput{Type: request.Type, ID: request.ID})
	}
	return targets
}

func dataJSON(value map[string]any) []byte {
	if value == nil {
		return nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	return encoded
}

func notificationPathID(r *http.Request) (int64, error) {
	raw := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id < 1 {
		return 0, apperr.New(apperr.InvalidInput, "The provided identifier is invalid.")
	}
	return id, nil
}
