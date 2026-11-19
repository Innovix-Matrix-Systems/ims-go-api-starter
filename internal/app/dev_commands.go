package app

import (
	"context"
	"fmt"
	"io"
	"runtime"
	"strconv"
	"text/tabwriter"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/modules/access"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/modules/activity"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/modules/backup"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/modules/dev"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/database"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/health"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/queue"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/version"
)

// devCommands is the allowlist the developer dashboard's Commands panel can
// run — the same work as the matching `ims` CLI commands, in-process. Add an
// entry here to expose another maintenance task; anything that changes data
// sets Confirm, anything slow sets Background.
func devCommands(p *Platform, permissions *access.PermissionService, healthRegistry *health.Registry) []dev.Command {
	cfg := p.Config
	return []dev.Command{
		{
			Name: "about", Label: "About", Group: "Info",
			Description: "Versions, drivers and storage of this instance.",
			Run: func(ctx context.Context, _ map[string]string, out io.Writer) error {
				w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
				rows := [][2]string{
					{"Application", cfg.App.Name}, {"Version", version.Version}, {"Environment", cfg.App.Env},
					{"Debug", strconv.FormatBool(cfg.App.Debug)}, {"URL", cfg.App.URL}, {"Go", runtime.Version()},
					{"Timezone", cfg.App.Timezone}, {"Cache driver", cfg.Cache.Driver}, {"Queue driver", cfg.Queue.Driver},
					{"Mail driver", cfg.Mail.Driver}, {"Files disk", cfg.Files.Disk},
					{"Disks", fmt.Sprint(p.Storage.Names())},
				}
				for _, row := range rows {
					fmt.Fprintf(w, "%s\t%s\n", row[0], row[1])
				}
				return w.Flush()
			},
		},
		{
			Name: "migrate:status", Label: "Migration status", Group: "Database",
			Description: "Every migration and whether it has run, plus the River queue schema.",
			Run: func(ctx context.Context, _ map[string]string, out io.Writer) error {
				pool := p.DB.Primary().Pool
				statuses, err := database.MigrationStatuses(ctx, pool)
				if err != nil {
					return err
				}
				w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
				fmt.Fprintln(w, "STATUS\tVERSION\tNAME")
				pending := 0
				for _, status := range statuses {
					state := "applied"
					if !status.Applied {
						state, pending = "pending", pending+1
					}
					fmt.Fprintf(w, "%s\t%d\t%s\n", state, status.Version, status.Name)
				}
				if err := w.Flush(); err != nil {
					return err
				}
				fmt.Fprintf(out, "\n%d migration(s), %d pending.\n", len(statuses), pending)
				if riverVersion, exists, err := database.RiverSchemaVersion(ctx, pool); err == nil && exists {
					fmt.Fprintf(out, "River schema: v%d\n", riverVersion)
				}
				return nil
			},
		},
		{
			Name: "health:check", Label: "Run health checks", Group: "Info",
			Description: "Run every health check now (database, cache, queue, scheduler, backups…).",
			Run: func(ctx context.Context, _ map[string]string, out io.Writer) error {
				results := healthRegistry.Run(ctx)
				for _, result := range results.CheckResults {
					fmt.Fprintf(out, "%-8s %s: %s\n", result.Status, result.Name, result.ShortSummary)
				}
				if results.HasFailures() {
					return fmt.Errorf("one or more health checks failed")
				}
				return nil
			},
		},
		{
			Name: "permission:sync", Label: "Sync permissions", Group: "Sync", Confirm: true,
			Description: "Write the permission catalog (internal/authz/catalog.go) to the database.",
			Run: func(ctx context.Context, _ map[string]string, out io.Writer) error {
				synced, err := permissions.SyncCatalog(ctx)
				if err != nil {
					return err
				}
				fmt.Fprintf(out, "Synced %d permission(s).\n", synced)
				return nil
			},
		},
		{
			Name: "settings:sync", Label: "Sync settings", Group: "Sync",
			Description: "Add settings groups and global settings missing from the database (existing values are kept).",
			Run: func(ctx context.Context, _ map[string]string, out io.Writer) error {
				created, err := p.Settings.Sync(ctx)
				if err != nil {
					return err
				}
				fmt.Fprintf(out, "Created %d setting(s).\n", created)
				return nil
			},
		},
		{
			Name: "feature-flags:sync", Label: "Sync feature flags", Group: "Sync",
			Description: "Insert missing global feature flags with their catalog defaults.",
			Run: func(ctx context.Context, _ map[string]string, out io.Writer) error {
				created, err := p.FeatureFlags.Sync(ctx)
				if err != nil {
					return err
				}
				fmt.Fprintf(out, "Created %d feature flag(s).\n", created)
				return nil
			},
		},
		{
			Name: "cache:clear", Label: "Clear cache", Group: "Maintenance", Confirm: true,
			Description: "Empty the application cache (permissions, settings, flags and tenancy lookups are rebuilt on demand).",
			Run: func(ctx context.Context, _ map[string]string, out io.Writer) error {
				if err := p.Cache.Clear(ctx); err != nil {
					return err
				}
				fmt.Fprintf(out, "Cache store %q cleared.\n", cfg.Cache.Driver)
				return nil
			},
		},
		{
			Name: "schedule:list", Label: "Scheduled tasks", Group: "Info",
			Description: "Every task the scheduler runs, with its cron expression.",
			Run: func(_ context.Context, _ map[string]string, out io.Writer) error {
				w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
				fmt.Fprintln(w, "TASK\tSCHEDULE\tDESCRIPTION")
				for _, task := range p.ScheduleRegistry.Tasks() {
					fmt.Fprintf(w, "%s\t%s\t%s\n", task.ID, task.Spec, task.Description)
				}
				return w.Flush()
			},
		},
		{
			Name: "queue:stats", Label: "Queue stats", Group: "Info",
			Description: "Jobs per queue: total, active and failed.",
			Run: func(ctx context.Context, _ map[string]string, out io.Writer) error {
				stats, err := queue.Stats(ctx, p.DB.Primary().Pool)
				if err != nil {
					return err
				}
				w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
				fmt.Fprintln(w, "QUEUE\tTOTAL\tACTIVE\tFAILED")
				if len(stats) == 0 {
					fmt.Fprintln(w, "(none)\t0\t0\t0")
				}
				for _, stat := range stats {
					fmt.Fprintf(w, "%s\t%d\t%d\t%d\n", stat.Queue, stat.Total, stat.Active, stat.Failed)
				}
				return w.Flush()
			},
		},
		{
			Name: "billing:expire-subscriptions", Label: "Expire subscriptions", Group: "Billing", Confirm: true,
			Description: "Mark subscriptions past their end date as expired (the scheduler does this daily).",
			Run: func(ctx context.Context, _ map[string]string, out io.Writer) error {
				expired, err := p.BillingSubs.ExpireOverdue(ctx)
				if err != nil {
					return err
				}
				fmt.Fprintf(out, "Expired %d subscription(s).\n", expired)
				return nil
			},
		},
		{
			Name: "backup:run", Label: "Back up the database", Group: "Backups", Confirm: true, Background: true,
			Description: "Create a database backup archive and copy it to the configured destinations.",
			Options: []dev.CommandOption{
				{Name: "with_files", Label: "Include files", Type: "bool", Default: "false",
					Description: "Also archive BACKUP_FILES_PATHS."},
			},
			Timeout: cfg.Backup.Timeout,
			Run: func(ctx context.Context, options map[string]string, out io.Writer) error {
				withFiles := options["with_files"] == "true"
				run, err := p.Backup.RunNow(ctx, backup.TriggerDashboard, 0, backup.RunOptions{IncludeFiles: &withFiles})
				if run != nil {
					fmt.Fprintf(out, "Backup #%d %s\nFile: %s\nSize: %d bytes\n", run.ID, run.Status, run.Filename, run.SizeBytes)
					for _, destination := range run.Destinations {
						fmt.Fprintf(out, "  %s: %s\n", destination.Destination, destination.Status)
					}
				}
				return err
			},
		},
		{
			Name: "backup:clean", Label: "Clean old backups", Group: "Backups", Confirm: true,
			Description: "Apply the backup retention policy and delete expired archives.",
			Options: []dev.CommandOption{
				{Name: "dry_run", Label: "Dry run", Type: "bool", Default: "true", Description: "Only list what would be deleted."},
			},
			Run: func(ctx context.Context, options map[string]string, out io.Writer) error {
				result, err := p.Backup.Clean(ctx, options["dry_run"] == "true")
				if err != nil {
					return err
				}
				if result.DryRun {
					fmt.Fprintf(out, "Dry run: %d archive(s) would be deleted %v. Nothing was removed.\n", result.Candidates, result.IDs)
					return nil
				}
				fmt.Fprintf(out, "Deleted %d of %d archive(s); %d failed.\n", result.Deleted, result.Candidates, result.Failed)
				return nil
			},
		},
		{
			Name: "activity:prune", Label: "Prune activity log", Group: "Maintenance", Confirm: true,
			Description: "Delete activity-log entries older than the given number of days.",
			Options: []dev.CommandOption{
				{Name: "days", Label: "Older than (days)", Type: "number", Default: strconv.Itoa(max(cfg.Activity.RetentionDays, 30))},
			},
			Run: func(ctx context.Context, options map[string]string, out io.Writer) error {
				days, _ := strconv.Atoi(options["days"])
				if days < 1 {
					return fmt.Errorf("days must be at least 1")
				}
				deleted, err := activity.NewService(p.Tx.Queries()).Purge(ctx, days)
				if err != nil {
					return err
				}
				fmt.Fprintf(out, "Deleted %d activity entr(ies) older than %d day(s).\n", deleted, days)
				return nil
			},
		},
		{
			Name: "notifications:purge-expired", Label: "Purge expired notifications", Group: "Maintenance", Confirm: true,
			Description: "Delete notifications that expired more than 7 days ago.",
			Run: func(ctx context.Context, _ map[string]string, out io.Writer) error {
				purged, err := p.Tx.Queries().PurgeExpiredNotifications(ctx)
				if err != nil {
					return err
				}
				fmt.Fprintf(out, "Purged %d notification(s).\n", purged)
				return nil
			},
		},
	}
}
