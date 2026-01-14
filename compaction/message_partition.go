package compaction

import (
	"context"

	"github.com/google/uuid"
	"github.com/youssefsiam38/agentpg/driver"
)

// MessagePartition categorizes messages for compaction processing.
// Messages are partitioned into mutually exclusive categories based on their
// attributes and position in the conversation.
type MessagePartition struct {
	// Protected messages are within the ProtectedTokens zone at the end
	// of the conversation. These are never touched during compaction.
	Protected []*driver.Message

	// Preserved messages have is_preserved=true and are never removed.
	// They may be from any position in the conversation.
	Preserved []*driver.Message

	// Recent messages are within the PreserveLastN count.
	// These are never summarized or removed.
	Recent []*driver.Message

	// Summaries are previous compaction summaries (is_summary=true).
	// These are kept as-is and included in new summarization context.
	Summaries []*driver.Message

	// Compactable messages are eligible for summarization and removal.
	// These are the messages that will be archived and replaced with a summary.
	Compactable []*driver.Message

	// Stats contains token counts for each partition.
	Stats PartitionStats
}

// PartitionStats contains token statistics for each partition.
type PartitionStats struct {
	ProtectedTokens   int
	PreservedTokens   int
	RecentTokens      int
	SummaryTokens     int
	CompactableTokens int
	TotalTokens       int
}

// Partitioner handles the logic of partitioning messages for compaction.
type Partitioner struct {
	tokenCounter *TokenCounter
	config       *Config
}

// NewPartitioner creates a new Partitioner with the given configuration.
func NewPartitioner(tokenCounter *TokenCounter, config *Config) *Partitioner {
	return &Partitioner{
		tokenCounter: tokenCounter,
		config:       config,
	}
}

// Partition categorizes messages into compaction groups.
// Messages are processed in reverse order (newest first) to correctly identify
// protected and recent messages.
func (p *Partitioner) Partition(ctx context.Context, messages []*driver.Message) (*MessagePartition, error) {
	if len(messages) == 0 {
		return &MessagePartition{}, nil
	}

	// Count tokens for all messages
	result, err := p.tokenCounter.CountTokens(ctx, messages)
	if err != nil {
		return nil, err
	}

	partition := &MessagePartition{
		Protected:   make([]*driver.Message, 0),
		Preserved:   make([]*driver.Message, 0),
		Recent:      make([]*driver.Message, 0),
		Summaries:   make([]*driver.Message, 0),
		Compactable: make([]*driver.Message, 0),
	}

	// Track which messages are in which categories
	// A message can only be in one category (mutually exclusive)
	messageCategories := make(map[uuid.UUID]string)
	perMessageTokens := p.estimatePerMessageTokens(messages, result)

	// First pass: identify protected zone (newest messages within ProtectedTokens limit)
	protectedTokenSum := 0
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		tokens := perMessageTokens[i]

		if protectedTokenSum+tokens <= p.config.ProtectedTokens {
			protectedTokenSum += tokens
			messageCategories[msg.ID] = "protected"
			partition.Protected = append([]*driver.Message{msg}, partition.Protected...)
			partition.Stats.ProtectedTokens += tokens
		} else {
			break
		}
	}

	// Second pass: identify recent messages (last N messages not already protected)
	recentCount := 0
	for i := len(messages) - 1; i >= 0 && recentCount < p.config.PreserveLastN; i-- {
		msg := messages[i]
		if _, exists := messageCategories[msg.ID]; !exists {
			tokens := perMessageTokens[i]
			messageCategories[msg.ID] = "recent"
			partition.Recent = append([]*driver.Message{msg}, partition.Recent...)
			partition.Stats.RecentTokens += tokens
			recentCount++
		}
	}

	// Third pass: categorize remaining messages
	for i, msg := range messages {
		if _, exists := messageCategories[msg.ID]; exists {
			continue // Already categorized
		}

		tokens := perMessageTokens[i]

		switch {
		case msg.IsSummary:
			messageCategories[msg.ID] = "summary"
			partition.Summaries = append(partition.Summaries, msg)
			partition.Stats.SummaryTokens += tokens

		case msg.IsPreserved:
			messageCategories[msg.ID] = "preserved"
			partition.Preserved = append(partition.Preserved, msg)
			partition.Stats.PreservedTokens += tokens

		default:
			messageCategories[msg.ID] = "compactable"
			partition.Compactable = append(partition.Compactable, msg)
			partition.Stats.CompactableTokens += tokens
		}
	}

	partition.Stats.TotalTokens = result.TotalTokens

	return partition, nil
}

// estimatePerMessageTokens estimates tokens per message.
// If the token counter provided per-message estimates (from approximation),
// use those. Otherwise, distribute the total evenly as a rough estimate.
func (p *Partitioner) estimatePerMessageTokens(messages []*driver.Message, result *TokenCountResult) []int {
	if len(result.PerMessage) == len(messages) {
		return result.PerMessage
	}

	// API was used - we only have total. Use approximation for per-message.
	perMessage := make([]int, len(messages))
	for i, msg := range messages {
		perMessage[i] = p.tokenCounter.estimateMessageTokens(msg)
	}
	return perMessage
}

// CanCompact returns true if there are messages eligible for compaction.
func (p *MessagePartition) CanCompact() bool {
	return len(p.Compactable) > 0
}

// CompactableIDs returns the IDs of all compactable messages.
func (p *MessagePartition) CompactableIDs() []uuid.UUID {
	ids := make([]uuid.UUID, len(p.Compactable))
	for i, msg := range p.Compactable {
		ids[i] = msg.ID
	}
	return ids
}

// AllPreservedIDs returns the IDs of all messages that should be preserved
// (protected, preserved, recent, and summaries).
func (p *MessagePartition) AllPreservedIDs() []uuid.UUID {
	total := len(p.Protected) + len(p.Preserved) + len(p.Recent) + len(p.Summaries)
	ids := make([]uuid.UUID, 0, total)

	for _, msg := range p.Protected {
		ids = append(ids, msg.ID)
	}
	for _, msg := range p.Preserved {
		ids = append(ids, msg.ID)
	}
	for _, msg := range p.Recent {
		ids = append(ids, msg.ID)
	}
	for _, msg := range p.Summaries {
		ids = append(ids, msg.ID)
	}

	return ids
}

// MessagesForSummarization returns all compactable messages in chronological order.
// These are the messages that will be summarized.
func (p *MessagePartition) MessagesForSummarization() []*driver.Message {
	return p.Compactable
}

// ContextMessages returns messages that should be included in the summarization
// context but not summarized themselves. This includes previous summaries and
// preserved messages to maintain continuity.
func (p *MessagePartition) ContextMessages() []*driver.Message {
	result := make([]*driver.Message, 0, len(p.Summaries)+len(p.Preserved))
	result = append(result, p.Summaries...)
	result = append(result, p.Preserved...)
	return result
}

// TokenReductionEstimate estimates how many tokens could be saved by compaction.
// This is calculated as: compactable tokens - estimated summary size.
func (p *MessagePartition) TokenReductionEstimate(summaryTokens int) int {
	reduction := p.Stats.CompactableTokens - summaryTokens
	if reduction < 0 {
		return 0
	}
	return reduction
}
