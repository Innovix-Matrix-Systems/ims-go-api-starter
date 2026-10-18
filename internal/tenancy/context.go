// Package tenancy resolves and propagates the tenant/branch execution scope
// for every request, job and command.
package tenancy

import "context"

// Acting describes impersonation state for the current request.
type Acting int

const (
	ActingNone Acting = iota
	ActingGlobal
	ActingTenant
	ActingBranch
)

// Scope is the resolved tenant/branch execution context.
type Scope struct {
	TenantID       int64
	BranchID       int64
	Acting         Acting
	ActingTenantID int64
	ActingBranchID int64
	Internal       bool
}

// EffectiveTenantID returns the tenant used for data access
// (impersonation overrides identity; zero means no tenant filter). An explicit
// global acting scope always yields zero — no tenant filter.
func (s Scope) EffectiveTenantID() int64 {
	switch s.Acting {
	case ActingGlobal:
		return 0
	case ActingTenant, ActingBranch:
		return s.ActingTenantID
	}
	return s.TenantID
}

// EffectiveBranchID returns the branch used for data access
// (zero means no branch filter).
func (s Scope) EffectiveBranchID() int64 {
	switch s.Acting {
	case ActingBranch:
		return s.ActingBranchID
	case ActingTenant, ActingGlobal:
		return 0
	}
	return s.BranchID
}

// IsImpersonating reports whether an acting scope overrides identity.
func (s Scope) IsImpersonating() bool {
	return s.Acting == ActingGlobal || s.Acting == ActingTenant || s.Acting == ActingBranch
}

// AllTenants reports whether reads should span every tenant: internal requests
// (tenant.exempt) and impersonators performing a global read (no acting
// headers). Writes never reach services with a global acting scope — the
// resolve middleware rejects them with 403 — so this is safe to use on read
// paths only.
func (s Scope) AllTenants() bool {
	return s.Internal || s.Acting == ActingGlobal
}

type contextKey string

const scopeKey contextKey = "tenancy_scope"

// WithScope stores the resolved scope in the context.
func WithScope(ctx context.Context, scope Scope) context.Context {
	return context.WithValue(ctx, scopeKey, scope)
}

// FromContext returns the resolved scope, if any.
func FromContext(ctx context.Context) (Scope, bool) {
	scope, ok := ctx.Value(scopeKey).(Scope)
	return scope, ok
}
