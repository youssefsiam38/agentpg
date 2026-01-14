# Context Compaction

Long-running conversations can exceed Claude's context window (200K tokens). AgentPG provides automatic and manual compaction to manage context size while preserving important information.

## Table of Contents

1. [Overview](#overview)
2. [Configuration](#configuration)
3. [Compaction Strategies](#compaction-strategies)
4. [Message Partitioning](#message-partitioning)
5. [Client Methods](#client-methods)
6. [Token Counting](#token-counting)
7. [Database Schema](#database-schema)
8. [Usage Examples](#usage-examples)
9. [Best Practices](#best-practices)
10. [Troubleshooting](#troubleshooting)

---

## Overview

Context compaction reduces the number of tokens in a conversation while preserving essential information. This is critical for:

- **Long conversations**: Multi-turn dialogues that accumulate tokens over time
- **Tool-heavy sessions**: Tool outputs can be verbose and consume significant context
- **Cost optimization**: Smaller contexts reduce API costs
- **Reliability**: Prevents context overflow errors

### How It Works

1. **Token counting**: Estimate current context size
2. **Threshold check**: Compare against trigger threshold (default 85%)
3. **Message partitioning**: Categorize messages into protected/compactable groups
4. **Strategy execution**: Apply hybrid or summarization strategy
5. **Archive and replace**: Archive old messages, insert summary

---

## Configuration

### Basic Configuration

```go
import "github.com/youssefsiam38/agentpg/compaction"

client, _ := agentpg.NewClient(drv, &agentpg.ClientConfig{
    APIKey: apiKey,

    // Enable auto-compaction after each run
    AutoCompactionEnabled: true,

    // Compaction settings
    CompactionConfig: &compaction.Config{
        Strategy:            compaction.StrategyHybrid,
        Trigger:             0.85,       // 85% context usage threshold
        TargetTokens:        80000,      // Target after compaction
        PreserveLastN:       10,         // Always keep last 10 messages
        ProtectedTokens:     40000,      // Never touch last 40K tokens
        MaxTokensForModel:   200000,     // Claude's context window
        SummarizerModel:     "claude-3-5-haiku-20241022",
        SummarizerMaxTokens: 4096,
        PreserveToolOutputs: false,      // Prune tool outputs in hybrid mode
        UseTokenCountingAPI: true,       // Use Claude's token counting API
    },
})
```

### Configuration Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `Strategy` | `Strategy` | `StrategyHybrid` | Compaction strategy to use |
| `Trigger` | `float64` | `0.85` | Context usage threshold (0.0-1.0) |
| `TargetTokens` | `int` | `80000` | Desired token count after compaction |
| `PreserveLastN` | `int` | `10` | Minimum recent messages to preserve |
| `ProtectedTokens` | `int` | `40000` | End-of-conversation tokens never touched |
| `MaxTokensForModel` | `int` | `200000` | Claude's context window size |
| `SummarizerModel` | `string` | `claude-3-5-haiku-20241022` | Model for summarization |
| `SummarizerMaxTokens` | `int` | `4096` | Max tokens for summary response |
| `PreserveToolOutputs` | `bool` | `false` | Keep tool outputs in hybrid mode |
| `UseTokenCountingAPI` | `bool` | `true` | Use Claude API for token counting |

### Default Configuration

Use `DefaultConfig()` for production-tested defaults:

```go
config := compaction.DefaultConfig()
// Modify as needed
config.TargetTokens = 60000
```

---

## Compaction Strategies

### Hybrid Strategy (Default)

The hybrid strategy is a two-phase approach that minimizes API costs:

**Phase 1: Pruning (Free)**
- Replaces tool output content with `[TOOL OUTPUT PRUNED]` placeholder
- Preserves tool structure and metadata
- No API calls required

**Phase 2: Summarization (If Needed)**
- Only triggered if Phase 1 is insufficient
- Uses Claude to summarize remaining compactable messages
- Creates structured 9-section summary

```go
CompactionConfig: &compaction.Config{
    Strategy:            compaction.StrategyHybrid,
    PreserveToolOutputs: false,  // Enable pruning
}
```

**Best for**: Tool-heavy conversations where outputs are verbose but tool invocations themselves provide context.

### Summarization Strategy

Direct summarization of all compactable messages:

```go
CompactionConfig: &compaction.Config{
    Strategy: compaction.StrategySummarization,
}
```

**Best for**: General conversations without heavy tool usage, or when full context preservation is important.

### Summary Format

Both strategies create a structured 9-section summary:

1. **Primary Request and Intent** - Original user goal
2. **Key Technical Concepts** - Important technical details
3. **Files and Code Sections** - Referenced files and code
4. **Errors and Fixes** - Problems encountered and solutions
5. **Problem Solving** - Approach taken and rationale
6. **User Preferences and Constraints** - Expressed requirements
7. **Pending Tasks** - Outstanding items
8. **Current Work** - Active work in progress
9. **Next Step** - Immediate next action

---

## Message Partitioning

Messages are categorized into **5 mutually exclusive groups**:

| Category | Description | Compactable |
|----------|-------------|-------------|
| **Protected** | Within last `ProtectedTokens` | No |
| **Preserved** | Marked `is_preserved=true` | No |
| **Recent** | Last `PreserveLastN` messages | No |
| **Summaries** | Previous summaries (`is_summary=true`) | No |
| **Compactable** | Everything else | Yes |

### Partitioning Algorithm

```
1. Start from newest message, work backwards
2. First 40K tokens (ProtectedTokens) → Protected zone
3. Next 10 messages (PreserveLastN) not in Protected → Recent
4. Messages with is_summary=true → Summaries
5. Messages with is_preserved=true → Preserved
6. Everything else → Compactable
```

### Partition Statistics

```go
stats, _ := client.GetCompactionStats(ctx, sessionID)

fmt.Printf("Total: %d messages, %d tokens\n", stats.TotalMessages, stats.TotalTokens)
fmt.Printf("Usage: %.1f%%\n", stats.UsagePercent*100)
fmt.Printf("Protected: %d messages\n", stats.ProtectedMessages)
fmt.Printf("Preserved: %d messages\n", stats.PreservedMessages)
fmt.Printf("Summaries: %d messages\n", stats.SummaryMessages)
fmt.Printf("Compactable: %d messages\n", stats.CompactableMessages)
fmt.Printf("Needs compaction: %v\n", stats.NeedsCompaction)
```

### Preserving Important Messages

Mark critical messages to never be compacted:

```sql
UPDATE agentpg_messages
SET is_preserved = true
WHERE id = 'important-message-id';
```

---

## Client Methods

### Check If Compaction Needed

```go
needs, err := client.NeedsCompaction(ctx, sessionID)
if needs {
    // Context usage exceeds threshold
}
```

### Get Compaction Statistics

```go
stats, err := client.GetCompactionStats(ctx, sessionID)

fmt.Printf("Session: %s\n", stats.SessionID)
fmt.Printf("Total: %d messages, %d tokens\n", stats.TotalMessages, stats.TotalTokens)
fmt.Printf("Usage: %.1f%% of context\n", stats.UsagePercent*100)
fmt.Printf("Compaction count: %d\n", stats.CompactionCount)
fmt.Printf("Compactable: %d messages\n", stats.CompactableMessages)
```

### Manual Compaction

```go
// Always compact (returns error if no compactable messages)
result, err := client.Compact(ctx, sessionID)

// Only compact if threshold exceeded (returns nil if not needed)
result, err := client.CompactIfNeeded(ctx, sessionID)
```

### Compaction with Custom Config

```go
// One-off override of configuration
result, err := client.CompactWithConfig(ctx, sessionID, &compaction.Config{
    Strategy:     compaction.StrategySummarization,
    TargetTokens: 50000,
})
```

### Compaction Result

```go
type Result struct {
    EventID             uuid.UUID     // ID of compaction event record
    Strategy            Strategy      // Strategy that was used
    OriginalTokens      int           // Token count before compaction
    CompactedTokens     int           // Token count after compaction
    MessagesRemoved     int           // Number of messages archived
    PreservedMessageIDs []uuid.UUID   // IDs of preserved messages
    SummaryCreated      bool          // Whether a summary was created
    Duration            time.Duration // How long compaction took
}

// Usage
result, _ := client.Compact(ctx, sessionID)
fmt.Printf("Reduced: %d -> %d tokens (%.1f%% reduction)\n",
    result.OriginalTokens,
    result.CompactedTokens,
    float64(result.OriginalTokens-result.CompactedTokens)/float64(result.OriginalTokens)*100,
)
fmt.Printf("Archived: %d messages\n", result.MessagesRemoved)
fmt.Printf("Duration: %v\n", result.Duration)
```

---

## Token Counting

### Primary Method: Claude API

When `UseTokenCountingAPI` is `true` (default), the compaction package uses Claude's token counting API for accurate counts:

```go
// Accurate token counting via API
client.Messages.CountTokens()
```

### Fallback: Character Approximation

When the API is unavailable or fails, a character-based approximation is used:

- Formula: `tokens ≈ (characters + 3) / 4`
- Conservative estimate for images/documents (~200 tokens)
- Automatic fallback, no configuration needed

### Token Count Result

```go
type CountTokenResult struct {
    TotalTokens int                // Sum of all tokens
    UsedAPI     bool               // true if API used, false if approximation
    PerMessage  map[uuid.UUID]int  // Token count per message (approximation only)
}
```

---

## Database Schema

### Compaction Events Table

```sql
CREATE TABLE agentpg_compaction_events (
    id UUID PRIMARY KEY,
    session_id UUID REFERENCES agentpg_sessions(id) ON DELETE CASCADE,
    strategy TEXT NOT NULL,           -- 'summarization' or 'hybrid'
    original_tokens INTEGER NOT NULL,
    compacted_tokens INTEGER NOT NULL,
    messages_removed INTEGER NOT NULL,
    summary_content TEXT,
    preserved_message_ids JSONB,      -- Array of preserved message UUIDs
    model_used TEXT,
    duration_ms BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_compaction_events_session ON agentpg_compaction_events(session_id, created_at DESC);
```

### Message Archive Table

Archived messages are stored for potential recovery:

```sql
CREATE TABLE agentpg_message_archive (
    id UUID PRIMARY KEY,              -- Original message ID
    compaction_event_id UUID REFERENCES agentpg_compaction_events(id) ON DELETE CASCADE,
    session_id UUID REFERENCES agentpg_sessions(id) ON DELETE CASCADE,
    original_message JSONB NOT NULL,  -- Full message JSON
    archived_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_message_archive_event ON agentpg_message_archive(compaction_event_id);
CREATE INDEX idx_message_archive_session ON agentpg_message_archive(session_id, archived_at DESC);
```

### Querying Compaction History

```sql
-- Recent compaction events for a session
SELECT
    strategy,
    original_tokens,
    compacted_tokens,
    messages_removed,
    duration_ms,
    created_at
FROM agentpg_compaction_events
WHERE session_id = 'session-123'
ORDER BY created_at DESC
LIMIT 10;

-- Calculate total reduction
SELECT
    SUM(original_tokens - compacted_tokens) as tokens_saved,
    SUM(messages_removed) as total_messages_archived,
    COUNT(*) as compaction_count
FROM agentpg_compaction_events
WHERE session_id = 'session-123';
```

### Retrieving Archived Messages

```sql
-- Get archived messages from a compaction event
SELECT
    id,
    original_message->>'role' as role,
    original_message->>'content' as content_preview,
    archived_at
FROM agentpg_message_archive
WHERE compaction_event_id = 'event-123'
ORDER BY archived_at;
```

---

## Usage Examples

### Auto-Compaction

Enable automatic compaction after each run:

```go
client, _ := agentpg.NewClient(drv, &agentpg.ClientConfig{
    APIKey: apiKey,
    AutoCompactionEnabled: true,
    CompactionConfig: &compaction.Config{
        Strategy:     compaction.StrategyHybrid,
        Trigger:      0.85,
        TargetTokens: 80000,
    },
})

client.Start(ctx)

// Long conversation...
for i := 0; i < 100; i++ {
    response, _ := client.RunSync(ctx, sessionID, "assistant", userPrompt)
    // Auto-compaction triggers when threshold exceeded
}
```

### Manual Compaction with Monitoring

```go
// Check stats before compaction
statsBefore, _ := client.GetCompactionStats(ctx, sessionID)
fmt.Printf("Before: %d tokens (%.1f%%)\n", statsBefore.TotalTokens, statsBefore.UsagePercent*100)

// Compact if needed
if statsBefore.NeedsCompaction {
    result, err := client.Compact(ctx, sessionID)
    if err != nil {
        log.Fatalf("Compaction failed: %v", err)
    }

    fmt.Printf("Compacted: %d -> %d tokens\n", result.OriginalTokens, result.CompactedTokens)
    fmt.Printf("Archived: %d messages\n", result.MessagesRemoved)
}

// Check stats after
statsAfter, _ := client.GetCompactionStats(ctx, sessionID)
fmt.Printf("After: %d tokens (%.1f%%)\n", statsAfter.TotalTokens, statsAfter.UsagePercent*100)
```

### Strategy Comparison

```go
// Test hybrid strategy
hybridResult, _ := client.CompactWithConfig(ctx, session1, &compaction.Config{
    Strategy: compaction.StrategyHybrid,
})

// Test summarization strategy
summaryResult, _ := client.CompactWithConfig(ctx, session2, &compaction.Config{
    Strategy: compaction.StrategySummarization,
})

fmt.Printf("Hybrid: %d -> %d tokens, %d messages archived\n",
    hybridResult.OriginalTokens, hybridResult.CompactedTokens, hybridResult.MessagesRemoved)
fmt.Printf("Summarization: %d -> %d tokens, %d messages archived\n",
    summaryResult.OriginalTokens, summaryResult.CompactedTokens, summaryResult.MessagesRemoved)
```

### Preserving Critical Messages

```go
// Before important context, mark as preserved
_, _ = pool.Exec(ctx, `
    UPDATE agentpg_messages
    SET is_preserved = true
    WHERE session_id = $1 AND content LIKE '%IMPORTANT%'
`, sessionID)

// These messages will never be compacted
result, _ := client.Compact(ctx, sessionID)
fmt.Printf("Preserved messages: %d\n", len(result.PreservedMessageIDs))
```

---

## Best Practices

### Configuration Recommendations

| Scenario | Strategy | Trigger | TargetTokens | PreserveToolOutputs |
|----------|----------|---------|--------------|---------------------|
| Tool-heavy | Hybrid | 0.80 | 60000 | false |
| General chat | Summarization | 0.85 | 80000 | N/A |
| Cost-sensitive | Hybrid | 0.75 | 50000 | false |
| Context-critical | Summarization | 0.90 | 100000 | N/A |

### Trigger Threshold

- **Lower (0.70-0.80)**: More frequent compaction, smaller context, lower cost
- **Higher (0.85-0.95)**: Less frequent compaction, more context preserved
- **Recommended**: Start at 0.85, adjust based on your conversation patterns

### Target Tokens

- Set to approximately 40% of max context window
- Leave headroom for new messages and responses
- Consider average response size in your use case

### Preserving Context

1. **Mark critical messages**: Use `is_preserved=true` for essential context
2. **Adjust PreserveLastN**: Increase for highly interactive sessions
3. **Increase ProtectedTokens**: Protect more recent context

### Monitoring

```go
// Periodic monitoring
go func() {
    ticker := time.NewTicker(5 * time.Minute)
    for range ticker.C {
        stats, _ := client.GetCompactionStats(ctx, sessionID)
        if stats.UsagePercent > 0.90 {
            log.Printf("Warning: Session %s at %.1f%% context usage", sessionID, stats.UsagePercent*100)
        }
    }
}()
```

---

## Troubleshooting

### Common Issues

#### "No messages to compact"

**Cause**: All messages are protected, preserved, recent, or summaries.

**Solution**: Check your configuration:
```go
stats, _ := client.GetCompactionStats(ctx, sessionID)
fmt.Printf("Compactable messages: %d\n", stats.CompactableMessages)
```

If 0 compactable messages:
- Reduce `ProtectedTokens`
- Reduce `PreserveLastN`
- Check for too many `is_preserved=true` messages

#### Compaction not reducing enough tokens

**Cause**: Preserved/protected zones too large, or compactable content is minimal.

**Solutions**:
1. Reduce `ProtectedTokens` from 40000 to 20000
2. Reduce `PreserveLastN` from 10 to 5
3. Use summarization strategy instead of hybrid
4. Reduce `TargetTokens` to force more aggressive compaction

#### Token counting inaccurate

**Cause**: Using character approximation instead of API.

**Solution**: Ensure `UseTokenCountingAPI: true` and check for API errors in logs.

#### Compaction taking too long

**Cause**: Large number of messages or slow summarization.

**Solutions**:
1. Use hybrid strategy (pruning is instant)
2. Reduce context size before it grows too large
3. Use faster model (`claude-3-5-haiku-20241022`)

### Debugging Queries

```sql
-- Check message distribution
SELECT
    is_preserved,
    is_summary,
    COUNT(*) as count
FROM agentpg_messages
WHERE session_id = 'session-123'
GROUP BY is_preserved, is_summary;

-- Find large messages
SELECT
    id,
    role,
    LENGTH(content::text) as content_length
FROM agentpg_messages
WHERE session_id = 'session-123'
ORDER BY content_length DESC
LIMIT 10;

-- Check compaction history
SELECT
    strategy,
    original_tokens,
    compacted_tokens,
    messages_removed,
    created_at
FROM agentpg_compaction_events
WHERE session_id = 'session-123'
ORDER BY created_at DESC;
```

### Error Handling

```go
result, err := client.Compact(ctx, sessionID)
if err != nil {
    var compactErr *compaction.CompactionError
    if errors.As(err, &compactErr) {
        log.Printf("Compaction failed: op=%s, session=%s, err=%v",
            compactErr.Op, compactErr.SessionID, compactErr.Err)
    }

    switch {
    case errors.Is(err, compaction.ErrNoMessagesToCompact):
        log.Println("No messages eligible for compaction")
    case errors.Is(err, compaction.ErrSummarizationFailed):
        log.Println("Claude summarization API failed")
    case errors.Is(err, compaction.ErrTokenCountingFailed):
        log.Println("Token counting failed (using approximation)")
    default:
        log.Printf("Unknown error: %v", err)
    }
}
```
