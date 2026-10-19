package dev

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/apperr"
)

// Telemetry handlers: request inspector, logs, exceptions and prune.

func (h *Handler) listRequests(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	limit, err := parseLimit(query.Get("limit"))
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	beforeID, err := parseCursor(query.Get("cursor"))
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	status := int32(0)
	if raw := strings.TrimSpace(query.Get("status")); raw != "" {
		value, parseErr := strconv.Atoi(raw)
		if parseErr != nil || value < 100 || value > 599 {
			h.responder.Error(w, r, apperr.Validation(map[string][]string{
				"status": {"The status must be a valid HTTP status code."},
			}))
			return
		}
		status = int32(value)
	}
	tenantID, err := parseOptionalID(query.Get("tenant_id"), "tenant_id")
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	userID, err := parseOptionalID(query.Get("user_id"), "user_id")
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	items, err := h.telemetry.ListRequests(r.Context(), RequestFilter{
		BeforeID:  beforeID,
		Limit:     int32(limit),
		Method:    strings.ToUpper(strings.TrimSpace(query.Get("method"))),
		Status:    status,
		Search:    strings.TrimSpace(query.Get("search")),
		TenantID:  tenantID,
		UserID:    userID,
		RequestID: strings.TrimSpace(query.Get("request_id")),
	})
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	nextCursor := ""
	if len(items) == limit && len(items) > 0 {
		nextCursor = strconv.FormatInt(items[len(items)-1].ID, 10)
	}
	h.responder.Cursor(w, r, http.StatusOK, items, map[string]any{"next_cursor": nextCursor}, nil)
}

func (h *Handler) getRequest(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	detail, err := h.telemetry.GetRequestDetail(r.Context(), id)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	if detail == nil {
		h.responder.Error(w, r, apperr.Newf(apperr.ResourceNotFound, "Captured request with ID '%d' was not found.", id))
		return
	}
	h.responder.Data(w, r, http.StatusOK, detail)
}

func (h *Handler) listLogs(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	limit, err := parseLimit(query.Get("limit"))
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	beforeID, err := parseCursor(query.Get("cursor"))
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	items, err := h.telemetry.ListLogs(r.Context(), beforeID, int32(limit),
		strings.ToLower(strings.TrimSpace(query.Get("level"))),
		strings.TrimSpace(query.Get("request_id")))
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	nextCursor := ""
	if len(items) == limit && len(items) > 0 {
		nextCursor = strconv.FormatInt(items[len(items)-1].ID, 10)
	}
	h.responder.Cursor(w, r, http.StatusOK, items, map[string]any{"next_cursor": nextCursor}, nil)
}

func (h *Handler) listExceptions(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	limit, err := parseLimit(query.Get("limit"))
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	beforeID, err := parseCursor(query.Get("cursor"))
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	items, err := h.telemetry.ListExceptions(r.Context(), beforeID, int32(limit),
		strings.TrimSpace(query.Get("request_id")))
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	nextCursor := ""
	if len(items) == limit && len(items) > 0 {
		nextCursor = strconv.FormatInt(items[len(items)-1].ID, 10)
	}
	h.responder.Cursor(w, r, http.StatusOK, items, map[string]any{"next_cursor": nextCursor}, nil)
}

func (h *Handler) getLog(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	item, err := h.telemetry.GetLog(r.Context(), id)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	if item == nil {
		h.responder.Error(w, r, apperr.Newf(apperr.ResourceNotFound, "Captured log with ID '%d' was not found.", id))
		return
	}
	h.responder.Data(w, r, http.StatusOK, item)
}

func (h *Handler) getException(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	item, err := h.telemetry.GetException(r.Context(), id)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	if item == nil {
		h.responder.Error(w, r, apperr.Newf(apperr.ResourceNotFound, "Captured exception with ID '%d' was not found.", id))
		return
	}
	h.responder.Data(w, r, http.StatusOK, item)
}

type pruneTelemetryRequest struct {
	RetentionDays int `json:"retention_days" validate:"omitempty,min=1,max=3650"`
}

func (h *Handler) pruneTelemetry(w http.ResponseWriter, r *http.Request) {
	actorID, ok := h.requireManage(w, r)
	if !ok {
		return
	}
	var request pruneTelemetryRequest
	if err := h.binder.JSON(w, r, &request); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	if err := h.validator.Struct(r.Context(), &request); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	days := request.RetentionDays
	if days == 0 {
		days = h.retentionDays
	}
	started := time.Now()
	deleted, err := h.telemetry.Prune(r.Context(), days)
	h.recordAction(r.Context(), actorID, "telemetry.prune", "", map[string]any{"retention_days": days}, started, err)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	h.responder.Data(w, r, http.StatusOK, map[string]any{
		"retention_days": days,
		"deleted":        deleted,
	})
}

func parseCursor(raw string) (int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < 0 {
		return 0, apperr.Validation(map[string][]string{"cursor": {"The cursor is invalid."}})
	}
	return value, nil
}

func parseOptionalID(raw, field string) (int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < 1 {
		return 0, apperr.Validation(map[string][]string{field: {"The identifier is invalid."}})
	}
	return value, nil
}
