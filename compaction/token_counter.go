package compaction

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/youssefsiam38/agentpg/driver"
)

// TokenCounter provides token counting for messages using the Claude API
// with a character-based approximation fallback.
type TokenCounter struct {
	client   *anthropic.Client
	useAPI   bool
	model    string
	fallback bool // tracks if API failed and we're using fallback
}

// TokenCountResult contains the result of a token count operation.
type TokenCountResult struct {
	// TotalTokens is the total token count for all messages.
	TotalTokens int

	// UsedAPI indicates whether the Claude API was used (true) or the
	// character-based approximation fallback was used (false).
	UsedAPI bool

	// PerMessage contains the estimated token count per message.
	// Only populated when using the fallback approximation.
	PerMessage []int
}

// NewTokenCounter creates a new TokenCounter with the given Anthropic client.
// If useAPI is false, only character-based approximation will be used.
func NewTokenCounter(client *anthropic.Client, model string, useAPI bool) *TokenCounter {
	return &TokenCounter{
		client: client,
		model:  model,
		useAPI: useAPI,
	}
}

// CountTokens counts the tokens in the given messages.
// It first attempts to use the Claude API for accurate counting,
// falling back to character-based approximation if the API is unavailable
// or if useAPI is false.
func (tc *TokenCounter) CountTokens(ctx context.Context, messages []*driver.Message) (*TokenCountResult, error) {
	if tc.useAPI && tc.client != nil && !tc.fallback {
		result, err := tc.countWithAPI(ctx, messages)
		if err == nil {
			return result, nil
		}
		// API failed, fall back to approximation
		tc.fallback = true
	}

	return tc.countWithApproximation(messages), nil
}

// CountTokensForContent counts the tokens for a single text content.
func (tc *TokenCounter) CountTokensForContent(ctx context.Context, text string) (int, error) {
	if tc.useAPI && tc.client != nil && !tc.fallback {
		// Create a minimal message for counting
		messages := []*driver.Message{
			{
				Role: "user",
				Content: []driver.ContentBlock{
					{Type: "text", Text: text},
				},
			},
		}
		result, err := tc.countWithAPI(ctx, messages)
		if err == nil {
			return result.TotalTokens, nil
		}
		tc.fallback = true
	}

	return approximateTokens(text), nil
}

// countWithAPI uses the Claude token counting API.
func (tc *TokenCounter) countWithAPI(ctx context.Context, messages []*driver.Message) (*TokenCountResult, error) {
	if len(messages) == 0 {
		return &TokenCountResult{TotalTokens: 0, UsedAPI: true}, nil
	}

	anthropicMessages, err := tc.convertToAnthropicMessages(messages)
	if err != nil {
		return nil, fmt.Errorf("failed to convert messages: %w", err)
	}

	result, err := tc.client.Messages.CountTokens(ctx, anthropic.MessageCountTokensParams{
		Model:    anthropic.Model(tc.model),
		Messages: anthropicMessages,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrTokenCountingFailed, err)
	}

	return &TokenCountResult{
		TotalTokens: int(result.InputTokens),
		UsedAPI:     true,
	}, nil
}

// countWithApproximation uses character-based estimation (~4 chars per token).
func (tc *TokenCounter) countWithApproximation(messages []*driver.Message) *TokenCountResult {
	perMessage := make([]int, len(messages))
	total := 0

	for i, msg := range messages {
		tokens := tc.estimateMessageTokens(msg)
		perMessage[i] = tokens
		total += tokens
	}

	return &TokenCountResult{
		TotalTokens: total,
		UsedAPI:     false,
		PerMessage:  perMessage,
	}
}

// estimateMessageTokens estimates tokens for a single message using character approximation.
func (tc *TokenCounter) estimateMessageTokens(msg *driver.Message) int {
	total := 0

	// Add overhead for message structure (~4 tokens for role, etc.)
	total += 4

	for _, block := range msg.Content {
		switch block.Type {
		case "text":
			total += approximateTokens(block.Text)
		case "tool_use":
			// Tool name + ID overhead
			total += approximateTokens(block.ToolName) + 10
			if len(block.ToolInput) > 0 {
				total += approximateTokens(string(block.ToolInput))
			}
		case "tool_result":
			// Tool result ID overhead
			total += 10
			total += approximateTokens(block.ToolContent)
		case "thinking":
			total += approximateTokens(block.Text)
		case "image", "document":
			// Images and documents have higher token overhead
			// A rough estimate: small images ~85 tokens, large images ~1600+ tokens
			total += 200 // Conservative estimate
		case "web_search_result":
			if len(block.SearchResults) > 0 {
				total += approximateTokens(string(block.SearchResults))
			}
		default:
			// Unknown type, estimate from available text
			if block.Text != "" {
				total += approximateTokens(block.Text)
			}
		}
	}

	return total
}

// approximateTokens estimates token count from character count.
// Uses the approximation of ~4 characters per token for English text.
// This is a conservative estimate that works reasonably well for most content.
func approximateTokens(text string) int {
	if len(text) == 0 {
		return 0
	}
	// Use ~4 characters per token with a minimum of 1 token
	tokens := (len(text) + 3) / 4
	if tokens < 1 {
		tokens = 1
	}
	return tokens
}

// convertToAnthropicMessages converts driver.Message to anthropic.MessageParam.
func (tc *TokenCounter) convertToAnthropicMessages(messages []*driver.Message) ([]anthropic.MessageParam, error) {
	result := make([]anthropic.MessageParam, 0, len(messages))

	for _, msg := range messages {
		role := anthropic.MessageParamRoleUser
		if msg.Role == "assistant" {
			role = anthropic.MessageParamRoleAssistant
		}

		content := make([]anthropic.ContentBlockParamUnion, 0, len(msg.Content))
		for _, block := range msg.Content {
			switch block.Type {
			case "text":
				content = append(content, anthropic.NewTextBlock(block.Text))
			case "tool_use":
				var input any
				if len(block.ToolInput) > 0 {
					if err := json.Unmarshal(block.ToolInput, &input); err != nil {
						// Use empty object if parsing fails
						input = map[string]any{}
					}
				}
				content = append(content, anthropic.NewToolUseBlock(block.ToolUseID, input, block.ToolName))
			case "tool_result":
				content = append(content, anthropic.NewToolResultBlock(block.ToolResultForUseID, block.ToolContent, block.IsError))
			case "thinking":
				// Thinking blocks are included as text for token counting
				content = append(content, anthropic.NewTextBlock(block.Text))
			}
			// Skip image, document, and other complex types for token counting
			// as they require special handling
		}

		if len(content) > 0 {
			result = append(result, anthropic.MessageParam{
				Role:    role,
				Content: content,
			})
		}
	}

	return result, nil
}

// EstimateTokensFromUsage calculates total tokens from a Usage struct.
func EstimateTokensFromUsage(usage driver.Usage) int {
	return usage.InputTokens + usage.OutputTokens
}
