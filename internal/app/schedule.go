package app

import (
	"context"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/modules/activity"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/modules/backup"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/modules/dataprocessing"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/modules/dev"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/scheduler"
)

// schedule lists the recurring tasks run by `ims schedule` (Laravel's
// Kernel::schedule). Overlapping runs are skipped by default; tasks that
// depend on a feature are only listed when it is enabled.
func (p *Platform) schedule() []scheduler.Task {
	cfg, logger, queries := p.Config, p.Logger, p.Tx.Queries()

	tasks := []scheduler.Task{
		{
			ID:          "scheduler.heartbeat",
			Spec:        "* * * * *",
			Description: "Records scheduler liveness for health checks.",
			Run: func(ctx context.Context) error {
				p.ScheduleRunner.Heartbeat(ctx)
				return nil
			},
		},
		{
			ID:          "dataprocessing.cleanup-completed",
			Spec:        "0 2 * * *",
			Description: "Deletes completed processing jobs older than the retention window.",
			Timeout:     30 * time.Minute,
			Run:         dataprocessing.CleanupTask(p.Processing, p.Settings, logger),
		},
		{
			ID:          "notifications.purge-expired",
			Spec:        "30 3 * * *",
			Description: "Deletes notifications that expired more than 7 days ago.",
			Timeout:     30 * time.Minute,
			Run: func(ctx context.Context) error {
				purged, err := queries.PurgeExpiredNotifications(ctx)
				if err != nil {
					return err
				}
				logger.Info("expired notifications purged", "count", purged)
				return nil
			},
		},
		{
			ID:          "billing.expire-subscriptions",
			Spec:        "0 */6 * * *",
			Description: "Marks overdue subscriptions as expired.",
			Timeout:     10 * time.Minute,
			Run: func(ctx context.Context) error {
				expired, err := p.BillingSubs.ExpireOverdue(ctx)
				if err != nil {
					return err
				}
				logger.Info("subscriptions expired", "count", expired)
				return nil
			},
		},
		{
			ID:          "billing.send-renewal-reminders",
			Spec:        "0 8 * * *",
			Description: "Logs renewal reminders for subscriptions expiring within 7/3/1 days.",
			Timeout:     10 * time.Minute,
			Run: func(ctx context.Context) error {
				sent, err := p.BillingSubs.SendRenewalReminders(ctx, logger, []int{7, 3, 1})
				if err != nil {
					return err
				}
				logger.Info("renewal reminders sent", "count", sent)
				return nil
			},
		},
	}

	if days := cfg.Activity.RetentionDays; days > 0 {
		feed := activity.NewService(queries)
		tasks = append(tasks, scheduler.Task{
			ID:          "activity.prune",
			Spec:        "45 3 * * *",
			Description: "Deletes activity-log entries older than ACTIVITY_RETENTION_DAYS.",
			Timeout:     30 * time.Minute,
			Run: func(ctx context.Context) error {
				deleted, err := feed.Purge(ctx, days)
				if err != nil {
					return err
				}
				logger.Info("activity log pruned", "deleted", deleted, "retention_days", days)
				return nil
			},
		})
	}

	if cfg.Cache.Driver == "database" {
		tasks = append(tasks, scheduler.Task{
			ID:          "cache.purge-expired",
			Spec:        "15 * * * *",
			Description: "Deletes expired rows from the database cache table.",
			Timeout:     10 * time.Minute,
			Run: func(ctx context.Context) error {
				purged, err := queries.CacheDeleteExpired(ctx)
				if err != nil {
					return err
				}
				logger.Info("expired cache rows purged", "count", purged)
				return nil
			},
		})
	}

	if cfg.Backup.Enabled {
		tasks = append(tasks,
			scheduler.Task{
				ID:          "backup.run",
				Spec:        cfg.Backup.RunCron,
				Description: "Creates a database backup archive and copies it to the configured destinations.",
				Timeout:     cfg.Backup.Timeout,
				Run: func(ctx context.Context) error {
					run, err := p.Backup.Enqueue(ctx, backup.TriggerSchedule, 0)
					if err != nil {
						return err
					}
					logger.Info("backup queued", "run_id", run.ID, "filename", run.Filename)
					return nil
				},
			},
			scheduler.Task{
				ID:          "backup.clean",
				Spec:        cfg.Backup.CleanCron,
				Description: "Applies the backup retention policy and deletes expired archives.",
				Timeout:     30 * time.Minute,
				Run: func(ctx context.Context) error {
					result, err := p.Backup.Clean(ctx, false)
					if err != nil {
						return err
					}
					logger.Info("backup cleanup finished", "deleted", result.Deleted, "candidates", result.Candidates, "failed", result.Failed)
					return nil
				},
			},
		)
	}

	if dash := cfg.DevDashboard; dash.Enabled {
		telemetry := dev.NewTelemetryRepository(queries)
		audit := dev.NewActionRepository(queries)
		tasks = append(tasks, scheduler.Task{
			ID:          "devdashboard.prune",
			Spec:        "0 3 * * *",
			Description: "Deletes dashboard telemetry and audit rows older than their retention windows.",
			Timeout:     10 * time.Minute,
			Run: func(ctx context.Context) error {
				deleted, err := telemetry.Prune(ctx, dash.RetentionDays)
				if err != nil {
					return err
				}
				logger.Info("dev dashboard telemetry pruned", "deleted", deleted, "retention_days", dash.RetentionDays)
				pruned, err := audit.Prune(ctx, dash.AuditRetentionDays)
				if err != nil {
					return err
				}
				logger.Info("dev dashboard audit log pruned", "deleted", pruned, "retention_days", dash.AuditRetentionDays)
				return nil
			},
		})
	}

	return tasks
}
