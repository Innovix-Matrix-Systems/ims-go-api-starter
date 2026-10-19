package dev

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/riverqueue/river/rivertype"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/authn"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/authz"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/queue"
)

const (
	defaultAdminLimit = 25
	maxAdminLimit     = 100
)

func (h *Handler) requireManage(w http.ResponseWriter, r *http.Request) (int64, bool) {
	identity, ok := authn.IdentityFromContext(r.Context())
	if !ok {
		h.responder.Error(w, r, apperr.New(apperr.Unauthorized, "Authentication is required."))
		return 0, false
	}
	if err := h.checker.MustCan(r.Context(), identity.UserID, authz.PermDevManage); err != nil {
		h.responder.Error(w, r, err)
		return 0, false
	}
	return identity.UserID, true
}

func (h *Handler) requireQueueAdmin(w http.ResponseWriter, r *http.Request) bool {
	if h.queueAdmin != nil {
		return true
	}
	h.responder.Error(w, r, apperr.New(apperr.ServiceUnavailable,
		"Queue administration is unavailable (QUEUE_DRIVER=sync)."))
	return false
}

func (h *Handler) respondQueueError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, rivertype.ErrNotFound):
		h.responder.Error(w, r, apperr.New(apperr.ResourceNotFound, "The requested job was not found."))
	case errors.Is(err, rivertype.ErrJobRunning):
		h.responder.Error(w, r, apperr.New(apperr.Conflict, "Running jobs cannot be deleted."))
	case errors.Is(err, queue.ErrInvalidCursor):
		h.responder.Error(w, r, apperr.Validation(map[string][]string{"cursor": {"The cursor is invalid or expired."}}))
	default:
		h.responder.Error(w, r, apperr.Wrap(apperr.InternalServerError, "queue admin", err))
	}
}

func (h *Handler) listQueues(w http.ResponseWriter, r *http.Request) {
	if !h.requireQueueAdmin(w, r) {
		return
	}
	queues, err := h.queueAdmin.ListQueues(r.Context())
	if err != nil {
		h.respondQueueError(w, r, err)
		return
	}
	h.responder.Data(w, r, http.StatusOK, queues)
}

func (h *Handler) pauseQueue(w http.ResponseWriter, r *http.Request) {
	actorID, ok := h.requireManage(w, r)
	if !ok {
		return
	}
	if !h.requireQueueAdmin(w, r) {
		return
	}
	name := strings.TrimSpace(chi.URLParam(r, "name"))
	if name == "" {
		h.responder.Error(w, r, apperr.New(apperr.InvalidInput, "The queue name is required."))
		return
	}
	started := time.Now()
	err := h.queueAdmin.PauseQueue(r.Context(), name)
	h.recordAction(r.Context(), actorID, "queue.pause", name, map[string]any{"queue": name}, started, err)
	if err != nil {
		h.respondQueueError(w, r, err)
		return
	}
	h.responder.Message(w, r, http.StatusOK, "Queue paused.")
}

func (h *Handler) resumeQueue(w http.ResponseWriter, r *http.Request) {
	actorID, ok := h.requireManage(w, r)
	if !ok {
		return
	}
	if !h.requireQueueAdmin(w, r) {
		return
	}
	name := strings.TrimSpace(chi.URLParam(r, "name"))
	if name == "" {
		h.responder.Error(w, r, apperr.New(apperr.InvalidInput, "The queue name is required."))
		return
	}
	started := time.Now()
	err := h.queueAdmin.ResumeQueue(r.Context(), name)
	h.recordAction(r.Context(), actorID, "queue.resume", name, map[string]any{"queue": name}, started, err)
	if err != nil {
		h.respondQueueError(w, r, err)
		return
	}
	h.responder.Message(w, r, http.StatusOK, "Queue resumed.")
}

func (h *Handler) listJobs(w http.ResponseWriter, r *http.Request) {
	if !h.requireQueueAdmin(w, r) {
		return
	}
	query := r.URL.Query()
	limit, err := parseLimit(query.Get("limit"))
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	state := strings.ToLower(strings.TrimSpace(query.Get("state")))
	if state != "" && !validAdminState(state) {
		h.responder.Error(w, r, apperr.Validation(map[string][]string{
			"state": {"The selected state is invalid."},
		}))
		return
	}
	result, err := h.queueAdmin.ListJobs(r.Context(), queue.AdminJobListParams{
		Queue:  strings.TrimSpace(query.Get("queue")),
		State:  state,
		Kind:   strings.TrimSpace(query.Get("kind")),
		Limit:  limit,
		Cursor: strings.TrimSpace(query.Get("cursor")),
	})
	if err != nil {
		h.respondQueueError(w, r, err)
		return
	}
	meta := map[string]any{"next_cursor": result.NextCursor}
	h.responder.Cursor(w, r, http.StatusOK, result.Jobs, meta, nil)
}

func (h *Handler) getJob(w http.ResponseWriter, r *http.Request) {
	if !h.requireQueueAdmin(w, r) {
		return
	}
	id, err := pathID(r)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	job, err := h.queueAdmin.GetJob(r.Context(), id)
	if err != nil {
		h.respondQueueError(w, r, err)
		return
	}
	h.responder.Data(w, r, http.StatusOK, job)
}

func (h *Handler) retryJob(w http.ResponseWriter, r *http.Request) {
	h.mutateJob(w, r, "queue.job.retry", h.queueAdmin.RetryJob)
}

func (h *Handler) cancelJob(w http.ResponseWriter, r *http.Request) {
	h.mutateJob(w, r, "queue.job.cancel", h.queueAdmin.CancelJob)
}

func (h *Handler) deleteJob(w http.ResponseWriter, r *http.Request) {
	h.mutateJob(w, r, "queue.job.delete", h.queueAdmin.DeleteJob)
}

func (h *Handler) mutateJob(w http.ResponseWriter, r *http.Request, action string, op func(ctx context.Context, id int64) (*queue.AdminJob, error)) {
	actorID, ok := h.requireManage(w, r)
	if !ok {
		return
	}
	if !h.requireQueueAdmin(w, r) {
		return
	}
	id, err := pathID(r)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	started := time.Now()
	job, err := op(r.Context(), id)
	h.recordAction(r.Context(), actorID, action, "job:"+strconv.FormatInt(id, 10), map[string]any{"job_id": id}, started, err)
	if err != nil {
		h.respondQueueError(w, r, err)
		return
	}
	h.responder.Data(w, r, http.StatusOK, job)
}

func (h *Handler) listActions(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	limit, err := parseLimit(query.Get("limit"))
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	beforeID := int64(0)
	if raw := strings.TrimSpace(query.Get("cursor")); raw != "" {
		value, parseErr := strconv.ParseInt(raw, 10, 64)
		if parseErr != nil || value < 0 {
			h.responder.Error(w, r, apperr.Validation(map[string][]string{
				"cursor": {"The cursor is invalid."},
			}))
			return
		}
		beforeID = value
	}
	actions, err := h.actions.List(r.Context(), beforeID, int32(limit))
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	nextCursor := ""
	if len(actions) == limit && len(actions) > 0 {
		nextCursor = strconv.FormatInt(actions[len(actions)-1].ID, 10)
	}
	meta := map[string]any{"next_cursor": nextCursor}
	h.responder.Cursor(w, r, http.StatusOK, actions, meta, nil)
}

func parseLimit(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultAdminLimit, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 || value > maxAdminLimit {
		return 0, apperr.Validation(map[string][]string{
			"limit": {"The limit must be between 1 and 100."},
		})
	}
	return value, nil
}

func validAdminState(state string) bool {
	for _, candidate := range queue.AdminStates {
		if candidate == state {
			return true
		}
	}
	return false
}

func pathID(r *http.Request) (int64, error) {
	raw := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id < 1 {
		return 0, apperr.New(apperr.InvalidInput, "The provided identifier is invalid.")
	}
	return id, nil
}
