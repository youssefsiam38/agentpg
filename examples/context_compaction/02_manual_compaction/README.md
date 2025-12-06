# Manual Compaction Example

This example demonstrates manual compaction control with custom configuration.

## When to Use Manual Control

- **Batch processing** - Compact at logical breakpoints
- **Critical conversations** - Preserve everything during important exchanges
- **Testing** - Observe context growth without intervention
- **Cost optimization** - Compact only when truly necessary

## Disabling Auto-Compaction

```go
agent, _ := agentpg.New(cfg,
    // Disable automatic compaction
    agentpg.WithAutoCompaction(false),

    // Still configure settings for when you DO compact
    agentpg.WithCompactionTrigger(0.80),
    agentpg.WithCompactionTarget(40000),
    agentpg.WithCompactionPreserveN(5),
    agentpg.WithCompactionProtectedTokens(20000),
    agentpg.WithSummarizerModel("claude-3-5-haiku-20241022"),
)
```

## Configuration Options

### WithCompactionTrigger
```go
agentpg.WithCompactionTrigger(0.80)  // 80% of context window
```
Sets the utilization threshold that triggers compaction.

### WithCompactionTarget
```go
agentpg.WithCompactionTarget(40000)  // Target 40K tokens
```
Target token count after compaction.

### WithCompactionPreserveN
```go
agentpg.WithCompactionPreserveN(5)  // Keep last 5 messages
```
Always preserve the last N messages (never summarize).

### WithCompactionProtectedTokens
```go
agentpg.WithCompactionProtectedTokens(20000)  // Protect 20K tokens
```
Never summarize the most recent N tokens (OpenCode pattern).

### WithSummarizerModel
```go
agentpg.WithSummarizerModel("claude-3-5-haiku-20241022")
```
Model used for summarization (Haiku is cost-effective).

## Monitoring Context Size

```go
// Get session info
session, _ := agent.GetSession(ctx, sessionID)
fmt.Printf("Compaction count: %d\n", session.CompactionCount)

// Get all messages
messages, _ := agent.GetMessages(ctx)
totalTokens := 0
for _, msg := range messages {
    totalTokens += msg.TokenCount
}
fmt.Printf("Total tokens: %d\n", totalTokens)
```

## Running the Example

```bash
export ANTHROPIC_API_KEY="your-api-key"
export DATABASE_URL="postgresql://user:pass@localhost:5432/agentpg"

cd examples/context_compaction/02_manual_compaction
go run main.go
```

## Expected Output

```
Created session: 550e8400-e29b-41d4-a716-446655440000

=== Configuration ===
- Auto-compaction: DISABLED
- Compaction trigger: 80%
- Target after compaction: 40,000 tokens
- Preserve last N messages: 5
- Protected tokens: 20,000
- Summarizer model: claude-3-5-haiku-20241022

=== Query 1: Search for information about microservices architecture ===
Based on the search results, microservices architecture is a design approach...
Tokens used - Input: 523, Output: 287

...

=== Session Status Before Compaction ===
Session ID: 550e8400-e29b-41d4-a716-446655440000
Total messages: 8
Estimated total tokens: 4500
Compaction count: 0

=== Manual Compaction Control ===
With auto-compaction disabled, you have full control over when
context is compacted. This is useful for:
1. Batch processing - compact at logical breakpoints
2. Critical conversations - preserve everything during important exchanges
3. Testing - observe context growth without automatic intervention
4. Cost optimization - compact only when truly necessary

=== Final Session Status ===
Total messages: 10
Estimated total tokens: 5200
(No automatic compaction occurred because it was disabled)

=== Demo Complete ===
```

## Use Cases for Manual Control

| Use Case | Strategy |
|----------|----------|
| Long research session | Compact after each topic |
| Customer support | Never compact during active issue |
| Code review | Compact after each file reviewed |
| Data analysis | Compact after each dataset processed |

## Best Practices

1. **Monitor token usage** - Track context growth with GetMessages
2. **Set alerts** - Warn when approaching context limit
3. **Batch compaction** - Compact at natural conversation breaks
4. **Test thoroughly** - Verify important context survives compaction

## Next Steps

- See [03_custom_strategy](../03_custom_strategy/) to build your own strategy
- See [04_compaction_monitoring](../04_compaction_monitoring/) for detailed auditing
