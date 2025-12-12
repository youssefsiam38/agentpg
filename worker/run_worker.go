// Package worker provides background workers for processing runs and tools.
//
// The worker package implements the core event-driven processing loop:
//
//  1. RunWorker: Claims pending runs, submits to Claude Batch API
//  2. BatchPoller: Polls for batch completion, processes responses
//  3. ToolWorker: Executes tools, handles agent-as-tool child runs
//
// Workers use PostgreSQL LISTEN/NOTIFY for real-time events with polling fallback.
// Race-safe claiming is achieved using SELECT FOR UPDATE SKIP LOCKED.
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
// RUN WORKER
// =============================================================================

// RunWorkerConfig holds configuration for the run worker.
type RunWorkerConfig struct {
	// InstanceID is this worker's instance identifier.
	InstanceID string

	// MaxConcurrent is the maximum concurrent runs to process.
	MaxConcurrent int

	// PollInterval is how often to poll when LISTEN/NOTIFY is unavailable.
	PollInterval time.Duration

	// ClaimBatchSize is how many runs to claim at once.
	ClaimBatchSize int

	// Logger for structured logging.
	Logger Logger
}

// Logger interface for worker logging.
type Logger interface {
	Debug(msg string, args ...any)
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
}

// RunWorker processes pending runs by submitting them to Claude Batch API.
//
// Flow:
//  1. Listen for 'agentpg_run_created' notifications (or poll)
//  2. Claim pending runs using agentpg_claim_runs() stored procedure
//  3. Build batch request with conversation history
//  4. Submit to Claude Batch API
//  5. Update run state and create iteration record
//  6. BatchPoller takes over for completion handling
type RunWorker struct {
	config   RunWorkerConfig
	store    RunStore
	batchAPI BatchAPI

	// Runtime state
	mu       sync.Mutex
	running  bool
	stopCh   chan struct{}
	wg       sync.WaitGroup
	activeCh chan struct{} // Semaphore for concurrency limiting
}

// RunStore defines the database operations needed by RunWorker.
type RunStore interface {
	// ClaimRuns claims pending runs for this instance.
	// Uses SELECT FOR UPDATE SKIP LOCKED for race safety.
	ClaimRuns(ctx context.Context, instanceID string, maxCount int) ([]RunRecord, error)

	// UpdateRunState updates a run's state.
	UpdateRunState(ctx context.Context, runID uuid.UUID, state RunState, opts UpdateRunOpts) error

	// CreateIteration creates a new iteration record for a run.
	CreateIteration(ctx context.Context, iter IterationRecord) error

	// GetSessionMessages retrieves conversation history for a session.
	GetSessionMessages(ctx context.Context, sessionID uuid.UUID) ([]MessageRecord, error)

	// GetAgentDefinition retrieves an agent's configuration.
	GetAgentDefinition(ctx context.Context, agentName string) (*AgentRecord, error)

	// GetAgentTools retrieves tool definitions for an agent.
	GetAgentTools(ctx context.Context, agentName string) ([]ToolRecord, error)
}

// BatchAPI defines the Claude Batch API operations.
type BatchAPI interface {
	// SubmitBatch submits a batch request to Claude.
	// Returns the batch ID and our request ID (custom_id).
	SubmitBatch(ctx context.Context, req BatchRequest) (batchID string, requestID string, err error)
}

// UpdateRunOpts holds optional fields for run state updates.
type UpdateRunOpts struct {
	BatchID            *string
	BatchRequestID     *string
	CurrentIterationID *uuid.UUID
	ErrorMessage       *string
	ErrorType          *string
	FinalizedAt        *time.Time
}

// NewRunWorker creates a new run worker.
func NewRunWorker(store RunStore, batchAPI BatchAPI, cfg RunWorkerConfig) *RunWorker {
	if cfg.MaxConcurrent <= 0 {
		cfg.MaxConcurrent = 10
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 1 * time.Second
	}
	if cfg.ClaimBatchSize <= 0 {
		cfg.ClaimBatchSize = 5
	}

	return &RunWorker{
		config:   cfg,
		store:    store,
		batchAPI: batchAPI,
		activeCh: make(chan struct{}, cfg.MaxConcurrent),
	}
}

// Start starts the run worker.
func (w *RunWorker) Start(ctx context.Context) error {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return fmt.Errorf("run worker already started")
	}
	w.running = true
	w.stopCh = make(chan struct{})
	w.mu.Unlock()

	w.wg.Add(1)
	go w.processLoop(ctx)

	if w.config.Logger != nil {
		w.config.Logger.Info("run worker started",
			"instance_id", w.config.InstanceID,
			"max_concurrent", w.config.MaxConcurrent,
		)
	}

	return nil
}

// Stop stops the run worker gracefully.
func (w *RunWorker) Stop(ctx context.Context) error {
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
			w.config.Logger.Info("run worker stopped", "instance_id", w.config.InstanceID)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// processLoop is the main processing loop.
func (w *RunWorker) processLoop(ctx context.Context) {
	defer w.wg.Done()

	ticker := time.NewTicker(w.config.PollInterval)
	defer ticker.Stop()

	// TODO: Implement LISTEN/NOTIFY subscription
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

// claimAndProcess claims pending runs and processes them.
func (w *RunWorker) claimAndProcess(ctx context.Context) {
	// Determine how many slots are available
	available := w.config.MaxConcurrent - len(w.activeCh)
	if available <= 0 {
		return
	}

	claimCount := w.config.ClaimBatchSize
	if claimCount > available {
		claimCount = available
	}

	// Claim runs
	runs, err := w.store.ClaimRuns(ctx, w.config.InstanceID, claimCount)
	if err != nil {
		if w.config.Logger != nil {
			w.config.Logger.Error("failed to claim runs", "error", err)
		}
		return
	}

	// Process each claimed run
	for _, run := range runs {
		w.wg.Add(1)
		w.activeCh <- struct{}{} // Acquire slot

		go func(r RunRecord) {
			defer w.wg.Done()
			defer func() { <-w.activeCh }() // Release slot

			if err := w.processRun(ctx, r); err != nil {
				if w.config.Logger != nil {
					w.config.Logger.Error("failed to process run",
						"run_id", r.ID,
						"error", err,
					)
				}
			}
		}(run)
	}
}

// processRun processes a single claimed run.
func (w *RunWorker) processRun(ctx context.Context, run RunRecord) error {
	// Get agent definition
	agent, err := w.store.GetAgentDefinition(ctx, run.AgentName)
	if err != nil {
		return w.failRun(ctx, run.ID, "agent_not_found", err.Error())
	}

	// Get agent tools
	tools, err := w.store.GetAgentTools(ctx, run.AgentName)
	if err != nil {
		return w.failRun(ctx, run.ID, "tools_error", err.Error())
	}

	// Get conversation history
	messages, err := w.store.GetSessionMessages(ctx, run.SessionID)
	if err != nil {
		return w.failRun(ctx, run.ID, "messages_error", err.Error())
	}

	// Build batch request
	batchReq := BuildBatchRequest(agent, tools, messages, run)

	// Submit to Batch API
	batchID, requestID, err := w.batchAPI.SubmitBatch(ctx, batchReq)
	if err != nil {
		return w.failRun(ctx, run.ID, "batch_submit_error", err.Error())
	}

	// Create iteration record
	iterID := uuid.New()
	iter := IterationRecord{
		ID:              iterID,
		RunID:           run.ID,
		IterationNumber: run.CurrentIteration + 1,
		BatchID:         batchID,
		BatchRequestID:  requestID,
		BatchStatus:     BatchStatusInProgress,
		TriggerType:     determineTriggerType(run),
		BatchSubmittedAt: func() *time.Time { t := time.Now(); return &t }(),
		BatchExpiresAt: func() *time.Time {
			t := time.Now().Add(24 * time.Hour)
			return &t
		}(),
	}

	if err := w.store.CreateIteration(ctx, iter); err != nil {
		return w.failRun(ctx, run.ID, "iteration_create_error", err.Error())
	}

	// Update run state to batch_pending
	if err := w.store.UpdateRunState(ctx, run.ID, RunStateBatchPending, UpdateRunOpts{
		BatchID:            &batchID,
		BatchRequestID:     &requestID,
		CurrentIterationID: &iterID,
	}); err != nil {
		return fmt.Errorf("update run state: %w", err)
	}

	if w.config.Logger != nil {
		w.config.Logger.Info("submitted batch",
			"run_id", run.ID,
			"batch_id", batchID,
			"iteration", iter.IterationNumber,
		)
	}

	return nil
}

// failRun marks a run as failed.
func (w *RunWorker) failRun(ctx context.Context, runID uuid.UUID, errorType, errorMsg string) error {
	now := time.Now()
	return w.store.UpdateRunState(ctx, runID, RunStateFailed, UpdateRunOpts{
		ErrorType:    &errorType,
		ErrorMessage: &errorMsg,
		FinalizedAt:  &now,
	})
}

// determineTriggerType determines what triggered this iteration.
func determineTriggerType(run RunRecord) string {
	if run.CurrentIteration == 0 {
		return "user_prompt"
	}
	return "tool_results"
}

// =============================================================================
// RECORDS AND TYPES
// =============================================================================

// RunState represents the state of a run.
type RunState string

const (
	RunStatePending         RunState = "pending"
	RunStateBatchSubmitting RunState = "batch_submitting"
	RunStateBatchPending    RunState = "batch_pending"
	RunStateBatchProcessing RunState = "batch_processing"
	RunStatePendingTools    RunState = "pending_tools"
	RunStateAwaitingInput   RunState = "awaiting_input"
	RunStateCompleted       RunState = "completed"
	RunStateCancelled       RunState = "cancelled"
	RunStateFailed          RunState = "failed"
)

// BatchStatus represents the status of a Claude Batch API request.
type BatchStatus string

const (
	BatchStatusInProgress BatchStatus = "in_progress"
	BatchStatusCanceling  BatchStatus = "canceling"
	BatchStatusEnded      BatchStatus = "ended"
)

// RunRecord represents a run row from the database.
type RunRecord struct {
	ID                    uuid.UUID
	SessionID             uuid.UUID
	AgentName             string
	ParentRunID           *uuid.UUID
	ParentToolExecutionID *uuid.UUID
	Depth                 int
	State                 RunState
	Prompt                string
	CurrentIteration      int
	CurrentIterationID    *uuid.UUID
	ResponseText          *string
	StopReason            *string
	InputTokens           int
	OutputTokens          int
	IterationCount        int
	ToolIterations        int
	ErrorMessage          *string
	ErrorType             *string
	CreatedByInstanceID   *string
	ClaimedByInstanceID   *string
	ClaimedAt             *time.Time
	Metadata              json.RawMessage
	CreatedAt             time.Time
	StartedAt             *time.Time
	FinalizedAt           *time.Time
}

// IterationRecord represents an iteration row from the database.
type IterationRecord struct {
	ID                       uuid.UUID
	RunID                    uuid.UUID
	IterationNumber          int
	BatchID                  string
	BatchRequestID           string
	BatchStatus              BatchStatus
	BatchSubmittedAt         *time.Time
	BatchCompletedAt         *time.Time
	BatchExpiresAt           *time.Time
	BatchPollCount           int
	BatchLastPollAt          *time.Time
	TriggerType              string
	RequestMessageIDs        json.RawMessage
	StopReason               *string
	ResponseMessageID        *uuid.UUID
	HasToolUse               bool
	ToolExecutionCount       int
	InputTokens              int
	OutputTokens             int
	CacheCreationInputTokens int
	CacheReadInputTokens     int
	ErrorMessage             *string
	ErrorType                *string
	CreatedAt                time.Time
	StartedAt                *time.Time
	CompletedAt              *time.Time
}

// AgentRecord represents an agent row from the database.
type AgentRecord struct {
	Name         string
	Description  *string
	Model        string
	SystemPrompt *string
	MaxTokens    *int
	Temperature  *float64
	TopK         *int
	TopP         *float64
	ToolNames    []string
	Config       json.RawMessage
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// ToolRecord represents a tool row from the database.
type ToolRecord struct {
	Name        string
	Description string
	InputSchema json.RawMessage
	IsAgentTool bool
	AgentName   *string
	Metadata    json.RawMessage
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// MessageRecord represents a message row from the database.
type MessageRecord struct {
	ID          uuid.UUID
	SessionID   uuid.UUID
	RunID       *uuid.UUID
	Role        string
	Content     []ContentBlockRecord
	Usage       json.RawMessage
	IsPreserved bool
	IsSummary   bool
	Metadata    json.RawMessage
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// ContentBlockRecord represents a content block.
type ContentBlockRecord struct {
	Type               string
	Text               *string
	ToolUseID          *string
	ToolName           *string
	ToolInput          json.RawMessage
	ToolResultForUseID *string
	ToolContent        *string
	IsError            bool
	Source             json.RawMessage
	SearchResults      json.RawMessage
	Metadata           json.RawMessage
}

// =============================================================================
// BATCH REQUEST BUILDING
// =============================================================================

// BatchRequest represents a Claude Batch API request.
type BatchRequest struct {
	CustomID string         // Our correlation ID
	Model    string         // Claude model
	System   string         // System prompt
	Messages []BatchMessage // Conversation history
	Tools    []BatchTool    // Available tools
	MaxTokens int           // Max tokens to generate
	Temperature *float64    // Sampling temperature
	TopK      *int          // Top-K sampling
	TopP      *float64      // Nucleus sampling
}

// BatchMessage represents a message in a batch request.
type BatchMessage struct {
	Role    string        // "user" or "assistant"
	Content []interface{} // Content blocks
}

// BatchTool represents a tool in a batch request.
type BatchTool struct {
	Name        string          // Tool name
	Description string          // Tool description
	InputSchema json.RawMessage // JSON Schema
}

// BuildBatchRequest builds a Claude Batch API request from run data.
func BuildBatchRequest(
	agent *AgentRecord,
	tools []ToolRecord,
	messages []MessageRecord,
	run RunRecord,
) BatchRequest {
	req := BatchRequest{
		CustomID:  uuid.New().String(),
		Model:     agent.Model,
		MaxTokens: 8192, // Default
	}

	if agent.SystemPrompt != nil {
		req.System = *agent.SystemPrompt
	}

	if agent.MaxTokens != nil {
		req.MaxTokens = *agent.MaxTokens
	}

	req.Temperature = agent.Temperature
	req.TopK = agent.TopK
	req.TopP = agent.TopP

	// Convert messages
	for _, msg := range messages {
		batchMsg := BatchMessage{
			Role:    msg.Role,
			Content: make([]interface{}, 0, len(msg.Content)),
		}
		for _, block := range msg.Content {
			batchMsg.Content = append(batchMsg.Content, convertContentBlock(block))
		}
		req.Messages = append(req.Messages, batchMsg)
	}

	// Convert tools
	for _, t := range tools {
		req.Tools = append(req.Tools, BatchTool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.InputSchema,
		})
	}

	return req
}

// convertContentBlock converts a content block record to batch format.
func convertContentBlock(block ContentBlockRecord) interface{} {
	switch block.Type {
	case "text":
		return map[string]interface{}{
			"type": "text",
			"text": block.Text,
		}
	case "tool_use":
		return map[string]interface{}{
			"type":  "tool_use",
			"id":    block.ToolUseID,
			"name":  block.ToolName,
			"input": block.ToolInput,
		}
	case "tool_result":
		result := map[string]interface{}{
			"type":        "tool_result",
			"tool_use_id": block.ToolResultForUseID,
			"content":     block.ToolContent,
		}
		if block.IsError {
			result["is_error"] = true
		}
		return result
	default:
		// Return as-is for other types
		return map[string]interface{}{
			"type": block.Type,
		}
	}
}
