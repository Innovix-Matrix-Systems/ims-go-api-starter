package httpx

import "context"

// RequestInfo is who a request ran as, filled in while it is handled and read
// after it returns.
//
// A context only carries values downward: what authn.Authenticate and
// tenancy.Resolve add is invisible to middleware that wraps them (telemetry,
// the access log). Those middleware put a *RequestInfo in the context first;
// the inner layers write into it, and the outer ones read it once the handler
// has returned. It lives here because both sides already import httpx.
//
// A request is handled on one goroutine, so no locking is needed. Handlers
// that start their own goroutines must not write to it.
type RequestInfo struct {
	UserID         int64 // the identity the request ran as (the target when acting as a user)
	ImpersonatorID int64 // who was really calling, when acting as a user
	TenantID       int64 // the effective tenant (0 = none or every tenant)
	BranchID       int64 // the effective branch (0 = none)
}

type requestInfoKey struct{}

// WithRequestInfo returns a context carrying info, or ctx unchanged when it
// already carries one (outer middleware share the first holder).
func WithRequestInfo(ctx context.Context) (context.Context, *RequestInfo) {
	if info, ok := ctx.Value(requestInfoKey{}).(*RequestInfo); ok {
		return ctx, info
	}
	info := &RequestInfo{}
	return context.WithValue(ctx, requestInfoKey{}, info), info
}

// RequestInfoFrom returns the request's holder, or nil outside a request
// (tests, CLI, jobs). Writers check for nil.
func RequestInfoFrom(ctx context.Context) *RequestInfo {
	info, _ := ctx.Value(requestInfoKey{}).(*RequestInfo)
	return info
}

// NoteUser records the identity the request runs as.
func NoteUser(ctx context.Context, userID, impersonatorID int64) {
	if info := RequestInfoFrom(ctx); info != nil {
		info.UserID = userID
		info.ImpersonatorID = impersonatorID
	}
}

// NoteScope records the effective tenant and branch.
func NoteScope(ctx context.Context, tenantID, branchID int64) {
	if info := RequestInfoFrom(ctx); info != nil {
		info.TenantID = tenantID
		info.BranchID = branchID
	}
}
