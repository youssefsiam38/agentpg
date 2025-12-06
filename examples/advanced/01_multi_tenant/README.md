# Multi-Tenant API Example

This example demonstrates building a multi-tenant HTTP API with AgentPG.

## Features

- **Tenant Isolation**: Each tenant's sessions are completely isolated
- **Session Persistence**: Sessions persist across requests for conversation continuity
- **Per-User Sessions**: Each user within a tenant has their own session
- **HTTP API**: RESTful endpoint for chat interactions

## Architecture

```
Client Request
     │
     ▼
┌─────────────────┐
│  HTTP Handler   │  ← Extracts X-Tenant-ID, X-User-ID headers
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│ Tenant Manager  │  ← Manages sessions per tenant:user
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│    AgentPG      │  ← Each session is isolated
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│   PostgreSQL    │  ← Sessions stored with tenant_id
└─────────────────┘
```

## API

### POST /chat

**Headers:**
- `X-Tenant-ID`: Tenant identifier (required)
- `X-User-ID`: User identifier (required)

**Request:**
```json
{
  "message": "Hello, how can you help?"
}
```

**Response:**
```json
{
  "response": "I can help you with...",
  "session_id": "abc123...",
  "tokens": {
    "input": 50,
    "output": 100
  }
}
```

## Key Concepts

### Tenant Isolation

Sessions are keyed by `tenantID:userID`, ensuring:
- Tenant A cannot access Tenant B's sessions
- User 1 in Tenant A has separate context from User 2

### Session Management

```go
// GetOrCreateSession manages session lifecycle
func (tm *TenantManager) GetOrCreateSession(ctx context.Context, tenantID, userID string) (string, *agentpg.Agent, error) {
    // Check for existing session
    // Resume if exists, create if not
    // Store session ID for future requests
}
```

### Production Considerations

1. **Agent Lifecycle**: Create agent per-request, close after use
2. **Session Cache**: Consider Redis for distributed session lookup
3. **Auth**: Add proper authentication/authorization
4. **Rate Limiting**: See 04_rate_limiting example

## Running

```bash
export ANTHROPIC_API_KEY="your-api-key"
export DATABASE_URL="postgres://user:pass@localhost:5432/agentpg"

go run main.go
```

The demo simulates requests from multiple tenants. To run as a real server, uncomment the `http.ListenAndServe` lines.
