package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

func TestExecuteParallel_NoRaceCondition(t *testing.T) {
	registry := NewRegistry()

	// Create a tool that increments a counter
	var counter int32
	counterTool := NewFuncTool(
		"counter",
		"Increments counter",
		ToolSchema{Type: "object", Properties: map[string]PropertyDef{}},
		func(ctx context.Context, input json.RawMessage) (string, error) {
			atomic.AddInt32(&counter, 1)
			// Simulate variable execution time
			time.Sleep(time.Millisecond * time.Duration(1+atomic.LoadInt32(&counter)%5))
			return "done", nil
		},
	)
	if err := registry.Register(counterTool); err != nil {
		t.Fatalf("Failed to register tool: %v", err)
	}

	executor := NewExecutor(registry)

	// Execute many calls in parallel
	numCalls := 50
	calls := make([]ToolCallRequest, numCalls)
	for i := range calls {
		calls[i] = ToolCallRequest{
			ID:       fmt.Sprintf("call-%d", i),
			ToolName: "counter",
			Input:    json.RawMessage(`{}`),
		}
	}

	results := executor.ExecuteParallel(context.Background(), calls)

	// All results should be present
	if len(results) != numCalls {
		t.Errorf("Expected %d results, got %d", numCalls, len(results))
	}

	// All results should be successful
	for i, r := range results {
		if r == nil {
			t.Errorf("Result %d is nil", i)
		} else if r.Error != nil {
			t.Errorf("Result %d has error: %v", i, r.Error)
		}
	}

	// Counter should equal number of calls
	if atomic.LoadInt32(&counter) != int32(numCalls) {
		t.Errorf("Expected counter %d, got %d", numCalls, counter)
	}
}

func TestExecuteParallel_EmptyCalls(t *testing.T) {
	registry := NewRegistry()
	executor := NewExecutor(registry)

	results := executor.ExecuteParallel(context.Background(), []ToolCallRequest{})

	if len(results) != 0 {
		t.Errorf("Expected 0 results, got %d", len(results))
	}
}

func TestExecuteParallel_ContextCancellation(t *testing.T) {
	registry := NewRegistry()

	// Create a tool that waits
	slowTool := NewFuncTool(
		"slow",
		"A slow tool",
		ToolSchema{Type: "object", Properties: map[string]PropertyDef{}},
		func(ctx context.Context, input json.RawMessage) (string, error) {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(time.Second * 5):
				return "done", nil
			}
		},
	)
	if err := registry.Register(slowTool); err != nil {
		t.Fatalf("Failed to register tool: %v", err)
	}

	executor := NewExecutor(registry)
	executor.SetDefaultTimeout(50 * time.Millisecond)

	calls := []ToolCallRequest{{ID: "1", ToolName: "slow", Input: json.RawMessage(`{}`)}}
	results := executor.ExecuteParallel(context.Background(), calls)

	// Should have error from timeout
	if len(results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(results))
	}
	if results[0].Error == nil {
		t.Error("Expected timeout error, got nil")
	}
}

func TestExecute_ToolNotFound(t *testing.T) {
	registry := NewRegistry()
	executor := NewExecutor(registry)

	result := executor.Execute(context.Background(), "nonexistent", json.RawMessage(`{}`))

	if result.Error == nil {
		t.Error("Expected error for nonexistent tool")
	}
}

func TestExecuteBatch_Sequential(t *testing.T) {
	registry := NewRegistry()

	var order []int
	orderTool := NewFuncTool(
		"order",
		"Records execution order",
		ToolSchema{Type: "object", Properties: map[string]PropertyDef{
			"id": {Type: "integer"},
		}},
		func(ctx context.Context, input json.RawMessage) (string, error) {
			var params struct{ ID int }
			json.Unmarshal(input, &params)
			order = append(order, params.ID)
			return "ok", nil
		},
	)
	registry.Register(orderTool)

	executor := NewExecutor(registry)

	calls := []ToolCallRequest{
		{ID: "1", ToolName: "order", Input: json.RawMessage(`{"id": 1}`)},
		{ID: "2", ToolName: "order", Input: json.RawMessage(`{"id": 2}`)},
		{ID: "3", ToolName: "order", Input: json.RawMessage(`{"id": 3}`)},
	}

	// Sequential execution
	results := executor.ExecuteBatch(context.Background(), calls, false)

	if len(results) != 3 {
		t.Fatalf("Expected 3 results, got %d", len(results))
	}

	// Order should be preserved for sequential
	for i, id := range []int{1, 2, 3} {
		if order[i] != id {
			t.Errorf("Expected order[%d] = %d, got %d", i, id, order[i])
		}
	}
}
