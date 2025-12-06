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
  -f storage/migrations/001_create_sessions.up.sql \
  -f storage/migrations/002_create_messages.up.sql \
  -f storage/migrations/003_create_compaction_events.up.sql \
  -f storage/migrations/004_create_message_archive.up.sql
```

### 3. Create Your First Agent

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"

    "github.com/jackc/pgx/v5/pgxpool"
    "github.com/youssefsiam38/agentpg"
    "github.com/youssefsiam38/agentpg/storage"
)

func main() {
    ctx := context.Background()

    // Connect to PostgreSQL
    pool, err := pgxpool.New(ctx, "postgres://agentpg:agentpg@localhost:5432/agentpg")
    if err != nil {
        log.Fatal(err)
    }
    defer pool.Close()

    // Create storage and agent
    store := storage.NewPostgresStore(pool)
    agent := agentpg.New(
        os.Getenv("ANTHROPIC_API_KEY"),
        store,
        agentpg.WithModel("claude-sonnet-4-5-20250929"),
        agentpg.WithSystemPrompt("You are a helpful assistant."),
    )

    // Create a new session
    session, err := agent.NewSession(ctx, "my-tenant", "user-123", nil)
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("Created session: %s\n", session.ID)

    // Send a message and get a response
    response, err := agent.Run(ctx, "Hello! What can you help me with?")
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("Assistant: %s\n", response)

    // The conversation is automatically persisted
    // You can resume it later with agent.LoadSession(ctx, session.ID)
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

    // Create agent with tools
    agent := agentpg.New(
        apiKey,
        store,
        agentpg.WithTools(registry),
        agentpg.WithSystemPrompt("You are a weather assistant. Use the get_weather tool to help users."),
    )

    // The agent will automatically use tools when appropriate
    response, _ := agent.Run(ctx, "What's the weather in Tokyo?")
    fmt.Println(response)
}
```

## Streaming Responses

For real-time output, use streaming:

```go
package main

import (
    "context"
    "fmt"

    "github.com/youssefsiam38/agentpg"
    "github.com/youssefsiam38/agentpg/streaming"
)

func main() {
    agent := agentpg.New(apiKey, store)
    agent.NewSession(ctx, "tenant", "user", nil)

    // Create a streaming handler
    handler := &streaming.PrintHandler{} // Prints tokens as they arrive

    // Or create a custom handler
    customHandler := &streaming.CallbackHandler{
        OnToken: func(token string) {
            fmt.Print(token) // Handle each token
        },
        OnComplete: func(fullText string) {
            fmt.Println("\n--- Complete ---")
        },
        OnError: func(err error) {
            fmt.Printf("Error: %v\n", err)
        },
    }

    // Run with streaming
    response, err := agent.RunStream(ctx, "Tell me a story", customHandler)
    if err != nil {
        log.Fatal(err)
    }
}
```

## Session Management

### Resuming Conversations

```go
// Load an existing session by ID
session, err := agent.LoadSession(ctx, "session-uuid-here")
if err != nil {
    log.Fatal(err)
}

// Or find by tenant and identifier
session, err = agent.LoadSessionByIdentifier(ctx, "my-tenant", "user-123")

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
session, _ := agent.NewSession(ctx, "tenant", "user", metadata)

// Access metadata later
session, _ := agent.LoadSession(ctx, sessionID)
userName := session.Metadata["user_name"].(string)
```

## Multi-Tenancy

AgentPG is designed for multi-tenant applications:

```go
// Each tenant's sessions are isolated
agent.NewSession(ctx, "company-a", "user-1", nil) // Company A's user
agent.NewSession(ctx, "company-b", "user-1", nil) // Company B's user (different session)

// Query sessions by tenant
companyASessions, _ := store.GetSessionsByTenant(ctx, "company-a")
companyBSessions, _ := store.GetSessionsByTenant(ctx, "company-b")
```

For single-tenant applications, use a constant tenant ID:

```go
const TenantID = "default"
agent.NewSession(ctx, TenantID, "user-123", nil)
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
