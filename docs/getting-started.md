# Getting Started with AgentPG

This guide will help you set up AgentPG and run your first AI agent in under 5 minutes.

## Table of Contents

1. [Prerequisites](#prerequisites)
2. [Installation](#installation)
3. [Database Setup](#database-setup)
4. [Your First Agent](#your-first-agent)
5. [Adding Tools](#adding-tools)
6. [API Modes](#api-modes)
7. [Next Steps](#next-steps)

---

## Prerequisites

- **Go 1.24+** (with modules enabled)
- **PostgreSQL 14+** (with a running instance)
- **Anthropic API Key** (from [console.anthropic.com](https://console.anthropic.com))

## Installation

Install AgentPG using Go modules:

```bash
go get github.com/youssefsiam38/agentpg
```

## Database Setup

AgentPG uses PostgreSQL for state management. Run the migration to create the required schema:

```bash
# Set your database URL
export DATABASE_URL="postgresql://user:password@localhost:5432/agentpg"

# Create the database (if needed)
createdb agentpg

# Run the migration
psql $DATABASE_URL -f storage/migrations/001_agentpg_migration.up.sql
```

The migration creates tables for sessions, runs, messages, tools, and distributed coordination.

## Your First Agent

Create a file called `main.go`:

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"

    "github.com/jackc/pgx/v5/pgxpool"
    "github.com/youssefsiam38/agentpg"
    "github.com/youssefsiam38/agentpg/driver/pgxv5"
)

func main() {
    ctx := context.Background()

    // 1. Connect to PostgreSQL
    pool, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
    if err != nil {
        log.Fatal(err)
    }
    defer pool.Close()

    // 2. Create driver and client
    drv := pgxv5.New(pool)
    client, err := agentpg.NewClient(drv, &agentpg.ClientConfig{
        APIKey: os.Getenv("ANTHROPIC_API_KEY"),
    })
    if err != nil {
        log.Fatal(err)
    }

    // 3. Register an agent
    err = client.RegisterAgent(&agentpg.AgentDefinition{
        Name:         "assistant",
        Model:        "claude-sonnet-4-5-20250929",
        SystemPrompt: "You are a helpful assistant. Be concise.",
    })
    if err != nil {
        log.Fatal(err)
    }

    // 4. Start the client (begins background processing)
    if err := client.Start(ctx); err != nil {
        log.Fatal(err)
    }
    defer client.Stop(context.Background())

    // 5. Create a session
    sessionID, err := client.NewSession(ctx, "tenant-1", "user-123", nil, nil)
    if err != nil {
        log.Fatal(err)
    }

    // 6. Run the agent
    response, err := client.RunSync(ctx, sessionID, "assistant", "What is 2+2?")
    if err != nil {
        log.Fatal(err)
    }

    // 7. Print the response
    fmt.Println("Response:", response.Text)
    fmt.Printf("Tokens: %d input, %d output\n",
        response.Usage.InputTokens,
        response.Usage.OutputTokens)
}
```

Run it:

```bash
export ANTHROPIC_API_KEY="your-api-key"
export DATABASE_URL="postgresql://user:password@localhost:5432/agentpg"

go run main.go
```

Expected output:
```
Response: 2+2 equals 4.
Tokens: 42 input, 8 output
```

## Adding Tools

Tools allow agents to perform actions. Here's how to add a simple calculator tool:

```go
package main

import (
    "context"
    "encoding/json"
    "fmt"

    "github.com/youssefsiam38/agentpg/tool"
)

// 1. Define the tool
type CalculatorTool struct{}

func (t *CalculatorTool) Name() string {
    return "calculator"
}

func (t *CalculatorTool) Description() string {
    return "Perform basic math operations (add, subtract, multiply, divide)"
}

func (t *CalculatorTool) InputSchema() tool.ToolSchema {
    return tool.ToolSchema{
        Type: "object",
        Properties: map[string]tool.PropertyDef{
            "operation": {
                Type:        "string",
                Description: "The operation to perform",
                Enum:        []string{"add", "subtract", "multiply", "divide"},
            },
            "a": {Type: "number", Description: "First number"},
            "b": {Type: "number", Description: "Second number"},
        },
        Required: []string{"operation", "a", "b"},
    }
}

func (t *CalculatorTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
    var params struct {
        Operation string  `json:"operation"`
        A         float64 `json:"a"`
        B         float64 `json:"b"`
    }
    if err := json.Unmarshal(input, &params); err != nil {
        return "", fmt.Errorf("invalid input: %w", err)
    }

    var result float64
    switch params.Operation {
    case "add":
        result = params.A + params.B
    case "subtract":
        result = params.A - params.B
    case "multiply":
        result = params.A * params.B
    case "divide":
        if params.B == 0 {
            return "", fmt.Errorf("division by zero")
        }
        result = params.A / params.B
    default:
        return "", fmt.Errorf("unknown operation: %s", params.Operation)
    }

    return fmt.Sprintf("%.2f", result), nil
}
```

Register the tool and assign it to an agent:

```go
// Register tool on client
client.RegisterTool(&CalculatorTool{})

// Register agent with tool access
client.RegisterAgent(&agentpg.AgentDefinition{
    Name:         "math-assistant",
    Model:        "claude-sonnet-4-5-20250929",
    SystemPrompt: "You are a math assistant. Use the calculator tool for calculations.",
    Tools:        []string{"calculator"},  // Grant access to the tool
})
```

Now the agent can use the calculator:

```go
response, _ := client.RunSync(ctx, sessionID, "math-assistant", "What is 15 * 7?")
// Agent will call calculator(operation="multiply", a=15, b=7)
// Response: "15 * 7 = 105"
```

## API Modes

AgentPG supports two API modes for different use cases:

### Batch API (Cost-Effective)

Uses Claude's Batch API with 50% cost savings. Best for background processing.

```go
// Async - returns immediately
runID, _ := client.Run(ctx, sessionID, "assistant", "Analyze this data...")
// Do other work...
response, _ := client.WaitForRun(ctx, runID)

// Sync - waits for completion
response, _ := client.RunSync(ctx, sessionID, "assistant", "Hello!")
```

### Streaming API (Real-Time)

Uses Claude's Streaming API for lower latency. Best for interactive applications.

```go
// Async
runID, _ := client.RunFast(ctx, sessionID, "assistant", "Quick question...")
response, _ := client.WaitForRun(ctx, runID)

// Sync (recommended for chat UIs)
response, _ := client.RunFastSync(ctx, sessionID, "assistant", "Hello!")
```

| Feature | Batch API | Streaming API |
|---------|-----------|---------------|
| Cost | 50% discount | Standard pricing |
| Latency | Higher | Lower |
| Best for | Background tasks | Interactive apps |
| Methods | `Run()`, `RunSync()` | `RunFast()`, `RunFastSync()` |

## Next Steps

Now that you have a working agent, explore these topics:

- **[Configuration](./configuration.md)** - Customize client settings, concurrency, and timeouts
- **[Tools Guide](./tools.md)** - Build advanced tools with database access and error handling
- **[Architecture](./architecture.md)** - Understand the event-driven design and data model
- **[Distributed Workers](./distributed.md)** - Scale across multiple instances
- **[Context Compaction](./compaction.md)** - Manage long conversations efficiently
- **[Admin UI](./ui.md)** - Set up the monitoring dashboard

### Example Code

Check the `/examples` directory for complete working examples:

| Example | Description |
|---------|-------------|
| `basic/01_simple_chat` | Minimal agent setup |
| `basic/02_shared_tools` | Multiple agents sharing tools |
| `custom_tools/01_struct_tool` | Full Tool interface implementation |
| `custom_tools/02_func_tool` | Quick function-based tools |
| `nested_agents/01_basic_delegation` | Agent-as-tool pattern |
| `distributed/main.go` | Multi-instance worker setup |
| `admin_ui/main.go` | Web dashboard setup |

### Using database/sql Instead of pgx

If you prefer the standard library:

```go
import (
    "database/sql"
    _ "github.com/lib/pq"
    "github.com/youssefsiam38/agentpg/driver/databasesql"
)

db, _ := sql.Open("postgres", os.Getenv("DATABASE_URL"))
drv := databasesql.New(db)
client, _ := agentpg.NewClient(drv, config)
```

### Transaction Support

For atomic operations across your app and AgentPG:

```go
// Begin transaction
tx, _ := pool.Begin(ctx)

// Create session and run in same transaction
sessionID, _ := client.NewSessionTx(ctx, tx, "tenant", "user", nil, nil)
runID, _ := client.RunTx(ctx, tx, sessionID, "assistant", "Process this order")

// Commit - run becomes visible to workers
tx.Commit(ctx)

// Wait for completion (OUTSIDE transaction)
response, _ := client.WaitForRun(ctx, runID)
```
