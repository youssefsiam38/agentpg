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
// BATCH POLLER
// =============================================================================

// BatchPollerConfig holds configuration for the batch poller.
type BatchPollerConfig struct {
	// InstanceID is this worker's instance identifier.
	InstanceID string

	// PollInterval is how often to check for batches needing polling.
	PollInterval time.Duration

	// MaxConcurrent is the maximum concurrent batch polls.
	MaxConcurrent int

	// MinPollGap is the minimum time between polls for the same batch.
	MinPollGap time.Duration

	// Logger for structured logging.
	Logger Logger
}

// BatchPoller polls Claude Batch API for completion and processes results.
//
// Flow:
//  1. Query for iterations with batch_status='in_progress' that need polling
//  2. Call Claude Batch API to get status
//  3. If completed: process response, create tool executions if needed
//  4. Update run state based on result
type BatchPoller struct {
	config   BatchPollerConfig
	store    BatchPollerStore
	batchAPI BatchPollerAPI

	// Runtime state
	mu       sync.Mutex
	running  bool
	stopCh   chan struct{}
	wg       sync.WaitGroup
	activeCh chan struct{} // Semaphore for concurrency limiting
}

// BatchPollerStore defines the database operations needed by BatchPoller.
type BatchPollerStore interface {
	// GetIterationsForPoll returns iterations that need batch status polling.
	GetIterationsForPoll(ctx context.Context, instanceID string, minPollGap time.Duration, maxCount int) ([]IterationRecord, error)

	// UpdateIterationBatchStatus updates an iteration's batch status after polling.
	UpdateIterationBatchStatus(ctx context.Context, iterID uuid.UUID, status BatchStatus, pollCount int) error

	// CompleteIteration processes a completed batch response.
	// Creates the assistant message, tool executions, and updates run state.
	CompleteIteration(ctx context.Context, opts CompleteIterationOpts) error

	// FailIteration marks an iteration and run as failed.
	FailIteration(ctx context.Context, iterID uuid.UUID, runID uuid.UUID, errorType, errorMsg string) error

	// GetRunByID retrieves a run by ID.
	GetRunByID(ctx context.Context, runID uuid.UUID) (*RunRecord, error)
}

// BatchPollerAPI defines the Claude Batch API operations for polling.
type BatchPollerAPI interface {
	// GetBatchStatus gets the current status of a batch.
	GetBatchStatus(ctx context.Context, batchID string) (*BatchStatusResponse, error)

	// GetBatchResult retrieves the result for a specific request in a completed batch.
	GetBatchResult(ctx context.Context, batchID string, requestID string) (*BatchResultResponse, error)
}

// BatchStatusResponse represents the status of a Claude batch.
type BatchStatusResponse struct {
	ID              string      // Batch ID
	Status          BatchStatus // in_progress, canceling, ended
	CreatedAt       time.Time
	EndedAt         *time.Time
	ExpiresAt       time.Time
	RequestCounts   BatchRequestCounts
	ResultsURL      *string // URL to download results (when ended)
}

// BatchRequestCounts tracks request completion within a batch.
type BatchRequestCounts struct {
	Processing int
	Succeeded  int
	Errored    int
	Canceled   int
	Expired    int
}

// BatchResultResponse represents the result of a single request in a batch.
type BatchResultResponse struct {
	CustomID   string          // Our request ID
	ResultType string          // "succeeded", "errored", "canceled", "expired"
	Message    *BatchMessage   // The Claude response (if succeeded)
	Error      *BatchError     // Error details (if errored)
	Usage      *BatchUsage
}

// BatchError represents an error in a batch response.
type BatchError struct {
	Type    string
	Message string
}

// BatchUsage represents token usage in a batch response.
type BatchUsage struct {
	InputTokens              int
	OutputTokens             int
	CacheCreationInputTokens int
	CacheReadInputTokens     int
}

// CompleteIterationOpts holds options for completing an iteration.
type CompleteIterationOpts struct {
	IterationID uuid.UUID
	RunID       uuid.UUID
	SessionID   uuid.UUID

	// Response data
	StopReason         string
	ResponseContent    []ContentBlockRecord
	HasToolUse         bool
	ToolExecutionCount int

	// Token usage
	InputTokens              int
	OutputTokens             int
	CacheCreationInputTokens int
	CacheReadInputTokens     int
}

// NewBatchPoller creates a new batch poller.
func NewBatchPoller(store BatchPollerStore, batchAPI BatchPollerAPI, cfg BatchPollerConfig) *BatchPoller {
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 30 * time.Second
	}
	if cfg.MaxConcurrent <= 0 {
		cfg.MaxConcurrent = 10
	}
	if cfg.MinPollGap <= 0 {
		cfg.MinPollGap = 30 * time.Second
	}

	return &BatchPoller{
		config:   cfg,
		store:    store,
		batchAPI: batchAPI,
		activeCh: make(chan struct{}, cfg.MaxConcurrent),
	}
}

// Start starts the batch poller.
func (p *BatchPoller) Start(ctx context.Context) error {
	p.mu.Lock()
	if p.running {
		p.mu.Unlock()
		return fmt.Errorf("batch poller already started")
	}
	p.running = true
	p.stopCh = make(chan struct{})
	p.mu.Unlock()

	p.wg.Add(1)
	go p.pollLoop(ctx)

	if p.config.Logger != nil {
		p.config.Logger.Info("batch poller started",
			"instance_id", p.config.InstanceID,
			"poll_interval", p.config.PollInterval,
		)
	}

	return nil
}

// Stop stops the batch poller gracefully.
func (p *BatchPoller) Stop(ctx context.Context) error {
	p.mu.Lock()
	if !p.running {
		p.mu.Unlock()
		return nil
	}
	p.running = false
	close(p.stopCh)
	p.mu.Unlock()

	// Wait for in-progress work
	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		if p.config.Logger != nil {
			p.config.Logger.Info("batch poller stopped", "instance_id", p.config.InstanceID)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// pollLoop is the main polling loop.
func (p *BatchPoller) pollLoop(ctx context.Context) {
	defer p.wg.Done()

	ticker := time.NewTicker(p.config.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-p.stopCh:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.pollBatches(ctx)
		}
	}
}

// pollBatches finds and polls batches that need status updates.
func (p *BatchPoller) pollBatches(ctx context.Context) {
	// Determine how many slots are available
	available := p.config.MaxConcurrent - len(p.activeCh)
	if available <= 0 {
		return
	}

	// Get iterations needing poll
	iterations, err := p.store.GetIterationsForPoll(ctx, p.config.InstanceID, p.config.MinPollGap, available)
	if err != nil {
		if p.config.Logger != nil {
			p.config.Logger.Error("failed to get iterations for poll", "error", err)
		}
		return
	}

	// Poll each iteration
	for _, iter := range iterations {
		p.wg.Add(1)
		p.activeCh <- struct{}{} // Acquire slot

		go func(i IterationRecord) {
			defer p.wg.Done()
			defer func() { <-p.activeCh }() // Release slot

			if err := p.pollIteration(ctx, i); err != nil {
				if p.config.Logger != nil {
					p.config.Logger.Error("failed to poll iteration",
						"iteration_id", i.ID,
						"batch_id", i.BatchID,
						"error", err,
					)
				}
			}
		}(iter)
	}
}

// pollIteration polls a single iteration's batch status.
func (p *BatchPoller) pollIteration(ctx context.Context, iter IterationRecord) error {
	// Get batch status from Claude API
	status, err := p.batchAPI.GetBatchStatus(ctx, iter.BatchID)
	if err != nil {
		// Update poll count even on error
		_ = p.store.UpdateIterationBatchStatus(ctx, iter.ID, iter.BatchStatus, iter.BatchPollCount+1)
		return fmt.Errorf("get batch status: %w", err)
	}

	// Update poll tracking
	if err := p.store.UpdateIterationBatchStatus(ctx, iter.ID, status.Status, iter.BatchPollCount+1); err != nil {
		return fmt.Errorf("update batch status: %w", err)
	}

	// If still in progress, nothing more to do
	if status.Status != BatchStatusEnded {
		if p.config.Logger != nil {
			p.config.Logger.Debug("batch still processing",
				"iteration_id", iter.ID,
				"batch_id", iter.BatchID,
				"status", status.Status,
			)
		}
		return nil
	}

	// Batch completed - get the result
	return p.processCompletedBatch(ctx, iter, status)
}

// processCompletedBatch processes a completed batch response.
func (p *BatchPoller) processCompletedBatch(ctx context.Context, iter IterationRecord, status *BatchStatusResponse) error {
	// Get the specific result for our request
	result, err := p.batchAPI.GetBatchResult(ctx, iter.BatchID, iter.BatchRequestID)
	if err != nil {
		return p.failIteration(ctx, iter, "batch_result_error", err.Error())
	}

	// Get the parent run
	run, err := p.store.GetRunByID(ctx, iter.RunID)
	if err != nil {
		return p.failIteration(ctx, iter, "run_not_found", err.Error())
	}

	// Handle different result types
	switch result.ResultType {
	case "succeeded":
		return p.handleSuccessfulResult(ctx, iter, run, result)

	case "errored":
		errMsg := "unknown error"
		if result.Error != nil {
			errMsg = fmt.Sprintf("%s: %s", result.Error.Type, result.Error.Message)
		}
		return p.failIteration(ctx, iter, "batch_error", errMsg)

	case "canceled":
		return p.failIteration(ctx, iter, "batch_canceled", "batch request was canceled")

	case "expired":
		return p.failIteration(ctx, iter, "batch_expired", "batch request expired (24h limit)")

	default:
		return p.failIteration(ctx, iter, "unknown_result_type", result.ResultType)
	}
}

// handleSuccessfulResult processes a successful batch response.
func (p *BatchPoller) handleSuccessfulResult(ctx context.Context, iter IterationRecord, run *RunRecord, result *BatchResultResponse) error {
	if result.Message == nil {
		return p.failIteration(ctx, iter, "missing_message", "batch succeeded but no message returned")
	}

	// Parse response content
	content, hasToolUse, toolCount := parseResponseContent(result.Message.Content)

	// Get token usage
	var usage BatchUsage
	if result.Usage != nil {
		usage = *result.Usage
	}

	// Determine stop reason
	// The Message struct should have a StopReason field from Claude's response
	stopReason := "end_turn" // Default
	// TODO: Extract stop_reason from result.Message when available

	// Complete the iteration
	if err := p.store.CompleteIteration(ctx, CompleteIterationOpts{
		IterationID:              iter.ID,
		RunID:                    run.ID,
		SessionID:                run.SessionID,
		StopReason:               stopReason,
		ResponseContent:          content,
		HasToolUse:               hasToolUse,
		ToolExecutionCount:       toolCount,
		InputTokens:              usage.InputTokens,
		OutputTokens:             usage.OutputTokens,
		CacheCreationInputTokens: usage.CacheCreationInputTokens,
		CacheReadInputTokens:     usage.CacheReadInputTokens,
	}); err != nil {
		return fmt.Errorf("complete iteration: %w", err)
	}

	if p.config.Logger != nil {
		p.config.Logger.Info("batch completed",
			"iteration_id", iter.ID,
			"run_id", run.ID,
			"stop_reason", stopReason,
			"has_tool_use", hasToolUse,
			"tool_count", toolCount,
		)
	}

	return nil
}

// failIteration marks an iteration and run as failed.
func (p *BatchPoller) failIteration(ctx context.Context, iter IterationRecord, errorType, errorMsg string) error {
	return p.store.FailIteration(ctx, iter.ID, iter.RunID, errorType, errorMsg)
}

// parseResponseContent extracts content blocks from the response.
func parseResponseContent(content []interface{}) ([]ContentBlockRecord, bool, int) {
	blocks := make([]ContentBlockRecord, 0, len(content))
	hasToolUse := false
	toolCount := 0

	for _, c := range content {
		block := parseContentBlock(c)
		blocks = append(blocks, block)

		if block.Type == "tool_use" {
			hasToolUse = true
			toolCount++
		}
	}

	return blocks, hasToolUse, toolCount
}

// parseContentBlock converts a raw content block to a record.
func parseContentBlock(raw interface{}) ContentBlockRecord {
	// Handle map[string]interface{} from JSON unmarshaling
	m, ok := raw.(map[string]interface{})
	if !ok {
		return ContentBlockRecord{Type: "unknown"}
	}

	block := ContentBlockRecord{}

	if t, ok := m["type"].(string); ok {
		block.Type = t
	}

	switch block.Type {
	case "text":
		if text, ok := m["text"].(string); ok {
			block.Text = &text
		}

	case "tool_use":
		if id, ok := m["id"].(string); ok {
			block.ToolUseID = &id
		}
		if name, ok := m["name"].(string); ok {
			block.ToolName = &name
		}
		if input, ok := m["input"]; ok {
			if inputBytes, err := json.Marshal(input); err == nil {
				block.ToolInput = inputBytes
			}
		}

	case "thinking":
		if text, ok := m["thinking"].(string); ok {
			block.Text = &text
		}
	}

	return block
}
