// Package activity is the tenant-scoped activity log ("who did what"), like
// spatie/laravel-activitylog. Services call Record after a change succeeds;
// tenant admins read their tenant's feed at GET /api/v1/activity.
package activity

import (
	"context"
	"log/slog"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/authn"
	dbgen "github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/httpx"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/telemetry"
)

// Entry is one thing that happened.
//
//	recorder.Record(ctx, activity.Entry{
//		TenantID: user.TenantID, Action: "user.created",
//		Subject: "user", SubjectID: user.ID,
//		Changes: map[string]any{"name": user.Name, "email": user.Email},
//	})
type Entry struct {
	TenantID  int64          // 0 = a platform-level action (no tenant)
	ActorID   int64          // 0 = the authenticated user in ctx (or the system)
	Action    string         // "<subject>.<verb>", e.g. "user.created"
	Subject   string         // "user", "role", "branch", ...
	SubjectID int64          // 0 when there is no single row
	Changes   map[string]any // new values; secrets are redacted before storing
}

// Recorder writes activity entries. A nil *Recorder records nothing, so a
// service works without one (unit tests, tools that do not need the log).
type Recorder struct {
	queries  *dbgen.Queries
	redactor *telemetry.Redactor
	logger   *slog.Logger
}

// NewRecorder creates the activity recorder.
func NewRecorder(queries *dbgen.Queries, redactor *telemetry.Redactor, logger *slog.Logger) *Recorder {
	return &Recorder{queries: queries, redactor: redactor, logger: logger}
}

// Record stores an entry. It never fails the caller: the change already
// happened, so a logging problem is reported in the application log instead.
// The IP and request id come from the request context when there is one.
func (r *Recorder) Record(ctx context.Context, entry Entry) {
	if r == nil {
		return
	}
	actorID := entry.ActorID
	if actorID == 0 {
		actorID = authn.UserIDFromContext(ctx)
	}
	changes := []byte("{}")
	if len(entry.Changes) > 0 {
		if encoded := r.redactor.AnyJSON(entry.Changes); encoded != nil {
			changes = encoded
		}
	}
	// The request may already be finishing; the insert must not be cancelled
	// with it.
	err := r.queries.InsertActivity(context.WithoutCancel(ctx), dbgen.InsertActivityParams{
		TenantID:    optional(entry.TenantID),
		ActorID:     optional(actorID),
		Action:      entry.Action,
		SubjectType: entry.Subject,
		SubjectID:   optional(entry.SubjectID),
		Changes:     changes,
		Ip:          optionalString(httpx.ClientIP(ctx)),
		RequestID:   optionalString(httpx.RequestIDFromContext(ctx)),
	})
	if err != nil && r.logger != nil {
		r.logger.WarnContext(ctx, "activity log write failed", "action", entry.Action, "error", err)
	}
}

func optional(value int64) *int64 {
	if value == 0 {
		return nil
	}
	return &value
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
