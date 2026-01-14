// Package compaction provides context window management for AI agent conversations.
//
// When conversations grow too long, they can exceed Claude's context window limits.
// This package implements strategies to compact conversation history while preserving
// essential context.
//
// # Strategies
//
// The package supports two compaction strategies:
//
//   - Summarization (StrategySummarization): Uses Claude to create a structured summary
//     of older messages. The summary follows a 9-section format that preserves critical
//     information about the conversation.
//
//   - Hybrid (StrategyHybrid): A two-phase approach that first prunes tool outputs
//     (replacing them with "[TOOL OUTPUT PRUNED]" placeholders), then summarizes if
//     still needed. This is the default and most cost-effective strategy.
//
// # Usage
//
// Create a Compactor with your configuration:
//
//	compactor := compaction.New(store, anthropicClient, &compaction.Config{
//	    Strategy:        compaction.StrategyHybrid,
//	    Trigger:         0.85,     // Trigger at 85% context usage
//	    TargetTokens:    80000,    // Target after compaction
//	    PreserveLastN:   10,       // Always keep last 10 messages
//	    ProtectedTokens: 40000,    // Never touch last 40K tokens
//	}, logger)
//
// Check if compaction is needed and compact if necessary:
//
//	result, err := compactor.CompactIfNeeded(ctx, sessionID)
//	if err != nil {
//	    return err
//	}
//	if result != nil {
//	    log.Printf("Compacted: %d -> %d tokens", result.OriginalTokens, result.CompactedTokens)
//	}
//
// # Message Partitioning
//
// Messages are partitioned into mutually exclusive categories:
//
//   - Protected: Within the ProtectedTokens zone at the end of conversation.
//   - Preserved: Messages marked with is_preserved=true.
//   - Recent: Last PreserveLastN messages.
//   - Summaries: Previous compaction summary messages (is_summary=true).
//   - Compactable: Everything else, eligible for summarization.
//
// # Token Counting
//
// Token counting uses Claude's API for accurate counts, with a character-based
// approximation fallback (~4 characters per token) if the API is unavailable.
//
// # Database Integration
//
// The compactor uses the driver.Store interface for database operations:
//   - GetMessages: Retrieve session messages
//   - CreateMessage: Create summary messages
//   - DeleteMessage: Remove archived messages
//   - ArchiveMessage: Archive messages before deletion
//   - CreateCompactionEvent: Record compaction operations
//   - UpdateSession: Update compaction count
//
// # Thread Safety
//
// The Compactor is safe for concurrent use. However, concurrent compactions on the
// same session should be avoided as they may lead to inconsistent results.
package compaction
