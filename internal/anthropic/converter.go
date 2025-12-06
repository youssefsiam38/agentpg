package anthropic

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/google/uuid"
	"github.com/youssefsiam38/agentpg/streaming"
	"github.com/youssefsiam38/agentpg/types"
)

// ConvertToAnthropicMessages converts agentpg messages to Anthropic message parameters
func ConvertToAnthropicMessages(messages []*types.Message) []anthropic.MessageParam {
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

// convertContentBlock converts a single content block
func convertContentBlock(block types.ContentBlock) anthropic.ContentBlockParamUnion {
	switch block.Type {
	case types.ContentTypeText:
		return anthropic.NewTextBlock(block.Text)

	case types.ContentTypeToolUse:
		// Tool use block - decode the raw input
		var input any
		if len(block.ToolInputRaw) > 0 {
			_ = json.Unmarshal(block.ToolInputRaw, &input)
		} else if block.ToolInput != nil {
			input = block.ToolInput
		}
		// Ensure input is a valid object (API requires a dictionary, not null)
		if input == nil {
			input = map[string]any{}
		}
		return anthropic.NewToolUseBlock(block.ToolUseID, input, block.ToolName)

	case types.ContentTypeToolResult:
		// Tool result block
		return anthropic.NewToolResultBlock(block.ToolResultID, block.ToolContent, block.IsError)

	case types.ContentTypeImage:
		// Image block
		if block.ImageSource != nil {
			if block.ImageSource.Type == "base64" {
				return anthropic.NewImageBlockBase64(
					block.ImageSource.MediaType,
					block.ImageSource.Data,
				)
			} else if block.ImageSource.Type == "url" {
				return anthropic.NewImageBlock(anthropic.URLImageSourceParam{
					URL: block.ImageSource.URL,
				})
			}
		}

	case types.ContentTypeDocument:
		// Document block (PDF)
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

// ConvertStreamingMessage converts a streaming message to agentpg Message
func ConvertStreamingMessage(streamMsg *streaming.Message, sessionID string) *types.Message {
	// Convert content blocks
	content := make([]types.ContentBlock, 0, len(streamMsg.Content))
	for _, block := range streamMsg.Content {
		content = append(content, types.ContentBlock{
			Type:         types.ContentType(block.Type),
			Text:         block.Text,
			ToolUseID:    block.ToolUseID,
			ToolName:     block.ToolName,
			ToolInput:    block.ToolInput,
			ToolInputRaw: block.ToolInputRaw,
		})
	}

	return &types.Message{
		ID:        uuid.New().String(),
		SessionID: sessionID,
		Role:      types.Role(streamMsg.Role),
		Content:   content,
		Metadata: map[string]any{
			"anthropic_message_id": streamMsg.ID,
		},
		CreatedAt: streamMsg.CreatedAt,
		UpdatedAt: streamMsg.CreatedAt,
	}
}

// ConvertUsage converts streaming usage to agentpg Usage
func ConvertUsage(streamUsage streaming.Usage) *types.Usage {
	return &types.Usage{
		InputTokens:         streamUsage.InputTokens,
		OutputTokens:        streamUsage.OutputTokens,
		CacheCreationTokens: streamUsage.CacheCreationTokens,
		CacheReadTokens:     streamUsage.CacheReadTokens,
	}
}

// ExtractToolCalls extracts tool calls from content blocks
func ExtractToolCalls(content []types.ContentBlock) []ToolCall {
	var calls []ToolCall
	for _, block := range content {
		if block.Type == types.ContentTypeToolUse {
			calls = append(calls, ToolCall{
				ID:    block.ToolUseID,
				Name:  block.ToolName,
				Input: block.ToolInputRaw,
			})
		}
	}
	return calls
}

// ToolCall represents a tool call from the assistant
type ToolCall struct {
	ID    string
	Name  string
	Input json.RawMessage
}

// HasToolCalls checks if a message contains tool calls
func HasToolCalls(msg *types.Message) bool {
	for _, block := range msg.Content {
		if block.Type == types.ContentTypeToolUse {
			return true
		}
	}
	return false
}

// CountTokens estimates token count for a message
// This is a rough approximation - use Anthropic's API for accurate counts
func CountTokens(content []types.ContentBlock) int {
	total := 0
	for _, block := range content {
		switch block.Type {
		case types.ContentTypeText:
			// Rough estimate: ~4 characters per token
			total += len(block.Text) / 4
		case types.ContentTypeToolUse:
			// Tool calls add overhead
			total += 50 + len(block.ToolName) + len(block.ToolInputRaw)/4
		case types.ContentTypeToolResult:
			// Tool results
			total += 20 + len(block.ToolContent)/4
		}
	}
	return total
}

// BuildSystemPrompt creates system prompt blocks
func BuildSystemPrompt(systemPrompt string) []anthropic.TextBlockParam {
	return []anthropic.TextBlockParam{
		{
			Type: "text",
			Text: systemPrompt,
		},
	}
}

// IsMaxTokensError checks if an error is a max_tokens error
func IsMaxTokensError(err error) bool {
	if err == nil {
		return false
	}

	// Check if it's an Anthropic API error
	var apiErr *anthropic.Error
	if !errors.As(err, &apiErr) {
		return false
	}

	// Check for max_tokens error
	errStr := apiErr.Error()
	return contains(errStr, "max_tokens") ||
		contains(errStr, "context_length") ||
		contains(errStr, "token limit")
}

// IsRetryableError checks if an error should be retried
func IsRetryableError(err error) bool {
	if err == nil {
		return false
	}

	var apiErr *anthropic.Error
	if !errors.As(err, &apiErr) {
		return false
	}

	// Retry on rate limits and server errors
	return apiErr.StatusCode == 429 || apiErr.StatusCode >= 500
}

// contains checks if a string contains a substring (case-insensitive)
func contains(s, substr string) bool {
	return len(s) >= len(substr) &&
		(s == substr || len(s) > len(substr) &&
			containsAt(s, substr, 0))
}

func containsAt(s, substr string, pos int) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// BuildExtendedContextHeaders creates headers for extended context
func BuildExtendedContextHeaders() map[string]string {
	return map[string]string{
		"anthropic-beta": "context-1m-2025-08-07",
	}
}

// ExtractTextContent extracts all text from content blocks
func ExtractTextContent(content []types.ContentBlock) string {
	var result string
	for _, block := range content {
		if block.Type == types.ContentTypeText {
			result += block.Text
		}
	}
	return result
}

// CreateToolResultBlocks creates tool result content blocks
func CreateToolResultBlocks(toolCalls []ToolCall, results []string, errors []error) []types.ContentBlock {
	blocks := make([]types.ContentBlock, 0, len(toolCalls))

	for i, call := range toolCalls {
		isError := false
		content := ""

		if i < len(errors) && errors[i] != nil {
			isError = true
			content = fmt.Sprintf("Error executing tool: %v", errors[i])
		} else if i < len(results) {
			content = results[i]
		}

		blocks = append(blocks, types.ContentBlock{
			Type:         types.ContentTypeToolResult,
			ToolResultID: call.ID,
			ToolContent:  content,
			IsError:      isError,
		})
	}

	return blocks
}
