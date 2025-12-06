# Custom Compaction Strategy Example

This example demonstrates how to implement a custom compaction strategy.

## Strategy Interface

```go
type Strategy interface {
    // Name returns the strategy name
    Name() string

    // ShouldCompact checks if compaction is needed
    ShouldCompact(messages []*types.Message, config CompactionConfig) bool

    // Compact performs the compaction
    Compact(ctx context.Context, messages []*types.Message, config CompactionConfig) (*CompactionResult, error)
}
```

## Custom Strategy: KeepToolResultsStrategy

This example implements a strategy that:
1. **Keeps tool results intact** - Tool call data is preserved
2. **Summarizes text messages** - Regular conversation is condensed
3. **Preserves recent messages** - Last N messages never compacted

## Implementation

```go
type KeepToolResultsStrategy struct {
    client *anthropic.Client
}

func (s *KeepToolResultsStrategy) Name() string {
    return "keep_tools"
}

func (s *KeepToolResultsStrategy) ShouldCompact(
    messages []*types.Message,
    config CompactionConfig,
) bool {
    totalTokens := sumTokens(messages)
    threshold := int(float64(config.MaxContextTokens) * config.TriggerThreshold)
    return totalTokens >= threshold
}

func (s *KeepToolResultsStrategy) Compact(
    ctx context.Context,
    messages []*types.Message,
    config CompactionConfig,
) (*CompactionResult, error) {
    // Custom compaction logic
    // - Separate tool messages from text messages
    // - Summarize text messages
    // - Keep tool messages intact
    // - Always preserve last N messages
}
```

## CompactionResult Structure

```go
type CompactionResult struct {
    Strategy             string   // Strategy name used
    OriginalTokens       int      // Tokens before compaction
    CompactedTokens      int      // Tokens after compaction
    SummaryContent       string   // Generated summary
    PreservedMessageIDs  []string // Messages kept intact
    MessagesRemoved      int      // Messages summarized
    ToolOutputsTruncated int      // Tool outputs shortened
}
```

## Use Cases for Custom Strategies

| Strategy | Use Case |
|----------|----------|
| Keep Tools | Data analysis with important query results |
| Time-Based | Keep last hour, summarize older |
| Priority-Based | Keep high-priority messages intact |
| Role-Based | Summarize user messages, keep assistant |
| Domain-Specific | Custom logic for specific applications |

## Running the Example

```bash
export ANTHROPIC_API_KEY="your-api-key"
export DATABASE_URL="postgresql://user:pass@localhost:5432/agentpg"

cd examples/context_compaction/03_custom_strategy
go run main.go
```

## Expected Output

```
=== Custom Compaction Strategy Demo ===

Strategy Name: keep_tools

This strategy:
1. Keeps all tool call results intact
2. Summarizes regular text messages
3. Always preserves recent messages

=== Sample Message Compaction ===

Original messages:
  [user] msg-1 (text, 10 tokens)
  [assistant] msg-2 (tool, 25 tokens)
  [user] msg-3 (tool, 20 tokens)
  [assistant] msg-4 (text, 15 tokens)
  [user] msg-5 (text, 12 tokens)
  [assistant] msg-6 (text, 45 tokens)
  [user] msg-7 (text, 8 tokens)
  [assistant] msg-8 (text, 20 tokens)

Total tokens: 155
Should compact: false

=== Compaction Result ===
Strategy: keep_tools
Original tokens: 155
Compacted tokens: 89
Reduction: 42.6%
Messages removed: 4
Preserved message IDs: [msg-7 msg-8 msg-2 msg-3]

=== Generated Summary ===
## Conversation Summary

**User discussed:**
- What is the weather in Tokyo?
- Tell me about Japanese culture and traditions.
- ... and 1 more topics

**Key points covered:**
- The weather in Tokyo is currently 22°C and sunny.
- Japanese culture is rich with traditions including tea ceremonies...

=== Demo Complete ===
```

## Best Practices

1. **Test thoroughly** - Verify important content survives
2. **Consider token limits** - Stay within model constraints
3. **Preserve context** - Keep messages that provide important context
4. **Handle edge cases** - Empty messages, single messages, etc.

## Built-in Strategies

| Strategy | Description |
|----------|-------------|
| `hybrid` | Prune tool outputs first, then summarize |
| `summarization` | Pure summarization of older messages |

## Next Steps

- See [04_compaction_monitoring](../04_compaction_monitoring/) for auditing
- See [advanced/02_observability](../../advanced/02_observability/) for metrics
