package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/config"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/database"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/tenancy"
)

// riverArgsKind is the single River job kind; the logical application kind
// travels inside the envelope so new job types never need River schema churn.
const riverArgsKind = "ims.job"

type jobArgs struct {
	JobKind  string          `json:"kind"`
	Payload  json.RawMessage `json:"payload"`
	TenantID int64           `json:"tenant_id,omitempty"`
	BranchID int64           `json:"branch_id,omitempty"`
}

// Kind implements river.JobArgs.
func (jobArgs) Kind() string { return riverArgsKind }

type dispatcherWorker struct {
	river.WorkerDefaults[jobArgs]
	registry *Registry
	cfg      config.Queue
	onDone   chan<- struct{}
}

// Work implements river.Worker: it resolves the typed definition, validates the
// payload, restores the tenant scope from the envelope and calls the handler.
func (w *dispatcherWorker) Work(ctx context.Context, job *river.Job[jobArgs]) error {
	def, ok := w.registry.Definition(job.Args.JobKind)
	if !ok {
		return river.JobCancel(fmt.Errorf("unknown job kind %q", job.Args.JobKind))
	}
	if def.Validate != nil {
		if err := def.Validate(job.Args.Payload); err != nil {
			return river.JobCancel(err)
		}
	}
	if job.Args.TenantID > 0 {
		ctx = tenancy.WithScope(ctx, tenancy.Scope{
			TenantID: job.Args.TenantID,
			BranchID: job.Args.BranchID,
		})
	}
	err := def.Handle(ctx, job.Args.Payload)
	if w.onDone != nil {
		select {
		case w.onDone <- struct{}{}:
		default:
		}
	}
	return err
}

// Timeout implements river.Worker using the per-kind timeout, falling back to
// the job's queue channel timeout.
func (w *dispatcherWorker) Timeout(job *river.Job[jobArgs]) time.Duration {
	def, ok := w.registry.Definition(job.Args.JobKind)
	if !ok {
		return 0
	}
	if def.Timeout > 0 {
		return def.Timeout
	}
	return ChannelConfig(w.cfg, def.Queue).Timeout
}

// RiverEngine runs typed jobs on River with transactional enqueue.
type RiverEngine struct {
	registry *Registry
	cfg      config.Queue
	client   *river.Client[pgx.Tx]
	done     chan struct{}
}

// RiverOptions overrides worker configuration for a specific process
// (e.g. `queue:work --queues=critical --max-workers=5`).
type RiverOptions struct {
	Queues      []Name
	Concurrency int
}

// NewRiverEngine wires River over the primary pool.
func NewRiverEngine(pool *pgxpool.Pool, registry *Registry, cfg config.Queue, logger *slog.Logger, opts *RiverOptions) (*RiverEngine, error) {
	queues := []Name{QueueCritical, QueueDefault, QueueHeavy}
	concurrency := cfg.WorkerConcurrency
	if opts != nil {
		if len(opts.Queues) > 0 {
			queues = opts.Queues
		}
		if opts.Concurrency > 0 {
			concurrency = opts.Concurrency
		}
	}
	queueConfig := make(map[string]river.QueueConfig, len(queues))
	for _, name := range queues {
		queueConfig[string(name)] = river.QueueConfig{MaxWorkers: concurrency}
	}

	engine := &RiverEngine{
		registry: registry,
		cfg:      cfg,
		done:     make(chan struct{}, 1),
	}

	workers := river.NewWorkers()
	river.AddWorker(workers, &dispatcherWorker{registry: registry, cfg: cfg, onDone: engine.done})

	client, err := river.NewClient(riverpgxv5.New(pool), &river.Config{
		Queues:  queueConfig,
		Workers: workers,
		Logger:  logger,
	})
	if err != nil {
		return nil, fmt.Errorf("queue: create river client: %w", err)
	}
	engine.client = client
	return engine, nil
}

// Kind implements Engine.
func (e *RiverEngine) Kind() string { return "river" }

// Enqueue implements Engine.
func (e *RiverEngine) Enqueue(ctx context.Context, kind string, payload any, opts EnqueueOptions) error {
	args, insertOpts, err := e.prepare(ctx, kind, payload, opts)
	if err != nil {
		return err
	}
	if _, err := e.client.Insert(ctx, args, insertOpts); err != nil {
		return database.MapError("queue.enqueue", err)
	}
	return nil
}

// EnqueueTx implements Engine with River's transactional insert: the job is
// committed with the caller's transaction or not at all.
func (e *RiverEngine) EnqueueTx(ctx context.Context, tx pgx.Tx, kind string, payload any, opts EnqueueOptions) error {
	args, insertOpts, err := e.prepare(ctx, kind, payload, opts)
	if err != nil {
		return err
	}
	if _, err := e.client.InsertTx(ctx, tx, args, insertOpts); err != nil {
		return database.MapError("queue.enqueue_tx", err)
	}
	return nil
}

// Start begins worker processing and blocks until ctx is cancelled. River's
// client.Start is non-blocking (it returns once its goroutines are up), so we
// wait on the context here to honour the Engine contract; the caller then stops
// the engine to drain in-flight jobs.
func (e *RiverEngine) Start(ctx context.Context) error {
	if err := e.client.Start(ctx); err != nil {
		return err
	}
	<-ctx.Done()
	return nil
}

// Stop drains in-flight jobs and stops the workers.
func (e *RiverEngine) Stop(ctx context.Context) error {
	return e.client.Stop(ctx)
}

// Once starts the workers, waits for the first completed job (bounded by
// timeout) and stops. Used by `queue:work --once` and tests.
func (e *RiverEngine) Once(ctx context.Context, timeout time.Duration) error {
	if err := e.client.Start(ctx); err != nil {
		return err
	}
	select {
	case <-e.done:
	case <-time.After(timeout):
	case <-ctx.Done():
	}
	stopCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	return e.client.Stop(stopCtx)
}

func (e *RiverEngine) prepare(ctx context.Context, kind string, payload any, opts EnqueueOptions) (jobArgs, *river.InsertOpts, error) {
	data, def, err := encodePayload(e.registry, kind, payload)
	if err != nil {
		return jobArgs{}, nil, err
	}

	args := jobArgs{JobKind: kind, Payload: data}
	if scope, ok := tenancy.FromContext(ctx); ok {
		args.TenantID = scope.EffectiveTenantID()
		args.BranchID = scope.EffectiveBranchID()
	}

	channel := ChannelConfig(e.cfg, def.Queue)
	attempts := channel.Tries
	if def.MaxRetries > 0 {
		attempts = def.MaxRetries + 1
	}
	insertOpts := &river.InsertOpts{
		Queue:       string(def.Queue),
		MaxAttempts: attempts,
	}
	if opts.Queue != nil {
		insertOpts.Queue = string(*opts.Queue)
	}
	if opts.MaxRetries != nil && *opts.MaxRetries > 0 {
		insertOpts.MaxAttempts = *opts.MaxRetries + 1
	}
	if opts.RunAt != nil {
		insertOpts.ScheduledAt = *opts.RunAt
	}
	return args, insertOpts, nil
}
