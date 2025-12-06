# Cost Tracking Example

This example demonstrates token-to-cost calculation and budget management for AgentPG applications.

## Features

- **Per-Request Cost Calculation**: Convert tokens to dollars
- **Session-Level Tracking**: Track costs per conversation
- **Budget Alerts**: Warnings at 80% and hard limits at 100%
- **Cost Reports**: Detailed usage summaries

## Pricing Model

The example uses configurable pricing:

```go
costTracker := NewCostTracker(
    3.00,  // $3 per 1M input tokens
    15.00, // $15 per 1M output tokens
)
```

Update these values based on:
- Your Claude model tier
- Current Anthropic pricing
- Any volume discounts

## Cost Calculation

```go
func (ct *CostTracker) CalculateCost(inputTokens, outputTokens int64) float64 {
    inputCost := float64(inputTokens) / 1_000_000 * ct.inputPricePer1M
    outputCost := float64(outputTokens) / 1_000_000 * ct.outputPricePer1M
    return inputCost + outputCost
}
```

## Budget Management

Set limits per session and globally:

```go
costTracker.SetBudgets(
    0.50,  // $0.50 max per session
    5.00,  // $5.00 total budget
)
```

## Alerts

The tracker generates warnings:
- **80% threshold**: "Session at 80% of budget"
- **100% exceeded**: "SESSION BUDGET EXCEEDED: $0.51 > $0.50"

## Integration

Use the OnAfterMessage hook:

```go
agent.OnAfterMessage(func(ctx context.Context, response *agentpg.Response) error {
    cost, warnings := costTracker.RecordUsage(
        sessionID,
        response.Usage.InputTokens,
        response.Usage.OutputTokens,
    )

    for _, warning := range warnings {
        log.Warn(warning)
    }

    return nil
})
```

## Sample Output

```
=== Session 1 ===

User: What is Go programming language?
Agent: Go is a statically typed, compiled programming language...
  [Cost] $0.000054 (in: 150, out: 80 tokens)

User: Give me 3 reasons to use it.
Agent: 1. Simple syntax...
  [Cost] $0.000089 (in: 280, out: 120 tokens)

[Session Total] Requests: 2, Cost: $0.000143

=== Cost Report ===
Session 1:
  Requests:      2
  Input tokens:  430
  Output tokens: 200
  Cost:          $0.000143

Total Spent: $0.000143 / $5.00 budget (0.0%)
```

## Cost Optimization Tips

1. **Limit max_tokens**: Shorter responses cost less
2. **Use system prompts wisely**: "Be concise" reduces output
3. **Enable compaction**: Reduces input tokens over time
4. **Cache common queries**: Avoid repeated API calls
5. **Choose the right model**: Smaller models are cheaper

## Production Considerations

1. **Store costs in database**: Track historical spending
2. **Export to billing system**: Integrate with your invoicing
3. **Per-tenant limits**: Multi-tenant budget isolation
4. **Automatic cutoffs**: Stop requests at budget limit

## Running

```bash
export ANTHROPIC_API_KEY="your-api-key"
export DATABASE_URL="postgres://user:pass@localhost:5432/agentpg"

go run main.go
```
