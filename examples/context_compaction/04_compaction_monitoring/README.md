# Compaction Monitoring Example

This example demonstrates comprehensive monitoring and auditing of compaction events.

## Features

- **Event Tracking** - Record all compaction events with metadata
- **Timing Metrics** - Measure compaction duration
- **Statistics** - Calculate aggregate metrics
- **Structured Logging** - Consistent log format for observability

## CompactionMonitor

```go
type CompactionMonitor struct {
    events []CompactionEvent
}

type CompactionEvent struct {
    Timestamp        time.Time
    SessionID        string
    Strategy         string
    OriginalTokens   int
    CompactedTokens  int
    Reduction        float64
    MessagesRemoved  int
    Duration         time.Duration
}
```

## Hook Registration

```go
var compactionStart time.Time

agent.OnBeforeCompaction(func(ctx context.Context, sessionID string) error {
    compactionStart = time.Now()
    log.Printf("Compaction starting for session %s", sessionID)
    return nil
})

agent.OnAfterCompaction(func(ctx context.Context, result *compaction.CompactionResult) error {
    duration := time.Since(compactionStart)

    log.Printf("Compaction completed in %v", duration)
    log.Printf("Tokens: %d -> %d (%.1f%% reduction)",
        result.OriginalTokens,
        result.CompactedTokens,
        calculateReduction(result))

    // Store for analysis
    monitor.RecordEvent(sessionID, result, duration)
    return nil
})
```

## CompactionResult Fields

| Field | Description |
|-------|-------------|
| `Strategy` | Strategy name used (hybrid, summarization) |
| `OriginalTokens` | Total tokens before compaction |
| `CompactedTokens` | Total tokens after compaction |
| `SummaryContent` | Generated summary text |
| `PreservedMessageIDs` | Messages kept intact |
| `MessagesRemoved` | Messages that were summarized |
| `ToolOutputsTruncated` | Tool outputs that were shortened |

## Metrics Collection

```go
func (m *CompactionMonitor) GetStats() (total int, avgReduction float64, totalTokensSaved int) {
    for _, e := range m.events {
        sumReduction += e.Reduction
        totalTokensSaved += e.OriginalTokens - e.CompactedTokens
    }
    return len(m.events), sumReduction / float64(len(m.events)), totalTokensSaved
}
```

## Running the Example

```bash
export ANTHROPIC_API_KEY="your-api-key"
export DATABASE_URL="postgresql://user:pass@localhost:5432/agentpg"

cd examples/context_compaction/04_compaction_monitoring
go run main.go
```

## Expected Output

```
Created session: 550e8400-e29b-41d4-a716-446655440000

=== Query 1/5 ===
Prompt: Explain the complete history of artificial intelligence...

[METRICS] Message received - Input: 523, Output: 2048 tokens
Response: Artificial intelligence has a rich history spanning several decades...

...

==================================================
[MONITOR] Compaction triggered
[MONITOR] Session: 550e8400-...
[MONITOR] Start time: 2024-12-06T14:30:25Z
==================================================

[COMPACTION EVENT] 2024-12-06T14:30:27Z
  Session: 550e8400...
  Strategy: hybrid
  Tokens: 45000 -> 12000 (73.3% reduction)
  Messages removed: 8
  Duration: 2.3s

[MONITOR] Compaction completed
[MONITOR] Summary content length: 1523 chars
[MONITOR] Preserved message count: 4

[MONITOR] Summary preview:
  The conversation covered AI history, neural network architectures...

...

============================================================
               COMPACTION MONITORING SUMMARY
============================================================

Total compaction events: 1
Average reduction: 73.3%
Total tokens saved: 33000

Event History:
------------------------------------------------------------
1. 14:30:27
   Strategy: hybrid | Reduction: 73.3% | Duration: 2.3s

Session compaction count: 1

============================================================
=== Demo Complete ===
```

## Integration with Observability Systems

### Prometheus Metrics
```go
var (
    compactionTotal = prometheus.NewCounter(...)
    compactionTokens = prometheus.NewHistogram(...)
    compactionDuration = prometheus.NewHistogram(...)
)

agent.OnAfterCompaction(func(ctx context.Context, result *compaction.CompactionResult) error {
    compactionTotal.Inc()
    compactionTokens.Observe(float64(result.OriginalTokens - result.CompactedTokens))
    compactionDuration.Observe(duration.Seconds())
    return nil
})
```

### Structured Logging
```go
agent.OnAfterCompaction(func(ctx context.Context, result *compaction.CompactionResult) error {
    slog.Info("compaction_completed",
        "strategy", result.Strategy,
        "original_tokens", result.OriginalTokens,
        "compacted_tokens", result.CompactedTokens,
        "reduction_percent", reduction,
        "messages_removed", result.MessagesRemoved,
    )
    return nil
})
```

## Best Practices

1. **Track all events** - Build historical data for analysis
2. **Monitor duration** - Detect slow compactions
3. **Alert on anomalies** - Unusual reduction rates may indicate issues
4. **Review summaries** - Spot-check that important context is preserved
5. **Cost tracking** - Calculate token savings = cost savings

## Next Steps

- See [advanced/02_observability](../../advanced/02_observability/) for full metrics setup
- See [advanced/03_cost_tracking](../../advanced/03_cost_tracking/) for cost analysis
