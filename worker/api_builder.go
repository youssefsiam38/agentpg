// Package worker provides background workers for processing runs and tool executions.
package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/google/uuid"
	"github.com/youssefsiam38/agentpg/internal/convert"
	"github.com/youssefsiam38/agentpg/runstate"
	"github.com/youssefsiam38/agentpg/storage"
	"github.com/youssefsiam38/agentpg/tool"
	"github.com/youssefsiam38/agentpg/types"
)

// AgentDefinitionProvider provides agent definitions.
// This interface is implemented by the global registry.
type AgentDefinitionProvider interface {
	GetAgent(name string) (AgentDef, bool)
}

// AgentDef contains the agent configuration needed to build API requests.
type AgentDef struct {
	Name         string
	Model        string
	SystemPrompt string
	MaxTokens    *int
	Temperature  *float32
	Tools        []string
}

// DefaultAPICallBuilder builds API calls using agent definitions.
type DefaultAPICallBuilder struct {
	agentProvider AgentDefinitionProvider
	toolRegistry  *tool.Registry
}

// NewAPICallBuilder creates a new API call builder.
func NewAPICallBuilder(agentProvider AgentDefinitionProvider, toolRegistry *tool.Registry) *DefaultAPICallBuilder {
	return &DefaultAPICallBuilder{
		agentProvider: agentProvider,
		toolRegistry:  toolRegistry,
	}
}

// BuildAPIRequest builds the Anthropic API request for a run.
func (b *DefaultAPICallBuilder) BuildAPIRequest(ctx context.Context, run *storage.Run, messages []*storage.Message) (*anthropic.MessageNewParams, error) {
	// Get agent definition
	def, ok := b.agentProvider.GetAgent(run.AgentName)
	if !ok {
		return nil, fmt.Errorf("agent not found: %s", run.AgentName)
	}

	// Convert storage messages to types.Message, then to Anthropic format
	typesMsgs := convert.FromStorageMessages(messages)
	anthropicMsgs := convertToAnthropicMessages(typesMsgs)

	// Build parameters
	maxTokens := int64(4096)
	if def.MaxTokens != nil {
		maxTokens = int64(*def.MaxTokens)
	}

	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(def.Model),
		MaxTokens: maxTokens,
		Messages:  anthropicMsgs,
	}

	// Add system prompt
	if def.SystemPrompt != "" {
		params.System = []anthropic.TextBlockParam{
			{
				Type: "text",
				Text: def.SystemPrompt,
			},
		}
	}

	// Add temperature
	if def.Temperature != nil {
		params.Temperature = anthropic.Float(float64(*def.Temperature))
	}

	// Add tools - only include tools explicitly listed for this agent
	if b.toolRegistry != nil && len(def.Tools) > 0 {
		params.Tools = b.toolRegistry.ToAnthropicToolUnionsFiltered(def.Tools)
	}

	return &params, nil
}

// ProcessResponse processes the API response and returns the next state.
func (b *DefaultAPICallBuilder) ProcessResponse(ctx context.Context, run *storage.Run, response *anthropic.Message) (*ProcessResult, error) {
	result := &ProcessResult{
		StopReason:   string(response.StopReason),
		InputTokens:  int(response.Usage.InputTokens),
		OutputTokens: int(response.Usage.OutputTokens),
	}

	// Determine next state based on stop reason
	switch response.StopReason {
	case anthropic.StopReasonEndTurn:
		result.NextState = runstate.RunStateCompleted
	case anthropic.StopReasonToolUse:
		result.NextState = runstate.RunStatePendingTools
	case anthropic.StopReasonMaxTokens:
		result.NextState = runstate.RunStateAwaitingContinuation
	case anthropic.StopReasonStopSequence:
		result.NextState = runstate.RunStateCompleted
	case anthropic.StopReasonRefusal:
		result.NextState = runstate.RunStateFailed
	case anthropic.StopReasonPauseTurn:
		result.NextState = runstate.RunStateAwaitingContinuation
	default:
		// Unknown stop reason - treat as completed
		result.NextState = runstate.RunStateCompleted
	}

	// Create assistant message
	messageID := uuid.New().String()
	assistantMsg := &storage.Message{
		ID:        messageID,
		SessionID: run.SessionID,
		RunID:     &run.ID,
		Role:      "assistant",
		Usage: &storage.MessageUsage{
			InputTokens:  int(response.Usage.InputTokens),
			OutputTokens: int(response.Usage.OutputTokens),
		},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	result.AssistantMessage = assistantMsg

	// Convert content blocks
	contentBlocks := make([]*storage.ContentBlock, len(response.Content))
	var responseText string
	var toolExecs []*storage.CreateToolExecutionParams

	for i, block := range response.Content {
		cb := &storage.ContentBlock{
			ID:         uuid.New().String(),
			MessageID:  messageID,
			BlockIndex: i,
			CreatedAt:  time.Now(),
		}

		switch block := block.AsAny().(type) {
		case anthropic.TextBlock:
			cb.Type = storage.ContentBlockTypeText
			text := block.Text
			cb.Text = &text
			responseText += text

		case anthropic.ToolUseBlock:
			cb.Type = storage.ContentBlockTypeToolUse
			toolUseID := block.ID
			toolName := block.Name
			cb.ToolUseID = &toolUseID
			cb.ToolName = &toolName

			// Marshal tool input
			inputMap := make(map[string]any)
			if err := json.Unmarshal(block.Input, &inputMap); err == nil {
				cb.ToolInput = inputMap
			}

			// Create tool execution record
			toolExec := &storage.CreateToolExecutionParams{
				RunID:          run.ID,
				ToolUseBlockID: cb.ID,
				ToolName:       toolName,
				ToolInput:      inputMap,
			}
			toolExecs = append(toolExecs, toolExec)
		}

		contentBlocks[i] = cb
	}

	result.ContentBlocks = contentBlocks
	result.ResponseText = responseText
	result.ToolExecutions = toolExecs

	return result, nil
}

// convertToAnthropicMessages converts types.Message slice to Anthropic API format.
func convertToAnthropicMessages(messages []*types.Message) []anthropic.MessageParam {
	params := make([]anthropic.MessageParam, 0, len(messages))

	for _, msg := range messages {
		// Skip system messages (handled separately)
		if msg.Role == types.RoleSystem {
			continue
		}

		// Convert content blocks
		contentBlocks := make([]anthropic.ContentBlockParamUnion, 0, len(msg.Content))
		for _, block := range msg.Content {
			contentBlocks = append(contentBlocks, convertContentBlock(block))
		}

		// Create message param
		param := anthropic.MessageParam{
			Role:    anthropic.MessageParamRole(msg.Role),
			Content: contentBlocks,
		}

		params = append(params, param)
	}

	return params
}

// convertContentBlock converts a single content block to Anthropic format.
func convertContentBlock(block types.ContentBlock) anthropic.ContentBlockParamUnion {
	switch block.Type {
	case types.ContentTypeText:
		return anthropic.NewTextBlock(block.Text)

	case types.ContentTypeToolUse:
		var input any
		if len(block.ToolInputRaw) > 0 {
			_ = json.Unmarshal(block.ToolInputRaw, &input)
		} else if block.ToolInput != nil {
			input = block.ToolInput
		}
		if input == nil {
			input = map[string]any{}
		}
		return anthropic.NewToolUseBlock(block.ToolUseID, input, block.ToolName)

	case types.ContentTypeToolResult:
		return anthropic.NewToolResultBlock(block.ToolResultID, block.ToolContent, block.IsError)

	case types.ContentTypeImage:
		if block.ImageSource != nil {
			if block.ImageSource.Type == "base64" {
				return anthropic.NewImageBlockBase64(
					block.ImageSource.MediaType,
					block.ImageSource.Data,
				)
			}
		}

	case types.ContentTypeDocument:
		if block.DocumentSource != nil {
			return anthropic.NewDocumentBlock(anthropic.Base64PDFSourceParam{
				MediaType: "application/pdf",
				Data:      block.DocumentSource.Data,
			})
		}
	}

	// Fallback to empty text block
	return anthropic.NewTextBlock("")
}
