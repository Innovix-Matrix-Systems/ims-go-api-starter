package dev

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/authn"
)

// Sessions panel: token/device directory, force logout and idle pruning.

func (h *Handler) sessionStats(w http.ResponseWriter, r *http.Request) {
	stats, err := h.sessions.Stats(r.Context())
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	h.responder.Data(w, r, http.StatusOK, stats)
}

func (h *Handler) listSessionUsers(w http.ResponseWriter, r *http.Request) {
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
	users, err := h.sessions.ListUsers(r.Context(), SessionUserFilter{
		Search:   strings.TrimSpace(query.Get("search")),
		BeforeID: beforeID,
		Limit:    int32(limit),
	})
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	nextCursor := ""
	if len(users) == limit && len(users) > 0 {
		nextCursor = strconv.FormatInt(users[len(users)-1].UserID, 10)
	}
	h.responder.Cursor(w, r, http.StatusOK, users, map[string]any{"next_cursor": nextCursor}, nil)
}

func (h *Handler) listUserSessions(w http.ResponseWriter, r *http.Request) {
	userID, err := pathID(r)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	exists, err := h.sessions.UserExists(r.Context(), userID)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	if !exists {
		h.responder.Error(w, r, apperr.Newf(apperr.ResourceNotFound, "User with ID '%d' was not found.", userID))
		return
	}
	tokens, err := h.sessions.ListTokens(r.Context(), userID)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	identity, _ := authn.IdentityFromContext(r.Context())
	for i := range tokens {
		if identity != nil && tokens[i].ID == identity.TokenID {
			tokens[i].Current = true
		}
	}
	h.responder.Data(w, r, http.StatusOK, tokens)
}

func (h *Handler) revokeSessionToken(w http.ResponseWriter, r *http.Request) {
	actorID, ok := h.requireManage(w, r)
	if !ok {
		return
	}
	tokenID, err := pathID(r)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	started := time.Now()
	deleted, opErr := h.sessions.DeleteToken(r.Context(), tokenID)
	if opErr == nil && !deleted {
		opErr = apperr.Newf(apperr.ResourceNotFound, "Session token with ID '%d' was not found.", tokenID)
	}
	h.recordAction(r.Context(), actorID, "sessions.token.revoke", formatTarget("token", tokenID),
		map[string]any{"token_id": tokenID}, started, opErr)
	if opErr != nil {
		h.responder.Error(w, r, opErr)
		return
	}
	h.responder.Data(w, r, http.StatusOK, map[string]any{"revoked": 1})
}

func (h *Handler) revokeUserSessions(w http.ResponseWriter, r *http.Request) {
	actorID, ok := h.requireManage(w, r)
	if !ok {
		return
	}
	userID, err := pathID(r)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	exists, err := h.sessions.UserExists(r.Context(), userID)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	if !exists {
		h.responder.Error(w, r, apperr.Newf(apperr.ResourceNotFound, "User with ID '%d' was not found.", userID))
		return
	}
	started := time.Now()
	revoked, opErr := h.sessions.DeleteUserTokens(r.Context(), userID)
	h.recordAction(r.Context(), actorID, "sessions.user.revoke", formatTarget("user", userID),
		map[string]any{"user_id": userID}, started, opErr)
	if opErr != nil {
		h.responder.Error(w, r, opErr)
		return
	}
	h.responder.Data(w, r, http.StatusOK, map[string]any{"revoked": revoked})
}

func (h *Handler) revokeAllSessions(w http.ResponseWriter, r *http.Request) {
	actorID, ok := h.requireManage(w, r)
	if !ok {
		return
	}
	identity, ok := authn.IdentityFromContext(r.Context())
	if !ok || identity.TokenID == 0 {
		h.responder.Error(w, r, apperr.New(apperr.Unauthorized, "Authentication is required."))
		return
	}
	started := time.Now()
	revoked, opErr := h.sessions.DeleteAllExcept(r.Context(), identity.TokenID)
	h.recordAction(r.Context(), actorID, "sessions.logout_all", "sessions",
		map[string]any{"excluded_token_id": identity.TokenID, "revoked": revoked}, started, opErr)
	if opErr != nil {
		h.responder.Error(w, r, opErr)
		return
	}
	h.responder.Data(w, r, http.StatusOK, map[string]any{
		"revoked":           revoked,
		"excluded_token_id": identity.TokenID,
	})
}

type pruneSessionsRequest struct {
	IdleDays int32 `json:"idle_days" validate:"omitempty,min=1,max=3650"`
	DryRun   bool  `json:"dry_run"`
}

func (h *Handler) pruneSessions(w http.ResponseWriter, r *http.Request) {
	actorID, ok := h.requireManage(w, r)
	if !ok {
		return
	}
	var request pruneSessionsRequest
	if err := h.binder.JSON(w, r, &request); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	if err := h.validator.Struct(r.Context(), &request); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	if request.IdleDays == 0 {
		request.IdleDays = 30
	}
	started := time.Now()
	affected, opErr := h.sessions.Prune(r.Context(), request.IdleDays, request.DryRun)
	h.recordAction(r.Context(), actorID, "sessions.prune", "sessions", map[string]any{
		"idle_days": request.IdleDays,
		"dry_run":   request.DryRun,
		"affected":  affected,
	}, started, opErr)
	if opErr != nil {
		h.responder.Error(w, r, opErr)
		return
	}
	h.responder.Data(w, r, http.StatusOK, SessionPruneResult{
		IdleDays: request.IdleDays,
		DryRun:   request.DryRun,
		Affected: affected,
	})
}

func formatTarget(kind string, id int64) string {
	return kind + ":" + strconv.FormatInt(id, 10)
}
