# Error Recovery Example

This example demonstrates building resilient agents with retry logic and graceful degradation.

## Features

- **Error Classification**: Categorize errors for appropriate handling
- **Exponential Backoff**: Gradual retry delay increase
- **Jitter**: Randomized delays to prevent thundering herd
- **Graceful Degradation**: Fallback responses when recovery fails

## Error Types

| Type | Behavior | Examples |
|------|----------|----------|
| Transient | Retry with backoff | Timeout, 503, connection refused |
| Rate Limit | Retry with max delay | 429, rate limit exceeded |
| Permanent | Fail immediately | 401, 403, invalid request |

## Error Classification

```go
func ClassifyError(err error) ErrorType {
    errStr := err.Error()

    if contains(errStr, "rate limit", "429") {
        return ErrorTypeRateLimit
    }
    if contains(errStr, "timeout", "503", "502") {
        return ErrorTypeTransient
    }
    if contains(errStr, "invalid", "401", "403") {
        return ErrorTypePermanent
    }

    return ErrorTypeTransient // Default
}
```

## Retry Configuration

```go
config := RetryConfig{
    MaxRetries:    3,               // Maximum retry attempts
    InitialDelay:  100 * time.Millisecond,
    MaxDelay:      10 * time.Second,
    BackoffFactor: 2.0,             // Delay doubles each retry
}
```

## Retry Pattern

```go
result, err := WithRetry(ctx, config, func() (*Response, error) {
    return agent.Run(ctx, prompt)
})

if err != nil {
    // All retries exhausted
    return fallbackResponse()
}
```

## Exponential Backoff

```
Attempt 1: Immediate
Attempt 2: 100ms delay
Attempt 3: 200ms delay (100ms × 2.0)
Attempt 4: 400ms delay (200ms × 2.0)
...
Capped at MaxDelay
```

## Jitter

Random variation prevents synchronized retries:

```go
// Add ±10% jitter
jitter := time.Duration(rand.Float64()*0.2*float64(delay) - 0.1*float64(delay))
delay += jitter
```

## Sample Output

```
=== Retry Logic Demo ===

Config: MaxRetries=3, InitialDelay=100ms, BackoffFactor=2.0

  [Error] connection timeout (type: transient)
  [Retry] Attempt 1/3 after 100ms delay
  [Error] connection timeout (type: transient)
  [Retry] Attempt 2/3 after 203ms delay
Final result: Success after retries! (after 3 attempts)
```

## Graceful Degradation

When retries are exhausted:

```go
if err != nil {
    // Log the failure
    logger.Error("Agent failed", "error", err)

    // Return fallback response
    return "I apologize, but I'm having trouble right now. Please try again later."
}
```

## Circuit Breaker Pattern

For production, consider adding a circuit breaker:

```go
type CircuitBreaker struct {
    failures    int
    threshold   int
    resetAfter  time.Duration
    lastFailure time.Time
}

func (cb *CircuitBreaker) Allow() bool {
    if cb.failures >= cb.threshold {
        if time.Since(cb.lastFailure) < cb.resetAfter {
            return false // Circuit open
        }
        cb.failures = 0 // Reset
    }
    return true
}
```

## Production Considerations

1. **Metrics**: Track retry rates and success percentages
2. **Alerting**: Alert on high failure rates
3. **Timeouts**: Set appropriate context timeouts
4. **Idempotency**: Ensure retried operations are safe
5. **Logging**: Log all retries for debugging

## Error Handling Best Practices

1. **Classify Early**: Determine error type immediately
2. **Fail Fast**: Don't retry permanent errors
3. **Limit Retries**: Prevent infinite loops
4. **User Feedback**: Show appropriate messages
5. **Background Retry**: Queue for later if appropriate

## Running

```bash
export ANTHROPIC_API_KEY="your-api-key"
export DATABASE_URL="postgres://user:pass@localhost:5432/agentpg"

go run main.go
```
