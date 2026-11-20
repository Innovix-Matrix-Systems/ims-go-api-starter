// Package activity is the tenant-scoped activity log ("who did what"), like
// spatie/laravel-activitylog. Services call Record after a change succeeds;
// tenant admins read their tenant's feed at GET /api/v1/activity.
//
// Rows with no tenant are the system audit log (internal users and roles,
// system settings, act-as). Only activity.view.all holders read them.
package activity

import (
	"context"
	"log/slog"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/authn"
	dbgen "github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/httpx"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/telemetry"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/tenancy"
)

var _ tenancy.ImpersonationAudit = (*Recorder)(nil)

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

// Action names for act-as (phase 21). They are system rows: tenant users never
// see them.
const (
	ActionImpersonationStarted = "impersonation.started"
	ActionImpersonationRequest = "impersonation.request"
	ActionImpersonationDenied  = "impersonation.denied"
)

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
//
// While someone acts as a user, the entry is marked with that person
// (impersonator_id and a name snapshot); the actor stays the user.
func (r *Recorder) Record(ctx context.Context, entry Entry) {
	if r == nil {
		return
	}
	var impersonatorID int64
	var impersonatorName string
	if identity, ok := authn.IdentityFromContext(ctx); ok && identity.Impersonated() {
		impersonatorID, impersonatorName = identity.ImpersonatorID, identity.ImpersonatorName
	}
	if err := r.insert(ctx, entry, impersonatorID, impersonatorName); err != nil && r.logger != nil {
		r.logger.WarnContext(ctx, "activity log write failed", "action", entry.Action, "error", err)
	}
}

func (r *Recorder) insert(ctx context.Context, entry Entry, impersonatorID int64, impersonatorName string) error {
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
	return r.queries.InsertActivity(context.WithoutCancel(ctx), dbgen.InsertActivityParams{
		TenantID:         optional(entry.TenantID),
		ActorID:          optional(actorID),
		ImpersonatorID:   optional(impersonatorID),
		ImpersonatorName: optionalString(impersonatorName),
		Action:           entry.Action,
		SubjectType:      entry.Subject,
		SubjectID:        optional(entry.SubjectID),
		Changes:          changes,
		Ip:               optionalString(httpx.ClientIP(ctx)),
		RequestID:        optionalString(httpx.RequestIDFromContext(ctx)),
	})
}

// The methods below implement tenancy.ImpersonationAudit. Act-as rows are
// system rows (no tenant) written as the impersonator; the target is the
// subject, and the target's tenant and branch are in changes.

// ImpersonationStarted records the start of acting as a user. Its error is
// returned, so tenancy refuses the request when the row is not stored.
func (r *Recorder) ImpersonationStarted(ctx context.Context, event tenancy.Impersonation) error {
	if r == nil {
		return nil
	}
	err := r.insert(ctx, impersonationEntry(ActionImpersonationStarted, event, map[string]any{
		"target_name": event.TargetName,
		"tenant_id":   event.TenantID,
		"branch_id":   event.BranchID,
	}), event.ImpersonatorID, event.ImpersonatorName)
	if err != nil && r.logger != nil {
		r.logger.ErrorContext(ctx, "impersonation start not recorded", "impersonator_id", event.ImpersonatorID,
			"target_id", event.TargetID, "error", err)
	}
	return err
}

// ImpersonationRequested records one request made while acting as a user.
func (r *Recorder) ImpersonationRequested(ctx context.Context, event tenancy.ImpersonationRequest) {
	if r == nil {
		return
	}
	err := r.insert(ctx, impersonationEntry(ActionImpersonationRequest, event.Impersonation, map[string]any{
		"method":    event.Method,
		"route":     event.Route,
		"status":    event.Status,
		"tenant_id": event.TenantID,
		"branch_id": event.BranchID,
	}), event.ImpersonatorID, event.ImpersonatorName)
	if err != nil && r.logger != nil {
		r.logger.ErrorContext(ctx, "impersonation request not recorded", "impersonator_id", event.ImpersonatorID,
			"target_id", event.TargetID, "error", err)
	}
}

// ImpersonationDenied records a refused act-as attempt.
func (r *Recorder) ImpersonationDenied(ctx context.Context, callerID, targetID int64, reason string) {
	if r == nil {
		return
	}
	entry := Entry{
		ActorID: callerID, Action: ActionImpersonationDenied, Subject: "user", SubjectID: targetID,
		Changes: map[string]any{"reason": reason},
	}
	if err := r.insert(ctx, entry, 0, ""); err != nil && r.logger != nil {
		r.logger.WarnContext(ctx, "activity log write failed", "action", entry.Action, "error", err)
	}
}

func impersonationEntry(action string, event tenancy.Impersonation, changes map[string]any) Entry {
	return Entry{
		ActorID: event.ImpersonatorID, Action: action, Subject: "user", SubjectID: event.TargetID,
		Changes: changes,
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
