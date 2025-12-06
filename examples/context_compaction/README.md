# Context Compaction Examples

This directory contains examples demonstrating AgentPG's context management and compaction features.

## Why Compaction?

Claude has a finite context window (200K tokens for most models). As conversations grow, you need to:
- Prevent hitting the context limit
- Reduce token costs
- Maintain conversation coherence

AgentPG provides automatic and manual compaction strategies.

## Examples

| Example | Description |
|---------|-------------|
| [01_auto_compaction](./01_auto_compaction/) | Automatic compaction with default settings |
| [02_manual_compaction](./02_manual_compaction/) | Manual control and custom configuration |
| [03_custom_strategy](./03_custom_strategy/) | Implement a custom compaction strategy |
| [04_compaction_monitoring](./04_compaction_monitoring/) | Monitor and audit compaction events |

## Compaction Strategies

### Hybrid Strategy (Default)
1. **Prune first** - Remove/truncate verbose tool outputs
2. **Summarize second** - Summarize remaining messages if needed

### Summarization Strategy
- Pure summarization approach
- Creates a condensed summary of conversation history

## Configuration Options

```go
agent, _ := agentpg.New(cfg,
    // Enable/disable automatic compaction
    agentpg.WithAutoCompaction(true),

    // Trigger at 85% context utilization
    agentpg.WithCompactionTrigger(0.85),

    // Target 40% of context after compaction
    agentpg.WithCompactionTarget(80000),

    // Always preserve last N messages
    agentpg.WithCompactionPreserveN(10),

    // Never summarize last N tokens
    agentpg.WithCompactionProtectedTokens(40000),

    // Model for summarization
    agentpg.WithSummarizerModel("claude-3-5-haiku-20241022"),
)
```

## Learning Path

1. Start with **01_auto_compaction** to see default behavior
2. Explore **02_manual_compaction** for custom configuration
3. Learn **03_custom_strategy** to build your own strategy
4. See **04_compaction_monitoring** for observability

## Prerequisites

- PostgreSQL database running
- Environment variables set:
  - `ANTHROPIC_API_KEY`
  - `DATABASE_URL`

## Running Examples

```bash
cd examples/context_compaction/01_auto_compaction
go run main.go
```
