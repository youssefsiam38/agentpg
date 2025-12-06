# Hooks Guide

Hooks provide extensibility points for monitoring, logging, and customizing agent behavior.

## Overview

Hooks allow you to:
- Log agent activity
- Collect metrics and telemetry
- Validate inputs/outputs
- Implement rate limiting
- Create audit trails
- Customize behavior

## Available Hooks

| Hook | Trigger | Use Cases |
|------|---------|-----------|
| `OnBeforeMessage` | Before sending to Claude | Logging, validation, rate limiting |
| `OnAfterMessage` | After receiving response | Logging, metrics, post-processing |
| `OnToolCall` | When a tool executes | Tool monitoring, audit trails |
| `OnBeforeCompaction` | Before context compaction | Logging, backup |
| `OnAfterCompaction` | After context compaction | Metrics, notifications |

---

## Registering Hooks

### OnBeforeMessage

Called before messages are sent to Claude:

```go
agent.OnBeforeMessage(func(ctx context.Context, messages []any) error {
    log.Printf("Sending %d messages to Claude", len(messages))

    // Validate messages
    for _, msg := range messages {
        if containsSensitiveData(msg) {
            return fmt.Errorf("message contains sensitive data")
        }
    }

    return nil  // Return error to abort the request
})
```

**Parameters:**
- `ctx` - Request context
- `messages` - Slice of messages being sent

**Return:**
- `nil` to continue
- `error` to abort the request

### OnAfterMessage

Called after receiving a response from Claude:

```go
agent.OnAfterMessage(func(ctx context.Context, response any) error {
    resp := response.(*agentpg.Response)

    log.Printf("Received response: %s", resp.StopReason)
    log.Printf("Tokens used: %d input, %d output",
        resp.Usage.InputTokens,
        resp.Usage.OutputTokens)

    // Record metrics
    metrics.RecordTokenUsage(resp.Usage)

    return nil
})
```

**Parameters:**
- `ctx` - Request context
- `response` - The `*agentpg.Response` object

### OnToolCall

Called when a tool is executed:

```go
agent.OnToolCall(func(ctx context.Context, toolName string, input json.RawMessage, output string, err error) error {
    if err != nil {
        log.Printf("Tool %s failed: %v", toolName, err)
        metrics.RecordToolFailure(toolName)
    } else {
        log.Printf("Tool %s succeeded", toolName)
        metrics.RecordToolSuccess(toolName)
    }

    // Audit trail
    auditLog.Record(audit.Entry{
        Tool:   toolName,
        Input:  string(input),
        Output: output,
        Error:  err,
        Time:   time.Now(),
    })

    return nil
})
```

**Parameters:**
- `ctx` - Request context
- `toolName` - Name of the tool called
- `input` - Raw JSON input to the tool
- `output` - Tool output string
- `err` - Error from tool execution (nil on success)

### OnBeforeCompaction

Called before context compaction starts:

```go
agent.OnBeforeCompaction(func(ctx context.Context, sessionID string) error {
    log.Printf("Starting compaction for session %s", sessionID)

    // Optionally back up the session before compaction
    if err := backupSession(ctx, sessionID); err != nil {
        log.Printf("Backup failed: %v", err)
        // Continue anyway, or return error to abort
    }

    return nil  // Return error to abort compaction
})
```

**Parameters:**
- `ctx` - Request context
- `sessionID` - Session being compacted

### OnAfterCompaction

Called after compaction completes:

```go
agent.OnAfterCompaction(func(ctx context.Context, result any) error {
    event := result.(*compaction.CompactionEvent)

    log.Printf("Compaction complete: %d -> %d tokens (%.1f%% reduction)",
        event.OriginalTokens,
        event.CompactedTokens,
        100*(1-float64(event.CompactedTokens)/float64(event.OriginalTokens)),
    )

    // Record metrics
    metrics.RecordCompaction(event)

    // Alert if compaction is happening too frequently
    if isCompactingTooOften(event.SessionID) {
        alerting.Notify("Frequent compaction detected")
    }

    return nil
})
```

**Parameters:**
- `ctx` - Request context
- `result` - The compaction event/result

---

## Use Cases

### Logging

```go
// Simple request logging
agent.OnBeforeMessage(func(ctx context.Context, messages []any) error {
    requestID := ctx.Value("request_id")
    log.Printf("[%s] Sending request with %d messages", requestID, len(messages))
    return nil
})

agent.OnAfterMessage(func(ctx context.Context, response any) error {
    requestID := ctx.Value("request_id")
    resp := response.(*agentpg.Response)
    log.Printf("[%s] Response received: %s", requestID, resp.StopReason)
    return nil
})
```

### Metrics Collection

```go
// Prometheus metrics example
var (
    requestsTotal = prometheus.NewCounter(...)
    tokensUsed    = prometheus.NewHistogram(...)
    toolCalls     = prometheus.NewCounterVec(...)
)

agent.OnAfterMessage(func(ctx context.Context, response any) error {
    resp := response.(*agentpg.Response)
    requestsTotal.Inc()
    tokensUsed.Observe(float64(resp.Usage.InputTokens + resp.Usage.OutputTokens))
    return nil
})

agent.OnToolCall(func(ctx context.Context, name string, _ json.RawMessage, _ string, err error) error {
    status := "success"
    if err != nil {
        status = "error"
    }
    toolCalls.WithLabelValues(name, status).Inc()
    return nil
})
```

### Rate Limiting

```go
type RateLimiter struct {
    limiter *rate.Limiter
}

func (r *RateLimiter) Hook(ctx context.Context, messages []any) error {
    if !r.limiter.Allow() {
        return fmt.Errorf("rate limit exceeded")
    }
    return nil
}

// Register
limiter := &RateLimiter{limiter: rate.NewLimiter(10, 1)} // 10 req/sec
agent.OnBeforeMessage(limiter.Hook)
```

### Input Validation

```go
agent.OnBeforeMessage(func(ctx context.Context, messages []any) error {
    for _, msg := range messages {
        // Check for PII
        if containsPII(msg) {
            return fmt.Errorf("message contains PII - request blocked")
        }

        // Check content length
        if getTokenCount(msg) > maxInputTokens {
            return fmt.Errorf("input too long")
        }
    }
    return nil
})
```

### Audit Trail

```go
type AuditLogger struct {
    db *sql.DB
}

func (a *AuditLogger) LogToolCall(ctx context.Context, name string, input json.RawMessage, output string, err error) error {
    userID := ctx.Value("user_id").(string)

    _, dbErr := a.db.ExecContext(ctx, `
        INSERT INTO audit_log (user_id, tool_name, input, output, error, created_at)
        VALUES ($1, $2, $3, $4, $5, NOW())
    `, userID, name, string(input), output, errorString(err))

    return dbErr
}

// Register
auditor := &AuditLogger{db: db}
agent.OnToolCall(auditor.LogToolCall)
```

### Cost Tracking

```go
type CostTracker struct {
    mu    sync.Mutex
    costs map[string]float64
}

func (c *CostTracker) Track(ctx context.Context, response any) error {
    resp := response.(*agentpg.Response)
    sessionID := ctx.Value("session_id").(string)

    // Calculate cost (example rates)
    inputCost := float64(resp.Usage.InputTokens) * 0.000003
    outputCost := float64(resp.Usage.OutputTokens) * 0.000015
    totalCost := inputCost + outputCost

    c.mu.Lock()
    c.costs[sessionID] += totalCost
    c.mu.Unlock()

    return nil
}

// Register
tracker := &CostTracker{costs: make(map[string]float64)}
agent.OnAfterMessage(tracker.Track)
```

---

## Error Handling

### Aborting Requests

Return an error from `OnBeforeMessage` to abort the request:

```go
agent.OnBeforeMessage(func(ctx context.Context, messages []any) error {
    if isSystemMaintenance() {
        return fmt.Errorf("system under maintenance")
    }
    return nil
})
```

### Aborting Compaction

Return an error from `OnBeforeCompaction` to abort compaction:

```go
agent.OnBeforeCompaction(func(ctx context.Context, sessionID string) error {
    if isImportantSession(sessionID) {
        return fmt.Errorf("cannot compact important session")
    }
    return nil
})
```

### Non-Blocking Hooks

For hooks that shouldn't block on errors (e.g., logging):

```go
agent.OnAfterMessage(func(ctx context.Context, response any) error {
    // Log errors but don't fail the request
    if err := sendToAnalytics(response); err != nil {
        log.Printf("Analytics error (non-fatal): %v", err)
    }
    return nil  // Always return nil to not affect the request
})
```

---

## Multiple Hooks

You can register multiple hooks for the same event:

```go
// All hooks will be called in registration order
agent.OnAfterMessage(loggingHook)
agent.OnAfterMessage(metricsHook)
agent.OnAfterMessage(costTrackingHook)
```

If any hook returns an error, subsequent hooks are still called but the operation may be affected.

---

## Best Practices

### Keep Hooks Fast

```go
// Good: Quick, non-blocking
agent.OnAfterMessage(func(ctx context.Context, response any) error {
    go sendToAnalytics(response)  // Async
    return nil
})

// Bad: Slow, blocking
agent.OnAfterMessage(func(ctx context.Context, response any) error {
    time.Sleep(time.Second)  // Blocks the response
    return nil
})
```

### Handle Errors Gracefully

```go
agent.OnToolCall(func(ctx context.Context, name string, input json.RawMessage, output string, err error) error {
    // Log but don't fail
    if logErr := writeToLog(name, input, output, err); logErr != nil {
        log.Printf("Logging failed: %v", logErr)
    }
    return nil  // Don't propagate logging errors
})
```

### Use Context

```go
agent.OnBeforeMessage(func(ctx context.Context, messages []any) error {
    // Check for cancellation
    select {
    case <-ctx.Done():
        return ctx.Err()
    default:
    }

    // Use context values
    userID := ctx.Value("user_id")
    log.Printf("User %s making request", userID)

    return nil
})
```

---

## See Also

- [API Reference](./api-reference.md) - Hook API details
- [Architecture](./architecture.md) - How hooks fit in the system
- [Tools](./tools.md) - Tool-related hooks
