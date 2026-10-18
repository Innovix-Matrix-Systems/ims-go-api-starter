package authn

import (
	"net/http"
	"strings"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/httpx"
)

// Authenticate resolves the bearer token into an Identity in the context.
func Authenticate(auth Authenticator, responder *httpx.Responder) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := r.Header.Get("Authorization")
			if !strings.HasPrefix(header, "Bearer ") {
				responder.Error(w, r, apperr.New(apperr.TokenMissing, "Authentication is required."))
				return
			}
			raw := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
			if raw == "" {
				responder.Error(w, r, apperr.New(apperr.TokenMissing, "Authentication is required."))
				return
			}

			identity, err := auth.Verify(r.Context(), raw)
			if err != nil {
				responder.Error(w, r, err)
				return
			}
			if !identity.IsActive {
				responder.Error(w, r, apperr.New(apperr.AccountDeactivated, "Your account is deactivated."))
				return
			}
			next.ServeHTTP(w, r.WithContext(WithIdentity(r.Context(), identity)))
		})
	}
}
