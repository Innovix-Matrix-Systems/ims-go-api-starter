// Package queue provides the typed job registry and pluggable execution
// engines: sync (inline, for tests/CLI) and River (durable, transactional
// enqueue on Postgres).
package queue

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/config"
)

// Name identifies a logical queue.
type Name string

// Logical queues with distinct retry/timeout policies.
const (
	QueueCritical Name = "critical"
	QueueDefault  Name = "default"
	QueueHeavy    Name = "heavy"
)

// ChannelConfig returns the configured retry/timeout policy for a queue.
func ChannelConfig(cfg config.Queue, name Name) config.Channel {
	switch name {
	case QueueCritical:
		return cfg.Critical
	case QueueHeavy:
		return cfg.Heavy
	default:
		return cfg.Default
	}
}

// EnqueueOptions overrides per-job defaults.
type EnqueueOptions struct {
	Queue      *Name
	MaxRetries *int
	RunAt      *time.Time
}

// Definition describes one typed job kind.
type Definition struct {
	Kind        string
	Description string
	Queue       Name
	MaxRetries  int
	Timeout     time.Duration
	Validate    func(payload []byte) error
	Handle      func(ctx context.Context, payload []byte) error
}

// Engine submits jobs for execution.
type Engine interface {
	Enqueue(ctx context.Context, kind string, payload any, opts EnqueueOptions) error
	EnqueueTx(ctx context.Context, tx pgx.Tx, kind string, payload any, opts EnqueueOptions) error
	Kind() string
}
