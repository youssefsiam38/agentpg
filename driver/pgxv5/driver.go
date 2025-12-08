// Package pgxv5 provides a pgx/v5 driver implementation for AgentPG.
//
// This is the primary/recommended driver for AgentPG, offering the best
// performance and feature support including native batch operations and
// nested transactions via savepoints.
//
// Usage:
//
//	pool, _ := pgxpool.New(ctx, databaseURL)
//	drv := pgxv5.New(pool)
//	agent, _ := agentpg.New(drv, agentpg.Config{...})
package pgxv5

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/youssefsiam38/agentpg/driver"
	"github.com/youssefsiam38/agentpg/storage"
)

// Driver implements driver.Driver for pgx/v5.
type Driver struct {
	pool *pgxpool.Pool
}

// New creates a new pgx/v5 driver with the given connection pool.
func New(pool *pgxpool.Pool) *Driver {
	return &Driver{pool: pool}
}

// GetExecutor returns an executor for non-transactional operations.
func (d *Driver) GetExecutor() driver.Executor {
	return &Executor{pool: d.pool}
}

// UnwrapExecutor converts a pgx.Tx to an ExecutorTx.
func (d *Driver) UnwrapExecutor(tx pgx.Tx) driver.ExecutorTx {
	return &ExecutorTx{tx: tx}
}

// UnwrapTx extracts the pgx.Tx from an ExecutorTx.
func (d *Driver) UnwrapTx(execTx driver.ExecutorTx) pgx.Tx {
	return execTx.(*ExecutorTx).tx
}

// Begin starts a new transaction and returns an ExecutorTx.
func (d *Driver) Begin(ctx context.Context) (driver.ExecutorTx, error) {
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &ExecutorTx{tx: tx}, nil
}

// PoolIsSet returns true if the driver has a database pool configured.
func (d *Driver) PoolIsSet() bool {
	return d.pool != nil
}

// GetStore returns a Store implementation using this driver.
func (d *Driver) GetStore() storage.Store {
	return NewStore(d)
}

// Pool returns the underlying pgxpool.Pool for advanced usage.
// Use this when you need direct access to the pool for custom operations.
func (d *Driver) Pool() *pgxpool.Pool {
	return d.pool
}

// SupportsListener returns true as pgx supports dedicated LISTEN connections.
func (d *Driver) SupportsListener() bool {
	return true
}

// SupportsNotify returns true as pgx supports NOTIFY.
func (d *Driver) SupportsNotify() bool {
	return true
}

// GetListener creates a new Listener for receiving PostgreSQL notifications.
// The listener uses a dedicated connection from the pool.
// The returned Listener must be closed when no longer needed.
func (d *Driver) GetListener(ctx context.Context) (driver.Listener, error) {
	conn, err := d.pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	return &Listener{conn: conn}, nil
}

// GetNotifier returns a Notifier for sending PostgreSQL notifications.
func (d *Driver) GetNotifier() driver.Notifier {
	return &Notifier{pool: d.pool}
}

// Executor wraps pgxpool.Pool for non-transactional operations.
type Executor struct {
	pool *pgxpool.Pool
}

// Begin starts a new transaction.
func (e *Executor) Begin(ctx context.Context) (driver.ExecutorTx, error) {
	tx, err := e.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &ExecutorTx{tx: tx}, nil
}

// Exec executes a query that doesn't return rows.
func (e *Executor) Exec(ctx context.Context, sql string, args ...any) (int64, error) {
	result, err := e.pool.Exec(ctx, sql, args...)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

// Query executes a query that returns rows.
func (e *Executor) Query(ctx context.Context, sql string, args ...any) (driver.Rows, error) {
	rows, err := e.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return &rowsWrapper{rows}, nil
}

// QueryRow executes a query that returns at most one row.
func (e *Executor) QueryRow(ctx context.Context, sql string, args ...any) driver.Row {
	return e.pool.QueryRow(ctx, sql, args...)
}

// SendBatch sends multiple queries as a batch.
// This is a pgx-specific optimization that executes all queries in a single round trip.
func (e *Executor) SendBatch(ctx context.Context, items []driver.BatchItem) (affected []int64, err error) {
	batch := &pgx.Batch{}
	for _, item := range items {
		batch.Queue(item.Query, item.Args...)
	}

	results := e.pool.SendBatch(ctx, batch)
	defer func() {
		if closeErr := results.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}()

	affected = make([]int64, len(items))
	for i := range items {
		result, execErr := results.Exec()
		if execErr != nil {
			return nil, execErr
		}
		affected[i] = result.RowsAffected()
	}
	return affected, nil
}

// ExecutorTx wraps pgx.Tx for transactional operations.
type ExecutorTx struct {
	tx pgx.Tx
}

// Begin starts a nested transaction (savepoint).
// pgx automatically handles savepoints for nested Begin calls.
func (e *ExecutorTx) Begin(ctx context.Context) (driver.ExecutorTx, error) {
	tx, err := e.tx.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &ExecutorTx{tx: tx}, nil
}

// Exec executes a query that doesn't return rows within the transaction.
func (e *ExecutorTx) Exec(ctx context.Context, sql string, args ...any) (int64, error) {
	result, err := e.tx.Exec(ctx, sql, args...)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

// Query executes a query that returns rows within the transaction.
func (e *ExecutorTx) Query(ctx context.Context, sql string, args ...any) (driver.Rows, error) {
	rows, err := e.tx.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return &rowsWrapper{rows}, nil
}

// QueryRow executes a query that returns at most one row within the transaction.
func (e *ExecutorTx) QueryRow(ctx context.Context, sql string, args ...any) driver.Row {
	return e.tx.QueryRow(ctx, sql, args...)
}

// Commit commits the transaction.
func (e *ExecutorTx) Commit(ctx context.Context) error {
	return e.tx.Commit(ctx)
}

// Rollback rolls back the transaction.
func (e *ExecutorTx) Rollback(ctx context.Context) error {
	return e.tx.Rollback(ctx)
}

// SendBatch sends multiple queries as a batch within the transaction.
func (e *ExecutorTx) SendBatch(ctx context.Context, items []driver.BatchItem) (affected []int64, err error) {
	batch := &pgx.Batch{}
	for _, item := range items {
		batch.Queue(item.Query, item.Args...)
	}

	results := e.tx.SendBatch(ctx, batch)
	defer func() {
		if closeErr := results.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}()

	affected = make([]int64, len(items))
	for i := range items {
		result, execErr := results.Exec()
		if execErr != nil {
			return nil, execErr
		}
		affected[i] = result.RowsAffected()
	}
	return affected, nil
}

// Tx returns the underlying pgx.Tx for advanced usage.
func (e *ExecutorTx) Tx() pgx.Tx {
	return e.tx
}

// rowsWrapper adapts pgx.Rows to driver.Rows.
type rowsWrapper struct {
	pgx.Rows
}

// Close closes the Rows.
func (r *rowsWrapper) Close() {
	r.Rows.Close()
}

// Err returns any error encountered during iteration.
func (r *rowsWrapper) Err() error {
	return r.Rows.Err()
}

// Next prepares the next row for reading.
func (r *rowsWrapper) Next() bool {
	return r.Rows.Next()
}

// Scan reads the current row into dest.
func (r *rowsWrapper) Scan(dest ...any) error {
	return r.Rows.Scan(dest...)
}
