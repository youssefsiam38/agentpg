// Package driver provides database driver abstractions for AgentPG.
//
// This package defines the interfaces that database drivers must implement
// to work with AgentPG. It enables support for multiple database backends
// (pgx/v5, database/sql) through a generic driver pattern.
package driver

import (
	"context"

	"github.com/youssefsiam38/agentpg/storage"
)

// Driver provides database operations for AgentPG.
// TTx is the native transaction type (e.g., pgx.Tx for pgx/v5, *sql.Tx for database/sql).
//
// Implementations should be created using the driver-specific New() functions:
//   - github.com/youssefsiam38/agentpg/driver/pgxv5.New(pool)
//   - github.com/youssefsiam38/agentpg/driver/databasesql.New(db)
type Driver[TTx any] interface {
	// GetExecutor returns an executor for non-transactional operations.
	// The returned Executor uses the underlying connection pool.
	GetExecutor() Executor

	// UnwrapExecutor converts a native transaction to an ExecutorTx.
	// This allows AgentPG to work with user-provided transactions.
	UnwrapExecutor(tx TTx) ExecutorTx

	// UnwrapTx extracts the native transaction from an ExecutorTx.
	// Used when the native transaction type is needed for user operations.
	UnwrapTx(execTx ExecutorTx) TTx

	// Begin starts a new transaction and returns an ExecutorTx.
	Begin(ctx context.Context) (ExecutorTx, error)

	// PoolIsSet returns true if the driver has a database pool configured.
	// This is used to validate the driver during agent initialization.
	PoolIsSet() bool

	// GetStore returns a Store implementation using this driver.
	// The store handles all persistence operations for sessions, messages, etc.
	GetStore() storage.Store
}

// Beginner is an interface for types that can begin transactions.
// This is used internally to handle driver abstraction in non-generic contexts.
type Beginner interface {
	Begin(ctx context.Context) (ExecutorTx, error)
}
