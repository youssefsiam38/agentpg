package agentpg

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/youssefsiam38/agentpg/driver"
	"github.com/youssefsiam38/agentpg/tool"
)

// toolWorker executes pending tool executions.
type toolWorker[TTx any] struct {
	client          *Client[TTx]
	triggerCh       chan struct{}
	toolsCompleteCh chan uuid.UUID
}

func newToolWorker[TTx any](c *Client[TTx]) *toolWorker[TTx] {
	return &toolWorker[TTx]{
		client:          c,
		triggerCh:       make(chan struct{}, 1),
		toolsCompleteCh: make(chan uuid.UUID, 10000),
	}
}

func (w *toolWorker[TTx]) trigger() {
	select {
	case w.triggerCh <- struct{}{}:
	default:
	}
}

func (w *toolWorker[TTx]) handleToolsComplete(runID uuid.UUID) {
	select {
	case w.toolsCompleteCh <- runID:
	default:
	}
}

func (w *toolWorker[TTx]) run(ctx context.Context) {
	ticker := time.NewTicker(w.client.config.ToolPollInterval)
	stuckCheckTicker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	defer stuckCheckTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-w.triggerCh:
			w.processToolExecutions(ctx)
		case <-ticker.C:
			w.processToolExecutions(ctx)
		case runID := <-w.toolsCompleteCh:
			w.handleAllToolsComplete(ctx, runID)
		case <-stuckCheckTicker.C:
			w.checkForStuckRuns(ctx)
		}
	}
}

func (w *toolWorker[TTx]) processToolExecutions(ctx context.Context) {
	store := w.client.driver.Store()

	// Claim pending tool executions
	executions, err := store.ClaimToolExecutions(ctx, w.client.instanceID, w.client.config.MaxConcurrentTools)
	if err != nil {
		w.client.log().Error("failed to claim tool executions", "error", err)
		return
	}

	for _, exec := range executions {
		go w.executeToolAsync(ctx, exec)
	}
}

func (w *toolWorker[TTx]) executeToolAsync(ctx context.Context, exec *driver.ToolExecution) {
	if err := w.executeTool(ctx, exec); err != nil {
		w.client.log().Error("tool execution failed",
			"execution_id", exec.ID,
			"tool_name", exec.ToolName,
			"error", err,
		)
	}
}

func (w *toolWorker[TTx]) executeTool(ctx context.Context, exec *driver.ToolExecution) error {
	store := w.client.driver.Store()
	log := w.client.log()

	log.Debug("executing tool",
		"execution_id", exec.ID,
		"tool_name", exec.ToolName,
		"is_agent_tool", exec.IsAgentTool,
	)

	now := time.Now()

	// Update state to running
	if err := store.UpdateToolExecution(ctx, exec.ID, map[string]any{
		"state":      string(ToolStateRunning),
		"started_at": now,
	}); err != nil {
		return fmt.Errorf("failed to update tool state: %w", err)
	}

	// Handle agent-as-tool
	if exec.IsAgentTool {
		return w.executeAgentTool(ctx, exec)
	}

	// Execute regular tool
	t := w.client.GetTool(exec.ToolName)
	if t == nil {
		return w.completeToolExecution(ctx, exec.ID, "", true, fmt.Sprintf("tool not found: %s", exec.ToolName))
	}

	// Execute with timeout
	execCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	output, err := t.Execute(execCtx, exec.ToolInput)
	if err != nil {
		return w.handleToolError(ctx, exec, err)
	}

	return w.completeToolExecution(ctx, exec.ID, output, false, "")
}

func (w *toolWorker[TTx]) executeAgentTool(ctx context.Context, exec *driver.ToolExecution) error {
	store := w.client.driver.Store()
	log := w.client.log()

	if exec.AgentName == nil {
		return w.completeToolExecution(ctx, exec.ID, "", true, "agent name is nil for agent tool")
	}

	agentName := *exec.AgentName

	// Parse input to get task
	var input struct {
		Task string `json:"task"`
	}
	if err := json.Unmarshal(exec.ToolInput, &input); err != nil {
		return w.completeToolExecution(ctx, exec.ID, "", true, fmt.Sprintf("invalid input: %v", err))
	}

	// Get parent run to determine session and depth
	parentRun, err := store.GetRun(ctx, exec.RunID)
	if err != nil {
		return fmt.Errorf("failed to get parent run: %w", err)
	}

	// Create child run (inherit run mode from parent for consistent latency behavior)
	childRun, err := store.CreateRun(ctx, driver.CreateRunParams{
		SessionID:             parentRun.SessionID,
		AgentName:             agentName,
		Prompt:                input.Task,
		RunMode:               parentRun.RunMode, // Inherit from parent
		ParentRunID:           &exec.RunID,
		ParentToolExecutionID: &exec.ID,
		Depth:                 parentRun.Depth + 1,
		CreatedByInstanceID:   w.client.instanceID,
	})
	if err != nil {
		return w.completeToolExecution(ctx, exec.ID, "", true, fmt.Sprintf("failed to create child run: %v", err))
	}

	log.Info("created child run for agent tool",
		"execution_id", exec.ID,
		"child_run_id", childRun.ID,
		"agent_name", agentName,
		"depth", childRun.Depth,
	)

	// Update tool execution with child run ID
	// The child run completion will be handled by DB trigger
	if err := store.UpdateToolExecution(ctx, exec.ID, map[string]any{
		"child_run_id": childRun.ID,
	}); err != nil {
		return fmt.Errorf("failed to update tool execution with child run: %w", err)
	}

	return nil
}

func (w *toolWorker[TTx]) completeToolExecution(ctx context.Context, execID uuid.UUID, output string, isError bool, errorMsg string) error {
	store := w.client.driver.Store()

	if err := store.CompleteToolExecution(ctx, execID, output, isError, errorMsg); err != nil {
		return fmt.Errorf("failed to complete tool execution: %w", err)
	}

	w.client.log().Debug("tool execution completed",
		"execution_id", execID,
		"is_error", isError,
	)

	return nil
}

// handleToolError handles tool execution errors with retry logic.
// It checks for special error types (Cancel, Discard, Snooze) and handles
// regular errors with exponential backoff retries.
func (w *toolWorker[TTx]) handleToolError(ctx context.Context, exec *driver.ToolExecution, err error) error {
	store := w.client.driver.Store()
	log := w.client.log()

	// Check for ToolCancelError - cancel immediately, no retry
	var cancelErr *tool.ToolCancelError
	if errors.As(err, &cancelErr) {
		log.Info("tool execution cancelled",
			"execution_id", exec.ID,
			"tool_name", exec.ToolName,
			"error", cancelErr.Error(),
		)
		return store.DiscardToolExecution(ctx, exec.ID, cancelErr.Error())
	}

	// Check for ToolDiscardError - discard permanently, invalid input
	var discardErr *tool.ToolDiscardError
	if errors.As(err, &discardErr) {
		log.Info("tool execution discarded",
			"execution_id", exec.ID,
			"tool_name", exec.ToolName,
			"error", discardErr.Error(),
		)
		return store.DiscardToolExecution(ctx, exec.ID, discardErr.Error())
	}

	// Check for ToolSnoozeError - retry after duration, does NOT consume attempt
	var snoozeErr *tool.ToolSnoozeError
	if errors.As(err, &snoozeErr) {
		scheduledAt := time.Now().Add(snoozeErr.Duration)
		log.Info("tool execution snoozed",
			"execution_id", exec.ID,
			"tool_name", exec.ToolName,
			"snooze_duration", snoozeErr.Duration,
			"scheduled_at", scheduledAt,
		)
		return store.SnoozeToolExecution(ctx, exec.ID, scheduledAt)
	}

	// Regular error - check if we should retry
	retryConfig := w.client.config.ToolRetryConfig
	if retryConfig == nil {
		retryConfig = DefaultToolRetryConfig()
	}

	// exec.AttemptCount is incremented by the claim function, so it reflects the current attempt
	if exec.AttemptCount >= exec.MaxAttempts {
		// Max attempts reached - fail permanently
		log.Warn("tool execution failed after max attempts",
			"execution_id", exec.ID,
			"tool_name", exec.ToolName,
			"attempt", exec.AttemptCount,
			"max_attempts", exec.MaxAttempts,
			"error", err.Error(),
		)
		return w.completeToolExecution(ctx, exec.ID, err.Error(), true, err.Error())
	}

	// Schedule retry with exponential backoff (attempt^4)
	delay := retryConfig.NextRetryDelay(exec.AttemptCount)
	scheduledAt := time.Now().Add(delay)

	log.Info("tool execution will be retried",
		"execution_id", exec.ID,
		"tool_name", exec.ToolName,
		"attempt", exec.AttemptCount,
		"max_attempts", exec.MaxAttempts,
		"retry_delay", delay,
		"scheduled_at", scheduledAt,
		"error", err.Error(),
	)

	return store.RetryToolExecution(ctx, exec.ID, scheduledAt, err.Error())
}

func (w *toolWorker[TTx]) handleAllToolsComplete(ctx context.Context, runID uuid.UUID) {
	store := w.client.driver.Store()
	log := w.client.log()

	log.Info("all tools complete for run", "run_id", runID)

	// Get run
	run, err := store.GetRun(ctx, runID)
	if err != nil {
		log.Error("failed to get run", "error", err, "run_id", runID)
		return
	}

	// Verify run is in pending_tools state
	if run.State != string(RunStatePendingTools) {
		log.Debug("run not in pending_tools state, skipping",
			"run_id", runID,
			"state", run.State,
		)
		return
	}

	// Get current iteration ID
	if run.CurrentIterationID == nil {
		log.Error("run has no current iteration ID", "run_id", runID)
		return
	}

	// Get completed tool executions for the current iteration only
	// This prevents including tool_results from previous iterations
	executions, err := store.GetToolExecutionsByIteration(ctx, *run.CurrentIterationID)
	if err != nil {
		log.Error("failed to get tool executions", "error", err, "run_id", runID)
		return
	}

	// Build tool result message content
	var contentBlocks []driver.ContentBlock
	for _, exec := range executions {
		output := Deref(exec.ToolOutput)
		if output == "" && exec.IsError {
			output = Deref(exec.ErrorMessage)
		}

		contentBlocks = append(contentBlocks, driver.ContentBlock{
			Type:               ContentTypeToolResult,
			ToolResultForUseID: exec.ToolUseID,
			ToolContent:        output,
			IsError:            exec.IsError,
		})
	}

	// Atomically create tool results message AND update run state to pending
	_, err = store.CompleteToolsAndContinueRun(ctx, run.SessionID, runID, contentBlocks)
	if err != nil {
		log.Error("failed to complete tools and continue run", "error", err, "run_id", runID)
		return
	}

	// Trigger run worker to pick up the run
	if w.client.runWorker != nil {
		w.client.runWorker.trigger()
	}
}

func (w *toolWorker[TTx]) checkForStuckRuns(ctx context.Context) {
	store := w.client.driver.Store()
	log := w.client.log()

	// Find runs stuck in pending_tools state with all tools complete
	runs, err := store.GetStuckPendingToolsRuns(ctx, 100)
	if err != nil {
		log.Error("failed to get stuck pending tools runs", "error", err)
		return
	}

	if len(runs) > 0 {
		log.Info("found stuck runs in pending_tools state", "count", len(runs))
	}

	for _, run := range runs {
		log.Info("recovering stuck run", "run_id", run.ID, "agent_name", run.AgentName)
		w.handleAllToolsComplete(ctx, run.ID)
	}
}
