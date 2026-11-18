package database

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	dbgen "github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/dbgen"
)

// TxManager exposes sqlc queries bound to a pool or to a transaction. Services
// own transaction boundaries; repositories receive the queries they need.
type TxManager struct {
	pool    *pgxpool.Pool
	queries *dbgen.Queries
}

// NewTxManager creates a transaction manager for a connection.
func NewTxManager(conn *Conn) *TxManager {
	return &TxManager{pool: conn.Pool, queries: dbgen.New(conn.Pool)}
}

// Pool returns the underlying pool.
func (m *TxManager) Pool() *pgxpool.Pool {
	return m.pool
}

// Queries returns queries bound to the pool (non-transactional access).
func (m *TxManager) Queries() *dbgen.Queries {
	return m.queries
}

// WithTx runs fn inside a transaction. The queries passed to fn share the
// transaction handle; the transaction is rolled back on error or panic and
// committed otherwise.
func (m *TxManager) WithTx(ctx context.Context, fn func(q *dbgen.Queries) error) error {
	tx, err := m.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	// Roll back with a context detached from cancellation so a timed-out or
	// cancelled request still releases the transaction and its locks.
	defer func() {
		rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(rollbackCtx)
	}()

	if err := fn(m.queries.WithTx(tx)); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

// WithTenantLock runs fn in a transaction that first takes a per-tenant
// advisory lock on resource (e.g. "users.max"). Use it for "count, check the
// plan limit, insert" so concurrent creates cannot exceed a quota.
func (m *TxManager) WithTenantLock(ctx context.Context, resource string, tenantID int64, fn func(q *dbgen.Queries) error) error {
	return m.WithTx(ctx, func(q *dbgen.Queries) error {
		if err := q.LockTenantResource(ctx, dbgen.LockTenantResourceParams{Resource: resource, TenantID: tenantID}); err != nil {
			return MapError("tenant_lock", err)
		}
		return fn(q)
	})
}
