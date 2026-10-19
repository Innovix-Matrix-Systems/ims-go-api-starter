package dev

import (
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/httpx"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/telemetry"
)

type simulateEventsRequest struct {
	Logs      int    `json:"logs" validate:"omitempty,min=0,max=20"`
	Level     string `json:"level" validate:"omitempty,oneof=warn error"`
	Exception bool   `json:"exception"`
}

// simulateEvents writes synthetic telemetry for dashboard testing. It forces
// the capture flag on the local context (the /dev prefix is excluded from the
// HTTP capture middleware) so the records land in the Logs/Exceptions screens.
func (h *Handler) simulateEvents(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	actorID, ok := h.requireManage(w, r)
	if !ok {
		return
	}
	var request simulateEventsRequest
	if err := h.binder.JSON(w, r, &request); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	if err := h.validator.Struct(r.Context(), &request); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	if h.collector == nil || !h.collector.Enabled() {
		h.responder.Error(w, r, apperr.New(apperr.Conflict,
			"Telemetry capture is disabled; nothing would be recorded."))
		return
	}

	ctx := telemetry.WithCapture(r.Context(), true)
	requestID := httpx.RequestIDFromContext(ctx)
	level := slog.LevelWarn
	if request.Level == "error" {
		level = slog.LevelError
	}
	for index := 1; index <= request.Logs; index++ {
		h.logger.Log(ctx, level, "Synthetic developer dashboard log",
			"source", "dev-simulator",
			"index", index,
		)
	}
	if request.Exception {
		h.collector.Exception(telemetry.ExceptionEntry{
			RequestID: requestID,
			Class:     "SimulatedException",
			Message:   "Synthetic exception generated from the developer dashboard.",
			Stack:     string(debug.Stack()),
			CreatedAt: time.Now(),
		})
	}

	h.recordAction(ctx, actorID, "dev.simulate-events", "", map[string]any{
		"logs":      request.Logs,
		"level":     level.String(),
		"exception": request.Exception,
	}, started, nil)

	h.responder.Data(w, r, http.StatusOK, map[string]any{
		"request_id": requestID,
		"logs":       request.Logs,
		"exception":  request.Exception,
		"level":      level.String(),
	})
}
