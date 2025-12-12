package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// =============================================================================
// TOOL WORKER
// =============================================================================

// ToolWorkerConfig holds configuration for the tool worker.
type ToolWorkerConfig struct {
	// InstanceID is this worker's instance identifier.
	InstanceID string

	// MaxConcurrent is the maximum concurrent tool executions.
	MaxConcurrent int

	// PollInterval is how often to poll when LISTEN/NOTIFY is unavailable.
	PollInterval time.Duration

	// ClaimBatchSize is how many tool executions to claim at once.
	ClaimBatchSize int

	// DefaultTimeout is the default timeout for tool execution.
	DefaultTimeout time.Duration

	// Logger for structured logging.
	Logger Logger
}

// ToolWorker processes pending tool executions.
//
// Flow:
//  1. Listen for 'agentpg_tool_pending' notifications (or poll)
//  2. Claim pending tool executions using agentpg_claim_tool_executions()
//  3. For regular tools: execute the tool and store result
//  4. For agent-as-tool: create child run and mark execution as 'running'
//     (child run completion is handled by database trigger)
//  5. When all tools for a run complete, trigger notifies run worker
type ToolWorker struct {
	config   ToolWorkerConfig
	store    ToolStore
	executor ToolExecutor

	// Runtime state
	mu       sync.Mutex
	running  bool
	stopCh   chan struct{}
	wg       sync.WaitGroup
	activeCh chan struct{} // Semaphore for concurrency limiting
}

// ToolStore defines the database operations needed by ToolWorker.
type ToolStore interface {
	// ClaimToolExecutions claims pending tool executions for this instance.
	// Uses SELECT FOR UPDATE SKIP LOCKED for race safety.
	ClaimToolExecutions(ctx context.Context, instanceID string, maxCount int) ([]ToolExecutionRecord, error)

	// CompleteToolExecution marks a tool execution as completed.
	CompleteToolExecution(ctx context.Context, id uuid.UUID, output string, isError bool, errorMsg *string) error

	// FailToolExecution marks a tool execution as failed.
	FailToolExecution(ctx context.Context, id uuid.UUID, errorMsg string) error

	// CreateChildRun creates a child run for agent-as-tool execution.
	// Returns the child run ID and child session ID.
	CreateChildRun(ctx context.Context, opts CreateChildRunOpts) (runID uuid.UUID, sessionID uuid.UUID, err error)

	// UpdateToolExecutionChildRun sets the child_run_id for an agent-as-tool execution.
	UpdateToolExecutionChildRun(ctx context.Context, execID uuid.UUID, childRunID uuid.UUID) error

	// GetToolDefinition retrieves a tool's definition.
	GetToolDefinition(ctx context.Context, toolName string) (*ToolRecord, error)

	// GetAgentDefinition retrieves an agent's definition.
	GetAgentDefinition(ctx context.Context, agentName string) (*AgentRecord, error)

	// GetRunByID retrieves a run by ID.
	GetRunByID(ctx context.Context, runID uuid.UUID) (*RunRecord, error)
}

// ToolExecutor executes registered tools.
type ToolExecutor interface {
	// Execute executes a tool with the given input.
	// Returns the output string or an error.
	Execute(ctx context.Context, toolName string, input json.RawMessage) (string, error)

	// HasTool checks if a tool is registered.
	HasTool(toolName string) bool
}

// CreateChildRunOpts holds options for creating a child run.
type CreateChildRunOpts struct {
	// Parent run details
	ParentRunID           uuid.UUID
	ParentSessionID       uuid.UUID
	ParentToolExecutionID uuid.UUID
	ParentDepth           int

	// Child run details
	AgentName string
	Prompt    string

	// Instance that created this run
	CreatedByInstanceID string
}

// NewToolWorker creates a new tool worker.
func NewToolWorker(store ToolStore, executor ToolExecutor, cfg ToolWorkerConfig) *ToolWorker {
	if cfg.MaxConcurrent <= 0 {
		cfg.MaxConcurrent = 50
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 500 * time.Millisecond
	}
	if cfg.ClaimBatchSize <= 0 {
		cfg.ClaimBatchSize = 10
	}
	if cfg.DefaultTimeout <= 0 {
		cfg.DefaultTimeout = 5 * time.Minute
	}

	return &ToolWorker{
		config:   cfg,
		store:    store,
		executor: executor,
		activeCh: make(chan struct{}, cfg.MaxConcurrent),
	}
}

// Start starts the tool worker.
func (w *ToolWorker) Start(ctx context.Context) error {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return fmt.Errorf("tool worker already started")
	}
	w.running = true
	w.stopCh = make(chan struct{})
	w.mu.Unlock()

	w.wg.Add(1)
	go w.processLoop(ctx)

	if w.config.Logger != nil {
		w.config.Logger.Info("tool worker started",
			"instance_id", w.config.InstanceID,
			"max_concurrent", w.config.MaxConcurrent,
		)
	}

	return nil
}

// Stop stops the tool worker gracefully.
func (w *ToolWorker) Stop(ctx context.Context) error {
	w.mu.Lock()
	if !w.running {
		w.mu.Unlock()
		return nil
	}
	w.running = false
	close(w.stopCh)
	w.mu.Unlock()

	// Wait for in-progress work
	done := make(chan struct{})
	go func() {
		w.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		if w.config.Logger != nil {
			w.config.Logger.Info("tool worker stopped", "instance_id", w.config.InstanceID)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// processLoop is the main processing loop.
func (w *ToolWorker) processLoop(ctx context.Context) {
	defer w.wg.Done()

	ticker := time.NewTicker(w.config.PollInterval)
	defer ticker.Stop()

	// TODO: Implement LISTEN/NOTIFY subscription for 'agentpg_tool_pending'
	// For now, we poll only

	for {
		select {
		case <-w.stopCh:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.claimAndProcess(ctx)
		}
	}
}

// claimAndProcess claims pending tool executions and processes them.
func (w *ToolWorker) claimAndProcess(ctx context.Context) {
	// Determine how many slots are available
	available := w.config.MaxConcurrent - len(w.activeCh)
	if available <= 0 {
		return
	}

	claimCount := w.config.ClaimBatchSize
	if claimCount > available {
		claimCount = available
	}

	// Claim tool executions
	executions, err := w.store.ClaimToolExecutions(ctx, w.config.InstanceID, claimCount)
	if err != nil {
		if w.config.Logger != nil {
			w.config.Logger.Error("failed to claim tool executions", "error", err)
		}
		return
	}

	// Process each claimed execution
	for _, exec := range executions {
		w.wg.Add(1)
		w.activeCh <- struct{}{} // Acquire slot

		go func(e ToolExecutionRecord) {
			defer w.wg.Done()
			defer func() { <-w.activeCh }() // Release slot

			if err := w.processToolExecution(ctx, e); err != nil {
				if w.config.Logger != nil {
					w.config.Logger.Error("failed to process tool execution",
						"execution_id", e.ID,
						"tool_name", e.ToolName,
						"error", err,
					)
				}
			}
		}(exec)
	}
}

// processToolExecution processes a single tool execution.
func (w *ToolWorker) processToolExecution(ctx context.Context, exec ToolExecutionRecord) error {
	if exec.IsAgentTool {
		return w.processAgentToolExecution(ctx, exec)
	}
	return w.processRegularToolExecution(ctx, exec)
}

// processRegularToolExecution executes a regular (non-agent) tool.
func (w *ToolWorker) processRegularToolExecution(ctx context.Context, exec ToolExecutionRecord) error {
	// Check if tool is registered locally
	if !w.executor.HasTool(exec.ToolName) {
		return w.store.FailToolExecution(ctx, exec.ID,
			fmt.Sprintf("tool %q not registered on this instance", exec.ToolName))
	}

	// Create timeout context
	execCtx, cancel := context.WithTimeout(ctx, w.config.DefaultTimeout)
	defer cancel()

	// Execute the tool
	output, err := w.executor.Execute(execCtx, exec.ToolName, exec.ToolInput)

	if err != nil {
		// Tool execution failed
		errMsg := err.Error()
		if ctxErr := ctx.Err(); ctxErr != nil {
			errMsg = fmt.Sprintf("timeout: %v", ctxErr)
		}

		if w.config.Logger != nil {
			w.config.Logger.Warn("tool execution failed",
				"execution_id", exec.ID,
				"tool_name", exec.ToolName,
				"error", errMsg,
			)
		}

		// Mark as error (tool_result with is_error=true)
		return w.store.CompleteToolExecution(ctx, exec.ID, errMsg, true, &errMsg)
	}

	// Tool execution succeeded
	if w.config.Logger != nil {
		w.config.Logger.Debug("tool execution completed",
			"execution_id", exec.ID,
			"tool_name", exec.ToolName,
			"output_length", len(output),
		)
	}

	return w.store.CompleteToolExecution(ctx, exec.ID, output, false, nil)
}

// processAgentToolExecution handles agent-as-tool execution by creating a child run.
func (w *ToolWorker) processAgentToolExecution(ctx context.Context, exec ToolExecutionRecord) error {
	if exec.AgentName == nil {
		return w.store.FailToolExecution(ctx, exec.ID, "agent-as-tool missing agent_name")
	}

	// Get parent run to determine depth and session
	parentRun, err := w.store.GetRunByID(ctx, exec.RunID)
	if err != nil {
		return w.store.FailToolExecution(ctx, exec.ID,
			fmt.Sprintf("failed to get parent run: %v", err))
	}

	// Get agent definition for the child agent
	agentDef, err := w.store.GetAgentDefinition(ctx, *exec.AgentName)
	if err != nil {
		return w.store.FailToolExecution(ctx, exec.ID,
			fmt.Sprintf("agent %q not found: %v", *exec.AgentName, err))
	}

	// Extract prompt from tool input
	prompt, err := extractPromptFromInput(exec.ToolInput, agentDef)
	if err != nil {
		return w.store.FailToolExecution(ctx, exec.ID,
			fmt.Sprintf("failed to extract prompt: %v", err))
	}

	// Create child run
	childRunID, childSessionID, err := w.store.CreateChildRun(ctx, CreateChildRunOpts{
		ParentRunID:           exec.RunID,
		ParentSessionID:       parentRun.SessionID,
		ParentToolExecutionID: exec.ID,
		ParentDepth:           parentRun.Depth,
		AgentName:             *exec.AgentName,
		Prompt:                prompt,
		CreatedByInstanceID:   w.config.InstanceID,
	})
	if err != nil {
		return w.store.FailToolExecution(ctx, exec.ID,
			fmt.Sprintf("failed to create child run: %v", err))
	}

	// Update tool execution with child run ID
	// The execution stays in 'running' state until child run completes
	// (handled by database trigger: trg_child_run_complete)
	if err := w.store.UpdateToolExecutionChildRun(ctx, exec.ID, childRunID); err != nil {
		return fmt.Errorf("failed to update tool execution: %w", err)
	}

	if w.config.Logger != nil {
		w.config.Logger.Info("created child run for agent-as-tool",
			"execution_id", exec.ID,
			"child_run_id", childRunID,
			"child_session_id", childSessionID,
			"agent_name", *exec.AgentName,
			"depth", parentRun.Depth+1,
		)
	}

	return nil
}

// extractPromptFromInput extracts the user prompt from tool input.
// Agent-as-tool typically has a standard input schema with a "prompt" or "task" field.
func extractPromptFromInput(input json.RawMessage, agent *AgentRecord) (string, error) {
	var inputMap map[string]interface{}
	if err := json.Unmarshal(input, &inputMap); err != nil {
		return "", fmt.Errorf("invalid tool input JSON: %w", err)
	}

	// Try common field names for the prompt
	for _, field := range []string{"prompt", "task", "request", "message", "input"} {
		if val, ok := inputMap[field]; ok {
			if str, ok := val.(string); ok {
				return str, nil
			}
		}
	}

	// If no standard field found, use the entire input as JSON string
	return string(input), nil
}

// =============================================================================
// TOOL EXECUTION RECORD
// =============================================================================

// ToolExecutionRecord represents a tool execution row from the database.
type ToolExecutionRecord struct {
	ID                  uuid.UUID
	RunID               uuid.UUID
	IterationID         uuid.UUID
	State               ToolExecutionState
	ToolUseID           string
	ToolName            string
	ToolInput           json.RawMessage
	IsAgentTool         bool
	AgentName           *string
	ChildRunID          *uuid.UUID
	ToolOutput          *string
	IsError             bool
	ErrorMessage        *string
	ClaimedByInstanceID *string
	ClaimedAt           *time.Time
	AttemptCount        int
	MaxAttempts         int
	CreatedAt           time.Time
	StartedAt           *time.Time
	CompletedAt         *time.Time
}

// ToolExecutionState represents the state of a tool execution.
type ToolExecutionState string

const (
	ToolExecutionStatePending   ToolExecutionState = "pending"
	ToolExecutionStateRunning   ToolExecutionState = "running"
	ToolExecutionStateCompleted ToolExecutionState = "completed"
	ToolExecutionStateFailed    ToolExecutionState = "failed"
	ToolExecutionStateSkipped   ToolExecutionState = "skipped"
)
