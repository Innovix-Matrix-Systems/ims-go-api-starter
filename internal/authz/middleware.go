package authz

import (
	"net/http"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/authn"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/httpx"
)

// Require gates a route on a permission — the equivalent of Laravel's
// `can:` middleware. Permission checks live at the HTTP edge (this middleware
// or MustCan at the top of a handler); services enforce tenancy and
// business invariants, not permissions.
func Require(checker *Checker, permission string, responder *httpx.Responder) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			identity, ok := authn.IdentityFromContext(r.Context())
			if !ok {
				responder.Error(w, r, apperr.New(apperr.Unauthorized, "Authentication is required."))
				return
			}
			if err := checker.MustCan(r.Context(), identity.UserID, permission); err != nil {
				responder.Error(w, r, err)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
