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
		return &Gate{checker: checker, permission: permission, responder: responder, next: next}
	}
}

// Gate is the handler Require wraps a route with. It is a named type so
// tooling (the dev dashboard's route inspector) can read which permission a
// route needs.
type Gate struct {
	checker    *Checker
	permission string
	responder  *httpx.Responder
	next       http.Handler
}

// Permission is the permission this gate requires.
func (g *Gate) Permission() string { return g.permission }

// Next is the wrapped handler.
func (g *Gate) Next() http.Handler { return g.next }

func (g *Gate) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	identity, ok := authn.IdentityFromContext(r.Context())
	if !ok {
		g.responder.Error(w, r, apperr.New(apperr.Unauthorized, "Authentication is required."))
		return
	}
	if err := g.checker.MustCan(r.Context(), identity.UserID, g.permission); err != nil {
		g.responder.Error(w, r, err)
		return
	}
	g.next.ServeHTTP(w, r)
}
