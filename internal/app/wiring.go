package app

import (
	"log/slog"
	"net/http"
	"time"

	webdev "github.com/Innovix-Matrix-Systems/ims-go-api-starter/web/dev"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/authn"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/authz"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/config"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/modules/access"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/modules/auth"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/modules/billing"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/modules/branches"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/modules/dataprocessing"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/modules/dev"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/modules/featureflags"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/modules/files"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/modules/identity"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/modules/notifications"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/modules/settings"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/modules/system"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/modules/tenants"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/cache"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/database"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/health"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/httpx"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/i18n"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/mail"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/metrics"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/queue"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/ratelimit"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/scheduler"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/webui"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/tenancy"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/version"
)

// Dependencies is the composition root's dependency graph. It is constructed
// per selected mode (serve, CLI commands) and passed explicitly to consumers.
type Dependencies struct {
	Config        *config.Config
	Logger        *slog.Logger
	Responder     *httpx.Responder
	Binder        *httpx.Binder
	Validator     *httpx.Validator
	I18n          *i18n.Translator
	Health        *health.Registry
	HealthHandler *health.Handler
	Metrics       *metrics.Registry
	Limiter       ratelimit.Limiter
	DB            *database.Registry
	Tx            *database.TxManager
	Cache         cache.Store
	Checker       *authz.Checker
	Authenticator authn.Authenticator
	TenancyOpts   tenancy.ResolveOptions
	Platform      *Platform
	System        *system.Module
	Auth          *auth.Handler
	Users         *identity.Handler
	Tenants       *tenants.Handler
	Branches      *branches.Handler
	Roles         *access.RoleHandler
	Permissions   *access.PermissionHandler
	Billing       *billing.Handler
	Files         *files.Handler
	Notifications *notifications.Handler
	Processing    *dataprocessing.Handler
	Settings      *settings.Handler
	Flags         *featureflags.Handler
	Dev           *dev.Handler
	DevUI         *webui.Handler
}

// NewDependencies constructs the dependency graph for the HTTP API mode. The
// returned cleanup function releases owned resources.
func NewDependencies(cfg *config.Config, logger *slog.Logger) (*Dependencies, func(), error) {
	platform, platformCleanup, err := NewPlatform(cfg, logger)
	if err != nil {
		return nil, nil, err
	}
	logger = platform.Logger

	responder := httpx.NewResponder(cfg.App.URL, cfg.App.Env, cfg.App.Debug)
	binder := httpx.NewBinder(cfg.HTTP.MaxRequestBodyBytes, cfg.HTTP.StrictJSONFields)
	validator := httpx.NewValidator()
	translator, err := i18n.Load()
	if err != nil {
		platformCleanup()
		return nil, nil, err
	}

	queries := platform.Tx.Queries()
	store := platform.Cache
	checker := platform.Checker

	healthRegistry := health.NewRegistry()
	healthRegistry.Add(health.EnvironmentCheck{Env: cfg.App.Env})
	healthRegistry.Add(health.DebugModeCheck{Env: cfg.App.Env, Debug: cfg.App.Debug})
	healthRegistry.Add(health.ApplicationCheck{NameValue: cfg.App.Name, VersionValue: version.Version})
	healthRegistry.Add(database.HealthCheck{Registry: platform.DB})
	healthRegistry.Add(database.MigrationCheck{Pool: platform.DB.Primary().Pool})
	healthRegistry.Add(cache.HealthCheck{Store: store})
	healthRegistry.Add(queue.HealthCheck{Pool: platform.DB.Primary().Pool})
	healthRegistry.Add(scheduler.HealthCheck{Store: store})
	healthHandler := health.NewHandler(healthRegistry, 30*time.Second)

	limiter := ratelimit.NewCacheLimiter(store, cfg.RateLimit.PerMinute, time.Minute)

	distFS, err := webdev.Dist()
	if err != nil {
		platformCleanup()
		return nil, nil, err
	}
	devUI := webui.New(http.FS(distFS))

	// Repositories
	identityRepo := platform.IdentityRepo
	roleRepo := access.NewRoleRepository(queries)
	tenantLookup := access.NewTenantLookup(queries)
	permissionRepo := access.NewPermissionRepository(queries)
	branchRepo := branches.NewRepository(queries)
	tenantRepo := tenants.NewRepository(queries)

	// Services
	roleService := access.NewRoleService(roleRepo, permissionRepo, checker, platform.Tx)
	permissionService := access.NewPermissionService(permissionRepo, checker)
	branchService := branches.NewService(branchRepo, identityRepo, store, checker, cfg.Tenancy, platform.BillingGuard)
	tenantService := tenants.NewService(tenantRepo, branchService, roleService, platform.Tx, store, cfg.Tenancy)

	authenticator := platform.Authenticator
	otpService := authn.NewOTPService(store, cfg.Auth, logger, cfg.IsDebug()).WithMailer(mail.EngineEnqueuer{Engine: platform.Queue})
	throttle := authn.NewThrottle(store, cfg.Auth.LoginMaxAttempts, cfg.Auth.LoginDecay)
	authService := authn.NewService(identityRepo, authenticator, otpService, throttle, cfg.Auth)
	userService := platform.Identity

	// Handlers and modules
	systemModule := system.NewModule(
		system.NewService(cfg.App.Name, cfg.App.Env, version.Version, cfg.App.Timezone),
		binder,
		validator,
		responder,
	)

	deps := &Dependencies{
		Config:        cfg,
		Logger:        logger,
		Responder:     responder,
		Binder:        binder,
		Validator:     validator,
		I18n:          translator,
		Health:        healthRegistry,
		HealthHandler: healthHandler,
		Metrics:       metrics.New(),
		Limiter:       limiter,
		DB:            platform.DB,
		Tx:            platform.Tx,
		Cache:         store,
		Checker:       checker,
		Authenticator: authenticator,
		TenancyOpts: tenancy.ResolveOptions{
			Queries:   queries,
			Cache:     store,
			Checker:   checker,
			Responder: responder,
		},
		Platform:      platform,
		System:        systemModule,
		Auth:          auth.NewHandler(authService, userService, checker, binder, validator, responder),
		Users:         identity.NewHandler(userService, platform.Files, platform.Exchange, checker, binder, validator, responder),
		Tenants:       tenants.NewHandler(tenantService, checker, binder, validator, responder),
		Branches:      branches.NewHandler(branchService, checker, binder, validator, responder),
		Roles:         access.NewRoleHandler(roleService, checker, tenantLookup, binder, validator, responder),
		Permissions:   access.NewPermissionHandler(permissionService, checker, binder, validator, responder),
		Files:         files.NewHandler(platform.Files, checker, responder),
		Notifications: notifications.NewHandler(platform.Notifications, checker, binder, validator, responder),
		Processing:    dataprocessing.NewHandler(platform.Processing, platform.Settings, checker, binder, validator, responder),
		Settings:      settings.NewHandler(platform.Settings, checker, binder, validator, responder),
		Flags:         featureflags.NewHandler(platform.FeatureFlags, checker, binder, validator, responder),
		Dev:           dev.NewHandler(checker, permissionService, platform.QueueAdmin, dev.NewActionRepository(queries), dev.NewTelemetryRepository(queries), platform.Telemetry, logger, cfg.DevDashboard.RetentionDays, binder, validator, responder),
		DevUI:         devUI,
		Billing: billing.NewHandler(
			platform.BillingPackages,
			platform.BillingSubs,
			platform.BillingPayments,
			platform.BillingApprovals,
			checker,
			binder,
			validator,
			responder,
		),
	}
	cleanup := func() {
		limiter.Close()
		platformCleanup()
	}
	return deps, cleanup, nil
}
