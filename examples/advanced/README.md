# Advanced AgentPG Examples

This directory contains advanced examples demonstrating production-ready patterns for building AI agents with AgentPG.

## Examples

### 01_multi_tenant/
Multi-tenant HTTP API server with per-tenant session isolation. Demonstrates:
- HTTP server wrapper for AgentPG
- Tenant identification from headers
- Session management per user/tenant
- Request/response handling

### 02_observability/
Comprehensive observability with structured logging. Demonstrates:
- All 5 hook types (OnBeforeMessage, OnAfterMessage, OnToolCall, etc.)
- Structured logging with log/slog
- Token usage metrics tracking
- Request correlation and tracing

### 03_cost_tracking/
Token-to-cost calculation and budget management. Demonstrates:
- Per-session cost tracking
- Budget thresholds and alerts
- Cost estimation per request
- Usage reporting

### 04_rate_limiting/
Request rate limiting using hooks. Demonstrates:
- Token bucket rate limiting
- Per-tenant rate limits
- Graceful rejection with proper messages
- Rate limit headers

### 05_database_tool/
Safe SQL query tool for database access. Demonstrates:
- Read-only query validation (SELECT only)
- Parameter binding for safety
- Result formatting for Claude
- Query timeout handling

### 06_http_api_tool/
External API integration tool. Demonstrates:
- HTTP client with timeout
- Response parsing and formatting
- Error handling for network issues
- Header and auth configuration

### 07_pii_filtering/
Sensitive data protection using hooks. Demonstrates:
- PII detection with regex patterns
- Message blocking for violations
- Audit logging of blocked content
- Configurable patterns

### 08_error_recovery/
Resilient agent with retry logic. Demonstrates:
- Exponential backoff retry
- Graceful degradation
- Error classification
- Fallback responses

## Running the Examples

Each example can be run independently:

```bash
# Set required environment variables
export ANTHROPIC_API_KEY="your-api-key"
export DATABASE_URL="postgres://user:pass@localhost:5432/agentpg"

# Run a specific example
cd 01_multi_tenant && go run main.go
```

## Common Patterns

### Hook-Based Middleware
Many examples use hooks to implement cross-cutting concerns:
- Rate limiting (OnBeforeMessage)
- Cost tracking (OnAfterMessage)
- PII filtering (OnBeforeMessage)
- Observability (all hooks)

### Tool Design
Database and HTTP API tools demonstrate:
- Input validation with JSON Schema
- Safe execution with timeouts
- Proper error handling
- Formatted output for Claude
