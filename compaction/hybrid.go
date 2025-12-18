package compaction

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/youssefsiam38/agentpg/driver"
)

// HybridStrategy implements a two-phase compaction approach:
// 1. First, prune tool outputs from compactable messages (free, no API call)
// 2. If still over target, summarize remaining messages using the summarization strategy
type HybridStrategy struct {
	summarizer          *Summarizer
	tokenCounter        *TokenCounter
	config              *Config
	preserveToolOutputs bool
}

// NewHybridStrategy creates a new hybrid compaction strategy.
func NewHybridStrategy(summarizer *Summarizer, tokenCounter *TokenCounter, config *Config) *HybridStrategy {
	return &HybridStrategy{
		summarizer:          summarizer,
		tokenCounter:        tokenCounter,
		config:              config,
		preserveToolOutputs: config.PreserveToolOutputs,
	}
}

// Name returns the strategy name.
func (h *HybridStrategy) Name() Strategy {
	return StrategyHybrid
}

// Execute performs the hybrid compaction strategy.
func (h *HybridStrategy) Execute(ctx context.Context, partition *MessagePartition) (*StrategyResult, error) {
	if !partition.CanCompact() {
		return nil, ErrNoMessagesToCompact
	}

	start := time.Now()

	// Phase 1: Prune tool outputs if enabled
	var prunedMessages []*driver.Message
	var tokensSavedByPruning int

	if !h.preserveToolOutputs {
		prunedMessages, tokensSavedByPruning = h.pruneToolOutputs(partition.Compactable)
	} else {
		prunedMessages = partition.Compactable
	}

	// Calculate tokens after pruning
	tokensAfterPruning := partition.Stats.TotalTokens - tokensSavedByPruning

	// Check if pruning alone is sufficient
	if tokensAfterPruning <= h.config.TargetTokens {
		// Pruning was sufficient - no summarization needed
		return &StrategyResult{
			SummaryText:        "", // No summary created
			SummaryTokens:      0,
			ArchivedMessageIDs: h.findPrunedMessageIDs(partition.Compactable, prunedMessages),
			TokensRemoved:      tokensSavedByPruning,
			TokensAfter:        tokensAfterPruning,
			Duration:           time.Since(start),
		}, nil
	}

	// Phase 2: Summarization needed
	// Create a modified partition with the pruned messages
	prunedPartition := &MessagePartition{
		Protected:   partition.Protected,
		Preserved:   partition.Preserved,
		Recent:      partition.Recent,
		Summaries:   partition.Summaries,
		Compactable: prunedMessages,
		Stats: PartitionStats{
			ProtectedTokens:   partition.Stats.ProtectedTokens,
			PreservedTokens:   partition.Stats.PreservedTokens,
			RecentTokens:      partition.Stats.RecentTokens,
			SummaryTokens:     partition.Stats.SummaryTokens,
			CompactableTokens: partition.Stats.CompactableTokens - tokensSavedByPruning,
			TotalTokens:       tokensAfterPruning,
		},
	}

	// Get context messages
	contextMsgs := prunedPartition.ContextMessages()

	// Summarize the pruned compactable messages
	summaryText, err := h.summarizer.SummarizeWithContext(ctx, contextMsgs, prunedPartition.Compactable)
	if err != nil {
		return nil, err
	}

	// Estimate tokens for the summary
	summaryTokens, err := h.tokenCounter.CountTokensForContent(ctx, summaryText)
	if err != nil {
		summaryTokens = approximateTokens(summaryText)
	}

	// Calculate final token reduction
	totalTokensRemoved := partition.Stats.CompactableTokens
	tokensAfter := partition.Stats.TotalTokens - totalTokensRemoved + summaryTokens

	return &StrategyResult{
		SummaryText:        summaryText,
		SummaryTokens:      summaryTokens,
		ArchivedMessageIDs: partition.CompactableIDs(),
		TokensRemoved:      totalTokensRemoved,
		TokensAfter:        tokensAfter,
		Duration:           time.Since(start),
	}, nil
}

// pruneToolOutputs replaces tool output content with a placeholder.
// Returns the modified messages and estimated tokens saved.
func (h *HybridStrategy) pruneToolOutputs(messages []*driver.Message) ([]*driver.Message, int) {
	const prunedPlaceholder = "[TOOL OUTPUT PRUNED]"
	tokensSaved := 0
	prunedMessages := make([]*driver.Message, 0, len(messages))

	for _, msg := range messages {
		// Skip messages that shouldn't be pruned
		if msg.IsPreserved || msg.IsSummary {
			prunedMessages = append(prunedMessages, msg)
			continue
		}

		// Check if message has tool results to prune
		hasPrunableContent := false
		for _, block := range msg.Content {
			if block.Type == "tool_result" && len(block.ToolContent) > len(prunedPlaceholder) {
				hasPrunableContent = true
				break
			}
		}

		if !hasPrunableContent {
			prunedMessages = append(prunedMessages, msg)
			continue
		}

		// Create a copy of the message with pruned content
		prunedMsg := &driver.Message{
			ID:          msg.ID,
			SessionID:   msg.SessionID,
			RunID:       msg.RunID,
			Role:        msg.Role,
			IsPreserved: msg.IsPreserved,
			IsSummary:   msg.IsSummary,
			Metadata:    msg.Metadata,
			CreatedAt:   msg.CreatedAt,
			UpdatedAt:   msg.UpdatedAt,
		}

		prunedContent := make([]driver.ContentBlock, 0, len(msg.Content))
		for _, block := range msg.Content {
			if block.Type == "tool_result" && len(block.ToolContent) > len(prunedPlaceholder) {
				// Calculate tokens saved
				originalTokens := approximateTokens(block.ToolContent)
				placeholderTokens := approximateTokens(prunedPlaceholder)
				tokensSaved += originalTokens - placeholderTokens

				// Create pruned block
				prunedBlock := driver.ContentBlock{
					Type:               block.Type,
					ToolResultForUseID: block.ToolResultForUseID,
					ToolContent:        prunedPlaceholder,
					IsError:            block.IsError,
					Metadata:           block.Metadata,
				}
				prunedContent = append(prunedContent, prunedBlock)
			} else {
				prunedContent = append(prunedContent, block)
			}
		}

		prunedMsg.Content = prunedContent
		prunedMessages = append(prunedMessages, prunedMsg)
	}

	return prunedMessages, tokensSaved
}

// findPrunedMessageIDs returns the IDs of messages that were modified during pruning.
// These are the messages where tool outputs were replaced with placeholders.
func (h *HybridStrategy) findPrunedMessageIDs(original, pruned []*driver.Message) []uuid.UUID {
	prunedIDs := make([]uuid.UUID, 0)

	for i, msg := range original {
		if i < len(pruned) && h.wasMessagePruned(msg, pruned[i]) {
			prunedIDs = append(prunedIDs, msg.ID)
		}
	}

	return prunedIDs
}

// wasMessagePruned checks if a message was modified during pruning.
func (h *HybridStrategy) wasMessagePruned(original, pruned *driver.Message) bool {
	if original.ID != pruned.ID {
		return false
	}

	for i, block := range original.Content {
		if i >= len(pruned.Content) {
			return true
		}
		if block.Type == "tool_result" &&
			block.ToolContent != pruned.Content[i].ToolContent {
			return true
		}
	}

	return false
}
