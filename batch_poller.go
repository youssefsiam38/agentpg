package agentpg

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/google/uuid"
	"github.com/youssefsiam38/agentpg/driver"
)

// batchPoller polls Claude Batch API for status updates and processes results.
type batchPoller[TTx any] struct {
	client *Client[TTx]
}

func newBatchPoller[TTx any](c *Client[TTx]) *batchPoller[TTx] {
	return &batchPoller[TTx]{
		client: c,
	}
}

func (p *batchPoller[TTx]) run(ctx context.Context) {
	ticker := time.NewTicker(p.client.config.BatchPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.pollBatches(ctx)
		}
	}
}

func (p *batchPoller[TTx]) pollBatches(ctx context.Context) {
	store := p.client.driver.Store()

	// Get iterations ready for polling
	iterations, err := store.GetIterationsForPoll(ctx, p.client.instanceID, p.client.config.BatchPollInterval, 10)
	if err != nil {
		p.client.log().Error("failed to get iterations for poll", "error", err)
		return
	}

	for _, iter := range iterations {
		if err := p.pollIteration(ctx, iter); err != nil {
			p.client.log().Error("failed to poll iteration",
				"iteration_id", iter.ID,
				"batch_id", Deref(iter.BatchID),
				"error", err,
			)
		}
	}
}

func (p *batchPoller[TTx]) pollIteration(ctx context.Context, iter *driver.Iteration) error {
	store := p.client.driver.Store()
	log := p.client.log()

	if iter.BatchID == nil {
		return fmt.Errorf("iteration has no batch_id")
	}

	batchID := *iter.BatchID

	// Get batch status from Anthropic
	batch, err := p.client.anthropic.Messages.Batches.Get(ctx, batchID)
	if err != nil {
		return fmt.Errorf("failed to get batch status: %w", err)
	}

	now := time.Now()

	// Update poll count
	if err := store.UpdateIteration(ctx, iter.ID, map[string]any{
		"batch_poll_count":   iter.BatchPollCount + 1,
		"batch_last_poll_at": now,
	}); err != nil {
		return fmt.Errorf("failed to update poll count: %w", err)
	}

	log.Debug("polled batch",
		"batch_id", batchID,
		"status", batch.ProcessingStatus,
		"poll_count", iter.BatchPollCount+1,
	)

	// Check if processing
	if batch.ProcessingStatus == anthropic.MessageBatchProcessingStatusInProgress {
		// Update run state to batch_processing if needed
		run, err := store.GetRun(ctx, iter.RunID)
		if err != nil {
			return fmt.Errorf("failed to get run: %w", err)
		}
		if run.State == string(RunStateBatchPending) {
			if err := store.UpdateRunState(ctx, iter.RunID, driver.RunState(RunStateBatchProcessing), nil); err != nil {
				return fmt.Errorf("failed to update run state: %w", err)
			}
		}
		return nil
	}

	// Check if ended
	if batch.ProcessingStatus == anthropic.MessageBatchProcessingStatusEnded {
		return p.handleBatchComplete(ctx, iter, batch)
	}

	// Check if canceling
	if batch.ProcessingStatus == anthropic.MessageBatchProcessingStatusCanceling {
		if err := store.UpdateIteration(ctx, iter.ID, map[string]any{
			"batch_status": string(BatchStatusCanceling),
		}); err != nil {
			return fmt.Errorf("failed to update batch status: %w", err)
		}
	}

	return nil
}

func (p *batchPoller[TTx]) handleBatchComplete(ctx context.Context, iter *driver.Iteration, batch *anthropic.MessageBatch) error {
	store := p.client.driver.Store()
	log := p.client.log()

	log.Info("batch completed",
		"batch_id", batch.ID,
		"iteration_id", iter.ID,
		"succeeded", batch.RequestCounts.Succeeded,
		"errored", batch.RequestCounts.Errored,
	)

	now := time.Now()

	// Update iteration batch status
	if err := store.UpdateIteration(ctx, iter.ID, map[string]any{
		"batch_status":       string(BatchStatusEnded),
		"batch_completed_at": now,
	}); err != nil {
		return fmt.Errorf("failed to update iteration batch status: %w", err)
	}

	// Stream results to find our request
	result, err := p.fetchBatchResult(ctx, batch.ID, iter.ID.String())
	if err != nil {
		// Mark run as failed
		if err := store.UpdateRunState(ctx, iter.RunID, driver.RunState(RunStateFailed), map[string]any{
			"error_type":    "batch_error",
			"error_message": err.Error(),
			"finalized_at":  now,
		}); err != nil {
			log.Error("failed to mark run as failed", "error", err)
		}
		return fmt.Errorf("failed to fetch batch result: %w", err)
	}

	if result == nil {
		return fmt.Errorf("result not found for request_id %s", iter.ID.String())
	}

	// Check for error result
	if result.Result.Type == "errored" {
		errorMsg := "batch processing error"
		if result.Result.Error != nil {
			errorMsg = result.Result.Error.Message
		}
		if err := store.UpdateRunState(ctx, iter.RunID, driver.RunState(RunStateFailed), map[string]any{
			"error_type":    "batch_error",
			"error_message": errorMsg,
			"finalized_at":  now,
		}); err != nil {
			log.Error("failed to mark run as failed", "error", err)
		}
		return nil
	}

	// Process successful result
	return p.processResult(ctx, iter, result)
}

// batchResultLine represents a single line from the batch results JSONL
type batchResultLine struct {
	CustomID string `json:"custom_id"`
	Result   struct {
		Type    string `json:"type"` // "succeeded" or "errored"
		Message *struct {
			ID      string `json:"id"`
			Type    string `json:"type"`
			Role    string `json:"role"`
			Content []struct {
				Type  string          `json:"type"`
				Text  string          `json:"text,omitempty"`
				ID    string          `json:"id,omitempty"`
				Name  string          `json:"name,omitempty"`
				Input json.RawMessage `json:"input,omitempty"`
			} `json:"content"`
			Model        string `json:"model"`
			StopReason   string `json:"stop_reason"`
			StopSequence string `json:"stop_sequence"`
			Usage        struct {
				InputTokens              int `json:"input_tokens"`
				OutputTokens             int `json:"output_tokens"`
				CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
				CacheReadInputTokens     int `json:"cache_read_input_tokens"`
			} `json:"usage"`
		} `json:"message,omitempty"`
		Error *struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error,omitempty"`
	} `json:"result"`
}

func (p *batchPoller[TTx]) fetchBatchResult(ctx context.Context, batchID, requestID string) (*batchResultLine, error) {
	// Use the streaming results endpoint
	url := fmt.Sprintf("https://api.anthropic.com/v1/messages/batches/%s/results", batchID)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("x-api-key", p.client.config.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("anthropic-beta", "message-batches-2024-09-24")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("batch results request failed: %s - %s", resp.Status, string(body))
	}

	// Parse JSONL response
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var result batchResultLine
		if err := json.Unmarshal([]byte(line), &result); err != nil {
			continue
		}

		if result.CustomID == requestID {
			return &result, nil
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to read results: %w", err)
	}

	return nil, nil
}

func (p *batchPoller[TTx]) processResult(ctx context.Context, iter *driver.Iteration, result *batchResultLine) error {
	store := p.client.driver.Store()
	log := p.client.log()

	if result.Result.Message == nil {
		return fmt.Errorf("no message in result")
	}

	msg := result.Result.Message
	now := time.Now()

	// Build content blocks
	contentBlocks := make([]driver.ContentBlock, 0, len(msg.Content))
	hasToolUse := false
	var responseText string

	for _, block := range msg.Content {
		cb := driver.ContentBlock{
			Type: block.Type,
		}

		switch block.Type {
		case ContentTypeText:
			cb.Text = block.Text
			responseText += block.Text
		case ContentTypeToolUse:
			cb.ToolUseID = block.ID
			cb.ToolName = block.Name
			cb.ToolInput = block.Input
			hasToolUse = true
		}

		contentBlocks = append(contentBlocks, cb)
	}

	// Create assistant message
	messageParams := driver.CreateMessageParams{
		SessionID: iter.RunID, // Will be updated below
		RunID:     &iter.RunID,
		Role:      driver.MessageRole(MessageRoleAssistant),
		Content:   contentBlocks,
		Usage: driver.Usage{
			InputTokens:              msg.Usage.InputTokens,
			OutputTokens:             msg.Usage.OutputTokens,
			CacheCreationInputTokens: msg.Usage.CacheCreationInputTokens,
			CacheReadInputTokens:     msg.Usage.CacheReadInputTokens,
		},
	}

	// Get run to get session ID
	run, err := store.GetRun(ctx, iter.RunID)
	if err != nil {
		return fmt.Errorf("failed to get run: %w", err)
	}
	messageParams.SessionID = run.SessionID

	message, err := store.CreateMessage(ctx, messageParams)
	if err != nil {
		return fmt.Errorf("failed to create message: %w", err)
	}

	log.Debug("created assistant message",
		"message_id", message.ID,
		"run_id", iter.RunID,
		"has_tool_use", hasToolUse,
	)

	// Update iteration
	toolExecutionCount := 0
	if hasToolUse {
		for _, block := range msg.Content {
			if block.Type == ContentTypeToolUse {
				toolExecutionCount++
			}
		}
	}

	if err := store.UpdateIteration(ctx, iter.ID, map[string]any{
		"stop_reason":                 msg.StopReason,
		"response_message_id":         message.ID,
		"has_tool_use":                hasToolUse,
		"tool_execution_count":        toolExecutionCount,
		"input_tokens":                msg.Usage.InputTokens,
		"output_tokens":               msg.Usage.OutputTokens,
		"cache_creation_input_tokens": msg.Usage.CacheCreationInputTokens,
		"cache_read_input_tokens":     msg.Usage.CacheReadInputTokens,
		"completed_at":                now,
	}); err != nil {
		return fmt.Errorf("failed to update iteration: %w", err)
	}

	// Determine next state and update run
	runUpdates := map[string]any{
		"input_tokens":                run.InputTokens + msg.Usage.InputTokens,
		"output_tokens":               run.OutputTokens + msg.Usage.OutputTokens,
		"cache_creation_input_tokens": run.CacheCreationInputTokens + msg.Usage.CacheCreationInputTokens,
		"cache_read_input_tokens":     run.CacheReadInputTokens + msg.Usage.CacheReadInputTokens,
		"iteration_count":             run.IterationCount + 1,
	}

	var nextState RunState
	if hasToolUse {
		nextState = RunStatePendingTools
		runUpdates["tool_iterations"] = run.ToolIterations + 1

		// Create tool executions
		if err := p.createToolExecutions(ctx, iter, run, msg.Content); err != nil {
			return fmt.Errorf("failed to create tool executions: %w", err)
		}
	} else {
		// Run completed
		nextState = RunStateCompleted
		runUpdates["response_text"] = responseText
		runUpdates["stop_reason"] = msg.StopReason
		runUpdates["finalized_at"] = now
	}

	if err := store.UpdateRunState(ctx, iter.RunID, driver.RunState(nextState), runUpdates); err != nil {
		return fmt.Errorf("failed to update run state: %w", err)
	}

	// Auto-compaction: check if session needs compaction after run completes
	if nextState == RunStateCompleted && p.client.config.AutoCompactionEnabled {
		p.checkAndCompact(ctx, run.SessionID)
	}

	log.Info("processed batch result",
		"run_id", iter.RunID,
		"iteration_id", iter.ID,
		"stop_reason", msg.StopReason,
		"next_state", nextState,
		"tool_executions", toolExecutionCount,
	)

	return nil
}

func (p *batchPoller[TTx]) createToolExecutions(ctx context.Context, iter *driver.Iteration, run *driver.Run, content []struct {
	Type  string          `json:"type"`
	Text  string          `json:"text,omitempty"`
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
}) error {
	store := p.client.driver.Store()

	var params []driver.CreateToolExecutionParams
	for _, block := range content {
		if block.Type != ContentTypeToolUse {
			continue
		}

		// Check if this is an agent-as-tool
		isAgentTool := false
		var agentName *string
		agent := p.client.GetAgent(block.Name)
		if agent != nil {
			isAgentTool = true
			agentName = &block.Name
		}

		params = append(params, driver.CreateToolExecutionParams{
			RunID:       run.ID,
			IterationID: iter.ID,
			ToolUseID:   block.ID,
			ToolName:    block.Name,
			ToolInput:   block.Input,
			IsAgentTool: isAgentTool,
			AgentName:   agentName,
			MaxAttempts: p.client.toolMaxAttempts(),
		})
	}

	if len(params) > 0 {
		if _, err := store.CreateToolExecutions(ctx, params); err != nil {
			return err
		}
	}

	return nil
}

// checkAndCompact checks if the session needs compaction and performs it if needed.
// This is called after a run completes when AutoCompactionEnabled is true.
// Errors are logged but do not fail the run.
func (p *batchPoller[TTx]) checkAndCompact(ctx context.Context, sessionID uuid.UUID) {
	compactor := p.client.getCompactor()
	if compactor == nil {
		return
	}

	needsCompaction, err := compactor.NeedsCompaction(ctx, sessionID)
	if err != nil {
		p.client.log().Warn("auto-compaction check failed",
			"session_id", sessionID,
			"error", err,
		)
		return
	}

	if !needsCompaction {
		return
	}

	p.client.log().Info("triggering auto-compaction",
		"session_id", sessionID,
	)

	result, err := compactor.Compact(ctx, sessionID)
	if err != nil {
		p.client.log().Warn("auto-compaction failed",
			"session_id", sessionID,
			"error", err,
		)
		return
	}

	p.client.log().Info("auto-compaction completed",
		"session_id", sessionID,
		"original_tokens", result.OriginalTokens,
		"compacted_tokens", result.CompactedTokens,
		"messages_removed", result.MessagesRemoved,
	)
}
