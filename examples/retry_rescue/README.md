# Retry and Rescue Examples

This directory demonstrates AgentPG's tool retry and run rescue system.

## Overview

AgentPG provides a robust retry system for tool executions:

- **Instant Retry (Default)**: Fast, snappy retries with no delay
- **Error Types**: Control retry behavior with `ToolCancel`, `ToolDiscard`, `ToolSnooze`
- **Exponential Backoff**: Opt-in backoff for rate-limited APIs

## Examples

### 01_instant_retry/

Demonstrates the default instant retry behavior:
- 2 attempts (1 retry on failure)
- No delay between retries
- Snappy user experience

```bash
go run examples/retry_rescue/01_instant_retry/main.go
```

### 02_error_types/

Shows the different tool error types:

| Error Type | Behavior | Use Case |
|------------|----------|----------|
| `ToolCancel(err)` | Fail immediately, no retry | Auth failures, permission denied |
| `ToolDiscard(err)` | Fail immediately, invalid input | Malformed data, impossible request |
| `ToolSnooze(duration, err)` | Retry after duration, does NOT consume attempt | Rate limits, temporary unavailability |
| Regular `error` | Retry instantly up to MaxAttempts | Transient failures |

```bash
go run examples/retry_rescue/02_error_types/main.go
```

### 03_exponential_backoff/

Demonstrates opt-in exponential backoff for external APIs:

```go
ToolRetryConfig: &agentpg.ToolRetryConfig{
    MaxAttempts: 5,   // More attempts for unreliable services
    Jitter:      0.1, // Enable backoff with 10% jitter
}
```

Backoff delays follow River's attempt^4 formula:

| Attempt | Delay |
|---------|-------|
| 1 | ~1 second |
| 2 | ~16 seconds |
| 3 | ~81 seconds |
| 4 | ~256 seconds |
| 5 | ~625 seconds |

```bash
go run examples/retry_rescue/03_exponential_backoff/main.go
```

## Configuration

### Default (Instant Retry)

```go
// No configuration needed - defaults are optimized for snappy UX
client, _ := agentpg.NewClient(drv, &agentpg.ClientConfig{
    APIKey: apiKey,
    // ToolRetryConfig: nil = use defaults
})
```

Default values:
- `MaxAttempts: 2` (1 retry)
- `Jitter: 0.0` (instant retry)

### Exponential Backoff

```go
client, _ := agentpg.NewClient(drv, &agentpg.ClientConfig{
    APIKey: apiKey,
    ToolRetryConfig: &agentpg.ToolRetryConfig{
        MaxAttempts: 5,   // More attempts
        Jitter:      0.1, // Enables backoff
    },
})
```

## Error Types in Code

```go
import "github.com/youssefsiam38/agentpg/tool"

func (t *MyTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
    // Auth failure - never retry
    if authFailed {
        return "", tool.ToolCancel(errors.New("invalid API key"))
    }

    // Invalid input - never retry
    if inputInvalid {
        return "", tool.ToolDiscard(errors.New("malformed request"))
    }

    // Rate limited - retry after delay (does NOT consume attempt)
    if rateLimited {
        return "", tool.ToolSnooze(30*time.Second, errors.New("rate limit exceeded"))
    }

    // Regular error - will be retried (consumes attempt)
    if temporaryError {
        return "", errors.New("network timeout")
    }

    return "success", nil
}
```

## Run Rescue

The rescue system handles runs stuck in non-terminal states:

```go
client, _ := agentpg.NewClient(drv, &agentpg.ClientConfig{
    APIKey: apiKey,
    RunRescueConfig: &agentpg.RunRescueConfig{
        RescueInterval:    time.Minute,     // Check every minute
        RescueTimeout:     5 * time.Minute, // Runs stuck > 5 min are rescued
        MaxRescueAttempts: 3,               // Max rescue attempts before failure
    },
})
```

Rescue is automatic and runs on the leader instance only.

## When to Use Each Approach

| Scenario | Recommended Approach |
|----------|---------------------|
| Interactive chat | Instant retry (default) |
| Background processing | Exponential backoff |
| External API with rate limits | Backoff + ToolSnooze |
| Auth/permission errors | ToolCancel |
| Invalid user input | ToolDiscard |
| Temporary outages | ToolSnooze |
