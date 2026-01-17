package agentpg

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/google/uuid"
	"github.com/youssefsiam38/agentpg/driver"
)

// streamingWorker processes pending streaming runs by claiming them, building messages,
// and using the Claude Streaming API for real-time responses.
type streamingWorker[TTx any] struct {
	client    *Client[TTx]
	triggerCh chan struct{}
}

func newStreamingWorker[TTx any](c *Client[TTx]) *streamingWorker[TTx] {
	return &streamingWorker[TTx]{
		client:    c,
		triggerCh: make(chan struct{}, 1),
	}
}

func (w *streamingWorker[TTx]) trigger() {
	select {
	case w.triggerCh <- struct{}{}:
	default:
	}
}

func (w *streamingWorker[TTx]) run(ctx context.Context) {
	ticker := time.NewTicker(w.client.config.RunPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-w.triggerCh:
			w.processRuns(ctx)
		case <-ticker.C:
			w.processRuns(ctx)
		}
	}
}

func (w *streamingWorker[TTx]) processRuns(ctx context.Context) {
	store := w.client.driver.Store()

	// Claim pending streaming runs only
	runs, err := store.ClaimRuns(ctx, w.client.instanceID, w.client.config.MaxConcurrentStreamingRuns, "streaming")
	if err != nil {
		w.client.log().Error("failed to claim streaming runs", "error", err)
		return
	}

	for _, run := range runs {
		if err := w.processRun(ctx, run); err != nil {
			w.client.log().Error("failed to process streaming run",
				"run_id", run.ID,
				"error", err,
			)
			// Mark run as failed
			w.failRun(ctx, run.ID, "streaming_error", err.Error())
		}
	}
}

func (w *streamingWorker[TTx]) processRun(ctx context.Context, run *driver.Run) error {
	store := w.client.driver.Store()
	log := w.client.log()

	log.Info("processing streaming run",
		"run_id", run.ID,
		"agent_id", run.AgentID,
		"iteration", run.CurrentIteration,
	)

	// Get agent definition from database
	agent, err := w.client.GetAgentByID(ctx, run.AgentID)
	if err != nil {
		return fmt.Errorf("agent not found: %w", err)
	}

	// Determine trigger type
	triggerType := "user_prompt"
	if run.CurrentIteration > 0 {
		triggerType = "tool_results"
	}

	// For first iteration, create the user message with the prompt
	if run.CurrentIteration == 0 && run.Prompt != "" {
		_, err := store.CreateMessage(ctx, driver.CreateMessageParams{
			SessionID: run.SessionID,
			RunID:     &run.ID,
			Role:      driver.MessageRole(MessageRoleUser),
			Content: []driver.ContentBlock{
				{
					Type: ContentTypeText,
					Text: run.Prompt,
				},
			},
		})
		if err != nil {
			return fmt.Errorf("failed to create user message: %w", err)
		}
	}

	// Create iteration with is_streaming=true
	iterationNumber := run.CurrentIteration + 1
	iteration, err := store.CreateIteration(ctx, driver.CreateIterationParams{
		RunID:           run.ID,
		IterationNumber: iterationNumber,
		TriggerType:     triggerType,
		IsStreaming:     true,
	})
	if err != nil {
		return fmt.Errorf("failed to create iteration: %w", err)
	}

	// Update iteration with streaming start time
	now := time.Now()
	if updateIterErr := store.UpdateIteration(ctx, iteration.ID, map[string]any{
		"streaming_started_at": now,
		"started_at":           now,
	}); updateIterErr != nil {
		return fmt.Errorf("failed to update iteration start time: %w", updateIterErr)
	}

	// Update run with current iteration info
	if updateRunErr := store.UpdateRun(ctx, run.ID, map[string]any{
		"current_iteration":    iterationNumber,
		"current_iteration_id": iteration.ID,
		"started_at":           now,
	}); updateRunErr != nil {
		return fmt.Errorf("failed to update run: %w", updateRunErr)
	}

	// Build messages for Claude API (reuse logic from runWorker)
	messages, err := w.buildMessages(ctx, run)
	if err != nil {
		return fmt.Errorf("failed to build messages: %w", err)
	}

	// Build tools for Claude API (reuse logic from runWorker)
	tools, err := w.buildTools(ctx, agent)
	if err != nil {
		return fmt.Errorf("failed to build tools: %w", err)
	}

	// Build system prompt
	var system []anthropic.TextBlockParam
	if agent.SystemPrompt != "" {
		system = []anthropic.TextBlockParam{
			{Text: agent.SystemPrompt},
		}
	}

	// Build streaming request parameters
	maxTokens := int64(4096)
	if agent.MaxTokens != nil {
		maxTokens = int64(*agent.MaxTokens)
	}

	streamParams := anthropic.MessageNewParams{
		Model:     anthropic.Model(agent.Model),
		MaxTokens: maxTokens,
		Messages:  messages,
		System:    system,
	}

	// Add tools if any
	if len(tools) > 0 {
		streamParams.Tools = tools
	}

	// Add optional parameters
	if agent.Temperature != nil {
		streamParams.Temperature = anthropic.Float(*agent.Temperature)
	}
	if agent.TopK != nil {
		streamParams.TopK = anthropic.Int(int64(*agent.TopK))
	}
	if agent.TopP != nil {
		streamParams.TopP = anthropic.Float(*agent.TopP)
	}

	log.Debug("starting streaming request",
		"run_id", run.ID,
		"iteration_id", iteration.ID,
	)

	// Call streaming API
	stream := w.client.anthropic.Messages.NewStreaming(ctx, streamParams)

	// Accumulate the response using SDK's Accumulate method
	var message anthropic.Message
	for stream.Next() {
		event := stream.Current()
		_ = message.Accumulate(event)
	}

	if err := stream.Err(); err != nil {
		return fmt.Errorf("streaming error: %w", err)
	}

	log.Info("streaming completed",
		"run_id", run.ID,
		"iteration_id", iteration.ID,
		"stop_reason", message.StopReason,
	)

	// Process the accumulated response
	return w.processResult(ctx, iteration, run, &message)
}

func (w *streamingWorker[TTx]) processResult(ctx context.Context, iter *driver.Iteration, run *driver.Run, msg *anthropic.Message) error {
	store := w.client.driver.Store()
	log := w.client.log()

	now := time.Now()

	// Build content blocks
	contentBlocks := make([]driver.ContentBlock, 0, len(msg.Content))
	hasToolUse := false
	var responseText string

	for _, block := range msg.Content {
		cb := driver.ContentBlock{}

		switch variant := block.AsAny().(type) {
		case anthropic.TextBlock:
			cb.Type = ContentTypeText
			cb.Text = variant.Text
			responseText += variant.Text
		case anthropic.ToolUseBlock:
			cb.Type = ContentTypeToolUse
			cb.ToolUseID = variant.ID
			cb.ToolName = variant.Name
			if inputBytes, err := json.Marshal(variant.Input); err == nil {
				cb.ToolInput = inputBytes
			}
			hasToolUse = true
		default:
			// Skip unknown block types
			continue
		}

		contentBlocks = append(contentBlocks, cb)
	}

	// Create assistant message
	messageParams := driver.CreateMessageParams{
		SessionID: run.SessionID,
		RunID:     &iter.RunID,
		Role:      driver.MessageRole(MessageRoleAssistant),
		Content:   contentBlocks,
		Usage: driver.Usage{
			InputTokens:              int(msg.Usage.InputTokens),
			OutputTokens:             int(msg.Usage.OutputTokens),
			CacheCreationInputTokens: int(msg.Usage.CacheCreationInputTokens),
			CacheReadInputTokens:     int(msg.Usage.CacheReadInputTokens),
		},
	}

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
			if _, ok := block.AsAny().(anthropic.ToolUseBlock); ok {
				toolExecutionCount++
			}
		}
	}

	if err := store.UpdateIteration(ctx, iter.ID, map[string]any{
		"stop_reason":                 string(msg.StopReason),
		"response_message_id":         message.ID,
		"has_tool_use":                hasToolUse,
		"tool_execution_count":        toolExecutionCount,
		"input_tokens":                int(msg.Usage.InputTokens),
		"output_tokens":               int(msg.Usage.OutputTokens),
		"cache_creation_input_tokens": int(msg.Usage.CacheCreationInputTokens),
		"cache_read_input_tokens":     int(msg.Usage.CacheReadInputTokens),
		"streaming_completed_at":      now,
		"completed_at":                now,
	}); err != nil {
		return fmt.Errorf("failed to update iteration: %w", err)
	}

	// Determine next state and update run
	runUpdates := map[string]any{
		"input_tokens":                run.InputTokens + int(msg.Usage.InputTokens),
		"output_tokens":               run.OutputTokens + int(msg.Usage.OutputTokens),
		"cache_creation_input_tokens": run.CacheCreationInputTokens + int(msg.Usage.CacheCreationInputTokens),
		"cache_read_input_tokens":     run.CacheReadInputTokens + int(msg.Usage.CacheReadInputTokens),
		"iteration_count":             run.IterationCount + 1,
	}

	var nextState RunState
	if hasToolUse {
		nextState = RunStatePendingTools
		runUpdates["tool_iterations"] = run.ToolIterations + 1

		// Build tool execution params
		toolParams := w.buildToolParams(ctx, iter, run, msg.Content)

		// Atomically create tool executions AND update run state
		if len(toolParams) > 0 {
			if _, err := store.CreateToolExecutionsAndUpdateRunState(ctx, toolParams, iter.RunID, driver.RunState(nextState), runUpdates); err != nil {
				return fmt.Errorf("failed to create tool executions and update run: %w", err)
			}
		} else {
			// No tool params but still need to update state
			if err := store.UpdateRunState(ctx, iter.RunID, driver.RunState(nextState), runUpdates); err != nil {
				return fmt.Errorf("failed to update run state: %w", err)
			}
		}
	} else {
		// Run completed
		nextState = RunStateCompleted
		runUpdates["response_text"] = responseText
		runUpdates["stop_reason"] = string(msg.StopReason)
		runUpdates["finalized_at"] = now

		if err := store.UpdateRunState(ctx, iter.RunID, driver.RunState(nextState), runUpdates); err != nil {
			return fmt.Errorf("failed to update run state: %w", err)
		}
	}

	// Auto-compaction: check if session needs compaction after run completes
	if nextState == RunStateCompleted && w.client.config.AutoCompactionEnabled {
		w.checkAndCompact(ctx, run.SessionID)
	}

	log.Info("processed streaming result",
		"run_id", iter.RunID,
		"iteration_id", iter.ID,
		"stop_reason", msg.StopReason,
		"next_state", nextState,
		"tool_executions", toolExecutionCount,
	)

	return nil
}

func (w *streamingWorker[TTx]) buildToolParams(ctx context.Context, iter *driver.Iteration, run *driver.Run, content []anthropic.ContentBlockUnion) []driver.CreateToolExecutionParams {
	params := make([]driver.CreateToolExecutionParams, 0, len(content))
	for _, block := range content {
		toolUse, ok := block.AsAny().(anthropic.ToolUseBlock)
		if !ok {
			continue
		}

		// Check if this is an agent-as-tool by looking up in database
		isAgentTool := false
		var agentID *uuid.UUID
		agent, _ := w.client.GetAgentByName(ctx, toolUse.Name, nil)
		if agent != nil {
			isAgentTool = true
			agentID = &agent.ID
		}

		// Convert input to JSON
		inputBytes, _ := json.Marshal(toolUse.Input)

		params = append(params, driver.CreateToolExecutionParams{
			RunID:       run.ID,
			IterationID: iter.ID,
			ToolUseID:   toolUse.ID,
			ToolName:    toolUse.Name,
			ToolInput:   inputBytes,
			IsAgentTool: isAgentTool,
			AgentID:     agentID,
			MaxAttempts: w.client.toolMaxAttempts(),
		})
	}

	return params
}

func (w *streamingWorker[TTx]) buildMessages(ctx context.Context, run *driver.Run) ([]anthropic.MessageParam, error) {
	store := w.client.driver.Store()

	// Get messages for this run's context
	// This excludes messages from child runs (agent-as-tool invocations)
	// to prevent tool_use blocks from being followed by non-tool_result messages
	sessionMessages, err := store.GetMessagesForRunContext(ctx, run.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get messages: %w", err)
	}

	messages := make([]anthropic.MessageParam, 0, len(sessionMessages))

	// Add existing messages from database
	// IMPORTANT: Claude API requires alternating user/assistant roles.
	// Consecutive messages with the same role must be merged into one message.
	for _, msg := range sessionMessages {
		role := anthropic.MessageParamRoleUser
		if msg.Role == string(MessageRoleAssistant) {
			role = anthropic.MessageParamRoleAssistant
		}

		content := make([]anthropic.ContentBlockParamUnion, 0, len(msg.Content))
		for _, block := range msg.Content {
			switch block.Type {
			case ContentTypeText:
				content = append(content, anthropic.NewTextBlock(block.Text))
			case ContentTypeToolUse:
				var input any
				if len(block.ToolInput) > 0 {
					_ = json.Unmarshal(block.ToolInput, &input)
				}
				content = append(content, anthropic.NewToolUseBlock(block.ToolUseID, input, block.ToolName))
			case ContentTypeToolResult:
				content = append(content, anthropic.NewToolResultBlock(block.ToolResultForUseID, block.ToolContent, block.IsError))
			}
		}

		if len(content) == 0 {
			continue
		}

		// Check if we can merge with the previous message (same role)
		if len(messages) > 0 && messages[len(messages)-1].Role == role {
			// Merge content blocks into the previous message
			messages[len(messages)-1].Content = append(messages[len(messages)-1].Content, content...)
		} else {
			messages = append(messages, anthropic.MessageParam{
				Role:    role,
				Content: content,
			})
		}
	}

	return messages, nil
}

func (w *streamingWorker[TTx]) buildTools(ctx context.Context, agent *AgentDefinition) ([]anthropic.ToolUnionParam, error) {
	if len(agent.Tools) == 0 && len(agent.AgentIDs) == 0 {
		return nil, nil
	}

	tools := make([]anthropic.ToolUnionParam, 0, len(agent.Tools)+len(agent.AgentIDs))

	// Add regular tools
	for _, toolName := range agent.Tools {
		t := w.client.GetTool(toolName)
		if t == nil {
			continue
		}

		schema := t.InputSchema()
		inputSchema := anthropic.ToolInputSchemaParam{
			Type:       "object",
			Properties: schemaPropertiesToMap(schema.Properties),
		}
		if len(schema.Required) > 0 {
			inputSchema.Required = schema.Required
		}

		toolParam := anthropic.ToolParam{
			Name:        t.Name(),
			Description: anthropic.String(t.Description()),
			InputSchema: inputSchema,
		}
		tools = append(tools, anthropic.ToolUnionParam{OfTool: &toolParam})
	}

	// Add agent-as-tool entries
	for _, delegateID := range agent.AgentIDs {
		delegateAgent, err := w.client.GetAgentByID(ctx, delegateID)
		if err != nil || delegateAgent == nil {
			continue
		}

		inputSchema := anthropic.ToolInputSchemaParam{
			Type: "object",
			Properties: map[string]any{
				"task": map[string]any{
					"type":        "string",
					"description": "The task to delegate to this agent",
				},
			},
			Required: []string{"task"},
		}

		toolParam := anthropic.ToolParam{
			Name:        delegateAgent.Name,
			Description: anthropic.String(delegateAgent.Description),
			InputSchema: inputSchema,
		}
		tools = append(tools, anthropic.ToolUnionParam{OfTool: &toolParam})
	}

	return tools, nil
}

func (w *streamingWorker[TTx]) failRun(ctx context.Context, runID uuid.UUID, errorType, errorMessage string) {
	store := w.client.driver.Store()
	now := time.Now()
	if err := store.UpdateRunState(ctx, runID, driver.RunState(RunStateFailed), map[string]any{
		"error_type":    errorType,
		"error_message": errorMessage,
		"finalized_at":  now,
	}); err != nil {
		w.client.log().Error("failed to mark run as failed",
			"run_id", runID,
			"error", err,
		)
	}
}

// checkAndCompact checks if the session needs compaction and performs it if needed.
// This is called after a run completes when AutoCompactionEnabled is true.
// Errors are logged but do not fail the run.
func (w *streamingWorker[TTx]) checkAndCompact(ctx context.Context, sessionID uuid.UUID) {
	compactor := w.client.getCompactor()
	if compactor == nil {
		return
	}

	needsCompaction, err := compactor.NeedsCompaction(ctx, sessionID)
	if err != nil {
		w.client.log().Warn("auto-compaction check failed",
			"session_id", sessionID,
			"error", err,
		)
		return
	}

	if !needsCompaction {
		return
	}

	w.client.log().Info("triggering auto-compaction",
		"session_id", sessionID,
	)

	result, err := compactor.Compact(ctx, sessionID)
	if err != nil {
		w.client.log().Warn("auto-compaction failed",
			"session_id", sessionID,
			"error", err,
		)
		return
	}

	w.client.log().Info("auto-compaction completed",
		"session_id", sessionID,
		"original_tokens", result.OriginalTokens,
		"compacted_tokens", result.CompactedTokens,
		"messages_removed", result.MessagesRemoved,
	)
}
