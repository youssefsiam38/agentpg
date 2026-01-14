package compaction

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/youssefsiam38/agentpg/driver"
)

// StrategyExecutor defines the interface for compaction strategy implementations.
// Each strategy handles the actual compaction logic differently.
type StrategyExecutor interface {
	// Name returns the strategy name (e.g., "summarization", "hybrid").
	Name() Strategy

	// Execute performs the compaction on the given messages.
	// It returns the result including the summary (if any) and the list of
	// message IDs to archive.
	Execute(ctx context.Context, partition *MessagePartition) (*StrategyResult, error)
}

// StrategyResult contains the result of executing a compaction strategy.
type StrategyResult struct {
	// SummaryText is the generated summary text (may be empty for prune-only).
	SummaryText string

	// SummaryTokens is the estimated token count of the summary.
	SummaryTokens int

	// ArchivedMessageIDs is the list of message IDs that should be archived.
	ArchivedMessageIDs []uuid.UUID

	// TokensRemoved is the estimated number of tokens removed.
	TokensRemoved int

	// TokensAfter is the estimated token count after compaction.
	TokensAfter int

	// Duration is how long the strategy execution took.
	Duration time.Duration
}

// StrategyFactory creates strategy executors based on configuration.
type StrategyFactory struct {
	config       *Config
	tokenCounter *TokenCounter
	summarizer   *Summarizer
}

// NewStrategyFactory creates a new strategy factory.
func NewStrategyFactory(config *Config, tokenCounter *TokenCounter, summarizer *Summarizer) *StrategyFactory {
	return &StrategyFactory{
		config:       config,
		tokenCounter: tokenCounter,
		summarizer:   summarizer,
	}
}

// Create returns the appropriate strategy executor for the configured strategy.
func (f *StrategyFactory) Create() StrategyExecutor {
	switch f.config.Strategy {
	case StrategySummarization:
		return NewSummarizationStrategy(f.summarizer, f.tokenCounter)
	case StrategyHybrid:
		return NewHybridStrategy(f.summarizer, f.tokenCounter, f.config)
	default:
		// Default to hybrid if unknown
		return NewHybridStrategy(f.summarizer, f.tokenCounter, f.config)
	}
}

// CompactionContext contains all the context needed for a compaction operation.
type CompactionContext struct {
	// SessionID is the session being compacted.
	SessionID uuid.UUID

	// Messages is the full list of messages in the session.
	Messages []*driver.Message

	// Partition is the result of partitioning the messages.
	Partition *MessagePartition

	// Config is the compaction configuration.
	Config *Config

	// TargetTokens is the target token count after compaction.
	TargetTokens int
}

// ValidateForCompaction checks if compaction should proceed.
func (c *CompactionContext) ValidateForCompaction() error {
	if len(c.Messages) == 0 {
		return ErrNoMessagesToCompact
	}
	if c.Partition == nil {
		return NewCompactionError("Validate", ErrInvalidConfig).
			WithContext("reason", "partition is nil")
	}
	if !c.Partition.CanCompact() {
		return ErrNoMessagesToCompact
	}
	return nil
}
