package agentpg

import (
	"context"
	"errors"
)

// nativeTxContextKey is the context key for storing the native transaction type.
type nativeTxContextKey struct{}

// ErrNoTransaction is returned when TxFromContextSafely is called
// but no transaction exists in context.
var ErrNoTransaction = errors.New("agentpg: no transaction in context, only available within agent execution")

// withNativeTx stores the native transaction in context.
// This is called internally by RunTx to make the transaction available to tools.
func withNativeTx[TTx any](ctx context.Context, tx TTx) context.Context {
	return context.WithValue(ctx, nativeTxContextKey{}, tx)
}

// TxFromContext returns the native database transaction from the context.
// This function can only be used within a Tool's Execute() method when the
// agent is running via RunTx().
//
// It panics if the context does not contain a transaction. Use TxFromContextSafely
// if you need to handle the case where no transaction is present.
//
// The type parameter TTx must match the transaction type of your driver:
//   - pgx.Tx for pgxv5.Driver
//   - *sql.Tx for databasesql.Driver
//
// Example:
//
//	func (t *MyTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
//	    tx := agentpg.TxFromContext[pgx.Tx](ctx)
//	    _, err := tx.Exec(ctx, "INSERT INTO logs ...")
//	    return "done", err
//	}
func TxFromContext[TTx any](ctx context.Context) TTx {
	tx, err := TxFromContextSafely[TTx](ctx)
	if err != nil {
		panic(err)
	}
	return tx
}

// TxFromContextSafely returns the native database transaction from the context.
// Unlike TxFromContext, it returns an error instead of panicking if no transaction
// is present.
//
// This is useful for tools that can operate both with and without a transaction:
//
//	func (t *MyTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
//	    tx, err := agentpg.TxFromContextSafely[pgx.Tx](ctx)
//	    if err != nil {
//	        // No transaction - use pool directly or skip DB operations
//	        return t.executeWithoutTx(ctx, input)
//	    }
//	    // Has transaction - use it
//	    _, err = tx.Exec(ctx, "INSERT INTO logs ...")
//	    return "done", err
//	}
func TxFromContextSafely[TTx any](ctx context.Context) (TTx, error) {
	var zero TTx
	val := ctx.Value(nativeTxContextKey{})
	if val == nil {
		return zero, ErrNoTransaction
	}
	tx, ok := val.(TTx)
	if !ok {
		return zero, ErrNoTransaction
	}
	return tx, nil
}

// WithTestTx creates a context with a native transaction for testing tools.
// This is intended for unit testing tools that use TxFromContext.
//
// Example:
//
//	func TestMyTool(t *testing.T) {
//	    tx, _ := pool.Begin(ctx)
//	    defer tx.Rollback(ctx)
//
//	    ctx := agentpg.WithTestTx(context.Background(), tx)
//	    result, err := myTool.Execute(ctx, input)
//	    // assertions...
//	}
func WithTestTx[TTx any](ctx context.Context, tx TTx) context.Context {
	return withNativeTx(ctx, tx)
}

// stripNativeTx removes the native transaction from context.
// Used for nested agents that should manage their own transactions.
func stripNativeTx(ctx context.Context) context.Context {
	return &nativeTxStrippedContext{ctx}
}

// nativeTxStrippedContext wraps a context to hide the native transaction
// while preserving deadline, cancellation, and other values.
type nativeTxStrippedContext struct {
	context.Context
}

// Value returns nil for the native tx key, delegating other keys to the parent.
func (c *nativeTxStrippedContext) Value(key any) any {
	if _, ok := key.(nativeTxContextKey); ok {
		return nil
	}
	return c.Context.Value(key)
}
