package tenancy

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/authn"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/authz"
	dbgen "github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/cache"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/httpx"
)

// CheckTTL is how long tenant/branch existence checks are cached.
const CheckTTL = 5 * time.Minute

// ResolveOptions configures tenant resolution.
type ResolveOptions struct {
	Queries   *dbgen.Queries
	Cache     cache.Store
	Checker   *authz.Checker
	Responder *httpx.Responder
}

// Resolve determines the effective tenant/branch scope for authenticated
// requests, honoring impersonation headers and cached membership checks.
func Resolve(opts ResolveOptions) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			identity, ok := authn.IdentityFromContext(ctx)
			if !ok {
				opts.Responder.Error(w, r, apperr.New(apperr.Unauthorized, "Authentication is required."))
				return
			}

			canImpersonate, err := opts.Checker.Can(ctx, identity.UserID, authz.PermUserTenantSwitch)
			if err != nil {
				opts.Responder.Error(w, r, err)
				return
			}

			if canImpersonate {
				actingTenant := parseIDHeader(r.Header.Get("X-Acting-Tenant"))
				actingBranch := parseIDHeader(r.Header.Get("X-Acting-Branch"))

				switch {
				case actingTenant == 0 && actingBranch == 0:
					if isWriteMethod(r.Method) {
						opts.Responder.Error(w, r, apperr.New(apperr.Forbidden, "Impersonating writes require the X-Acting-Tenant and X-Acting-Branch headers."))
						return
					}
					serve(next, w, r, Scope{Acting: ActingGlobal})
				case actingTenant == 0:
					opts.Responder.Error(w, r, apperr.New(apperr.TenantContextNotResolved, "X-Acting-Tenant is required when X-Acting-Branch is provided."))
				default:
					exists, err := tenantExists(ctx, opts, actingTenant)
					if err != nil {
						opts.Responder.Error(w, r, err)
						return
					}
					if !exists {
						opts.Responder.Error(w, r, apperr.New(apperr.TenantContextNotResolved, "The acting tenant does not exist."))
						return
					}
					if actingBranch != 0 {
						belongs, err := branchBelongsToTenant(ctx, opts, actingTenant, actingBranch)
						if err != nil {
							opts.Responder.Error(w, r, err)
							return
						}
						if !belongs {
							opts.Responder.Error(w, r, apperr.New(apperr.TenantContextNotResolved, "The selected branch does not belong to the tenant."))
							return
						}
					}
					scope := Scope{
						Acting:         ActingBranch,
						ActingTenantID: actingTenant,
						ActingBranchID: actingBranch,
					}
					if actingBranch == 0 {
						scope.Acting = ActingTenant
					}
					serve(next, w, r, scope)
				}
				return
			}

			tenantID := identity.TenantID
			if tenantID == 0 {
				opts.Responder.Error(w, r, apperr.New(apperr.Forbidden, "No tenant is assigned to this account."))
				return
			}
			branchID := identity.EffectiveBranchID()
			if branchID == 0 {
				opts.Responder.Error(w, r, apperr.New(apperr.Forbidden, "No active branch is assigned to this account."))
				return
			}
			belongs, err := branchBelongsToTenant(ctx, opts, tenantID, branchID)
			if err != nil {
				opts.Responder.Error(w, r, err)
				return
			}
			if !belongs {
				opts.Responder.Error(w, r, apperr.New(apperr.Forbidden, "The assigned branch does not belong to the tenant."))
				return
			}
			serve(next, w, r, Scope{TenantID: tenantID, BranchID: branchID})
		})
	}
}

// Exempt marks requests that operate outside tenant scoping (global/internal
// endpoints such as tenant management or permission administration).
func Exempt() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			scope := Scope{Internal: true}
			if existing, ok := FromContext(r.Context()); ok {
				scope = existing
				scope.Internal = true
			}
			next.ServeHTTP(w, r.WithContext(WithScope(r.Context(), scope)))
		})
	}
}

// BranchCheckKey namespaces cached branch-membership checks.
func BranchCheckKey(tenantID, branchID int64) string {
	return fmt.Sprintf("branch_check:%d:%d", branchID, tenantID)
}

// InvalidateBranchCheck drops a cached membership check after branch mutation.
func InvalidateBranchCheck(ctx context.Context, store cache.Store, tenantID, branchID int64) {
	_ = store.Delete(ctx, BranchCheckKey(tenantID, branchID))
}

// InvalidateTenantCheck drops a cached tenant-existence check after a tenant is
// created, updated or deleted.
func InvalidateTenantCheck(ctx context.Context, store cache.Store, tenantID int64) {
	_ = store.Delete(ctx, tenantCheckKey(tenantID))
}

func tenantCheckKey(tenantID int64) string {
	return fmt.Sprintf("tenant_check:%d", tenantID)
}

func serve(next http.Handler, w http.ResponseWriter, r *http.Request, scope Scope) {
	next.ServeHTTP(w, r.WithContext(WithScope(r.Context(), scope)))
}

func branchBelongsToTenant(ctx context.Context, opts ResolveOptions, tenantID, branchID int64) (bool, error) {
	key := BranchCheckKey(tenantID, branchID)
	var cached bool
	found, err := opts.Cache.Get(ctx, key, &cached)
	if err != nil {
		return false, apperr.Wrap(apperr.InternalServerError, "read branch check cache", err)
	}
	if found {
		return cached, nil
	}
	belongs, err := opts.Queries.BranchBelongsToTenant(ctx, dbgen.BranchBelongsToTenantParams{
		BranchID: branchID,
		TenantID: tenantID,
	})
	if err != nil {
		return false, err
	}
	_ = opts.Cache.Set(ctx, key, belongs, CheckTTL)
	return belongs, nil
}

func tenantExists(ctx context.Context, opts ResolveOptions, tenantID int64) (bool, error) {
	key := tenantCheckKey(tenantID)
	var cached bool
	found, err := opts.Cache.Get(ctx, key, &cached)
	if err != nil {
		return false, apperr.Wrap(apperr.InternalServerError, "read tenant check cache", err)
	}
	if found {
		return cached, nil
	}
	_, err = opts.Queries.GetTenant(ctx, tenantID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return false, err
	}
	exists := err == nil
	_ = opts.Cache.Set(ctx, key, exists, CheckTTL)
	return exists, nil
}

func parseIDHeader(raw string) int64 {
	if raw == "" {
		return 0
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id < 0 {
		return 0
	}
	return id
}

func isWriteMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}
