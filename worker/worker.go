// Package worker provides background workers for processing runs and tool executions.
//
// The worker system provides event-driven execution of runs:
//   - RunWorker picks up pending runs, calls the Claude API, and manages state transitions
//   - ToolWorker picks up pending tool executions and runs them in parallel
//   - Uses PostgreSQL LISTEN/NOTIFY for real-time events (pgx) or polling (database/sql)
//
// Workers are embedded in the Client and start automatically with Client.Start().
package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/google/uuid"
	"github.com/youssefsiam38/agentpg/notifier"
	"github.com/youssefsiam38/agentpg/runstate"
	"github.com/youssefsiam38/agentpg/storage"
	"github.com/youssefsiam38/agentpg/tool"
)

// Config holds configuration for workers.
type Config struct {
	// InstanceID is the unique identifier for this worker instance.
	InstanceID string

	// MaxConcurrentRuns limits how many runs can be processed simultaneously.
	// Default: 10
	MaxConcurrentRuns int

	// MaxConcurrentTools limits how many tool executions can run simultaneously.
	// Default: 50 (tools from different runs can run in parallel)
	MaxConcurrentTools int

	// PollInterval is how often to poll for work when not using LISTEN/NOTIFY.
	// Default: 5s
	PollInterval time.Duration

	// ClaimTimeout is how long a claim is valid before considered stale.
	// Default: 5m
	ClaimTimeout time.Duration

	// ToolExecutionTimeout is the default timeout for tool execution.
	// Default: 30s
	ToolExecutionTimeout time.Duration

	// MaxToolRetries is the maximum number of retries for failed tool executions.
	// Default: 3
	MaxToolRetries int

	// OnError is called when an error occurs during processing.
	OnError func(err error)

	// OnRunComplete is called when a run reaches a terminal state.
	OnRunComplete func(runID string, state runstate.RunState)
}

// DefaultConfig returns the default worker configuration.
func DefaultConfig() *Config {
	return &Config{
		MaxConcurrentRuns:    10,
		MaxConcurrentTools:   50,
		PollInterval:         5 * time.Second,
		ClaimTimeout:         5 * time.Minute,
		ToolExecutionTimeout: 5 * time.Minute, // Increased for nested agent tools that make API calls
		MaxToolRetries:       3,
	}
}

// Worker processes runs and tool executions.
type Worker struct {
	store        storage.Store
	client       *anthropic.Client
	toolRegistry *tool.Registry
	notifier     *notifier.Notifier
	config       *Config

	// Semaphores for concurrency control
	runSem  chan struct{}
	toolSem chan struct{}

	// State
	started atomic.Bool
	done    chan struct{}
	cancel  context.CancelFunc
	wg      sync.WaitGroup

	// API call builder (injected from agent)
	apiCallBuilder APICallBuilder
}

// APICallBuilder builds API call parameters for a run.
// This is injected by the agent to allow custom configuration.
type APICallBuilder interface {
	// BuildAPIRequest builds the API request for a run.
	BuildAPIRequest(ctx context.Context, run *storage.Run, messages []*storage.Message) (*anthropic.MessageNewParams, error)

	// ProcessResponse processes the API response and returns the next state.
	ProcessResponse(ctx context.Context, run *storage.Run, response *anthropic.Message) (*ProcessResult, error)
}

// ProcessResult contains the result of processing an API response.
type ProcessResult struct {
	// NextState is the next run state.
	NextState runstate.RunState

	// AssistantMessage is the message to save.
	AssistantMessage *storage.Message

	// ContentBlocks are the content blocks to save.
	ContentBlocks []*storage.ContentBlock

	// ToolExecutions are the tool executions to create (if stop_reason is tool_use).
	ToolExecutions []*storage.CreateToolExecutionParams

	// StopReason from the API response.
	StopReason string

	// ResponseText extracted from the response.
	ResponseText string

	// Usage information from the API.
	InputTokens  int
	OutputTokens int
}

// New creates a new worker.
func New(
	store storage.Store,
	client *anthropic.Client,
	toolRegistry *tool.Registry,
	notif *notifier.Notifier,
	config *Config,
) *Worker {
	// Start with defaults and merge user config
	cfg := DefaultConfig()
	if config != nil {
		// Override with non-zero values from user config
		if config.InstanceID != "" {
			cfg.InstanceID = config.InstanceID
		}
		if config.MaxConcurrentRuns > 0 {
			cfg.MaxConcurrentRuns = config.MaxConcurrentRuns
		}
		if config.MaxConcurrentTools > 0 {
			cfg.MaxConcurrentTools = config.MaxConcurrentTools
		}
		if config.PollInterval > 0 {
			cfg.PollInterval = config.PollInterval
		}
		if config.ClaimTimeout > 0 {
			cfg.ClaimTimeout = config.ClaimTimeout
		}
		if config.ToolExecutionTimeout > 0 {
			cfg.ToolExecutionTimeout = config.ToolExecutionTimeout
		}
		if config.MaxToolRetries > 0 {
			cfg.MaxToolRetries = config.MaxToolRetries
		}
		if config.OnError != nil {
			cfg.OnError = config.OnError
		}
		if config.OnRunComplete != nil {
			cfg.OnRunComplete = config.OnRunComplete
		}
	}

	return &Worker{
		store:        store,
		client:       client,
		toolRegistry: toolRegistry,
		notifier:     notif,
		config:       cfg,
		runSem:       make(chan struct{}, cfg.MaxConcurrentRuns),
		toolSem:      make(chan struct{}, cfg.MaxConcurrentTools),
		done:         make(chan struct{}),
	}
}

// SetAPICallBuilder sets the API call builder.
func (w *Worker) SetAPICallBuilder(builder APICallBuilder) {
	w.apiCallBuilder = builder
}

// Start begins processing runs and tool executions.
func (w *Worker) Start(ctx context.Context) error {
	if !w.started.CompareAndSwap(false, true) {
		return fmt.Errorf("worker already started")
	}

	ctx, w.cancel = context.WithCancel(ctx)

	// Subscribe to events if notifier is available
	if w.notifier != nil && w.notifier.IsRunning() {
		w.subscribeToEvents()
	}

	// Start polling goroutines
	w.wg.Add(2)
	go w.runPollingLoop(ctx)
	go w.toolPollingLoop(ctx)

	return nil
}

// Stop stops the worker gracefully.
func (w *Worker) Stop(ctx context.Context) error {
	if !w.started.Load() {
		return nil
	}

	w.cancel()

	// Wait for goroutines with timeout
	done := make(chan struct{})
	go func() {
		w.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-ctx.Done():
		return ctx.Err()
	}

	close(w.done)
	w.started.Store(false)
	return nil
}

// subscribeToEvents sets up event subscriptions.
func (w *Worker) subscribeToEvents() {
	// Subscribe to run created events
	w.notifier.Subscribe(notifier.EventRunCreated, func(event *notifier.Event) {
		w.wg.Add(1)
		go func() {
			defer w.wg.Done()
			w.handleRunCreated(event)
		}()
	})

	// Subscribe to tool pending events
	w.notifier.Subscribe(notifier.EventToolPending, func(event *notifier.Event) {
		w.wg.Add(1)
		go func() {
			defer w.wg.Done()
			w.handleToolPending(event)
		}()
	})

	// Subscribe to tools complete events
	w.notifier.Subscribe(notifier.EventRunToolsComplete, func(event *notifier.Event) {
		w.wg.Add(1)
		go func() {
			defer w.wg.Done()
			w.handleToolsComplete(event)
		}()
	})
}

// handleRunCreated processes a run created event.
func (w *Worker) handleRunCreated(event *notifier.Event) {
	var payload struct {
		RunID     string `json:"run_id"`
		SessionID string `json:"session_id"`
		AgentID   string `json:"agent_id"`
	}

	if err := json.Unmarshal([]byte(event.Payload), &payload); err != nil {
		w.logError(fmt.Errorf("failed to parse run_created payload: %w", err))
		return
	}

	// Try to claim and process the run
	ctx := context.Background()
	if err := w.claimAndProcessRun(ctx, payload.RunID); err != nil {
		w.logError(fmt.Errorf("failed to process run %s: %w", payload.RunID, err))
	}
}

// handleToolPending processes a tool pending event.
func (w *Worker) handleToolPending(event *notifier.Event) {
	var payload struct {
		ExecutionID string `json:"execution_id"`
		RunID       string `json:"run_id"`
		ToolName    string `json:"tool_name"`
	}

	if err := json.Unmarshal([]byte(event.Payload), &payload); err != nil {
		w.logError(fmt.Errorf("failed to parse tool_pending payload: %w", err))
		return
	}

	// Try to claim and process the tool execution
	ctx := context.Background()
	if err := w.claimAndProcessToolExecution(ctx, payload.ExecutionID); err != nil {
		w.logError(fmt.Errorf("failed to process tool execution %s: %w", payload.ExecutionID, err))
	}
}

// handleToolsComplete processes a tools complete event.
func (w *Worker) handleToolsComplete(event *notifier.Event) {
	var payload struct {
		RunID          string `json:"run_id"`
		CompletedCount int    `json:"completed_count"`
		FailedCount    int    `json:"failed_count"`
	}

	if err := json.Unmarshal([]byte(event.Payload), &payload); err != nil {
		w.logError(fmt.Errorf("failed to parse tools_complete payload: %w", err))
		return
	}

	// Continue the run with tool results
	ctx := context.Background()
	if err := w.continueRunWithToolResults(ctx, payload.RunID); err != nil {
		w.logError(fmt.Errorf("failed to continue run %s with tool results: %w", payload.RunID, err))
	}
}

// runPollingLoop polls for pending runs.
func (w *Worker) runPollingLoop(ctx context.Context) {
	defer w.wg.Done()

	ticker := time.NewTicker(w.config.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.pollPendingRuns(ctx)
		}
	}
}

// pollPendingRuns finds and processes pending runs.
func (w *Worker) pollPendingRuns(ctx context.Context) {
	// Get pending runs
	runs, err := w.store.GetPendingRuns(ctx, []runstate.RunState{
		runstate.RunStatePending,
		runstate.RunStateAwaitingContinuation,
	}, w.config.MaxConcurrentRuns)
	if err != nil {
		w.logError(fmt.Errorf("failed to get pending runs: %w", err))
		return
	}

	for _, run := range runs {
		// Try to acquire semaphore without blocking
		select {
		case w.runSem <- struct{}{}:
			w.wg.Add(1)
			go func(runID string) {
				defer w.wg.Done()
				defer func() { <-w.runSem }()

				if err := w.claimAndProcessRun(ctx, runID); err != nil {
					w.logError(fmt.Errorf("failed to process run %s: %w", runID, err))
				}
			}(run.ID)
		default:
			// All slots are full, skip this run
			return
		}
	}
}

// toolPollingLoop polls for pending tool executions.
func (w *Worker) toolPollingLoop(ctx context.Context) {
	defer w.wg.Done()

	ticker := time.NewTicker(w.config.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.pollPendingToolExecutions(ctx)
		}
	}
}

// pollPendingToolExecutions finds and processes pending tool executions.
func (w *Worker) pollPendingToolExecutions(ctx context.Context) {
	// Get pending tool executions
	executions, err := w.store.GetPendingToolExecutions(ctx, w.config.MaxConcurrentTools)
	if err != nil {
		w.logError(fmt.Errorf("failed to get pending tool executions: %w", err))
		return
	}

	for _, exec := range executions {
		// Try to acquire semaphore without blocking
		select {
		case w.toolSem <- struct{}{}:
			w.wg.Add(1)
			go func(execID string) {
				defer w.wg.Done()
				defer func() { <-w.toolSem }()

				if err := w.claimAndProcessToolExecution(ctx, execID); err != nil {
					w.logError(fmt.Errorf("failed to process tool execution %s: %w", execID, err))
				}
			}(exec.ID)
		default:
			// All slots are full, skip
			return
		}
	}
}

// claimAndProcessRun claims a run and processes it.
func (w *Worker) claimAndProcessRun(ctx context.Context, runID string) error {
	// Try to claim the run
	claimed, err := w.store.ClaimRun(ctx, runID, w.config.InstanceID)
	if err != nil {
		return fmt.Errorf("failed to claim run: %w", err)
	}
	if !claimed {
		return nil // Another worker claimed it
	}

	// Process the run
	return w.processRun(ctx, runID)
}

// processRun processes a claimed run.
func (w *Worker) processRun(ctx context.Context, runID string) error {
	// Get the run
	run, err := w.store.GetRun(ctx, runID)
	if err != nil {
		return fmt.Errorf("failed to get run: %w", err)
	}

	// Check if we should make an API call
	// Note: ClaimRun sets state to pending_api, so we also check for that state
	if run.State == runstate.RunStatePending || run.State == runstate.RunStatePendingAPI || run.State == runstate.RunStateAwaitingContinuation {
		return w.makeAPICall(ctx, run)
	}

	// Check if we're waiting for tools
	if run.State == runstate.RunStatePendingTools {
		// Check if all tools are complete
		complete, err := w.store.AreAllToolExecutionsComplete(ctx, runID)
		if err != nil {
			return fmt.Errorf("failed to check tool completion: %w", err)
		}
		if complete {
			return w.continueRunWithToolResults(ctx, runID)
		}
	}

	return nil
}

// makeAPICall makes the Claude API call for a run.
func (w *Worker) makeAPICall(ctx context.Context, run *storage.Run) error {
	if w.apiCallBuilder == nil {
		return fmt.Errorf("no API call builder configured")
	}

	// Update state to pending_api
	if run.State == runstate.RunStatePending {
		if err := w.store.UpdateRunState(ctx, run.ID, &storage.UpdateRunStateParams{
			State: runstate.RunStatePendingAPI,
		}); err != nil {
			return fmt.Errorf("failed to update run state: %w", err)
		}
		run.State = runstate.RunStatePendingAPI
	}

	// Get messages for this run's session
	messages, err := w.store.GetMessages(ctx, run.SessionID)
	if err != nil {
		return fmt.Errorf("failed to get messages: %w", err)
	}

	// Load content blocks for each message
	for _, msg := range messages {
		blocks, blockErr := w.store.GetMessageContentBlocks(ctx, msg.ID)
		if blockErr != nil {
			return fmt.Errorf("failed to get content blocks for message %s: %w", msg.ID, blockErr)
		}
		msg.ContentBlocks = blocks
	}

	// Build API request
	params, err := w.apiCallBuilder.BuildAPIRequest(ctx, run, messages)
	if err != nil {
		return w.failRun(ctx, run.ID, "api", err.Error())
	}

	// Make API call
	response, err := w.client.Messages.New(ctx, *params)
	if err != nil {
		return w.failRun(ctx, run.ID, "api", err.Error())
	}

	// Process response
	result, err := w.apiCallBuilder.ProcessResponse(ctx, run, response)
	if err != nil {
		return w.failRun(ctx, run.ID, "internal", err.Error())
	}

	// Save assistant message
	if result.AssistantMessage != nil {
		if err := w.store.SaveMessage(ctx, result.AssistantMessage); err != nil {
			return fmt.Errorf("failed to save assistant message: %w", err)
		}
	}

	// Save content blocks
	if len(result.ContentBlocks) > 0 {
		if err := w.store.SaveContentBlocks(ctx, result.ContentBlocks); err != nil {
			return fmt.Errorf("failed to save content blocks: %w", err)
		}
	}

	// Create tool executions if needed
	if len(result.ToolExecutions) > 0 {
		if err := w.store.CreateToolExecutions(ctx, result.ToolExecutions); err != nil {
			return fmt.Errorf("failed to create tool executions: %w", err)
		}
	}

	// Update run iteration (token counts)
	iterParams := &storage.UpdateRunIterationParams{
		IncrementIteration: true,
		InputTokens:        result.InputTokens,
		OutputTokens:       result.OutputTokens,
		LastAPICallAt:      time.Now(),
	}

	if err := w.store.UpdateRunIteration(ctx, run.ID, iterParams); err != nil {
		return fmt.Errorf("failed to update run iteration: %w", err)
	}

	// Update run state
	stateParams := &storage.UpdateRunStateParams{
		State:        result.NextState,
		StopReason:   &result.StopReason,
		InputTokens:  result.InputTokens,
		OutputTokens: result.OutputTokens,
	}

	if result.NextState.IsTerminal() {
		stateParams.ResponseText = &result.ResponseText
	}

	if err := w.store.UpdateRunState(ctx, run.ID, stateParams); err != nil {
		return fmt.Errorf("failed to update run state: %w", err)
	}

	// Notify on completion
	if result.NextState.IsTerminal() && w.config.OnRunComplete != nil {
		w.config.OnRunComplete(run.ID, result.NextState)
	}

	return nil
}

// continueRunWithToolResults creates a tool result message and continues the run.
func (w *Worker) continueRunWithToolResults(ctx context.Context, runID string) error {
	// Atomically transition from pending_tools to pending_api to prevent race conditions
	// in distributed deployments. Only one worker will succeed.
	err := w.store.UpdateRunState(ctx, runID, &storage.UpdateRunStateParams{
		State:         runstate.RunStatePendingAPI,
		RequiredState: runstate.RunStatePendingTools, // Only transition if in pending_tools
	})
	if err != nil {
		if errors.Is(err, storage.ErrStateTransitionFailed) {
			// Another worker already transitioned this run, skip silently
			return nil
		}
		// Unexpected error
		return fmt.Errorf("failed atomic state transition: %w", err)
	}

	// Get all completed tool executions
	executions, err := w.store.GetCompletedToolExecutions(ctx, runID)
	if err != nil {
		return fmt.Errorf("failed to get tool executions: %w", err)
	}

	if len(executions) == 0 {
		return nil // Nothing to do
	}

	// Get the run
	run, err := w.store.GetRun(ctx, runID)
	if err != nil {
		return fmt.Errorf("failed to get run: %w", err)
	}

	// Create user message with tool results
	messageID := uuid.New().String()
	userMsg := &storage.Message{
		ID:        messageID,
		SessionID: run.SessionID,
		RunID:     &runID,
		Role:      "user",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	// Create tool result content blocks
	contentBlocks := make([]*storage.ContentBlock, len(executions))
	for i, exec := range executions {
		content := exec.ToolOutput
		if content == nil {
			empty := ""
			content = &empty
		}
		isError := exec.ErrorMessage != nil

		// Look up the content block to get Claude's tool_use_id
		toolUseBlock, err := w.store.GetContentBlock(ctx, exec.ToolUseBlockID)
		if err != nil {
			return fmt.Errorf("failed to get tool use block: %w", err)
		}
		if toolUseBlock.ToolUseID == nil {
			return fmt.Errorf("tool use block missing tool_use_id: %s", exec.ToolUseBlockID)
		}

		// Store internal block ID for DB, but also store Claude's tool_use_id for API
		toolUseBlockID := exec.ToolUseBlockID
		contentBlocks[i] = &storage.ContentBlock{
			ID:              uuid.New().String(),
			MessageID:       messageID,
			BlockIndex:      i,
			Type:            storage.ContentBlockTypeToolResult,
			ToolResultForID: &toolUseBlockID,        // Internal UUID for DB foreign key
			ToolUseID:       toolUseBlock.ToolUseID, // Claude's tool_use_id for API
			ToolContent:     content,
			IsError:         isError,
			CreatedAt:       time.Now(),
		}
	}

	if err := w.store.SaveMessage(ctx, userMsg); err != nil {
		return fmt.Errorf("failed to save tool result message: %w", err)
	}

	if err := w.store.SaveContentBlocks(ctx, contentBlocks); err != nil {
		return fmt.Errorf("failed to save tool result content blocks: %w", err)
	}

	// Link tool executions to their result blocks
	for i, exec := range executions {
		if err := w.store.LinkToolExecutionToResultBlock(ctx, exec.ID, contentBlocks[i].ID); err != nil {
			// Log but don't fail - the tool result was already saved
			log.Printf("agentpg/worker: failed to link tool execution %s to result block: %v", exec.ID, err)
		}
	}

	// State already transitioned at start, process the run (will make API call)
	return w.processRun(ctx, runID)
}

// claimAndProcessToolExecution claims and processes a tool execution.
func (w *Worker) claimAndProcessToolExecution(ctx context.Context, execID string) error {
	// Try to claim
	claimed, err := w.store.ClaimToolExecution(ctx, execID, w.config.InstanceID)
	if err != nil {
		return fmt.Errorf("failed to claim tool execution: %w", err)
	}
	if !claimed {
		return nil // Another worker claimed it
	}

	return w.processToolExecution(ctx, execID)
}

// processToolExecution executes a tool and updates the result.
func (w *Worker) processToolExecution(ctx context.Context, execID string) error {
	// Get the tool execution by ID
	exec, err := w.store.GetToolExecution(ctx, execID)
	if err != nil {
		return fmt.Errorf("failed to get tool execution: %w", err)
	}

	// Create execution context with timeout
	execCtx, cancel := context.WithTimeout(ctx, w.config.ToolExecutionTimeout)
	defer cancel()

	// Get the tool
	t, exists := w.toolRegistry.Get(exec.ToolName)
	if !exists {
		errMsg := fmt.Sprintf("tool not found: %s", exec.ToolName)
		return w.store.UpdateToolExecutionState(ctx, execID, &storage.UpdateToolExecutionStateParams{
			State:        runstate.ToolExecFailed,
			ErrorMessage: &errMsg,
		})
	}

	// Marshal tool input
	input, err := json.Marshal(exec.ToolInput)
	if err != nil {
		errMsg := fmt.Sprintf("failed to marshal tool input: %v", err)
		return w.store.UpdateToolExecutionState(ctx, execID, &storage.UpdateToolExecutionStateParams{
			State:        runstate.ToolExecFailed,
			ErrorMessage: &errMsg,
		})
	}

	// Execute the tool
	output, execErr := t.Execute(execCtx, input)

	// Update execution state
	updateParams := &storage.UpdateToolExecutionStateParams{
		State: runstate.ToolExecCompleted,
	}

	if execErr != nil {
		updateParams.State = runstate.ToolExecFailed
		errMsg := execErr.Error()
		updateParams.ErrorMessage = &errMsg
		updateParams.ToolOutput = &output // Include partial output if any
	} else {
		updateParams.ToolOutput = &output
	}

	if updateErr := w.store.UpdateToolExecutionState(ctx, execID, updateParams); updateErr != nil {
		return fmt.Errorf("failed to update tool execution state: %w", updateErr)
	}

	// Check if all tools for this run are complete
	complete, completeErr := w.store.AreAllToolExecutionsComplete(ctx, exec.RunID)
	if completeErr != nil {
		return fmt.Errorf("failed to check tool completion: %w", completeErr)
	}

	if complete {
		// All tools done - notify or continue
		if w.notifier != nil && w.notifier.IsRunning() {
			payload := `{"run_id":"` + exec.RunID + `"}`
			if err := w.notifier.Notify(ctx, notifier.EventRunToolsComplete, payload); err != nil {
				// Fallback to direct processing
				return w.continueRunWithToolResults(ctx, exec.RunID)
			}
		} else {
			// Direct processing
			return w.continueRunWithToolResults(ctx, exec.RunID)
		}
	}

	return nil
}

// failRun marks a run as failed.
func (w *Worker) failRun(ctx context.Context, runID string, errorType string, errorMsg string) error {
	if err := w.store.UpdateRunState(ctx, runID, &storage.UpdateRunStateParams{
		State:        runstate.RunStateFailed,
		ErrorType:    &errorType,
		ErrorMessage: &errorMsg,
	}); err != nil {
		return fmt.Errorf("failed to fail run: %w", err)
	}

	if w.config.OnRunComplete != nil {
		w.config.OnRunComplete(runID, runstate.RunStateFailed)
	}

	return fmt.Errorf("%s error: %s", errorType, errorMsg)
}

// logError logs an error using the configured handler or default logger.
func (w *Worker) logError(err error) {
	if w.config.OnError != nil {
		w.config.OnError(err)
	} else {
		log.Printf("agentpg/worker: %v", err)
	}
}

// IsRunning returns true if the worker is running.
func (w *Worker) IsRunning() bool {
	return w.started.Load()
}
