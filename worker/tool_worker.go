package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/youssefsiam38/agentpg/runstate"
	"github.com/youssefsiam38/agentpg/storage"
	"github.com/youssefsiam38/agentpg/tool"
)

// ToolExecutionResult contains the result of executing a tool.
type ToolExecutionResult struct {
	ExecutionID string
	ToolName    string
	Output      string
	Error       error
	Duration    time.Duration
}

// ToolExecutor handles parallel tool execution for a run.
type ToolExecutor struct {
	store      storage.Store
	registry   *tool.Registry
	instanceID string
	timeout    time.Duration
	maxRetries int
}

// NewToolExecutor creates a new tool executor.
func NewToolExecutor(
	store storage.Store,
	registry *tool.Registry,
	instanceID string,
	timeout time.Duration,
	maxRetries int,
) *ToolExecutor {
	return &ToolExecutor{
		store:      store,
		registry:   registry,
		instanceID: instanceID,
		timeout:    timeout,
		maxRetries: maxRetries,
	}
}

// ExecuteAllPending executes all pending tool executions for a run in parallel.
// Returns when all executions are complete.
func (e *ToolExecutor) ExecuteAllPending(ctx context.Context, runID string) ([]*ToolExecutionResult, error) {
	// Get pending tool executions for this run
	executions, err := e.store.GetRunToolExecutions(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("failed to get tool executions: %w", err)
	}

	// Filter to only pending ones
	var pending []*storage.ToolExecution
	for _, exec := range executions {
		if exec.State == runstate.ToolExecPending {
			pending = append(pending, exec)
		}
	}

	if len(pending) == 0 {
		return nil, nil
	}

	// Execute all in parallel
	results := make([]*ToolExecutionResult, len(pending))
	var wg sync.WaitGroup
	var mu sync.Mutex
	errs := make([]error, 0)

	for i, exec := range pending {
		wg.Add(1)
		go func(idx int, execution *storage.ToolExecution) {
			defer wg.Done()

			result, err := e.executeOne(ctx, execution)
			results[idx] = result

			if err != nil {
				mu.Lock()
				errs = append(errs, err)
				mu.Unlock()
			}
		}(i, exec)
	}

	wg.Wait()

	if len(errs) > 0 {
		// Return first error, but all results are still available
		return results, errs[0]
	}

	return results, nil
}

// executeOne executes a single tool execution.
func (e *ToolExecutor) executeOne(ctx context.Context, exec *storage.ToolExecution) (*ToolExecutionResult, error) {
	result := &ToolExecutionResult{
		ExecutionID: exec.ID,
		ToolName:    exec.ToolName,
	}

	start := time.Now()
	defer func() {
		result.Duration = time.Since(start)
	}()

	// Try to claim the execution
	claimed, err := e.store.ClaimToolExecution(ctx, exec.ID, e.instanceID)
	if err != nil {
		result.Error = fmt.Errorf("failed to claim: %w", err)
		return result, err
	}
	if !claimed {
		// Another worker claimed it
		return result, nil
	}

	// Create execution context with timeout
	execCtx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()

	// Get the tool
	t, exists := e.registry.Get(exec.ToolName)
	if !exists {
		result.Error = fmt.Errorf("tool not found: %s", exec.ToolName)
		return result, e.failExecution(ctx, exec.ID, result.Error.Error())
	}

	// Marshal tool input
	input, err := json.Marshal(exec.ToolInput)
	if err != nil {
		result.Error = fmt.Errorf("failed to marshal input: %w", err)
		return result, e.failExecution(ctx, exec.ID, result.Error.Error())
	}

	// Execute the tool
	output, execErr := t.Execute(execCtx, input)
	result.Output = output

	if execErr != nil {
		result.Error = execErr

		// Check if we should retry
		if exec.AttemptCount < e.maxRetries {
			// Mark as failed for retry
			return result, e.store.UpdateToolExecutionState(ctx, exec.ID, &storage.UpdateToolExecutionStateParams{
				State:        runstate.ToolExecFailed,
				ErrorMessage: strPtr(execErr.Error()),
				ToolOutput:   &output,
			})
		}

		// No more retries
		return result, e.failExecution(ctx, exec.ID, execErr.Error())
	}

	// Success
	if err := e.store.UpdateToolExecutionState(ctx, exec.ID, &storage.UpdateToolExecutionStateParams{
		State:      runstate.ToolExecCompleted,
		ToolOutput: &output,
	}); err != nil {
		return result, fmt.Errorf("failed to update execution state: %w", err)
	}

	return result, nil
}

// failExecution marks an execution as permanently failed.
func (e *ToolExecutor) failExecution(ctx context.Context, execID, errMsg string) error {
	return e.store.UpdateToolExecutionState(ctx, execID, &storage.UpdateToolExecutionStateParams{
		State:        runstate.ToolExecFailed,
		ErrorMessage: &errMsg,
	})
}

// strPtr returns a pointer to a string.
func strPtr(s string) *string {
	return &s
}

// BatchExecutor handles batch tool execution with result aggregation.
type BatchExecutor struct {
	executor *ToolExecutor
	store    storage.Store
}

// NewBatchExecutor creates a new batch executor.
func NewBatchExecutor(executor *ToolExecutor, store storage.Store) *BatchExecutor {
	return &BatchExecutor{
		executor: executor,
		store:    store,
	}
}

// ExecuteAndCollect executes all tools for a run and collects results.
// Returns when all tools are complete (success or failure).
func (b *BatchExecutor) ExecuteAndCollect(ctx context.Context, runID string) (*BatchResult, error) {
	result := &BatchResult{
		RunID: runID,
	}

	// Execute all pending tools
	// Note: We intentionally ignore errors here and continue to collect all results
	execResults, _ := b.executor.ExecuteAllPending(ctx, runID)

	// Collect results
	for _, r := range execResults {
		if r == nil {
			continue
		}

		if r.Error != nil {
			result.FailedCount++
			result.Errors = append(result.Errors, ToolError{
				ExecutionID: r.ExecutionID,
				ToolName:    r.ToolName,
				Error:       r.Error,
			})
		} else {
			result.CompletedCount++
		}

		result.Results = append(result.Results, r)
	}

	// Check if all are complete
	complete, err := b.store.AreAllToolExecutionsComplete(ctx, runID)
	if err != nil {
		return result, fmt.Errorf("failed to check completion: %w", err)
	}

	result.AllComplete = complete

	return result, nil
}

// BatchResult contains the results of batch tool execution.
type BatchResult struct {
	RunID          string
	Results        []*ToolExecutionResult
	CompletedCount int
	FailedCount    int
	AllComplete    bool
	Errors         []ToolError
}

// ToolError represents an error from a tool execution.
type ToolError struct {
	ExecutionID string
	ToolName    string
	Error       error
}

// HasErrors returns true if any tools failed.
func (r *BatchResult) HasErrors() bool {
	return r.FailedCount > 0
}

// TotalCount returns the total number of tool executions.
func (r *BatchResult) TotalCount() int {
	return r.CompletedCount + r.FailedCount
}
