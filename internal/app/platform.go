package app

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/authn"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/authz"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/config"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/modules/access"
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
	Exchange         *identity.ExchangeService
	BillingPackages  *billingservice.PackageService
	BillingSubs      *billingservice.SubscriptionService
	BillingPayments  *billingservice.PaymentService
	BillingApprovals *billingservice.ApprovalService
	BillingGuard     *billingservice.Guard
}

// NewPlatform constructs the platform dependencies. Pools connect lazily, so
// this succeeds even when the database is temporarily unavailable.
func NewPlatform(cfg *config.Config, logger *slog.Logger) (*Platform, func(), error) {
	lazyTracer := telemetry.NewLazyTracer()
	dbRegistry, err := database.Open(context.Background(), cfg.Database, database.WithTracer(lazyTracer))
	if err != nil {
		return nil, nil, err
	}
	txManager := database.NewTxManager(dbRegistry.Primary())

	store, cacheCleanup, err := cachefactory.New(*cfg, txManager.Queries())
	if err != nil {
		dbRegistry.Close()
		return nil, nil, err
	}

	// Telemetry: the collector's sink writes through the same pools it
	// observes, so the tracer attaches lazily once the collector exists.
	collector := telemetry.NewCollector(telemetry.CaptureConfig{
		Enabled:      cfg.DevDashboard.Enabled,
		Capture:      cfg.DevDashboard.Capture,
		SampleRate:   cfg.DevDashboard.SampleRate,
		MaxBodyBytes: cfg.DevDashboard.MaxBodyBytes,
	}, telemetry.NewRedactor(cfg.DevDashboard.RedactKeys), dev.NewTelemetrySink(txManager.Queries()), logger)
	lazyTracer.Attach(collector)
	logger = slog.New(telemetry.WrapLogHandler(logger.Handler(), collector))
	store = telemetry.WrapCache(store, collector)

	queueRegistry := queue.NewRegistry()
	scheduleRegistry := scheduler.NewRegistry()
	settingsService := settings.NewService(settings.NewRepository(txManager.Queries()), store)
	featureFlagsService := featureflags.NewService(featureflags.NewRepository(txManager.Queries()), store)

	if cfg.DevDashboard.Enabled && cfg.IsLocal() {
		if err := dev.RegisterPlaygroundJob(queueRegistry); err != nil {
			cacheCleanup()
			dbRegistry.Close()
			return nil, nil, err
		}
	}

	queueEngine, err := newQueueEngine(cfg, dbRegistry.Primary().Pool, queueRegistry, logger)
	if err != nil {
		cacheCleanup()
		dbRegistry.Close()
		return nil, nil, err
	}
	// Queues are observed through RiverEngine's JobObserver (which carries the
	// inserted job id for deep links); no interface decorator is needed here.
	telemetryQueue := queueEngine

	mailDriver, err := mail.New(cfg.Mail, logger, cfg.IsDebug())
	if err != nil {
		cacheCleanup()
		dbRegistry.Close()
		return nil, nil, err
	}
	if err := mail.RegisterJob(queueRegistry, mailDriver); err != nil {
		cacheCleanup()
		dbRegistry.Close()
		return nil, nil, err
	}

	notificationsService := notifications.NewService(
		notifications.NewRepository(txManager.Queries(), txManager),
		store,
		telemetryQueue,
		settingsService,
		logger,
	)
	if err := notificationsService.RegisterJobs(queueRegistry); err != nil {
		cacheCleanup()
		dbRegistry.Close()
		return nil, nil, err
	}

	storageRegistry, err := storage.New(cfg, logger)
	if err != nil {
		cacheCleanup()
		dbRegistry.Close()
		return nil, nil, err
	}
	filesService := files.NewService(files.NewRepository(txManager.Queries()), storageRegistry, cfg.Files.Disk, txManager, logger)
	processingService := dataprocessing.NewService(dataprocessing.NewRepository(txManager.Queries())).
		WithFiles(storageRegistry, cfg.Processing).
		WithLogger(logger)
	backupService := backup.NewService(backup.Options{
		Config:     cfg.Backup,
		Repository: backup.NewRepository(txManager.Queries()),
		Disks:      storageRegistry,
		Pool:       dbRegistry.Primary().Pool,
		Queue:      telemetryQueue,
		DSN:        cfg.Database.Primary.DSN,
		AppVersion: version.Version,
		Logger:     logger,
	})
	if cfg.Backup.Enabled {
		if err := backup.RegisterJob(queueRegistry, backupService); err != nil {
			cacheCleanup()
			dbRegistry.Close()
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
	billingPackageService := billingservice.NewPackageService(billingPackages)
	billingSubscriptionService := billingservice.NewSubscriptionService(billingSubscriptions, billingPackages, billingPayments, billingApprovals, orchestrator).
		WithNotifier(notificationsService)
	billingPaymentService := billingservice.NewPaymentService(billingPayments, cfg.Billing.Currency)
	billingApprovalService := billingservice.NewApprovalService(billingApprovals, billingPayments, billingSubscriptions, txManager)
	billingGuard := billingservice.NewGuard(billingSubscriptions, billingPackages)

	// Identity (shared by HTTP handlers, CLI commands and job handlers).
	identityRepo := identity.NewRepository(txManager.Queries())
	roleRepo := access.NewRoleRepository(txManager.Queries())
	checker := authz.NewChecker(txManager.Queries(), store)
	authenticator := authn.NewOpaqueAuthenticator(txManager.Queries(), identityRepo, cfg.Auth)
	identityService := identity.NewService(identityRepo, roleRepo, branches.NewRepository(txManager.Queries()), checker, authenticator, cfg.Auth, billingGuard).WithTx(txManager)
	exchangeService := identity.NewExchangeService(identityService, identityRepo, roleRepo, processingService, storageRegistry, telemetryQueue, cfg, logger)
	if err := exchangeService.RegisterJobs(queueRegistry); err != nil {
		cacheCleanup()
		dbRegistry.Close()
		return nil, nil, err
	}

	runner, err := scheduler.NewRunner(scheduleRegistry, logger, store, cfg.Schedule.Timezone)
	if err != nil {
		cacheCleanup()
		dbRegistry.Close()
		return nil, nil, err
	}

	if err := scheduleRegistry.Register(scheduler.Task{
		ID:          "scheduler.heartbeat",
		Spec:        "* * * * *",
		Description: "Records scheduler liveness for health checks.",
		Overlap:     scheduler.OverlapSkip,
		Run: func(ctx context.Context) error {
			runner.Heartbeat(ctx)
			return nil
		},
	}); err != nil {
		cacheCleanup()
		dbRegistry.Close()
		return nil, nil, err
	}

	if err := scheduleRegistry.Register(scheduler.Task{
		ID:          "dataprocessing.cleanup-completed",
		Spec:        "0 2 * * *",
		Description: "Deletes completed processing jobs older than the retention window.",
		Timeout:     30 * time.Minute,
		Overlap:     scheduler.OverlapSkip,
		Run:         dataprocessing.CleanupTask(processingService, settingsService, logger),
	}); err != nil {
		cacheCleanup()
		dbRegistry.Close()
		return nil, nil, err
	}

	if err := scheduleRegistry.Register(scheduler.Task{
		ID:          "billing.expire-subscriptions",
		Spec:        "0 */6 * * *",
		Description: "Marks overdue subscriptions as expired.",
		Timeout:     10 * time.Minute,
		Overlap:     scheduler.OverlapSkip,
		Run: func(ctx context.Context) error {
			expired, err := billingSubscriptionService.ExpireOverdue(ctx)
			if err != nil {
				return err
			}
			logger.Info("subscriptions expired", "count", expired)
			return nil
		},
	}); err != nil {
		cacheCleanup()
		dbRegistry.Close()
		return nil, nil, err
	}

	if err := scheduleRegistry.Register(scheduler.Task{
		ID:          "billing.send-renewal-reminders",
		Spec:        "0 8 * * *",
		Description: "Logs renewal reminders for subscriptions expiring within 7/3/1 days.",
		Timeout:     10 * time.Minute,
		Overlap:     scheduler.OverlapSkip,
		Run: func(ctx context.Context) error {
			sent, err := billingSubscriptionService.SendRenewalReminders(ctx, logger, []int{7, 3, 1})
			if err != nil {
				return err
			}
			logger.Info("renewal reminders sent", "count", sent)
			return nil
		},
	}); err != nil {
		cacheCleanup()
		dbRegistry.Close()
		return nil, nil, err
	}

	if cfg.Backup.Enabled {
		if err := scheduleRegistry.Register(scheduler.Task{
			ID:          "backup.run",
			Spec:        cfg.Backup.RunCron,
			Description: "Creates a database backup archive and copies it to the configured destinations.",
			Timeout:     cfg.Backup.Timeout,
			Overlap:     scheduler.OverlapSkip,
			Run: func(ctx context.Context) error {
				run, err := backupService.Enqueue(ctx, backup.TriggerSchedule, 0)
				if err != nil {
					return err
				}
				logger.Info("backup queued", "run_id", run.ID, "filename", run.Filename)
				return nil
			},
		}); err != nil {
			cacheCleanup()
			dbRegistry.Close()
			return nil, nil, err
		}
		if err := scheduleRegistry.Register(scheduler.Task{
			ID:          "backup.clean",
			Spec:        cfg.Backup.CleanCron,
			Description: "Applies the backup retention policy and deletes expired archives.",
			Timeout:     30 * time.Minute,
			Overlap:     scheduler.OverlapSkip,
			Run: func(ctx context.Context) error {
				result, err := backupService.Clean(ctx, false)
				if err != nil {
					return err
				}
				logger.Info("backup cleanup finished", "deleted", result.Deleted, "candidates", result.Candidates, "failed", result.Failed)
				return nil
			},
		}); err != nil {
			cacheCleanup()
			dbRegistry.Close()
			return nil, nil, err
		}
	}

	if cfg.DevDashboard.Enabled {
		telemetryRepo := dev.NewTelemetryRepository(txManager.Queries())
		retentionDays := cfg.DevDashboard.RetentionDays
		if err := scheduleRegistry.Register(scheduler.Task{
			ID:          "devdashboard.prune",
			Spec:        "0 3 * * *",
			Description: "Deletes developer dashboard telemetry older than the retention window.",
			Timeout:     10 * time.Minute,
			Overlap:     scheduler.OverlapSkip,
			Run: func(ctx context.Context) error {
				deleted, err := telemetryRepo.Prune(ctx, retentionDays)
				if err != nil {
					return err
				}
				logger.Info("dev dashboard telemetry pruned", "deleted", deleted, "retention_days", retentionDays)
				return nil
			},
		}); err != nil {
			cacheCleanup()
			dbRegistry.Close()
			return nil, nil, err
		}
	}

	platform := &Platform{
		Config:           cfg,
		Logger:           logger,
		DB:               dbRegistry,
		Tx:               txManager,
		Cache:            store,
		Storage:          storageRegistry,
		Queue:            telemetryQueue,
		QueueRegistry:    queueRegistry,
		Telemetry:        collector,
		ScheduleRegistry: scheduleRegistry,
		ScheduleRunner:   runner,
		Checker:          checker,
		Authenticator:    authenticator,
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
	cleanup := func() {
		collector.Close()
		cacheCleanup()
		dbRegistry.Close()
	}
	return platform, cleanup, nil
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
