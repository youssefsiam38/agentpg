# HTTP API Tool Example

This example demonstrates a safe HTTP client tool that allows Claude to make API requests.

## Features

- **Host Whitelist**: Only allowed domains can be accessed
- **Request Timeout**: Prevents hanging on slow endpoints
- **Response Size Limits**: Caps response body size
- **Default Headers**: Consistent headers across requests
- **JSON Formatting**: Pretty-prints JSON responses

## Security Layers

### 1. Host Whitelist
```go
httpTool := NewHTTPAPITool(
    []string{"api.weather.com", "localhost:8888"},
    10*time.Second,
)
```

### 2. Request Timeout
```go
client: &http.Client{
    Timeout: timeout,
}
```

### 3. Response Size Limit
```go
body, err := io.ReadAll(io.LimitReader(resp.Body, 1024*100)) // 100KB
```

## Tool Schema

```go
func (h *HTTPAPITool) InputSchema() tool.ToolSchema {
    return tool.ToolSchema{
        Type: "object",
        Properties: map[string]tool.PropertyDef{
            "url": {
                Type:        "string",
                Description: "Full URL to request (must be from allowed hosts)",
            },
            "headers": {
                Type:        "object",
                Description: "Optional headers to include",
            },
        },
        Required: []string{"url"},
    }
}
```

## Mock Weather API

The example includes a mock weather API for demonstration:

```
GET /weather?city=Tokyo
{
  "city": "Tokyo",
  "temperature": 22,
  "unit": "celsius",
  "condition": "Partly Cloudy",
  "humidity": 65,
  "forecast": [...]
}

GET /cities
{
  "cities": ["London", "Paris", "Tokyo", "New York", "Sydney"]
}
```

## Sample Conversation

```
User: What's the weather like in Tokyo?

Agent: [Uses http_request tool]
URL: http://localhost:8888/weather?city=Tokyo

Response:
{
  "city": "Tokyo",
  "temperature": 22,
  "unit": "celsius",
  "condition": "Partly Cloudy"
}

Agent: The weather in Tokyo is currently 22°C and partly cloudy with 65% humidity.
```

## Blocked Requests

```
BLOCKED: https://api.openai.com/v1/chat
BLOCKED: http://internal-service.local/admin
BLOCKED: https://example.com/api
```

## Adding Authentication

For APIs requiring authentication:

```go
httpTool.SetDefaultHeader("Authorization", "Bearer " + apiKey)
httpTool.SetDefaultHeader("X-API-Key", "your-api-key")
```

## Production Considerations

1. **Secrets Management**: Don't hardcode API keys
2. **Rate Limiting**: Limit requests per time window
3. **Caching**: Cache responses for repeated queries
4. **Retry Logic**: Handle transient failures
5. **Circuit Breaker**: Fail fast on repeated errors

## Extending to POST/PUT

Modify the tool to support other methods:

```go
type HTTPAPITool struct {
    allowedMethods []string  // ["GET", "POST"]
}

// In Execute:
if !slices.Contains(h.allowedMethods, params.Method) {
    return "", fmt.Errorf("method not allowed: %s", params.Method)
}
```

## Running

```bash
export ANTHROPIC_API_KEY="your-api-key"
export DATABASE_URL="postgres://user:pass@localhost:5432/agentpg"

go run main.go
```
