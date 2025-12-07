package agentpg

import (
	"context"
	"errors"
	"testing"
)

// mockTx is a simple mock transaction type for testing
type mockTx struct {
	id string
}

func TestTxFromContext(t *testing.T) {
	t.Run("returns transaction when present", func(t *testing.T) {
		ctx := context.Background()
		expectedTx := &mockTx{id: "test-tx"}

		ctx = withNativeTx(ctx, expectedTx)

		tx := TxFromContext[*mockTx](ctx)
		if tx != expectedTx {
			t.Errorf("got %v, want %v", tx, expectedTx)
		}
	})

	t.Run("panics when no transaction in context", func(t *testing.T) {
		ctx := context.Background()

		defer func() {
			if r := recover(); r == nil {
				t.Error("expected panic, got none")
			}
		}()

		TxFromContext[*mockTx](ctx)
	})

	t.Run("panics when wrong type", func(t *testing.T) {
		ctx := context.Background()
		ctx = withNativeTx(ctx, "not a transaction")

		defer func() {
			if r := recover(); r == nil {
				t.Error("expected panic, got none")
			}
		}()

		TxFromContext[*mockTx](ctx)
	})
}

func TestTxFromContextSafely(t *testing.T) {
	t.Run("returns transaction when present", func(t *testing.T) {
		ctx := context.Background()
		expectedTx := &mockTx{id: "test-tx"}

		ctx = withNativeTx(ctx, expectedTx)

		tx, err := TxFromContextSafely[*mockTx](ctx)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if tx != expectedTx {
			t.Errorf("got %v, want %v", tx, expectedTx)
		}
	})

	t.Run("returns error when no transaction in context", func(t *testing.T) {
		ctx := context.Background()

		tx, err := TxFromContextSafely[*mockTx](ctx)
		if !errors.Is(err, ErrNoTransaction) {
			t.Errorf("got error %v, want %v", err, ErrNoTransaction)
		}
		if tx != nil {
			t.Errorf("expected nil transaction, got %v", tx)
		}
	})

	t.Run("returns error when wrong type", func(t *testing.T) {
		ctx := context.Background()
		ctx = withNativeTx(ctx, "not a transaction")

		tx, err := TxFromContextSafely[*mockTx](ctx)
		if !errors.Is(err, ErrNoTransaction) {
			t.Errorf("got error %v, want %v", err, ErrNoTransaction)
		}
		if tx != nil {
			t.Errorf("expected nil transaction, got %v", tx)
		}
	})
}

func TestWithTestTx(t *testing.T) {
	t.Run("injects transaction into context", func(t *testing.T) {
		ctx := context.Background()
		expectedTx := &mockTx{id: "test-tx"}

		ctx = WithTestTx(ctx, expectedTx)

		tx, err := TxFromContextSafely[*mockTx](ctx)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if tx != expectedTx {
			t.Errorf("got %v, want %v", tx, expectedTx)
		}
	})
}

func TestStripNativeTx(t *testing.T) {
	t.Run("removes transaction from context", func(t *testing.T) {
		ctx := context.Background()
		tx := &mockTx{id: "test-tx"}

		ctx = withNativeTx(ctx, tx)
		ctx = stripNativeTx(ctx)

		_, err := TxFromContextSafely[*mockTx](ctx)
		if !errors.Is(err, ErrNoTransaction) {
			t.Errorf("expected ErrNoTransaction after stripping, got %v", err)
		}
	})

	t.Run("preserves other context values", func(t *testing.T) {
		type customKey struct{}
		ctx := context.Background()
		ctx = context.WithValue(ctx, customKey{}, "custom-value")
		ctx = withNativeTx(ctx, &mockTx{id: "test-tx"})

		ctx = stripNativeTx(ctx)

		// Transaction should be stripped
		_, err := TxFromContextSafely[*mockTx](ctx)
		if !errors.Is(err, ErrNoTransaction) {
			t.Errorf("expected ErrNoTransaction after stripping, got %v", err)
		}

		// Custom value should be preserved
		val := ctx.Value(customKey{})
		if val != "custom-value" {
			t.Errorf("custom value was lost, got %v", val)
		}
	})

	t.Run("preserves context cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		ctx = withNativeTx(ctx, &mockTx{id: "test-tx"})
		ctx = stripNativeTx(ctx)

		cancel()

		select {
		case <-ctx.Done():
			// Expected
		default:
			t.Error("context should be cancelled")
		}
	})
}
