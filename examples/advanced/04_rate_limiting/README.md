# Rate Limiting Example

This example demonstrates implementing request rate limiting using AgentPG's hook system.

## Features

- **Token Bucket Algorithm**: Allows bursts while enforcing average rate
- **Per-Tenant Limits**: Each tenant has isolated rate limits
- **Hook-Based Implementation**: Clean separation from business logic
- **Retry-After Support**: Tells clients when to retry

## Token Bucket Algorithm

The token bucket is a simple but effective rate limiting algorithm:

1. Bucket holds tokens (up to capacity)
2. Tokens are added at a constant rate
3. Each request consumes one token
4. If no tokens available, request is rejected

```go
type TokenBucket struct {
    tokens     float64
    capacity   float64
    rate       float64 // tokens per second
    lastRefill time.Time
}
```

## Configuration

```go
// Allow 2 requests per second with burst of 5
rateLimiter := NewRateLimiter(2.0, 5)
```

- **Rate (2.0)**: Average of 2 requests per second
- **Capacity (5)**: Allow bursts of up to 5 requests

## Integration

Use the OnBeforeMessage hook:

```go
agent.OnBeforeMessage(func(ctx context.Context, sessionID string, prompt string) error {
    allowed, waitTime := rateLimiter.Allow(tenantID)

    if !allowed {
        return fmt.Errorf("%w: retry after %v", ErrRateLimited, waitTime)
    }

    return nil
})
```

## Behavior

```
Request 1: Say hi
  [Rate] Tokens: 4.0/5 | ALLOWED
  Response: Hi!

Request 2: Say hello
  [Rate] Tokens: 3.0/5 | ALLOWED
  Response: Hello!

...

Request 6: Say howdy
  [Rate] Tokens: 0.0/5 | BLOCKED - wait 500ms
  Result: Rate limited!
```

## Per-Tenant Isolation

Each tenant has their own token bucket:

```go
// Tenant A makes 3 requests
tenant-a request 1: allowed (4.0 tokens left)
tenant-a request 2: allowed (3.0 tokens left)
tenant-a request 3: allowed (2.0 tokens left)

// Tenant B starts fresh
tenant-b request 1: allowed (4.0 tokens left)
tenant-b request 2: allowed (3.0 tokens left)
```

## HTTP Integration

In an HTTP server, return proper headers:

```go
if !allowed {
    w.Header().Set("Retry-After", fmt.Sprintf("%.0f", waitTime.Seconds()))
    w.Header().Set("X-RateLimit-Remaining", "0")
    w.WriteHeader(http.StatusTooManyRequests)
    return
}
```

## Production Considerations

1. **Distributed Rate Limiting**: Use Redis for multi-instance deployments
2. **Sliding Window**: Consider sliding window counters for smoother limits
3. **Tiered Limits**: Different limits for different subscription tiers
4. **Graceful Degradation**: Queue requests instead of rejecting

## Alternative: golang.org/x/time/rate

For production, consider the standard library rate limiter:

```go
import "golang.org/x/time/rate"

limiter := rate.NewLimiter(rate.Limit(2), 5)

if !limiter.Allow() {
    return ErrRateLimited
}
```

## Running

```bash
export ANTHROPIC_API_KEY="your-api-key"
export DATABASE_URL="postgres://user:pass@localhost:5432/agentpg"

go run main.go
```
