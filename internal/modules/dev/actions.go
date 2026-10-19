package dev

import (
	"context"
	"encoding/json"
	"time"
)

const actionStatusOK = "ok"

// recordAction persists an audit row for a dashboard mutation. Audit failures
// are best-effort and never change the API outcome.
func (h *Handler) recordAction(ctx context.Context, actorID int64, action, target string, params any, started time.Time, opErr error) {
	if h.actions == nil {
		return
	}
	payload, err := json.Marshal(params)
	if err != nil || len(payload) == 0 {
		payload = json.RawMessage("{}")
	}
	status := actionStatusOK
	var message *string
	if opErr != nil {
		status = "error"
		value := opErr.Error()
		message = &value
	}
	var targetRef *string
	if target != "" {
		targetRef = &target
	}
	duration := time.Since(started)
	if duration < 0 {
		duration = 0
	}
	_, _ = h.actions.Insert(ctx, InsertActionInput{
		ActorUserID:  actorID,
		Action:       action,
		Target:       targetRef,
		Params:       payload,
		Status:       status,
		ErrorMessage: message,
		DurationMS:   int32(duration.Milliseconds()),
	})
}
