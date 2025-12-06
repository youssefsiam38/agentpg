# Observability Example

This example demonstrates comprehensive observability using AgentPG's hook system with structured logging.

## Features

- **All 5 Hook Types**: Before/after message, tool call, before/after compaction
- **Structured Logging**: JSON-formatted logs using log/slog
- **Metrics Collection**: Token usage, request counts, tool calls
- **Production-Ready Patterns**: Correlation IDs, timestamps, proper log levels

## Hooks Demonstrated

### 1. OnBeforeMessage
Called before each message is sent to Claude:
```go
agent.OnBeforeMessage(func(ctx context.Context, sessionID string, prompt string) error {
    logger.Info("message.started",
        slog.String("session_id", sessionID),
        slog.Int("prompt_length", len(prompt)),
    )
    return nil
})
```

### 2. OnAfterMessage
Called after receiving a response:
```go
agent.OnAfterMessage(func(ctx context.Context, response *agentpg.Response) error {
    logger.Info("message.completed",
        slog.Int64("input_tokens", response.Usage.InputTokens),
        slog.Int64("output_tokens", response.Usage.OutputTokens),
    )
    return nil
})
```

### 3. OnToolCall
Called when Claude invokes a tool:
```go
agent.OnToolCall(func(ctx context.Context, toolName string, input json.RawMessage) error {
    logger.Info("tool.called",
        slog.String("tool_name", toolName),
        slog.String("input", string(input)),
    )
    return nil
})
```

### 4. OnBeforeCompaction
Called before context compaction:
```go
agent.OnBeforeCompaction(func(ctx context.Context, sessionID string) error {
    logger.Warn("compaction.starting", slog.String("session_id", sessionID))
    return nil
})
```

### 5. OnAfterCompaction
Called after compaction completes:
```go
agent.OnAfterCompaction(func(ctx context.Context, result *compaction.CompactionResult) error {
    logger.Info("compaction.completed",
        slog.Int("original_tokens", result.OriginalTokens),
        slog.Int("compacted_tokens", result.CompactedTokens),
    )
    return nil
})
```

## Sample Log Output

```json
{"time":"2024-01-15T10:30:00Z","level":"INFO","msg":"message.started","request_id":"req-1","session_id":"abc123","prompt_length":25}
{"time":"2024-01-15T10:30:01Z","level":"INFO","msg":"tool.called","tool_name":"get_time","input":"{}"}
{"time":"2024-01-15T10:30:02Z","level":"INFO","msg":"message.completed","input_tokens":150,"output_tokens":50,"stop_reason":"end_turn"}
```

## Metrics Collection

The example includes a simple metrics collector:

```go
type Metrics struct {
    TotalRequests     atomic.Int64
    TotalInputTokens  atomic.Int64
    TotalOutputTokens atomic.Int64
    TotalToolCalls    atomic.Int64
    TotalCompactions  atomic.Int64
}
```

In production, export these to:
- Prometheus
- DataDog
- CloudWatch
- Your preferred monitoring system

## Use Cases

1. **Debugging**: Trace request flow through the system
2. **Cost Tracking**: Monitor token usage (see 03_cost_tracking)
3. **Audit Logging**: Track all tool invocations
4. **Performance Monitoring**: Measure response times
5. **Alerting**: Detect anomalies in usage patterns

## Running

```bash
export ANTHROPIC_API_KEY="your-api-key"
export DATABASE_URL="postgres://user:pass@localhost:5432/agentpg"

go run main.go
```
