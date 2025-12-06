# Extended Context Example

This example demonstrates the 1M token extended context feature in AgentPG.

## What is Extended Context?

Claude models have a standard context window of 200K tokens. With extended context, you can access up to **1 million tokens** - 5x the standard limit.

## Enabling Extended Context

```go
agent, _ := agentpg.New(cfg,
    // Enable 1M token context window
    agentpg.WithExtendedContext(true),

    // Optionally disable compaction (rely on extended context)
    agentpg.WithAutoCompaction(false),

    agentpg.WithMaxTokens(4096),
)
```

## How It Works

1. **Automatic Fallback**: If an API call fails with a `max_tokens` error, AgentPG automatically retries with extended context headers.

2. **Beta Header**: Adds `anthropic-beta: context-1m-2025-08-07` header to enable the extended context window.

3. **Seamless Integration**: No code changes needed - just enable the option.

## Use Cases

| Use Case | Description |
|----------|-------------|
| Document Analysis | Process entire books, manuals, or codebases |
| Code Review | Analyze large repositories in context |
| Research Synthesis | Combine multiple papers or reports |
| Legal Documents | Full contract or regulation analysis |
| Data Processing | Large dataset descriptions and queries |

## Running the Example

```bash
export ANTHROPIC_API_KEY="your-api-key"
export DATABASE_URL="postgresql://user:pass@localhost:5432/agentpg"

cd examples/extended_context
go run main.go
```

## Expected Output

```
=== Extended Context (1M Token) Example ===

Configuration:
- Extended context: ENABLED
- Auto-compaction: DISABLED (relying on 1M context)
- Max output tokens: 4096

Created session: 550e8400-e29b-41d4-a716-446655440000

=== Processing Long Document ===

Generated document: 52000 characters (~13000 tokens estimated)

Submitting document for analysis...

Agent response:
I've received and analyzed the comprehensive technical documentation.
The document is organized into 20 main sections covering various aspects
of your system...

Tokens - Input: 13523, Output: 487

=== Follow-up Questions ===

Question 1: What are the main sections covered in the document?
Answer: The document covers 10 main topic areas across 20 sections:
1. System Architecture
2. Database Design
...

=== Extended Context Features ===

When WithExtendedContext(true) is enabled:

1. AUTOMATIC FALLBACK:
   If the API returns a max_tokens error, AgentPG
   automatically retries with the extended context header.

2. BETA HEADER INJECTION:
   Adds 'anthropic-beta: context-1m-2025-08-07' header
   to enable 1M token context window.

3. NO CODE CHANGES NEEDED:
   Just add WithExtendedContext(true) to your agent
   configuration - everything else is handled automatically.

=== Demo Complete ===
```

## Extended Context vs Compaction

| Aspect | Extended Context | Compaction |
|--------|------------------|------------|
| Token Limit | Up to 1M | Standard 200K |
| Cost | Higher (all tokens billed) | Lower (summarized) |
| Context Loss | None | Some (summarized) |
| Use Case | Full document analysis | Long conversations |

## Using Both Together

```go
agent, _ := agentpg.New(cfg,
    // Enable extended context as fallback
    agentpg.WithExtendedContext(true),

    // Still use compaction for cost optimization
    agentpg.WithAutoCompaction(true),
    agentpg.WithCompactionTrigger(0.7),  // More aggressive
)
```

This combination:
1. Uses compaction to manage most conversations
2. Falls back to extended context if compaction isn't enough
3. Balances cost and capability

## Considerations

### Pricing
Extended context uses more tokens, which means higher costs. Consider:
- Input token pricing for your model
- Whether you need full context or if summarization works
- Budget constraints for your application

### Latency
Processing very long contexts may increase response time. Consider:
- User experience for real-time applications
- Timeout settings for your client
- Breaking very long documents into chunks

### Best Practices

1. **Use for specific needs** - Don't enable by default if not needed
2. **Monitor usage** - Track token consumption
3. **Combine strategies** - Use with compaction when appropriate
4. **Test thoroughly** - Verify behavior with your document sizes

## Next Steps

- See [context_compaction](../context_compaction/) for alternative approaches
- See [advanced/03_cost_tracking](../advanced/03_cost_tracking/) for cost monitoring
