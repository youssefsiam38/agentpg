package compaction

import (
	"context"
	"encoding/json"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/youssefsiam38/agentpg/types"
)

const (
	// DefaultPruneProtect is default recent tokens protected (OpenCode pattern)
	// Used when config.ProtectedTokens is 0
	DefaultPruneProtect = 40000

	// DefaultPruneMinimum only prunes if tool outputs exceed this threshold
	// Used as a floor to avoid unnecessary pruning of small outputs
	DefaultPruneMinimum = 1000
)

// HybridStrategy implements prune-then-summarize (OpenCode pattern)
// Step 1: Prune tool outputs (free, no API call)
// Step 2: If still over target, summarize
type HybridStrategy struct {
	client      *anthropic.Client
	summarizer  *SummarizationStrategy
	counter     *TokenCounter
	partitioner *Partitioner
}

// NewHybridStrategy creates a new hybrid strategy
func NewHybridStrategy(client *anthropic.Client) *HybridStrategy {
	return &HybridStrategy{
		client:      client,
		summarizer:  NewSummarizationStrategy(client),
		counter:     NewTokenCounter(client),
		partitioner: NewPartitioner(),
	}
}

func (h *HybridStrategy) Name() string {
	return "hybrid"
}

func (h *HybridStrategy) ShouldCompact(messages []*types.Message, config CompactionConfig) bool {
	totalTokens := SumTokens(messages)
	threshold := int(float64(config.MaxContextTokens) * config.TriggerThreshold)
	return totalTokens >= threshold
}

func (h *HybridStrategy) Compact(
	ctx context.Context,
	messages []*types.Message,
	config CompactionConfig,
) (*CompactionResult, error) {
	originalTokens := SumTokens(messages)
	originalCount := len(messages)

	// Step 1: Prune tool outputs first (cheaper, no API call)
	pruned, prunedCount := h.pruneToolOutputs(messages, config)
	prunedTokens := SumTokens(pruned)

	// Step 2: Check if pruning was sufficient to reach target
	// Use TargetTokens as the goal, not the trigger threshold
	if prunedTokens <= config.TargetTokens {
		// Pruning was sufficient, no summarization needed
		result := &CompactionResult{
			Summary:           "[Tool outputs pruned]",
			PreservedMessages: pruned,
			OriginalTokens:    originalTokens,
			CompactedTokens:   prunedTokens,
			MessagesRemoved:   0,
			Strategy:          h.Name(),
		}
		// Only return this if we actually pruned something
		if prunedCount > 0 {
			return result, nil
		}
	}

	// Step 3: Still over target or nothing was pruned, need summarization
	result, err := h.summarizer.Compact(ctx, pruned, config)
	if err != nil {
		return nil, err
	}

	// Fix the token counts to reflect original -> final (not pruned -> final)
	if result != nil {
		result.OriginalTokens = originalTokens
		result.MessagesRemoved = originalCount - len(result.PreservedMessages)
	}

	return result, nil
}

// pruneToolOutputs removes verbose tool outputs outside protected zone
func (h *HybridStrategy) pruneToolOutputs(
	messages []*types.Message,
	config CompactionConfig,
) ([]*types.Message, int) {
	// Create deep copy to avoid modifying original
	result := make([]*types.Message, len(messages))
	for i := range messages {
		result[i] = h.copyMessage(messages[i])
	}

	totalPruned := 0
	toolOutputTokens := 0

	// Use config.ProtectedTokens, fallback to default if not set
	protectedTokens := config.ProtectedTokens
	if protectedTokens == 0 {
		protectedTokens = DefaultPruneProtect
	}

	// Calculate protected zone from end
	protectedIdx := h.partitioner.findProtectedIndex(messages, protectedTokens)

	// First pass: count tool output tokens outside protected zone
	for i := 0; i < protectedIdx; i++ {
		for _, block := range result[i].Content {
			if block.Type == types.ContentTypeToolResult && block.ToolContent != "" {
				// Approximate tokens in tool result
				tokens := ApproximateTokens(block.ToolContent)
				toolOutputTokens += tokens
			}
		}
	}

	// Only prune if tool outputs exceed minimum threshold
	if toolOutputTokens < DefaultPruneMinimum {
		return result, 0
	}

	// Second pass: prune tool outputs outside protected zone
	for i := 0; i < protectedIdx; i++ {
		for j := range result[i].Content {
			block := &result[i].Content[j]
			if block.Type == types.ContentTypeToolResult && block.ToolContent != "" {
				// Save original token count
				originalTokens := ApproximateTokens(block.ToolContent)

				// Replace with pruned marker
				block.ToolContent = "[TOOL OUTPUT PRUNED]"
				block.IsError = false

				// Update token count
				prunedTokens := 4 // "[TOOL OUTPUT PRUNED]" is ~4 tokens
				totalPruned += originalTokens - prunedTokens
			}
		}

		// Recalculate message token count
		result[i].Usage = &types.Usage{
			InputTokens: h.calculateMessageTokens(result[i]),
		}
	}

	return result, totalPruned
}

// copyMessage creates a deep copy of a message
func (h *HybridStrategy) copyMessage(msg *types.Message) *types.Message {
	msgCopy := *msg

	// Deep copy content blocks
	msgCopy.Content = make([]types.ContentBlock, len(msg.Content))
	copy(msgCopy.Content, msg.Content)

	// Deep copy usage if present
	if msg.Usage != nil {
		usageCopy := *msg.Usage
		msgCopy.Usage = &usageCopy
	}

	// Deep copy metadata if present
	if msg.Metadata != nil {
		msgCopy.Metadata = make(map[string]any, len(msg.Metadata))
		for k, v := range msg.Metadata {
			msgCopy.Metadata[k] = v
		}
	}

	return &msgCopy
}

// calculateMessageTokens estimates tokens for a message
func (h *HybridStrategy) calculateMessageTokens(msg *types.Message) int {
	totalTokens := 0
	for _, block := range msg.Content {
		switch block.Type {
		case types.ContentTypeText:
			totalTokens += ApproximateTokens(block.Text)
		case types.ContentTypeToolUse:
			inputJSON, _ := json.Marshal(block.ToolInput)
			totalTokens += ApproximateTokens(string(inputJSON))
			totalTokens += 10 // Overhead for tool call structure
		case types.ContentTypeToolResult:
			totalTokens += ApproximateTokens(block.ToolContent)
			totalTokens += 10 // Overhead for tool result structure
		}
	}
	return totalTokens + 4 // Message overhead
}
