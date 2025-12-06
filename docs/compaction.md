# Context Compaction Guide

This guide explains how AgentPG manages context windows for long conversations through intelligent compaction strategies.

## The Problem

Claude models have finite context windows (e.g., 200K tokens). As conversations grow, you must either:
1. **Truncate** - Lose old messages entirely
2. **Summarize** - Compress old messages into summaries
3. **Hybrid** - Smart combination of strategies

AgentPG implements option 3 with full auditability and rollback support.

## How Compaction Works

### Overview

```
┌──────────────────────────────────────────────────────────────────┐
│                    Before Compaction                              │
│  ┌─────────┬─────────┬─────────┬─────────┬─────────┬─────────┐  │
│  │ System  │ Msg 1   │ Msg 2   │ Msg 3   │ Msg 4   │ Msg 5   │  │
│  │ 1K      │ 30K     │ 40K     │ 35K     │ 50K     │ 20K     │  │
│  └─────────┴─────────┴─────────┴─────────┴─────────┴─────────┘  │
│  Total: 176K tokens (88% of 200K limit)                          │
└──────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌──────────────────────────────────────────────────────────────────┐
│                    After Compaction                               │
│  ┌─────────┬─────────────────────┬─────────┬─────────┐           │
│  │ System  │      Summary        │ Msg 4   │ Msg 5   │           │
│  │ 1K      │        8K           │ 50K     │ 20K     │           │
│  └─────────┴─────────────────────┴─────────┴─────────┘           │
│  Total: 79K tokens (40% of 200K limit)                           │
└──────────────────────────────────────────────────────────────────┘
```

### Process Flow

1. **Detection** - Monitor token usage against trigger threshold
2. **Partitioning** - Separate protected vs compactable messages
3. **Strategy Execution** - Apply selected compaction strategy
4. **Persistence** - Save atomically with transaction support
5. **Audit** - Record compaction event for analysis

---

## Configuration Options

### Basic Configuration

```go
agent, _ := agentpg.New(cfg,
    // Enable/disable auto-compaction (default: true)
    agentpg.WithAutoCompaction(true),

    // When to trigger (default: 0.85 = 85% of context)
    agentpg.WithCompactionTrigger(0.85),

    // Target tokens after compaction (default: 40% of max)
    agentpg.WithCompactionTarget(80000),

    // Strategy to use (default: hybrid)
    agentpg.WithCompactionStrategy(agentpg.HybridStrategy),
)
```

### Protection Settings

```go
agent, _ := agentpg.New(cfg,
    // Always preserve last N messages (default: 10)
    agentpg.WithCompactionPreserveN(10),

    // Never compact last N tokens (default: 40000)
    agentpg.WithCompactionProtectedTokens(40000),

    // Keep full tool outputs (default: false)
    agentpg.WithPreserveToolOutputs(true),
)
```

### Summarizer Configuration

```go
agent, _ := agentpg.New(cfg,
    // Model for generating summaries (default: haiku)
    agentpg.WithSummarizerModel("claude-3-5-haiku-20241022"),

    // Override context window size
    agentpg.WithMaxContextTokens(200000),
)
```

---

## Strategies

### Hybrid Strategy (Default)

The hybrid strategy is the recommended approach for most use cases.

**Process:**
1. **Phase 1: Prune Tool Outputs** - Replace large tool results with truncated versions
2. **Phase 2: Summarize** - If still over target, summarize old messages

**Benefits:**
- Preserves more conversation detail
- Reduces API costs (less summarization)
- Maintains tool context when possible

```go
agentpg.WithCompactionStrategy(agentpg.HybridStrategy)
```

### Summarization Strategy

Pure summarization approach using Claude to create conversation summaries.

**Process:**
1. Identify compactable messages
2. Send to summarizer model
3. Replace with summary message

**Benefits:**
- Maximum compression
- Consistent output size
- Good for conversation-heavy sessions

```go
agentpg.WithCompactionStrategy(agentpg.SummarizationStrategy)
```

---

## Message Protection

### Automatic Protection

These messages are automatically protected from compaction:

1. **System Messages** - Never compacted
2. **Recent Messages** - Last N messages (configurable)
3. **Recent Tokens** - Last N tokens (configurable)

### Manual Protection

Mark individual messages as preserved:

```go
// When creating messages
msg := &Message{
    // ...
    IsPreserved: true,  // Never compacted
}

// Messages marked is_preserved=true in the database are always kept
```

**Use Cases for Manual Protection:**
- Critical user instructions
- Important context that must persist
- Key decisions or agreements

---

## Compaction Events

Every compaction operation is logged for auditing:

```go
type CompactionEvent struct {
    ID                  string
    SessionID           string
    Strategy            string      // "hybrid", "summarization"
    OriginalTokens      int         // Tokens before
    CompactedTokens     int         // Tokens after
    MessagesRemoved     int         // Count of removed messages
    SummaryContent      string      // Generated summary
    PreservedMessageIDs []string    // IDs of preserved messages
    ModelUsed           string      // Summarizer model
    DurationMs          int64       // Operation time
    CreatedAt           time.Time
}
```

### Querying Compaction History

```go
history, err := store.GetCompactionHistory(ctx, sessionID)
for _, event := range history {
    fmt.Printf("Compaction %s: %d -> %d tokens (removed %d messages)\n",
        event.Strategy,
        event.OriginalTokens,
        event.CompactedTokens,
        event.MessagesRemoved,
    )
}
```

---

## Message Archive

Archived messages are preserved for potential rollback:

```sql
-- View archived messages for a session
SELECT
    ma.id,
    ma.original_message->>'role' as role,
    ma.archived_at,
    ce.strategy
FROM message_archive ma
JOIN compaction_events ce ON ma.compaction_event_id = ce.id
WHERE ma.session_id = $1
ORDER BY ma.archived_at DESC;
```

### Archive Retention

Archives are automatically cleaned up when:
- The session is deleted (CASCADE)
- The compaction event is deleted (CASCADE)

For manual cleanup:
```sql
-- Remove archives older than 90 days
DELETE FROM message_archive
WHERE archived_at < NOW() - INTERVAL '90 days';
```

---

## Transaction Safety

Compaction operations are atomic:

```go
// Inside compaction manager
tx, _ := store.BeginTx(ctx)
defer tx.Rollback(ctx)

// 1. Archive original messages
tx.ArchiveMessages(ctx, eventID, sessionID, toArchive)

// 2. Delete originals
tx.DeleteMessages(ctx, messageIDs)

// 3. Insert summary
tx.SaveMessage(ctx, summaryMessage)

// 4. Record event
tx.SaveCompactionEvent(ctx, event)

// Commit atomically
tx.Commit(ctx)
```

If any step fails, the entire operation rolls back.

---

## Hooks

Monitor and customize compaction:

```go
// Before compaction starts
agent.OnBeforeCompaction(func(ctx context.Context, sessionID string) error {
    log.Printf("Starting compaction for session %s", sessionID)
    return nil  // Return error to abort compaction
})

// After compaction completes
agent.OnAfterCompaction(func(ctx context.Context, result any) error {
    event := result.(*CompactionEvent)
    log.Printf("Compacted: %d -> %d tokens",
        event.OriginalTokens,
        event.CompactedTokens)

    // Send metrics
    metrics.RecordCompaction(event)
    return nil
})
```

---

## Tuning Guidelines

### High-Volume Chat Applications

```go
// Trigger early, compact aggressively
agentpg.WithCompactionTrigger(0.70),
agentpg.WithCompactionTarget(40000),
agentpg.WithCompactionPreserveN(5),
agentpg.WithSummarizerModel("claude-3-5-haiku-20241022"),
```

### Complex Tool-Heavy Agents

```go
// Preserve more context, especially tool outputs
agentpg.WithCompactionTrigger(0.90),
agentpg.WithCompactionPreserveN(20),
agentpg.WithCompactionProtectedTokens(60000),
agentpg.WithPreserveToolOutputs(true),
```

### Long-Running Research Sessions

```go
// Maximize context, high-quality summaries
agentpg.WithExtendedContext(true),
agentpg.WithCompactionTrigger(0.95),
agentpg.WithSummarizerModel("claude-sonnet-4-5-20250929"),
agentpg.WithCompactionPreserveN(30),
```

### Cost-Sensitive Applications

```go
// Use cheapest summarizer, aggressive compaction
agentpg.WithSummarizerModel("claude-3-5-haiku-20241022"),
agentpg.WithCompactionTrigger(0.60),
agentpg.WithCompactionTarget(30000),
agentpg.WithCompactionStrategy(agentpg.HybridStrategy),
```

---

## Metrics to Monitor

| Metric | Description | Target |
|--------|-------------|--------|
| Compaction frequency | How often compaction triggers | < 1 per 10 messages |
| Compression ratio | Original / Compacted tokens | 2:1 to 5:1 |
| Summary quality | Manual review of summaries | Preserve key info |
| Compaction duration | Time per compaction | < 5 seconds |
| Token utilization | Steady-state token usage | 40-70% of max |

---

## Troubleshooting

### Compaction Triggering Too Often

**Symptoms:** Compaction runs every few messages

**Solutions:**
- Increase `WithCompactionTrigger(0.90)`
- Decrease `WithCompactionTarget()`
- Use `WithExtendedContext(true)` for more headroom

### Important Context Being Lost

**Symptoms:** Agent forgetting critical information

**Solutions:**
- Increase `WithCompactionPreserveN(20)`
- Mark critical messages as `IsPreserved`
- Increase `WithCompactionProtectedTokens()`

### Summaries Missing Key Details

**Symptoms:** Poor summary quality

**Solutions:**
- Use better summarizer model: `claude-sonnet-4-5-20250929`
- Reduce amount being summarized at once
- Consider custom summarization prompts (advanced)

### Compaction Taking Too Long

**Symptoms:** > 10 second compaction times

**Solutions:**
- Use faster summarizer: `claude-3-5-haiku-20241022`
- Reduce messages being summarized
- Trigger compaction earlier (smaller batches)

---

## See Also

- [Architecture](./architecture.md) - Compaction in system context
- [Configuration](./configuration.md) - All configuration options
- [Storage](./storage.md) - Database schema for compaction
