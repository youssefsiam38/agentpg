package compaction

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/youssefsiam38/agentpg/driver"
	"github.com/youssefsiam38/agentpg/internal/convert"
	"github.com/youssefsiam38/agentpg/storage"
	"github.com/youssefsiam38/agentpg/types"
)

// Manager orchestrates compaction strategies and persistence
type Manager struct {
	strategies map[string]Strategy
	store      storage.Store
	driver     driver.Beginner // Stored as Beginner interface to avoid generic constraint
	counter    *TokenCounter
	config     CompactionConfig
}

// NewManager creates a new compaction manager.
// The driver parameter is used for transactional operations.
func NewManager[TTx any](
	client *anthropic.Client,
	store storage.Store,
	drv driver.Driver[TTx],
	config CompactionConfig,
) *Manager {
	m := &Manager{
		strategies: make(map[string]Strategy),
		store:      store,
		driver:     drv,
		counter:    NewTokenCounter(client),
		config:     config,
	}

	// Register default strategies
	m.RegisterStrategy(NewSummarizationStrategy(client))
	m.RegisterStrategy(NewHybridStrategy(client))

	return m
}

// RegisterStrategy adds a strategy to the manager
func (m *Manager) RegisterStrategy(strategy Strategy) {
	m.strategies[strategy.Name()] = strategy
}

// ShouldCompact checks if compaction is needed for a session
func (m *Manager) ShouldCompact(ctx context.Context, sessionID string) (bool, error) {
	messages, err := m.store.GetMessages(ctx, sessionID)
	if err != nil {
		return false, fmt.Errorf("failed to get messages: %w", err)
	}

	// Convert storage messages to agentpg messages
	agentMessages := convert.FromStorageMessages(messages)

	// Use hybrid strategy for check (default)
	strategy, ok := m.strategies["hybrid"]
	if !ok {
		// Fallback to simple token threshold
		totalTokens := SumTokens(agentMessages)
		threshold := int(float64(m.config.MaxContextTokens) * m.config.TriggerThreshold)
		return totalTokens >= threshold, nil
	}

	return strategy.ShouldCompact(agentMessages, m.config), nil
}

// Compact performs compaction on a session
func (m *Manager) Compact(
	ctx context.Context,
	sessionID string,
	strategyName string,
) (*CompactionResult, error) {
	strategy, ok := m.strategies[strategyName]
	if !ok {
		return nil, fmt.Errorf("unknown strategy: %s", strategyName)
	}

	// Get current messages
	messages, err := m.store.GetMessages(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get messages: %w", err)
	}

	// Convert to agentpg messages
	agentMessages := convert.FromStorageMessages(messages)

	if len(agentMessages) == 0 {
		return nil, fmt.Errorf("no messages to compact")
	}

	// Perform compaction
	start := time.Now()
	result, err := strategy.Compact(ctx, agentMessages, m.config)
	if err != nil {
		return nil, fmt.Errorf("compaction failed: %w", err)
	}

	if result == nil {
		return nil, nil // Nothing was compacted
	}

	duration := time.Since(start)

	// Archive removed messages for reversibility
	archivedMessages := m.getArchivedMessages(agentMessages, result.PreservedMessages)

	// Build compaction event
	event := &storage.CompactionEvent{
		SessionID:       sessionID,
		Strategy:        strategyName,
		OriginalTokens:  result.OriginalTokens,
		CompactedTokens: result.CompactedTokens,
		MessagesRemoved: result.MessagesRemoved,
		SummaryContent:  result.Summary,
		ModelUsed:       m.config.SummarizerModel,
		DurationMs:      duration.Milliseconds(),
		CreatedAt:       time.Now(),
	}

	// Build preserved message IDs
	event.PreservedMessageIDs = make([]string, len(result.PreservedMessages))
	for i, msg := range result.PreservedMessages {
		event.PreservedMessageIDs[i] = msg.ID
	}

	// Build old message IDs for deletion
	oldMessageIDs := make([]string, 0, len(agentMessages))
	for _, msg := range agentMessages {
		oldMessageIDs = append(oldMessageIDs, msg.ID)
	}

	// Build new storage messages
	newStorageMessages := make([]*storage.Message, len(result.PreservedMessages))
	for i, msg := range result.PreservedMessages {
		newStorageMessages[i] = convert.ToStorageMessage(msg)
	}

	// Check if there's already an executor in context (from parent Run/RunTx)
	if driver.ExecutorFromContext(ctx) != nil {
		// Use existing transaction - store methods will pick it up from context
		if err := m.compactInContext(ctx, sessionID, event, archivedMessages, oldMessageIDs, newStorageMessages); err != nil {
			return nil, err
		}
	} else if m.driver != nil {
		// No existing transaction, create a new one for atomicity
		if err := m.compactWithNewTx(ctx, sessionID, event, archivedMessages, oldMessageIDs, newStorageMessages); err != nil {
			return nil, err
		}
	} else {
		// No transaction support, execute directly
		if err := m.compactInContext(ctx, sessionID, event, archivedMessages, oldMessageIDs, newStorageMessages); err != nil {
			return nil, err
		}
	}

	return result, nil
}

// compactWithNewTx performs compaction atomically within a NEW transaction
// Used when no transaction exists in context
func (m *Manager) compactWithNewTx(
	ctx context.Context,
	sessionID string,
	event *storage.CompactionEvent,
	archivedMessages []*storage.Message,
	oldMessageIDs []string,
	newStorageMessages []*storage.Message,
) error {
	execTx, err := m.driver.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		if err := execTx.Rollback(ctx); err != nil {
			// Only log if it's a real error (not "already committed")
			log.Printf("agentpg/compaction: rollback failed: %v", err)
		}
	}()

	// Create context with executor
	txCtx := driver.WithExecutor(ctx, execTx)

	// Execute compaction operations
	if err := m.compactInContext(txCtx, sessionID, event, archivedMessages, oldMessageIDs, newStorageMessages); err != nil {
		return err
	}

	// Commit transaction
	if err := execTx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit compaction: %w", err)
	}

	return nil
}

// compactInContext performs compaction operations using the provided context
// If context has a transaction, store methods will use it automatically
func (m *Manager) compactInContext(
	ctx context.Context,
	sessionID string,
	event *storage.CompactionEvent,
	archivedMessages []*storage.Message,
	oldMessageIDs []string,
	newStorageMessages []*storage.Message,
) error {
	// Save compaction event
	if err := m.store.SaveCompactionEvent(ctx, event); err != nil {
		return fmt.Errorf("failed to save compaction event: %w", err)
	}

	// Archive removed messages
	if len(archivedMessages) > 0 {
		if err := m.store.ArchiveMessages(ctx, event.ID, archivedMessages); err != nil {
			return fmt.Errorf("failed to archive messages: %w", err)
		}
	}

	// Delete old messages
	if err := m.store.DeleteMessages(ctx, oldMessageIDs); err != nil {
		return fmt.Errorf("failed to delete old messages: %w", err)
	}

	// Save new compacted messages
	if err := m.store.SaveMessages(ctx, newStorageMessages); err != nil {
		return fmt.Errorf("failed to save compacted messages: %w", err)
	}

	// Update session compaction count
	if err := m.store.UpdateSessionCompactionCount(ctx, sessionID); err != nil {
		return fmt.Errorf("failed to update compaction count: %w", err)
	}

	return nil
}

// GetCompactionStats returns statistics for a session
func (m *Manager) GetCompactionStats(ctx context.Context, sessionID string) (*CompactionStats, error) {
	messages, err := m.store.GetMessages(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	history, err := m.store.GetCompactionHistory(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	agentMessages := convert.FromStorageMessages(messages)
	currentTokens := SumTokens(agentMessages)

	stats := &CompactionStats{
		CurrentTokens:   currentTokens,
		MaxTokens:       m.config.MaxContextTokens,
		UtilizationPct:  float64(currentTokens) / float64(m.config.MaxContextTokens) * 100,
		MessageCount:    len(messages),
		CompactionCount: len(history),
		ShouldCompact:   currentTokens >= int(float64(m.config.MaxContextTokens)*m.config.TriggerThreshold),
	}

	return stats, nil
}

// CompactionStats contains session compaction statistics
type CompactionStats struct {
	CurrentTokens   int
	MaxTokens       int
	UtilizationPct  float64
	MessageCount    int
	CompactionCount int
	ShouldCompact   bool
}

// getArchivedMessages returns messages that were removed during compaction
func (m *Manager) getArchivedMessages(
	original []*types.Message,
	preserved []*types.Message,
) []*storage.Message {
	preservedIDs := make(map[string]bool)
	for _, msg := range preserved {
		preservedIDs[msg.ID] = true
	}

	var archived []*storage.Message
	for _, msg := range original {
		if !preservedIDs[msg.ID] && !msg.IsSummary {
			archived = append(archived, convert.ToStorageMessage(msg))
		}
	}

	return archived
}
