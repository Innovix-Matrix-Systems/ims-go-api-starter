package app

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/authn"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/authz"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/modules/dev"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/docs"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/httpx"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/ratelimit"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/storage"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/tenancy"
)

// Router assembles the HTTP handler with the standard middleware chain:
// RequestID → RealIP → SecurityHeaders → CORS → AccessLog → Recover → Metrics →
// Timeout → RateLimit → Locale, then the authentication and tenancy groups per
// route family.
func (d *Dependencies) Router() http.Handler {
	r := chi.NewRouter()

	r.Use(httpx.RequestID)
	if !d.Config.IsLocal() {
		r.Use(httpx.PublicOrigin(d.Config.App.URL))
	}
	r.Use(httpx.RealIP(d.Config.HTTP.TrustedProxies))
	if d.Config.Security.HeadersEnabled {
		r.Use(httpx.SecurityHeaders)
	}
	r.Use(httpx.CORS(
		d.Config.Ops.CORS.AllowedOrigins,
		d.Config.Ops.CORS.AllowedMethods,
		d.Config.Ops.CORS.AllowedHeaders,
		d.Config.Ops.CORS.MaxAge,
	))
	r.Use(httpx.AccessLog(d.Logger))
	r.Use(httpx.Recover(d.Logger, d.Responder))
	if d.Platform != nil && d.Platform.Telemetry != nil {
		r.Use(d.Platform.Telemetry.HTTPMiddleware)
	}
	if d.Config.Ops.MetricsEnabled && d.Metrics != nil {
		r.Use(d.Metrics.Middleware)
	}
	r.Use(httpx.Timeout(d.Config.HTTP.RequestTimeout, d.Responder))
	// Health probes and metrics scrapes never spend the per-IP budget.
	r.Use(ratelimit.Middleware(d.Limiter, ratelimit.KeyByIP, d.Responder, "/up", "/api/health", "/metrics"))
	r.Use(d.I18n.Middleware(d.Config.App.SupportedLocales, d.Config.App.FallbackLocale))

	r.Get("/up", d.up)
	r.Get("/api/health", d.HealthHandler.ServeHTTP)
	r.Handle("/storage/*", http.StripPrefix("/storage/", storage.StaticHandler(d.Config.Storage.PublicDir)))

	if d.Config.Ops.DocsEnabled {
		r.Get("/docs", docs.UI)
		r.Get("/docs/openapi.yaml", docs.Spec)
	}
	if d.Config.Ops.MetricsEnabled && d.Metrics != nil {
		r.Handle("/metrics", httpx.ProtectOps(d.Config.Ops.MetricsToken, d.Metrics.Handler()))
	}

	// Developer dashboard: one gate for the SPA, its JSON API and the telemetry
	// capture surface. Registered unconditionally so the route table stays
	// stable for openapi:check; disabled deployments answer 404 before
	// authentication.
	r.Group(func(r chi.Router) {
		r.Use(dev.Gate(d.Config.DevDashboard.Enabled, d.Responder))
		r.Get("/dev", d.DevUI.ServeHTTP)
		r.HandleFunc("/dev/*", d.DevUI.ServeHTTP)
		r.Route("/dev/api", func(r chi.Router) {
			r.Use(authn.Authenticate(d.Authenticator, d.Responder))
			r.Use(tenancy.Exempt())
			r.Use(authz.Require(d.Checker, authz.PermDevView, d.Responder))
			d.Dev.RegisterRoutes(r)
		})
	})

	r.Route("/api/v1", func(r chi.Router) {
		d.System.RegisterRoutes(r)
		d.Auth.RegisterPublicRoutes(r)
		d.Billing.RegisterPublicRoutes(r)

		r.Group(func(r chi.Router) {
			r.Use(authn.Authenticate(d.Authenticator, d.Responder))

			r.Group(func(r chi.Router) {
				r.Use(tenancy.Resolve(d.TenancyOpts))
				d.Auth.RegisterRoutes(r)
				d.Users.RegisterRoutesResolve(r)
				d.Branches.RegisterRoutes(r)
				d.Roles.RegisterRoutesResolve(r)
				d.Billing.RegisterRoutesResolve(r)
				d.Notifications.RegisterRoutesResolve(r)
				d.Processing.RegisterRoutes(r)
				d.Settings.RegisterRoutes(r)
				d.Flags.RegisterRoutes(r)
			})

			r.Group(func(r chi.Router) {
				r.Use(tenancy.Exempt())

				// Shared exempt surfaces: files are creator-scoped (plus the
				// system-only file.view.all / file.delete permissions) and
				// permission metadata carries no tenant data.
				d.Files.RegisterRoutes(r)
				d.Permissions.RegisterRoutes(r)

				// System-only exempt families: tenant-bound identities are
				// rejected before their handlers run, so a granted system
				// permission alone can never reach cross-tenant endpoints.
				r.Group(func(r chi.Router) {
					r.Use(tenancy.InternalOnly(d.Responder))
					d.Tenants.RegisterRoutes(r)
					d.Users.RegisterRoutesExempt(r)
					d.Roles.RegisterRoutesExempt(r)
					d.Billing.RegisterRoutesExempt(r)
					d.Notifications.RegisterRoutesExempt(r)
					d.Settings.RegisterInternalRoutes(r)
					d.Flags.RegisterInternalRoutes(r)
				})
			})
		})
	})

	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		d.Responder.Error(w, r, apperr.New(apperr.ResourceNotFound, "The requested resource was not found."))
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		d.Responder.Error(w, r, apperr.New(apperr.MethodNotAllowed, "The HTTP method is not supported for this resource."))
	})

	return r
}

func (d *Dependencies) up(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}
