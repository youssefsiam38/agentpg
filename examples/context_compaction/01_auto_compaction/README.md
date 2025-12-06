# Auto Compaction Example

This example demonstrates automatic context compaction with default settings.

## How Auto Compaction Works

1. **Monitor**: Before each `Run()`, check context utilization
2. **Trigger**: If utilization exceeds threshold (default 85%), compact
3. **Strategy**: Apply hybrid strategy (prune then summarize)
4. **Resume**: Continue conversation with compacted context

## Configuration

```go
agent, _ := agentpg.New(cfg,
    // Enable auto-compaction
    agentpg.WithAutoCompaction(true),

    // Trigger when context reaches 85% full
    agentpg.WithCompactionTrigger(0.85),

    // Target 80K tokens after compaction
    agentpg.WithCompactionTarget(80000),
)
```

## Default Values

| Setting | Default | Description |
|---------|---------|-------------|
| AutoCompaction | true | Enabled by default |
| CompactionTrigger | 0.85 | 85% utilization |
| CompactionTarget | 40% of context | Target after compaction |
| PreserveN | 10 | Keep last 10 messages |
| ProtectedTokens | 40K | Never summarize recent 40K tokens |
| SummarizerModel | claude-3-5-haiku | Cost-effective summarization |

## Monitoring with Hooks

```go
agent.OnBeforeCompaction(func(ctx context.Context, sessionID string) error {
    fmt.Printf("Compaction starting for %s\n", sessionID)
    return nil
})

agent.OnAfterCompaction(func(ctx context.Context, result *compaction.CompactionResult) error {
    fmt.Printf("Compacted: %d -> %d tokens\n",
        result.OriginalTokens, result.CompactedTokens)
    return nil
})
```

## Running the Example

```bash
export ANTHROPIC_API_KEY="your-api-key"
export DATABASE_URL="postgresql://user:pass@localhost:5432/agentpg"

cd examples/context_compaction/01_auto_compaction
go run main.go
```

## Expected Output

```
Created session: 550e8400-e29b-41d4-a716-446655440000

=== Question 1/5 ===
Q: Explain the history of computer programming from the 1950s to today...

A: The history of computer programming spans several decades of innovation...

Tokens - Input: 245, Output: 1523

=== Question 2/5 ===
...

[COMPACTION] Starting compaction for session 550e8400...
[COMPACTION] Completed: 45000 -> 12000 tokens (73.3% reduction)

=== Question 5/5 ===
...

=== Compaction Summary ===
Total compactions triggered: 1

Last compaction details:
  Strategy: hybrid
  Original tokens: 45000
  Compacted tokens: 12000
  Messages preserved: 10

=== Demo Complete ===
```

## When Compaction Triggers

Compaction triggers when:
- Context utilization > trigger threshold
- After saving user message, before API call

## What Gets Compacted

1. **Tool outputs** - Truncated/removed first (hybrid strategy)
2. **Older messages** - Summarized if still over target
3. **Recent messages** - Always preserved (protected)

## Best Practices

1. **Set appropriate trigger** - 0.85 gives buffer before limit
2. **Monitor with hooks** - Track when compaction occurs
3. **Review summaries** - Ensure important context is preserved
4. **Adjust for use case** - Lower trigger for tool-heavy apps

## Next Steps

- See [02_manual_compaction](../02_manual_compaction/) for custom configuration
- See [04_compaction_monitoring](../04_compaction_monitoring/) for detailed monitoring
