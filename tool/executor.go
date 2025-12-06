package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// Executor handles tool execution with error handling and timeouts
type Executor struct {
	registry       *Registry
	validator      *Validator
	defaultTimeout time.Duration
}

// NewExecutor creates a new tool executor
func NewExecutor(registry *Registry) *Executor {
	return &Executor{
		registry:       registry,
		validator:      NewValidator(),
		defaultTimeout: 30 * time.Second, // Default 30 second timeout
	}
}

// SetDefaultTimeout sets the default execution timeout
func (e *Executor) SetDefaultTimeout(timeout time.Duration) {
	e.defaultTimeout = timeout
}

// ExecuteResult represents the result of a tool execution
type ExecuteResult struct {
	ToolName string
	Input    json.RawMessage
	Output   string
	Error    error
	Duration time.Duration
}

// Execute executes a single tool call
func (e *Executor) Execute(ctx context.Context, toolName string, input json.RawMessage) *ExecuteResult {
	start := time.Now()

	result := &ExecuteResult{
		ToolName: toolName,
		Input:    input,
	}

	// Create context with timeout
	execCtx, cancel := context.WithTimeout(ctx, e.defaultTimeout)
	defer cancel()

	// Execute the tool
	output, err := e.registry.Execute(execCtx, toolName, input)
	result.Output = output
	result.Error = err
	result.Duration = time.Since(start)

	// Check for context errors
	if execCtx.Err() != nil {
		if execCtx.Err() == context.DeadlineExceeded {
			result.Error = fmt.Errorf("tool execution timeout after %v", e.defaultTimeout)
		} else if execCtx.Err() == context.Canceled {
			result.Error = fmt.Errorf("tool execution canceled")
		}
	}

	return result
}

// ExecuteMultiple executes multiple tool calls in sequence
func (e *Executor) ExecuteMultiple(ctx context.Context, calls []ToolCallRequest) []*ExecuteResult {
	results := make([]*ExecuteResult, len(calls))

	for i, call := range calls {
		results[i] = e.Execute(ctx, call.ToolName, call.Input)
	}

	return results
}

// ExecuteParallel executes multiple tool calls in parallel
func (e *Executor) ExecuteParallel(ctx context.Context, calls []ToolCallRequest) []*ExecuteResult {
	if len(calls) == 0 {
		return []*ExecuteResult{}
	}

	results := make([]*ExecuteResult, len(calls))
	var wg sync.WaitGroup

	wg.Add(len(calls))
	for i, call := range calls {
		go func(idx int, c ToolCallRequest) {
			defer wg.Done()
			results[idx] = e.Execute(ctx, c.ToolName, c.Input)
		}(i, call)
	}

	wg.Wait()
	return results
}

// ToolCallRequest represents a request to execute a tool
type ToolCallRequest struct {
	ID       string          // Unique ID for this call
	ToolName string          // Name of the tool to execute
	Input    json.RawMessage // Input parameters
}

// ExecuteBatch executes a batch of tool calls with the given strategy
func (e *Executor) ExecuteBatch(ctx context.Context, calls []ToolCallRequest, parallel bool) []*ExecuteResult {
	if parallel {
		return e.ExecuteParallel(ctx, calls)
	}
	return e.ExecuteMultiple(ctx, calls)
}

// ValidateInput validates tool input against its schema
func (e *Executor) ValidateInput(toolName string, input json.RawMessage) error {
	tool, exists := e.registry.Get(toolName)
	if !exists {
		return fmt.Errorf("tool not found: %s", toolName)
	}

	return e.validator.ValidateInput(tool.InputSchema(), input)
}
