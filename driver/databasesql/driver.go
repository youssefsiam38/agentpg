// Package databasesql provides a database/sql driver implementation for AgentPG.
//
// This driver enables AgentPG to work with any database/sql compatible driver
// (lib/pq, pgx/stdlib, etc.). It supports nested transactions via savepoints.
//
// Usage:
//
//	import (
//	    "database/sql"
//	    _ "github.com/lib/pq"
//	    "github.com/youssefsiam38/agentpg/driver/databasesql"
//	)
//
//	db, _ := sql.Open("postgres", databaseURL)
//	drv := databasesql.New(db)
//	agent, _ := agentpg.New(drv, agentpg.Config{...})
package databasesql

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/youssefsiam38/agentpg/driver"
	"github.com/youssefsiam38/agentpg/storage"
)

// Driver implements driver.Driver for database/sql.
type Driver struct {
	db *sql.DB
}

// New creates a new database/sql driver with the given database connection.
func New(db *sql.DB) *Driver {
	return &Driver{db: db}
}

// GetExecutor returns an executor for non-transactional operations.
func (d *Driver) GetExecutor() driver.Executor {
	return &Executor{db: d.db}
}

// UnwrapExecutor converts a *sql.Tx to an ExecutorTx.
func (d *Driver) UnwrapExecutor(tx *sql.Tx) driver.ExecutorTx {
	return &ExecutorTx{tx: tx, savepointNum: 0, parent: nil}
}

// UnwrapTx extracts the *sql.Tx from an ExecutorTx.
func (d *Driver) UnwrapTx(execTx driver.ExecutorTx) *sql.Tx {
	return execTx.(*ExecutorTx).tx
}

// Begin starts a new transaction and returns an ExecutorTx.
func (d *Driver) Begin(ctx context.Context) (driver.ExecutorTx, error) {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	return &ExecutorTx{tx: tx, savepointNum: 0, parent: nil}, nil
}

// PoolIsSet returns true if the driver has a database connection configured.
func (d *Driver) PoolIsSet() bool {
	return d.db != nil
}

// GetStore returns a Store implementation using this driver.
func (d *Driver) GetStore() storage.Store {
	return NewStore(d)
}

// DB returns the underlying *sql.DB for advanced usage.
func (d *Driver) DB() *sql.DB {
	return d.db
}

// Executor wraps *sql.DB for non-transactional operations.
type Executor struct {
	db *sql.DB
}

// Begin starts a new transaction.
func (e *Executor) Begin(ctx context.Context) (driver.ExecutorTx, error) {
	tx, err := e.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	return &ExecutorTx{tx: tx, savepointNum: 0, parent: nil}, nil
}

// Exec executes a query that doesn't return rows.
func (e *Executor) Exec(ctx context.Context, query string, args ...any) (int64, error) {
	result, err := e.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	return n, nil
}

// Query executes a query that returns rows.
func (e *Executor) Query(ctx context.Context, query string, args ...any) (driver.Rows, error) {
	rows, err := e.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return &rowsWrapper{rows}, nil
}

// QueryRow executes a query that returns at most one row.
func (e *Executor) QueryRow(ctx context.Context, query string, args ...any) driver.Row {
	return e.db.QueryRowContext(ctx, query, args...)
}

// SendBatch executes multiple queries sequentially.
// database/sql doesn't support native batching, so queries are executed one at a time.
func (e *Executor) SendBatch(ctx context.Context, items []driver.BatchItem) ([]int64, error) {
	affected := make([]int64, len(items))
	for i, item := range items {
		result, err := e.db.ExecContext(ctx, item.Query, item.Args...)
		if err != nil {
			return nil, err
		}
		n, _ := result.RowsAffected()
		affected[i] = n
	}
	return affected, nil
}

// ExecutorTx wraps *sql.Tx for transactional operations.
// Supports nested transactions via savepoints.
type ExecutorTx struct {
	tx           *sql.Tx
	savepointNum int
	parent       *ExecutorTx
}

// Begin starts a nested transaction via savepoint.
// database/sql doesn't support nested transactions natively, so we use savepoints.
func (e *ExecutorTx) Begin(ctx context.Context) (driver.ExecutorTx, error) {
	nextNum := e.savepointNum + 1
	savepointName := fmt.Sprintf("agentpg_sp_%d", nextNum)
	_, err := e.tx.ExecContext(ctx, fmt.Sprintf("SAVEPOINT %s", savepointName))
	if err != nil {
		return nil, fmt.Errorf("failed to create savepoint: %w", err)
	}
	return &ExecutorTx{
		tx:           e.tx,
		savepointNum: nextNum,
		parent:       e,
	}, nil
}

// Exec executes a query that doesn't return rows within the transaction.
func (e *ExecutorTx) Exec(ctx context.Context, query string, args ...any) (int64, error) {
	result, err := e.tx.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	return n, nil
}

// Query executes a query that returns rows within the transaction.
func (e *ExecutorTx) Query(ctx context.Context, query string, args ...any) (driver.Rows, error) {
	rows, err := e.tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return &rowsWrapper{rows}, nil
}

// QueryRow executes a query that returns at most one row within the transaction.
func (e *ExecutorTx) QueryRow(ctx context.Context, query string, args ...any) driver.Row {
	return e.tx.QueryRowContext(ctx, query, args...)
}

// Commit commits the transaction or releases the savepoint.
func (e *ExecutorTx) Commit(ctx context.Context) error {
	if e.parent != nil {
		// Nested transaction: release savepoint
		savepointName := fmt.Sprintf("agentpg_sp_%d", e.savepointNum)
		_, err := e.tx.ExecContext(ctx, fmt.Sprintf("RELEASE SAVEPOINT %s", savepointName))
		if err != nil {
			return fmt.Errorf("failed to release savepoint: %w", err)
		}
		return nil
	}
	// Root transaction: commit
	return e.tx.Commit()
}

// Rollback rolls back the transaction or rolls back to the savepoint.
func (e *ExecutorTx) Rollback(ctx context.Context) error {
	if e.parent != nil {
		// Nested transaction: rollback to savepoint
		savepointName := fmt.Sprintf("agentpg_sp_%d", e.savepointNum)
		_, err := e.tx.ExecContext(ctx, fmt.Sprintf("ROLLBACK TO SAVEPOINT %s", savepointName))
		if err != nil {
			return fmt.Errorf("failed to rollback to savepoint: %w", err)
		}
		return nil
	}
	// Root transaction: rollback
	return e.tx.Rollback()
}

// SendBatch executes multiple queries sequentially within the transaction.
func (e *ExecutorTx) SendBatch(ctx context.Context, items []driver.BatchItem) ([]int64, error) {
	affected := make([]int64, len(items))
	for i, item := range items {
		result, err := e.tx.ExecContext(ctx, item.Query, item.Args...)
		if err != nil {
			return nil, err
		}
		n, _ := result.RowsAffected()
		affected[i] = n
	}
	return affected, nil
}

// Tx returns the underlying *sql.Tx for advanced usage.
func (e *ExecutorTx) Tx() *sql.Tx {
	return e.tx
}

// rowsWrapper adapts *sql.Rows to driver.Rows.
type rowsWrapper struct {
	*sql.Rows
}

// Close closes the Rows.
func (r *rowsWrapper) Close() {
	_ = r.Rows.Close()
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
