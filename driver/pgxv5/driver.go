// Package pgxv5 provides a pgx/v5 driver implementation for AgentPG.
package pgxv5

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/youssefsiam38/agentpg/driver"
)

// Driver implements driver.Driver using pgx/v5.
type Driver struct {
	pool     *pgxpool.Pool
	store    *Store
	listener *Listener
}

// New creates a new pgx/v5 driver using the provided connection pool.
func New(pool *pgxpool.Pool) *Driver {
	store := &Store{pool: pool}
	return &Driver{
		pool:  pool,
		store: store,
	}
}

// Store returns the store interface for database operations.
func (d *Driver) Store() driver.Store[pgx.Tx] {
	return d.store
}

// Listener returns the listener for LISTEN/NOTIFY.
// The listener is created lazily on first call.
func (d *Driver) Listener() driver.Listener {
	if d.listener == nil {
		d.listener = NewListener(d.pool)
	}
	return d.listener
}

// BeginTx starts a new transaction.
func (d *Driver) BeginTx(ctx context.Context) (pgx.Tx, error) {
	return d.pool.Begin(ctx)
}

// CommitTx commits a transaction.
func (d *Driver) CommitTx(ctx context.Context, tx pgx.Tx) error {
	return tx.Commit(ctx)
}

// RollbackTx rolls back a transaction.
func (d *Driver) RollbackTx(ctx context.Context, tx pgx.Tx) error {
	return tx.Rollback(ctx)
}

// Close closes the driver and releases resources.
func (d *Driver) Close() error {
	if d.listener != nil {
		if err := d.listener.Close(); err != nil {
			return err
		}
	}
	// Note: We don't close the pool as it was provided externally
	return nil
}

// Pool returns the underlying connection pool.
// This can be used for advanced operations that require direct pool access.
func (d *Driver) Pool() *pgxpool.Pool {
	return d.pool
}

// Compile-time check that Driver implements driver.Driver
var _ driver.Driver[pgx.Tx] = (*Driver)(nil)
