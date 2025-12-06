package hooks

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/youssefsiam38/agentpg/compaction"
	"github.com/youssefsiam38/agentpg/types"
)

func TestNewRegistry(t *testing.T) {
	r := NewRegistry()
	if r == nil {
		t.Fatal("NewRegistry returned nil")
	}
}

func TestOnBeforeMessage(t *testing.T) {
	r := NewRegistry()
	called := false

	r.OnBeforeMessage(func(ctx context.Context, messages []*types.Message) error {
		called = true
		return nil
	})

	err := r.TriggerBeforeMessage(context.Background(), nil)
	if err != nil {
		t.Errorf("TriggerBeforeMessage returned error: %v", err)
	}
	if !called {
		t.Error("hook was not called")
	}
}

func TestOnAfterMessage(t *testing.T) {
	r := NewRegistry()
	called := false

	r.OnAfterMessage(func(ctx context.Context, response *types.Response) error {
		called = true
		return nil
	})

	err := r.TriggerAfterMessage(context.Background(), nil)
	if err != nil {
		t.Errorf("TriggerAfterMessage returned error: %v", err)
	}
	if !called {
		t.Error("hook was not called")
	}
}

func TestOnToolCall(t *testing.T) {
	r := NewRegistry()
	var capturedName string
	var capturedOutput string

	r.OnToolCall(func(ctx context.Context, name string, input json.RawMessage, output string, err error) error {
		capturedName = name
		capturedOutput = output
		return nil
	})

	err := r.TriggerToolCall(context.Background(), "test_tool", nil, "test output", nil)
	if err != nil {
		t.Errorf("TriggerToolCall returned error: %v", err)
	}
	if capturedName != "test_tool" {
		t.Errorf("expected name 'test_tool', got '%s'", capturedName)
	}
	if capturedOutput != "test output" {
		t.Errorf("expected output 'test output', got '%s'", capturedOutput)
	}
}

func TestOnBeforeCompaction(t *testing.T) {
	r := NewRegistry()
	var capturedSessionID string

	r.OnBeforeCompaction(func(ctx context.Context, sessionID string) error {
		capturedSessionID = sessionID
		return nil
	})

	err := r.TriggerBeforeCompaction(context.Background(), "session-123")
	if err != nil {
		t.Errorf("TriggerBeforeCompaction returned error: %v", err)
	}
	if capturedSessionID != "session-123" {
		t.Errorf("expected sessionID 'session-123', got '%s'", capturedSessionID)
	}
}

func TestOnAfterCompaction(t *testing.T) {
	r := NewRegistry()
	var capturedResult *compaction.CompactionResult

	r.OnAfterCompaction(func(ctx context.Context, result *compaction.CompactionResult) error {
		capturedResult = result
		return nil
	})

	testResult := &compaction.CompactionResult{
		OriginalTokens:  1000,
		CompactedTokens: 500,
	}

	err := r.TriggerAfterCompaction(context.Background(), testResult)
	if err != nil {
		t.Errorf("TriggerAfterCompaction returned error: %v", err)
	}
	if capturedResult != testResult {
		t.Error("result was not passed to hook")
	}
}

func TestHookError(t *testing.T) {
	r := NewRegistry()
	expectedErr := errors.New("hook error")

	r.OnBeforeMessage(func(ctx context.Context, messages []*types.Message) error {
		return expectedErr
	})

	err := r.TriggerBeforeMessage(context.Background(), nil)
	if !errors.Is(err, expectedErr) {
		t.Errorf("expected error %v, got %v", expectedErr, err)
	}
}

func TestMultipleHooks(t *testing.T) {
	r := NewRegistry()
	callOrder := []int{}

	r.OnBeforeMessage(func(ctx context.Context, messages []*types.Message) error {
		callOrder = append(callOrder, 1)
		return nil
	})

	r.OnBeforeMessage(func(ctx context.Context, messages []*types.Message) error {
		callOrder = append(callOrder, 2)
		return nil
	})

	r.OnBeforeMessage(func(ctx context.Context, messages []*types.Message) error {
		callOrder = append(callOrder, 3)
		return nil
	})

	err := r.TriggerBeforeMessage(context.Background(), nil)
	if err != nil {
		t.Errorf("TriggerBeforeMessage returned error: %v", err)
	}

	if len(callOrder) != 3 {
		t.Errorf("expected 3 hooks to be called, got %d", len(callOrder))
	}

	// Verify hooks are called in order
	for i, v := range callOrder {
		if v != i+1 {
			t.Errorf("expected call order %d at index %d, got %d", i+1, i, v)
		}
	}
}

func TestHookStopsOnError(t *testing.T) {
	r := NewRegistry()
	called := []int{}
	expectedErr := errors.New("stop here")

	r.OnBeforeMessage(func(ctx context.Context, messages []*types.Message) error {
		called = append(called, 1)
		return nil
	})

	r.OnBeforeMessage(func(ctx context.Context, messages []*types.Message) error {
		called = append(called, 2)
		return expectedErr // This should stop execution
	})

	r.OnBeforeMessage(func(ctx context.Context, messages []*types.Message) error {
		called = append(called, 3) // This should NOT be called
		return nil
	})

	err := r.TriggerBeforeMessage(context.Background(), nil)
	if !errors.Is(err, expectedErr) {
		t.Errorf("expected error %v, got %v", expectedErr, err)
	}

	if len(called) != 2 {
		t.Errorf("expected 2 hooks to be called before error, got %d", len(called))
	}
}

func TestConcurrentHookRegistration(t *testing.T) {
	r := NewRegistry()
	var wg sync.WaitGroup
	numGoroutines := 100

	// Concurrently register hooks
	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			r.OnBeforeMessage(func(ctx context.Context, messages []*types.Message) error {
				return nil
			})
		}()
	}
	wg.Wait()

	// Trigger should work without panic
	err := r.TriggerBeforeMessage(context.Background(), nil)
	if err != nil {
		t.Errorf("TriggerBeforeMessage returned error: %v", err)
	}
}

func TestConcurrentHookTrigger(t *testing.T) {
	r := NewRegistry()
	var callCount int
	var mu sync.Mutex

	r.OnBeforeMessage(func(ctx context.Context, messages []*types.Message) error {
		mu.Lock()
		callCount++
		mu.Unlock()
		return nil
	})

	var wg sync.WaitGroup
	numGoroutines := 100

	// Concurrently trigger hooks
	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			r.TriggerBeforeMessage(context.Background(), nil)
		}()
	}
	wg.Wait()

	if callCount != numGoroutines {
		t.Errorf("expected %d calls, got %d", numGoroutines, callCount)
	}
}

func TestConcurrentRegistrationAndTrigger(t *testing.T) {
	r := NewRegistry()
	var wg sync.WaitGroup

	// Pre-register some hooks
	for i := 0; i < 10; i++ {
		r.OnBeforeMessage(func(ctx context.Context, messages []*types.Message) error {
			return nil
		})
	}

	// Concurrently register and trigger
	wg.Add(200)
	for i := 0; i < 100; i++ {
		go func() {
			defer wg.Done()
			r.OnBeforeMessage(func(ctx context.Context, messages []*types.Message) error {
				return nil
			})
		}()
		go func() {
			defer wg.Done()
			r.TriggerBeforeMessage(context.Background(), nil)
		}()
	}
	wg.Wait()

	// No panic means success - the mutex is working
}
