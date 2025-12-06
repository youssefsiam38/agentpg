package driver

import "context"

// Row represents a single database row.
// This interface is compatible with both pgx.Row and *sql.Row.
type Row interface {
	// Scan copies the columns from the matched row into the values pointed at by dest.
	Scan(dest ...any) error
}

// Rows represents a result set from a query.
// This interface is compatible with both pgx.Rows and *sql.Rows.
type Rows interface {
	// Close closes the Rows, preventing further enumeration.
	Close()

	// Err returns the error, if any, that was encountered during iteration.
	Err() error

	// Next prepares the next result row for reading with the Scan method.
	// Returns true if there is another row, false otherwise.
	Next() bool

	// Scan copies the columns in the current row into the values pointed at by dest.
	Scan(dest ...any) error
}

// Executor provides database operations.
// It can represent either a connection pool or a transaction.
type Executor interface {
	// Begin starts a new transaction or subtransaction (savepoint).
	// For database/sql, nested calls create savepoints.
	Begin(ctx context.Context) (ExecutorTx, error)

	// Exec executes a query that doesn't return rows.
	// Returns the number of rows affected.
	Exec(ctx context.Context, sql string, args ...any) (int64, error)

	// Query executes a query that returns rows.
	Query(ctx context.Context, sql string, args ...any) (Rows, error)

	// QueryRow executes a query that returns at most one row.
	QueryRow(ctx context.Context, sql string, args ...any) Row
}

// ExecutorTx is an Executor that supports commit/rollback.
// It represents an active database transaction.
type ExecutorTx interface {
	Executor

	// Commit commits the transaction.
	// For savepoint-based nested transactions, this releases the savepoint.
	Commit(ctx context.Context) error

	// Rollback rolls back the transaction.
	// For savepoint-based nested transactions, this rolls back to the savepoint.
	Rollback(ctx context.Context) error
}

// BatchItem represents a single operation in a batch.
type BatchItem struct {
	// Query is the SQL query to execute
	Query string

	// Args are the query arguments
	Args []any
}

// BatchExecutor is an optional interface for drivers that support batch operations.
// pgx/v5 supports native batching; database/sql falls back to sequential execution.
type BatchExecutor interface {
	Executor

	// SendBatch sends multiple queries as a batch.
	// Returns the number of rows affected per operation.
	// Drivers without native batch support execute queries sequentially.
	SendBatch(ctx context.Context, items []BatchItem) ([]int64, error)
}
