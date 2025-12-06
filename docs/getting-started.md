# Getting Started with AgentPG

This guide will help you build your first AI agent with PostgreSQL persistence in under 10 minutes.

## Prerequisites

- Go 1.21 or later
- PostgreSQL 14 or later
- Anthropic API key

## Installation

```bash
go get github.com/youssefsiam38/agentpg
```

## Quick Start

### 1. Set Up PostgreSQL

Start a PostgreSQL instance (using Docker):

```bash
docker run -d \
  --name agentpg-db \
  -e POSTGRES_USER=agentpg \
  -e POSTGRES_PASSWORD=agentpg \
  -e POSTGRES_DB=agentpg \
  -p 5432:5432 \
  postgres:16-alpine
```

### 2. Run Migrations

Apply the database schema:

```bash
psql "postgres://agentpg:agentpg@localhost:5432/agentpg" \
  -f storage/migrations/001_initial_schema.up.sql
```

### 3. Create Your First Agent

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/anthropics/anthropic-sdk-go"
    "github.com/jackc/pgx/v5/pgxpool"
    "github.com/youssefsiam38/agentpg"
    "github.com/youssefsiam38/agentpg/driver/pgxv5"
)

func main() {
    ctx := context.Background()

    // Connect to PostgreSQL
    pool, err := pgxpool.New(ctx, "postgres://agentpg:agentpg@localhost:5432/agentpg")
    if err != nil {
        log.Fatal(err)
    }
    defer pool.Close()

    // Create driver and agent
    drv := pgxv5.New(pool)
    client := anthropic.NewClient() // Uses ANTHROPIC_API_KEY env var

    agent, err := agentpg.New(drv, agentpg.Config{
        Client:       &client,
        Model:        "claude-sonnet-4-5-20250929",
        SystemPrompt: "You are a helpful assistant.",
    })
    if err != nil {
        log.Fatal(err)
    }

    // Create a new session
    sessionID, err := agent.NewSession(ctx, "my-tenant", "user-123", nil, nil)
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("Created session: %s\n", sessionID)

    // Send a message and get a response
    response, err := agent.Run(ctx, "Hello! What can you help me with?")
    if err != nil {
        log.Fatal(err)
    }

    // Access the response content
    for _, block := range response.Message.Content {
        if block.Type == agentpg.ContentTypeText {
            fmt.Printf("Assistant: %s\n", block.Text)
        }
    }

    // The conversation is automatically persisted
    // You can resume it later with agent.LoadSession(ctx, sessionID)
}
```

### 4. Run Your Agent

```bash
export ANTHROPIC_API_KEY="your-api-key"
go run main.go
```

## Adding Tools

Tools allow your agent to perform actions and access external data:

```go
package main

import (
    "context"
    "encoding/json"
    "fmt"
    "time"

    "github.com/youssefsiam38/agentpg/tool"
)

func main() {
    // Create a tool registry
    registry := tool.NewRegistry()

    // Register a simple tool
    weatherTool := tool.NewFuncTool(
        "get_weather",
        "Get the current weather for a city",
        tool.ToolSchema{
            Type: "object",
            Properties: map[string]tool.PropertyDef{
                "city": {
                    Type:        "string",
                    Description: "The city name",
                },
            },
            Required: []string{"city"},
        },
        func(ctx context.Context, input json.RawMessage) (string, error) {
            var params struct {
                City string `json:"city"`
            }
            json.Unmarshal(input, &params)

            // Your weather API call here
            return fmt.Sprintf("Weather in %s: 72F, Sunny", params.City), nil
        },
    )
    registry.Register(weatherTool)

    // Create agent with tools (assuming drv and client are already set up)
    agent, _ := agentpg.New(drv, agentpg.Config{
        Client:       &client,
        Model:        "claude-sonnet-4-5-20250929",
        SystemPrompt: "You are a weather assistant. Use the get_weather tool to help users.",
    }, agentpg.WithTools(weatherTool))

    // Create session and run
    agent.NewSession(ctx, "tenant", "user", nil, nil)

    // The agent will automatically use tools when appropriate
    response, _ := agent.Run(ctx, "What's the weather in Tokyo?")
    for _, block := range response.Message.Content {
        if block.Type == agentpg.ContentTypeText {
            fmt.Println(block.Text)
        }
    }
}
```

## Response Processing

AgentPG uses streaming internally for all API calls but returns complete responses. Process the response content blocks:

```go
response, err := agent.Run(ctx, "Tell me a story")
if err != nil {
    log.Fatal(err)
}

// Process content blocks
for _, block := range response.Message.Content {
    switch block.Type {
    case agentpg.ContentTypeText:
        fmt.Printf("Text: %s\n", block.Text)
    case agentpg.ContentTypeToolUse:
        fmt.Printf("Tool call: %s with input %v\n", block.ToolName, block.ToolInput)
    case agentpg.ContentTypeToolResult:
        fmt.Printf("Tool result: %s\n", block.ToolContent)
    }
}

// Access usage statistics
fmt.Printf("Tokens used: %d input, %d output\n",
    response.Usage.InputTokens, response.Usage.OutputTokens)
```

## Session Management

### Resuming Conversations

```go
// Load an existing session by ID
err := agent.LoadSession(ctx, "session-uuid-here")
if err != nil {
    log.Fatal(err)
}

// Continue the conversation
response, _ := agent.Run(ctx, "What were we talking about?")
```

### Listing Sessions

```go
// Get all sessions for a tenant
sessions, err := store.GetSessionsByTenant(ctx, "my-tenant")
for _, s := range sessions {
    fmt.Printf("Session %s: %s (created: %v)\n", s.ID, s.Identifier, s.CreatedAt)
}
```

### Session Metadata

```go
// Create session with metadata
metadata := map[string]any{
    "user_name": "Alice",
    "plan":      "premium",
    "tags":      []string{"support", "billing"},
}
sessionID, _ := agent.NewSession(ctx, "tenant", "user", nil, metadata)

// Access metadata later
sessionInfo, _ := agent.GetSession(ctx, sessionID)
userName := sessionInfo.Metadata["user_name"].(string)
```

## Multi-Tenancy

AgentPG is designed for multi-tenant applications:

```go
// Each tenant's sessions are isolated
agent.NewSession(ctx, "company-a", "user-1", nil, nil) // Company A's user
agent.NewSession(ctx, "company-b", "user-1", nil, nil) // Company B's user (different session)
```

For single-tenant applications, use a constant tenant ID:

```go
const TenantID = "default"
agent.NewSession(ctx, TenantID, "user-123", nil, nil)
```

## Error Handling

```go
response, err := agent.Run(ctx, "Hello")
if err != nil {
    switch {
    case errors.Is(err, agentpg.ErrNoSession):
        // No session loaded - call NewSession or LoadSession first
    case errors.Is(err, context.DeadlineExceeded):
        // Request timed out
    default:
        // API or other error
        log.Printf("Agent error: %v", err)
    }
}
```

## Next Steps

- [Configuration](./configuration.md) - Customize agent behavior
- [Tools](./tools.md) - Build powerful tool integrations
- [Architecture](./architecture.md) - Understand system design
- [Compaction](./compaction.md) - Manage long conversations
- [Deployment](./deployment.md) - Production best practices
