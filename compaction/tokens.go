package compaction

import (
	"context"
	"crypto/sha256"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/youssefsiam38/agentpg/types"
)

// TokenCounter provides token counting with caching
type TokenCounter struct {
	client *anthropic.Client
	cache  map[string]int
}

// NewTokenCounter creates a new token counter
func NewTokenCounter(client *anthropic.Client) *TokenCounter {
	return &TokenCounter{
		client: client,
		cache:  make(map[string]int),
	}
}

// CountTokens uses Claude's token counting API with caching
func (c *TokenCounter) CountTokens(ctx context.Context, model string, content string) (int, error) {
	// Check cache first
	cacheKey := c.cacheKey(model, content)
	if count, ok := c.cache[cacheKey]; ok {
		return count, nil
	}

	// Use Anthropic token counting API
	resp, err := c.client.Messages.CountTokens(ctx, anthropic.MessageCountTokensParams{
		Model: anthropic.Model(model),
		Messages: []anthropic.MessageParam{
			{
				Role: anthropic.MessageParamRoleUser,
				Content: []anthropic.ContentBlockParamUnion{
					anthropic.NewTextBlock(content),
				},
			},
		},
	})
	if err != nil {
		// Fallback to approximation if API fails
		return ApproximateTokens(content), nil
	}

	count := int(resp.InputTokens)
	c.cache[cacheKey] = count
	return count, nil
}

// CountMessagesTokens counts tokens for message array with overhead
func (c *TokenCounter) CountMessagesTokens(
	ctx context.Context,
	model string,
	messages []*types.Message,
) (int, error) {
	if len(messages) == 0 {
		return 0, nil
	}

	// Build Anthropic message params
	anthropicMsgs := make([]anthropic.MessageParam, 0, len(messages))
	for _, msg := range messages {
		if msg.Role == types.RoleSystem {
			continue // System messages handled separately
		}

		// Convert content blocks to Anthropic format
		contentBlocks := make([]anthropic.ContentBlockParamUnion, 0, len(msg.Content))
		for _, block := range msg.Content {
			if block.Type == types.ContentTypeText {
				contentBlocks = append(contentBlocks, anthropic.NewTextBlock(block.Text))
			}
		}

		if len(contentBlocks) > 0 {
			anthropicMsgs = append(anthropicMsgs, anthropic.MessageParam{
				Role:    anthropic.MessageParamRole(msg.Role),
				Content: contentBlocks,
			})
		}
	}

	if len(anthropicMsgs) == 0 {
		return 0, nil
	}

	resp, err := c.client.Messages.CountTokens(ctx, anthropic.MessageCountTokensParams{
		Model:    anthropic.Model(model),
		Messages: anthropicMsgs,
	})
	if err != nil {
		// Fallback: sum individual counts + overhead
		total := 0
		for _, msg := range messages {
			total += msg.TokenCount() + 4 // ~4 tokens overhead per message
		}
		return total, nil
	}

	return int(resp.InputTokens), nil
}

// ApproximateTokens provides fast estimation without API call
func ApproximateTokens(content string) int {
	// Claude tokenizes roughly 3.5 characters per token for English text
	return len(content) * 10 / 35
}

// SumTokens calculates total tokens across messages
func SumTokens(messages []*types.Message) int {
	total := 0
	for _, msg := range messages {
		total += msg.TokenCount()
	}
	return total
}

// cacheKey generates cache key for content
func (c *TokenCounter) cacheKey(model, content string) string {
	hash := sha256.Sum256([]byte(content))
	return fmt.Sprintf("%s:%x", model, hash[:8])
}
