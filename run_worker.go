package agentpg

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/google/uuid"
	"github.com/youssefsiam38/agentpg/driver"
	"github.com/youssefsiam38/agentpg/tool"
)

// runWorker processes pending batch runs by claiming them, building messages,
// and submitting to Claude Batch API. Only claims runs with run_mode='batch'.
type runWorker[TTx any] struct {
	client    *Client[TTx]
	triggerCh chan struct{}
}

func newRunWorker[TTx any](c *Client[TTx]) *runWorker[TTx] {
	return &runWorker[TTx]{
		client:    c,
		triggerCh: make(chan struct{}, 1),
	}
}

func (w *runWorker[TTx]) trigger() {
	select {
	case w.triggerCh <- struct{}{}:
	default:
	}
}

func (w *runWorker[TTx]) run(ctx context.Context) {
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

func (w *runWorker[TTx]) processRuns(ctx context.Context) {
	store := w.client.driver.Store()

	// Claim pending batch runs only
	runs, err := store.ClaimRuns(ctx, w.client.instanceID, w.client.config.MaxConcurrentRuns, "batch")
	if err != nil {
		w.client.log().Error("failed to claim batch runs", "error", err)
		return
	}

	for _, run := range runs {
		if err := w.processRun(ctx, run); err != nil {
			w.client.log().Error("failed to process run",
				"run_id", run.ID,
				"error", err,
			)
			// Mark run as failed
			w.failRun(ctx, run.ID, "processing_error", err.Error())
		}
	}
}

func (w *runWorker[TTx]) processRun(ctx context.Context, run *driver.Run) error {
	store := w.client.driver.Store()
	log := w.client.log()

	log.Info("processing run",
		"run_id", run.ID,
		"agent_name", run.AgentName,
		"iteration", run.CurrentIteration,
	)

	// Get agent definition
	agent := w.client.GetAgent(run.AgentName)
	if agent == nil {
		return fmt.Errorf("agent not found: %s", run.AgentName)
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

	// Create iteration
	iterationNumber := run.CurrentIteration + 1
	iteration, err := store.CreateIteration(ctx, driver.CreateIterationParams{
		RunID:           run.ID,
		IterationNumber: iterationNumber,
		TriggerType:     triggerType,
	})
	if err != nil {
		return fmt.Errorf("failed to create iteration: %w", err)
	}

	// Build messages for Claude API
	messages, err := w.buildMessages(ctx, run)
	if err != nil {
		return fmt.Errorf("failed to build messages: %w", err)
	}

	// Build tools for Claude API
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

	// Build batch request
	maxTokens := int64(4096)
	if agent.MaxTokens != nil {
		maxTokens = int64(*agent.MaxTokens)
	}

	batchParams := anthropic.MessageBatchNewParams{
		Requests: []anthropic.MessageBatchNewParamsRequest{
			{
				CustomID: iteration.ID.String(),
				Params: anthropic.MessageBatchNewParamsRequestParams{
					Model:     anthropic.Model(agent.Model),
					MaxTokens: maxTokens,
					Messages:  messages,
					System:    system,
				},
			},
		},
	}

	// Add tools if any
	if len(tools) > 0 {
		batchParams.Requests[0].Params.Tools = tools
	}

	// Add optional parameters
	if agent.Temperature != nil {
		batchParams.Requests[0].Params.Temperature = anthropic.Float(*agent.Temperature)
	}
	if agent.TopK != nil {
		batchParams.Requests[0].Params.TopK = anthropic.Int(int64(*agent.TopK))
	}
	if agent.TopP != nil {
		batchParams.Requests[0].Params.TopP = anthropic.Float(*agent.TopP)
	}

	// Submit batch
	batch, err := w.client.anthropic.Messages.Batches.New(ctx, batchParams)
	if err != nil {
		return fmt.Errorf("failed to submit batch: %w", err)
	}

	log.Info("batch submitted",
		"run_id", run.ID,
		"batch_id", batch.ID,
		"iteration_id", iteration.ID,
	)

	// Update iteration with batch info
	batchStatus := BatchStatusInProgress
	now := time.Now()
	expiresAt := now.Add(24 * time.Hour)
	if err := store.UpdateIteration(ctx, iteration.ID, map[string]any{
		"batch_id":           batch.ID,
		"batch_request_id":   iteration.ID.String(),
		"batch_status":       string(batchStatus),
		"batch_submitted_at": now,
		"batch_expires_at":   expiresAt,
		"started_at":         now,
	}); err != nil {
		return fmt.Errorf("failed to update iteration: %w", err)
	}

	// Update run state
	if err := store.UpdateRunState(ctx, run.ID, driver.RunState(RunStateBatchPending), map[string]any{
		"current_iteration":    iterationNumber,
		"current_iteration_id": iteration.ID,
		"started_at":           now,
	}); err != nil {
		return fmt.Errorf("failed to update run state: %w", err)
	}

	return nil
}

func (w *runWorker[TTx]) buildMessages(ctx context.Context, run *driver.Run) ([]anthropic.MessageParam, error) {
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
					json.Unmarshal(block.ToolInput, &input)
				}
				content = append(content, anthropic.NewToolUseBlock(block.ToolUseID, input, block.ToolName))
			case ContentTypeToolResult:
				content = append(content, anthropic.NewToolResultBlock(block.ToolResultForUseID, block.ToolContent, block.IsError))
			}
		}

		if len(content) > 0 {
			messages = append(messages, anthropic.MessageParam{
				Role:    role,
				Content: content,
			})
		}
	}

	return messages, nil
}

func (w *runWorker[TTx]) buildTools(ctx context.Context, agent *AgentDefinition) ([]anthropic.ToolUnionParam, error) {
	if len(agent.Tools) == 0 && len(agent.Agents) == 0 {
		return nil, nil
	}

	tools := make([]anthropic.ToolUnionParam, 0, len(agent.Tools)+len(agent.Agents))

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
	for _, agentName := range agent.Agents {
		delegateAgent := w.client.GetAgent(agentName)
		if delegateAgent == nil {
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
			Name:        agentName,
			Description: anthropic.String(delegateAgent.Description),
			InputSchema: inputSchema,
		}
		tools = append(tools, anthropic.ToolUnionParam{OfTool: &toolParam})
	}

	return tools, nil
}

func (w *runWorker[TTx]) failRun(ctx context.Context, runID uuid.UUID, errorType, errorMessage string) {
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

// schemaPropertiesToMap converts tool schema properties to the format expected by Anthropic API
func schemaPropertiesToMap(props map[string]tool.PropertyDef) map[string]any {
	result := make(map[string]any)
	for k, v := range props {
		result[k] = v.ToJSON()
	}
	return result
}
