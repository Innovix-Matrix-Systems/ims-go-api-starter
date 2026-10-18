package scheduler

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/cache"
)

// HeartbeatKey stores the scheduler's last execution timestamp in the cache.
const HeartbeatKey = "schedule:heartbeat"

// Runner executes the registry on a cron schedule with overlap protection,
// per-task timeouts and structured logging.
type Runner struct {
	registry *Registry
	logger   *slog.Logger
	location *time.Location
	store    cache.Store

	mu      sync.Mutex
	running map[string]bool
}

// NewRunner creates a scheduler runner for the given timezone.
func NewRunner(registry *Registry, logger *slog.Logger, store cache.Store, timezone string) (*Runner, error) {
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return nil, err
	}
	return &Runner{
		registry: registry,
		logger:   logger,
		location: location,
		store:    store,
		running:  map[string]bool{},
	}, nil
}

// Start runs the scheduler until the context is cancelled, then waits for
// in-flight tasks to finish.
func (r *Runner) Start(ctx context.Context) error {
	runner := cron.New(cron.WithLocation(r.location))
	for _, task := range r.registry.Tasks() {
		task := task
		if _, err := runner.AddFunc(task.Spec, func() { r.Execute(ctx, task) }); err != nil {
			return err
		}
	}

	r.logger.Info("scheduler started",
		"tasks", len(r.registry.Tasks()),
		"timezone", r.location.String(),
	)
	runner.Start()
	// Record liveness immediately so health is green before the first tick
	// (the every-minute heartbeat task would otherwise leave a ~60s gap).
	r.Heartbeat(ctx)

	<-ctx.Done()
	stopped := runner.Stop()
	select {
	case <-stopped.Done():
	case <-time.After(30 * time.Second):
		r.logger.Warn("scheduler shutdown timed out waiting for running tasks")
	}
	r.logger.Info("scheduler stopped")
	return nil
}

// RunNow executes a task immediately (used by `schedule:run`).
func (r *Runner) RunNow(ctx context.Context, id string) error {
	task, ok := r.registry.Get(id)
	if !ok {
		return ErrUnknownTask(id)
	}
	return r.execute(ctx, task)
}

// Execute runs a task through the overlap policy (exported for tests).
func (r *Runner) Execute(ctx context.Context, task Task) {
	_ = r.execute(ctx, task)
}

func (r *Runner) execute(ctx context.Context, task Task) error {
	if task.Overlap == OverlapSkip {
		r.mu.Lock()
		if r.running[task.ID] {
			r.mu.Unlock()
			r.logger.Warn("scheduled task skipped (still running)", "task", task.ID)
			return nil
		}
		r.running[task.ID] = true
		r.mu.Unlock()
		defer func() {
			r.mu.Lock()
			delete(r.running, task.ID)
			r.mu.Unlock()
		}()
	}

	runCtx := ctx
	if task.Timeout > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, task.Timeout)
		defer cancel()
	}

	start := time.Now()
	r.logger.Info("scheduled task started", "task", task.ID)
	err := task.Run(runCtx)
	durationMS := float64(time.Since(start).Microseconds()) / 1000.0

	if err != nil {
		r.logger.Error("scheduled task failed", "task", task.ID, "duration_ms", durationMS, "error", err)
		return err
	}
	r.logger.Info("scheduled task finished", "task", task.ID, "duration_ms", durationMS)
	return nil
}

// Heartbeat records scheduler liveness for health checks.
func (r *Runner) Heartbeat(ctx context.Context) {
	_ = r.store.Set(ctx, HeartbeatKey, time.Now().Unix(), 0)
}
