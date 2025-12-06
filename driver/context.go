package driver

import "context"

// executorTxContextKey is the context key for storing ExecutorTx.
type executorTxContextKey struct{}

// WithExecutor returns a new context with the given executor transaction.
// This allows database operations to participate in an existing transaction.
//
// Example:
//
//	tx, _ := driver.Begin(ctx)
//	txCtx := driver.WithExecutor(ctx, tx)
//	// All store operations using txCtx will use the transaction
func WithExecutor(ctx context.Context, exec ExecutorTx) context.Context {
	return context.WithValue(ctx, executorTxContextKey{}, exec)
}

// ExecutorFromContext retrieves the executor from context, or nil if not present.
// Store implementations use this to determine if they should use a transaction.
//
// Example:
//
//	func (s *Store) SaveMessage(ctx context.Context, msg *Message) error {
//	    exec := driver.ExecutorFromContext(ctx)
//	    if exec == nil {
//	        exec = s.defaultExecutor
//	    }
//	    // Use exec for database operations
//	}
func ExecutorFromContext(ctx context.Context) ExecutorTx {
	if exec, ok := ctx.Value(executorTxContextKey{}).(ExecutorTx); ok {
		return exec
	}
	return nil
}

// StripExecutor creates a new context without the executor value.
// This is used when nested agents should have their own independent transactions.
//
// Example:
//
//	// Parent agent's transaction
//	parentCtx := driver.WithExecutor(ctx, parentTx)
//
//	// Nested agent should not inherit parent's transaction
//	nestedCtx := driver.StripExecutor(parentCtx)
//	// nestedCtx has no executor, nested agent will create its own
func StripExecutor(ctx context.Context) context.Context {
	return &executorStrippedContext{ctx}
}

// executorStrippedContext wraps a context to hide the executor value
// while preserving deadline, cancellation, and other values.
type executorStrippedContext struct {
	context.Context
}

// Value returns nil for the executor key, delegating other keys to the parent.
func (c *executorStrippedContext) Value(key any) any {
	if _, ok := key.(executorTxContextKey); ok {
		return nil
	}
	return c.Context.Value(key)
}
