package app

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/authn"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/authz"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/config"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/modules/access"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/modules/activity"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/modules/backup"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/modules/billing"
	billingdomain "github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/modules/billing/domain"
	billingservice "github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/modules/billing/service"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/modules/branches"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/modules/dataprocessing"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/modules/dev"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/modules/featureflags"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/modules/files"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/modules/identity"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/modules/notifications"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/modules/settings"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/cache"
	cachefactory "github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/cache/factory"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/database"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/mail"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/queue"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/scheduler"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/storage"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/telemetry"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/version"
)

// Platform is the infrastructure dependency set shared by the HTTP API and the
// CLI modes: database connections, cache store, registries and platform
// services.
type Platform struct {
	Config           *config.Config
	Logger           *slog.Logger
	DB               *database.Registry
	Tx               *database.TxManager
	Cache            cache.Store
	Storage          *storage.Registry
	Queue            queue.Engine
	Recovery         *authn.Recovery
	QueueAdmin       queue.Admin
	QueueRegistry    *queue.Registry
	Telemetry        *telemetry.Collector
	ScheduleRegistry *scheduler.Registry
	ScheduleRunner   *scheduler.Runner
	Checker          *authz.Checker
	Authenticator    authn.Authenticator
	IdentityRepo     identity.Repository
	Identity         *identity.Service
	Mail             mail.Driver
	Settings         *settings.Service
	FeatureFlags     *featureflags.Service
	Processing       *dataprocessing.Service
	Files            *files.Service
	Backup           *backup.Service
	Notifications    *notifications.Service
	Activity         *activity.Recorder
	Exchange         *identity.ExchangeService
	BillingPackages  *billingservice.PackageService
	BillingSubs      *billingservice.SubscriptionService
	BillingPayments  *billingservice.PaymentService
	BillingApprovals *billingservice.ApprovalService
	BillingGuard     *billingservice.Guard
}

// NewPlatform constructs the platform dependencies. Pools connect lazily, so
// this succeeds even when the database is temporarily unavailable.
func NewPlatform(cfg *config.Config, logger *slog.Logger) (_ *Platform, _ func(), err error) {
	// One cleanup owner: every resource registers its closer as soon as it
	// exists, and any error return closes them all (newest first).
	var closers []func()
	closeAll := func() {
		for i := len(closers) - 1; i >= 0; i-- {
			closers[i]()
		}
	}
	defer func() {
		if err != nil {
			closeAll()
		}
	}()

	lazyTracer := telemetry.NewLazyTracer()
	dbRegistry, err := database.Open(context.Background(), cfg.Database, database.WithTracer(lazyTracer))
	if err != nil {
		return nil, nil, err
	}
	closers = append(closers, dbRegistry.Close)
	txManager := database.NewTxManager(dbRegistry.Primary())

	store, cacheCleanup, err := cachefactory.New(*cfg, txManager.Queries())
	if err != nil {
		return nil, nil, err
	}
	closers = append(closers, cacheCleanup)
	if !cfg.IsLocal() && (cfg.Cache.Driver == "memory" || cfg.Cache.Driver == "file") {
		logger.Warn("per-process cache driver outside local development: permission/tenancy caches, rate limits and the scheduler heartbeat are not shared between the API, worker and scheduler processes; use CACHE_DRIVER=database or redis",
			"driver", cfg.Cache.Driver, "env", cfg.App.Env)
	}

	// Telemetry: the collector's sink writes through the same pools it
	// observes, so the tracer attaches lazily once the collector exists.
	redactor := telemetry.NewRedactor(cfg.DevDashboard.RedactKeys)
	collector := telemetry.NewCollector(telemetry.CaptureConfig{
		Enabled:      cfg.DevDashboard.Enabled,
		Capture:      cfg.DevDashboard.Capture,
		SampleRate:   cfg.DevDashboard.SampleRate,
		MaxBodyBytes: cfg.DevDashboard.MaxBodyBytes,
	}, redactor, dev.NewTelemetrySink(txManager.Queries()), logger)
	closers = append(closers, collector.Close)
	lazyTracer.Attach(collector)
	logger = slog.New(telemetry.WrapLogHandler(logger.Handler(), collector))
	store = telemetry.WrapCache(store, collector)

	queueRegistry := queue.NewRegistry()
	scheduleRegistry := scheduler.NewRegistry()
	activityRecorder := activity.NewRecorder(txManager.Queries(), redactor, logger)
	settingsService := settings.NewService(settings.NewRepository(txManager.Queries()), store).
		WithActivity(activityRecorder)
	featureFlagsService := featureflags.NewService(featureflags.NewRepository(txManager.Queries()), store)

	if cfg.DevDashboard.Enabled && cfg.IsLocal() {
		if err := dev.RegisterPlaygroundJob(queueRegistry); err != nil {
			return nil, nil, err
		}
	}

	queueEngine, err := newQueueEngine(cfg, dbRegistry.Primary().Pool, queueRegistry, logger)
	if err != nil {
		return nil, nil, err
	}

	mailDriver, err := mail.New(cfg.Mail, logger, cfg.IsDebug())
	if err != nil {
		return nil, nil, err
	}
	if err := mail.RegisterJob(queueRegistry, mailDriver); err != nil {
		return nil, nil, err
	}

	notificationsService := notifications.NewService(
		notifications.NewRepository(txManager.Queries(), txManager),
		store,
		queueEngine,
		settingsService,
		logger,
	)
	if err := notificationsService.RegisterJobs(queueRegistry); err != nil {
		return nil, nil, err
	}

	storageRegistry, err := storage.New(cfg, logger)
	if err != nil {
		return nil, nil, err
	}
	filesService := files.NewService(files.NewRepository(txManager.Queries()), storageRegistry, cfg.Files.Disk, txManager, logger).
		WithPublicDisk(cfg.Files.PublicDisk)
	processingService := dataprocessing.NewService(dataprocessing.NewRepository(txManager.Queries())).
		WithFiles(storageRegistry, cfg.Processing).
		WithLogger(logger)
	backupService := backup.NewService(backup.Options{
		Config:     cfg.Backup,
		Repository: backup.NewRepository(txManager.Queries()),
		Disks:      storageRegistry,
		Pool:       dbRegistry.Primary().Pool,
		Queue:      queueEngine,
		DSN:        cfg.Database.Primary.DSN,
		AppVersion: version.Version,
		Logger:     logger,
	})
	if cfg.Backup.Enabled {
		if err := backup.RegisterJob(queueRegistry, backupService); err != nil {
			return nil, nil, err
		}
	}

	// Billing
	billingPackages := billingdomain.NewPackageRepository(txManager.Queries())
	billingSubscriptions := billingdomain.NewSubscriptionRepository(txManager.Queries())
	billingPayments := billingdomain.NewPaymentRepository(txManager.Queries())
	billingApprovals := billingdomain.NewApprovalRepository(txManager.Queries())
	orchestrator := billing.NewOrchestrator(billingPayments, cfg.Billing.Currency).
		Register(billingdomain.MethodBkash, billing.NewBkashDriver(cfg.Billing.Bkash, store)).
		Register(billingdomain.MethodNagad, billing.NagadDriver{}).
		Register(billingdomain.MethodCash, billing.CashDriver{}).
		Register(billingdomain.MethodBankTransfer, billing.BankTransferDriver{})
	billingPackageService := billingservice.NewPackageService(billingPackages).WithTx(txManager)
	billingSubscriptionService := billingservice.NewSubscriptionService(billingSubscriptions, billingPackages, billingPayments, billingApprovals, orchestrator).
		WithNotifier(notificationsService).
		WithCurrency(cfg.Billing.Currency).
		WithTx(txManager).
		WithActivity(activityRecorder)
	billingPaymentService := billingservice.NewPaymentService(billingPayments, cfg.Billing.Currency)
	billingApprovalService := billingservice.NewApprovalService(billingApprovals, billingPayments, billingSubscriptions, txManager)
	billingGuard := billingservice.NewGuard(billingSubscriptions, billingPackages)
	filesService.WithStorageLimit(billingGuard)

	// Identity (shared by HTTP handlers, CLI commands and job handlers).
	identityRepo := identity.NewRepository(txManager.Queries())
	roleRepo := access.NewRoleRepository(txManager.Queries())
	checker := authz.NewChecker(txManager.Queries(), store)
	authenticator := authn.NewOpaqueAuthenticator(txManager.Queries(), identityRepo, cfg.Auth)
	recovery := authn.NewRecovery(identityRepo, identityRepo, authenticator, store, mail.EngineEnqueuer{Engine: queueEngine}, cfg.Auth, logger)
	identityService := identity.NewService(identityRepo, roleRepo, branches.NewRepository(txManager.Queries()), checker, authenticator, cfg.Auth, billingGuard).
		WithTx(txManager).
		WithVerifier(recovery).
		WithActivity(activityRecorder)
	exchangeService := identity.NewExchangeService(identityService, identityRepo, roleRepo, processingService, storageRegistry, queueEngine, cfg, logger)
	if err := exchangeService.RegisterJobs(queueRegistry); err != nil {
		return nil, nil, err
	}

	runner, err := scheduler.NewRunner(scheduleRegistry, logger, store, cfg.Schedule.Timezone)
	if err != nil {
		return nil, nil, err
	}

	platform := &Platform{
		Activity:         activityRecorder,
		Config:           cfg,
		Logger:           logger,
		DB:               dbRegistry,
		Tx:               txManager,
		Cache:            store,
		Storage:          storageRegistry,
		Queue:            queueEngine,
		QueueRegistry:    queueRegistry,
		Telemetry:        collector,
		ScheduleRegistry: scheduleRegistry,
		ScheduleRunner:   runner,
		Checker:          checker,
		Authenticator:    authenticator,
		Recovery:         recovery,
		IdentityRepo:     identityRepo,
		Identity:         identityService,
		Exchange:         exchangeService,
		Mail:             mailDriver,
		Settings:         settingsService,
		FeatureFlags:     featureFlagsService,
		Processing:       processingService,
		Files:            filesService,
		Backup:           backupService,
		Notifications:    notificationsService,
		BillingPackages:  billingPackageService,
		BillingSubs:      billingSubscriptionService,
		BillingPayments:  billingPaymentService,
		BillingApprovals: billingApprovalService,
		BillingGuard:     billingGuard,
	}
	if riverEngine, ok := queueEngine.(*queue.RiverEngine); ok {
		riverEngine.SetObserver(collector)
		platform.QueueAdmin = riverEngine.Admin()
	}
	for _, task := range platform.schedule() {
		if err := scheduleRegistry.Register(task); err != nil {
			return nil, nil, err
		}
	}
	return platform, closeAll, nil
}

// NewQueueEngine selects the configured queue engine.
func (p *Platform) NewQueueEngine(logger *slog.Logger) (queue.Engine, error) {
	engine, err := newQueueEngine(p.Config, p.DB.Primary().Pool, p.QueueRegistry, logger)
	if err != nil {
		return nil, err
	}
	if riverEngine, ok := engine.(*queue.RiverEngine); ok {
		riverEngine.SetObserver(p.Telemetry)
	}
	return engine, nil
}

func newQueueEngine(cfg *config.Config, pool *pgxpool.Pool, registry *queue.Registry, logger *slog.Logger) (queue.Engine, error) {
	switch cfg.Queue.Driver {
	case "sync":
		return queue.NewSyncEngine(registry), nil
	case "river":
		return queue.NewRiverEngine(pool, registry, cfg.Queue, logger, nil)
	default:
		return nil, fmt.Errorf("queue: unsupported driver %q", cfg.Queue.Driver)
	}
}
