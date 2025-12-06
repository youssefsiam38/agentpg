package compaction

import (
	"context"
	"time"

	"github.com/youssefsiam38/agentpg/types"
)

// CompactionResult contains the output of a compaction operation
type CompactionResult struct {
	Summary           string           `json:"summary"`
	PreservedMessages []*types.Message `json:"preserved_messages"`
	OriginalTokens    int              `json:"original_tokens"`
	CompactedTokens   int              `json:"compacted_tokens"`
	MessagesRemoved   int              `json:"messages_removed"`
	Strategy          string           `json:"strategy"`
	CompactedAt       time.Time        `json:"compacted_at"`
}

// CompactionConfig holds configurable thresholds
type CompactionConfig struct {
	// TriggerThreshold is percentage of context to trigger (e.g., 0.85 for 85%)
	TriggerThreshold float64

	// TargetTokens is target token count after compaction
	TargetTokens int

	// PreserveLastN always preserves last N messages
	PreserveLastN int

	// ProtectedTokens are recent tokens to never summarize (OpenCode: 40K)
	ProtectedTokens int

	// SummarizerModel is model for summarization (cheaper/faster)
	SummarizerModel string

	// MainModel is main conversation model
	MainModel string

	// MaxContextTokens is model's context window
	MaxContextTokens int

	// CustomInstructions for domain-specific preservation
	CustomInstructions string
}

// Strategy defines the interface for compaction strategies
type Strategy interface {
	// Name returns the strategy name
	Name() string

	// ShouldCompact checks if compaction is needed
	ShouldCompact(messages []*types.Message, config CompactionConfig) bool

	// Compact performs the compaction
	Compact(ctx context.Context, messages []*types.Message, config CompactionConfig) (*CompactionResult, error)
}

// DefaultConfig returns production-ready configuration for Claude Sonnet 4
func DefaultConfig() CompactionConfig {
	return CompactionConfig{
		TriggerThreshold:   0.85,  // 85% context utilization
		TargetTokens:       80000, // Target 80K after compaction
		PreserveLastN:      10,    // Always keep last 10 messages
		ProtectedTokens:    40000, // Never touch last 40K tokens (OpenCode pattern)
		SummarizerModel:    "claude-3-5-haiku-20241022",
		MainModel:          "claude-sonnet-4-5-20250929",
		MaxContextTokens:   200000,
		CustomInstructions: "",
	}
}
