package compaction

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/google/uuid"
	"github.com/youssefsiam38/agentpg/driver"
)

// Logger interface for compaction logging.
type Logger interface {
	Debug(msg string, args ...any)
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
}

// noopLogger is a no-op implementation of Logger.
type noopLogger struct{}

func (noopLogger) Debug(msg string, args ...any) {}
func (noopLogger) Info(msg string, args ...any)  {}
func (noopLogger) Warn(msg string, args ...any)  {}
func (noopLogger) Error(msg string, args ...any) {}

// Result contains the outcome of a compaction operation.
type Result struct {
	// EventID is the ID of the compaction event record.
	EventID uuid.UUID

	// Strategy is the strategy that was used.
	Strategy Strategy

	// OriginalTokens is the token count before compaction.
	OriginalTokens int

	// CompactedTokens is the token count after compaction.
	CompactedTokens int

	// MessagesRemoved is the number of messages archived.
	MessagesRemoved int

	// PreservedMessageIDs is the list of message IDs that were preserved.
	PreservedMessageIDs []uuid.UUID

	// SummaryCreated indicates whether a summary message was created.
	SummaryCreated bool

	// Duration is how long the compaction took.
	Duration time.Duration
}

// Stats contains statistics about a session's compaction state.
type Stats struct {
	// SessionID is the session being analyzed.
	SessionID uuid.UUID

	// TotalMessages is the number of messages in the session.
	TotalMessages int

	// TotalTokens is the estimated total token count.
	TotalTokens int

	// UsagePercent is the percentage of context window used.
	UsagePercent float64

	// CompactionCount is the number of times this session has been compacted.
	CompactionCount int

	// PreservedMessages is the count of preserved (non-compactable) messages.
	PreservedMessages int

	// SummaryMessages is the count of summary messages from previous compactions.
	SummaryMessages int

	// CompactableMessages is the count of messages eligible for compaction.
	CompactableMessages int

	// NeedsCompaction indicates if compaction should be triggered.
	NeedsCompaction bool
}

// Compactor provides context compaction for agent sessions.
// It uses the generic type parameter TTx to work with different database drivers.
type Compactor[TTx any] struct {
	store        driver.Store[TTx]
	anthropic    *anthropic.Client
	config       *Config
	logger       Logger
	strategy     StrategyExecutor
	partitioner  *Partitioner
	tokenCounter *TokenCounter
}

// New creates a new Compactor with the given configuration.
// If config is nil, default configuration is used.
func New[TTx any](store driver.Store[TTx], client *anthropic.Client, config *Config, logger Logger) *Compactor[TTx] {
	if config == nil {
		config = DefaultConfig()
	} else {
		config.ApplyDefaults()
	}

	if logger == nil {
		logger = noopLogger{}
	}

	tokenCounter := NewTokenCounter(client, config.SummarizerModel, config.UseTokenCountingAPI)
	summarizer := NewSummarizer(client, config.SummarizerModel, config.SummarizerMaxTokens)
	partitioner := NewPartitioner(tokenCounter, config)
	factory := NewStrategyFactory(config, tokenCounter, summarizer)

	return &Compactor[TTx]{
		store:        store,
		anthropic:    client,
		config:       config,
		logger:       logger,
		strategy:     factory.Create(),
		partitioner:  partitioner,
		tokenCounter: tokenCounter,
	}
}

// NeedsCompaction checks if a session needs compaction based on token usage.
func (c *Compactor[TTx]) NeedsCompaction(ctx context.Context, sessionID uuid.UUID) (bool, error) {
	stats, err := c.GetStats(ctx, sessionID)
	if err != nil {
		return false, err
	}
	return stats.NeedsCompaction, nil
}

// GetStats returns statistics about a session's compaction state.
func (c *Compactor[TTx]) GetStats(ctx context.Context, sessionID uuid.UUID) (*Stats, error) {
	messages, err := c.store.GetMessages(ctx, sessionID, 0) // 0 = no limit
	if err != nil {
		return nil, WrapError("GetStats", fmt.Errorf("%w: %v", ErrStorageError, err))
	}

	session, err := c.store.GetSession(ctx, sessionID)
	if err != nil {
		return nil, WrapError("GetStats", fmt.Errorf("%w: %v", ErrSessionNotFound, err))
	}

	// Count tokens
	result, err := c.tokenCounter.CountTokens(ctx, messages)
	if err != nil {
		return nil, WrapError("GetStats", err)
	}

	// Partition messages to get counts
	partition, err := c.partitioner.Partition(ctx, messages)
	if err != nil {
		return nil, WrapError("GetStats", err)
	}

	usagePercent := float64(result.TotalTokens) / float64(c.config.MaxTokensForModel)
	needsCompaction := result.TotalTokens >= c.config.TriggerThreshold()

	return &Stats{
		SessionID:           sessionID,
		TotalMessages:       len(messages),
		TotalTokens:         result.TotalTokens,
		UsagePercent:        usagePercent,
		CompactionCount:     session.CompactionCount,
		PreservedMessages:   len(partition.Protected) + len(partition.Preserved) + len(partition.Recent),
		SummaryMessages:     len(partition.Summaries),
		CompactableMessages: len(partition.Compactable),
		NeedsCompaction:     needsCompaction,
	}, nil
}

// Compact performs compaction on the specified session.
func (c *Compactor[TTx]) Compact(ctx context.Context, sessionID uuid.UUID) (*Result, error) {
	start := time.Now()

	c.logger.Info("starting compaction", "session_id", sessionID, "strategy", c.config.Strategy)

	// Get all messages for the session
	messages, err := c.store.GetMessages(ctx, sessionID, 0)
	if err != nil {
		return nil, NewCompactionError("GetMessages", fmt.Errorf("%w: %v", ErrStorageError, err)).
			WithSession(sessionID)
	}

	if len(messages) == 0 {
		return nil, NewCompactionError("Compact", ErrNoMessagesToCompact).
			WithSession(sessionID)
	}

	// Partition messages
	partition, err := c.partitioner.Partition(ctx, messages)
	if err != nil {
		return nil, NewCompactionError("Partition", err).
			WithSession(sessionID)
	}

	if !partition.CanCompact() {
		c.logger.Info("no messages eligible for compaction", "session_id", sessionID)
		return nil, NewCompactionError("Compact", ErrNoMessagesToCompact).
			WithSession(sessionID)
	}

	c.logger.Debug("partition complete",
		"session_id", sessionID,
		"compactable", len(partition.Compactable),
		"protected", len(partition.Protected),
		"preserved", len(partition.Preserved),
		"recent", len(partition.Recent),
		"summaries", len(partition.Summaries),
	)

	// Execute the compaction strategy
	strategyResult, err := c.strategy.Execute(ctx, partition)
	if err != nil {
		return nil, NewCompactionError("ExecuteStrategy", err).
			WithSession(sessionID).
			WithContext("strategy", string(c.config.Strategy))
	}

	// Create the compaction event
	var summaryContent *string
	if strategyResult.SummaryText != "" {
		summaryContent = &strategyResult.SummaryText
	}

	var modelUsed *string
	if summaryContent != nil {
		modelUsed = &c.config.SummarizerModel
	}

	durationMS := strategyResult.Duration.Milliseconds()
	event, err := c.store.CreateCompactionEvent(ctx, driver.CreateCompactionEventParams{
		SessionID:           sessionID,
		Strategy:            string(c.config.Strategy),
		OriginalTokens:      partition.Stats.TotalTokens,
		CompactedTokens:     strategyResult.TokensAfter,
		MessagesRemoved:     len(strategyResult.ArchivedMessageIDs),
		SummaryContent:      summaryContent,
		PreservedMessageIDs: partition.AllPreservedIDs(),
		ModelUsed:           modelUsed,
		DurationMS:          &durationMS,
	})
	if err != nil {
		return nil, NewCompactionError("CreateCompactionEvent", fmt.Errorf("%w: %v", ErrStorageError, err)).
			WithSession(sessionID)
	}

	// Archive messages before deleting
	for _, msg := range partition.Compactable {
		originalJSON, err := json.Marshal(msg)
		if err != nil {
			c.logger.Warn("failed to marshal message for archive",
				"message_id", msg.ID,
				"error", err,
			)
			continue
		}

		var originalMap map[string]any
		if err := json.Unmarshal(originalJSON, &originalMap); err != nil {
			c.logger.Warn("failed to unmarshal message for archive",
				"message_id", msg.ID,
				"error", err,
			)
			continue
		}

		if err := c.store.ArchiveMessage(ctx, event.ID, msg.ID, sessionID, originalMap); err != nil {
			c.logger.Warn("failed to archive message",
				"message_id", msg.ID,
				"error", err,
			)
			// Continue with other messages
		}
	}

	// Delete archived messages
	for _, msgID := range strategyResult.ArchivedMessageIDs {
		if err := c.store.DeleteMessage(ctx, msgID); err != nil {
			c.logger.Warn("failed to delete archived message",
				"message_id", msgID,
				"error", err,
			)
			// Continue with other deletions
		}
	}

	// Create summary message if one was generated
	summaryCreated := false
	if strategyResult.SummaryText != "" {
		_, err := c.store.CreateMessage(ctx, driver.CreateMessageParams{
			SessionID: sessionID,
			Role:      "assistant",
			Content: []driver.ContentBlock{
				{
					Type: "text",
					Text: strategyResult.SummaryText,
				},
			},
			IsSummary: true,
		})
		if err != nil {
			c.logger.Warn("failed to create summary message",
				"session_id", sessionID,
				"error", err,
			)
			// Don't fail the whole operation for this
		} else {
			summaryCreated = true
		}
	}

	// Update session compaction count
	if err := c.store.UpdateSession(ctx, sessionID, map[string]any{
		"compaction_count": 1, // This should increment, handled by SQL
	}); err != nil {
		c.logger.Warn("failed to update session compaction count",
			"session_id", sessionID,
			"error", err,
		)
	}

	result := &Result{
		EventID:             event.ID,
		Strategy:            c.config.Strategy,
		OriginalTokens:      partition.Stats.TotalTokens,
		CompactedTokens:     strategyResult.TokensAfter,
		MessagesRemoved:     len(strategyResult.ArchivedMessageIDs),
		PreservedMessageIDs: partition.AllPreservedIDs(),
		SummaryCreated:      summaryCreated,
		Duration:            time.Since(start),
	}

	c.logger.Info("compaction complete",
		"session_id", sessionID,
		"strategy", c.config.Strategy,
		"original_tokens", result.OriginalTokens,
		"compacted_tokens", result.CompactedTokens,
		"messages_removed", result.MessagesRemoved,
		"summary_created", result.SummaryCreated,
		"duration_ms", result.Duration.Milliseconds(),
	)

	return result, nil
}

// CompactIfNeeded performs compaction only if the session exceeds the trigger threshold.
// Returns nil result if compaction was not needed.
func (c *Compactor[TTx]) CompactIfNeeded(ctx context.Context, sessionID uuid.UUID) (*Result, error) {
	needs, err := c.NeedsCompaction(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	if !needs {
		c.logger.Debug("compaction not needed", "session_id", sessionID)
		return nil, nil
	}

	return c.Compact(ctx, sessionID)
}

// Config returns the compactor's configuration.
func (c *Compactor[TTx]) Config() *Config {
	return c.config
}
