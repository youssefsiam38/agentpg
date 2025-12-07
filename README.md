# AgentPG

**Stateful AI agents for Go and Postgres, transaction-safe.**

AgentPG is an opinionated, batteries-included package for building AI agents powered by Anthropic's Claude with PostgreSQL persistence. Built for long-context operations, tool use, and agent composition.

## Features

- ✅ **Streaming-First Architecture** - All operations use streaming internally for long context support
- ✅ **Stateful Conversations** - PostgreSQL persistence with full message history
- ✅ **Transaction-Safe** - All operations are atomic; combine agent + business logic in one transaction
- ✅ **Automatic Context Management** - Smart compaction at 85% threshold using production patterns
- ✅ **Tool Support** - Clean interface-based tool system with required parameter specification
- ✅ **Nested Agents** - Agents can use other agents as tools automatically
- ✅ **Extended Context** - Automatic 1M token context with beta header support
- ✅ **Hooks & Observability** - Before/after message, tool call, and compaction hooks

## Installation

```bash
# Core package
go get github.com/youssefsiam38/agentpg

# Choose your database driver:
go get github.com/youssefsiam38/agentpg/driver/pgxv5      # Recommended: pgx/v5
go get github.com/youssefsiam38/agentpg/driver/databasesql # Alternative: database/sql
```

## Quick Start

### 1. Apply Database Migrations

```bash
# Using psql
psql -U myuser -d mydb -f storage/migrations/001_initial_schema.up.sql
```

Or use your preferred migration tool (goose, golang-migrate, etc.). See `storage/migrations/README.md` for details.

### 2. Create Your First Agent

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"

    "github.com/anthropics/anthropic-sdk-go"
    "github.com/anthropics/anthropic-sdk-go/option"
    "github.com/jackc/pgx/v5/pgxpool"
    "github.com/youssefsiam38/agentpg"
    "github.com/youssefsiam38/agentpg/driver/pgxv5"
)

func main() {
    ctx := context.Background()

    // Create PostgreSQL connection
    pool, _ := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
    defer pool.Close()

    // Create Anthropic client
    client := anthropic.NewClient(
        option.WithAPIKey(os.Getenv("ANTHROPIC_API_KEY")),
    )

    // Create database driver
    drv := pgxv5.New(pool)

    // Create agent (driver first, then config)
    agent, err := agentpg.New(
        drv,
        agentpg.Config{
            Client:       &client,
            Model:        "claude-sonnet-4-5-20250929",
            SystemPrompt: "You are a helpful coding assistant",
        },
        agentpg.WithMaxTokens(4096),
        agentpg.WithTemperature(0.7),
    )
    if err != nil {
        log.Fatal(err)
    }

    // Create session
    // For single-tenant apps, use "1" as tenant_id
    // identifier can be user ID, conversation name, etc.
    sessionID, _ := agent.NewSession(ctx, "1", "user-123", nil, nil)
    fmt.Printf("Session: %s\n", sessionID)

    // Run agent
    response, err := agent.Run(ctx, "Explain recursion in 2 sentences.")
    if err != nil {
        log.Fatal(err)
    }

    // Print response
    for _, block := range response.Message.Content {
        if block.Type == agentpg.ContentTypeText {
            fmt.Println(block.Text)
        }
    }
}
```

## Core Concepts

### Configuration

**Required Parameters**:
- `Driver` - Database driver (first argument to `New()`)
  - `pgxv5.New(pool)` - For pgx/v5 users (recommended)
  - `databasesql.New(db)` - For database/sql users
- `Config.Client` - Anthropic API client
- `Config.Model` - Model ID (e.g., "claude-sonnet-4-5-20250929")
- `Config.SystemPrompt` - System prompt for the agent

**Optional Parameters** (via functional options):
- `WithMaxTokens(n)` - Maximum output tokens
- `WithTemperature(t)` - Sampling temperature (0.0-1.0)
- `WithTools(tools...)` - Register tools
- `WithAutoCompaction(bool)` - Enable/disable auto-compaction
- `WithExtendedContext(bool)` - Enable 1M context support
- `WithMaxRetries(n)` - Set retry attempts
- `WithToolTimeout(d)` - Set tool execution timeout (default: 5 minutes)

### Sessions

Sessions represent conversations and are persisted in PostgreSQL with multi-tenancy support:

```go
// Create new session
// tenantID: for multi-tenant apps (use "1" for single-tenant)
// identifier: custom identifier (user ID, conversation name, etc.)
// parentSessionID: nil for top-level sessions, or parent ID for nested agents
sessionID, err := agent.NewSession(ctx, "tenant-123", "user-456", nil, map[string]any{
    "tags": []string{"support", "urgent"},
})

// For single-tenant apps, use constant tenant_id
sessionID, err := agent.NewSession(ctx, "1", "conversation-abc", nil, nil)

// Load existing session
err = agent.LoadSession(ctx, sessionID)

// Get session info
info, err := agent.GetSession(ctx, sessionID)
```

### Tool System

Tools must implement the `Tool` interface:

```go
type MyTool struct{}

func (t *MyTool) Name() string {
    return "my_tool"
}

func (t *MyTool) Description() string {
    return "Does something useful"
}

func (t *MyTool) InputSchema() agentpg.ToolSchema {
    return agentpg.ToolSchema{
        Type: "object",
        Properties: map[string]agentpg.PropertyDef{
            "query": {
                Type:        "string",
                Description: "The query to process",
            },
            "limit": {
                Type:        "number",
                Description: "Maximum results (optional)",
            },
        },
        Required: []string{"query"}, // Specify required params
    }
}

func (t *MyTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
    var params struct {
        Query string  `json:"query"`
        Limit float64 `json:"limit"`
    }

    json.Unmarshal(input, &params)

    // Tool logic here
    result := doSomething(params.Query, int(params.Limit))

    return result, nil
}

// Register with agent
agent, _ := agentpg.New(drv, config, agentpg.WithTools(&MyTool{}))
```

### Nested Agents

Agents can use other agents as tools automatically:

```go
// Create database driver
drv := pgxv5.New(pool)

// Create specialist agents
dbAgent, _ := agentpg.New(drv, agentpg.Config{
    Client:       client,
    Model:        "claude-sonnet-4-5-20250929",
    SystemPrompt: "You are a PostgreSQL database expert",
})

apiAgent, _ := agentpg.New(drv, agentpg.Config{
    Client:       client,
    Model:        "claude-sonnet-4-5-20250929",
    SystemPrompt: "You are a REST API design expert",
})

// Create orchestrator
orchestrator, _ := agentpg.New(drv, agentpg.Config{
    Client:       client,
    Model:        "claude-sonnet-4-5-20250929",
    SystemPrompt: "You coordinate other agents",
})

// Register specialists as tools (automatic!)
dbAgent.AsToolFor(orchestrator)
apiAgent.AsToolFor(orchestrator)

// Orchestrator can now delegate to specialists
response, _ := orchestrator.Run(ctx, "Design a user management API")
```

### Hooks & Observability

Add hooks to observe agent behavior:

```go
// Before sending messages
agent.OnBeforeMessage(func(ctx context.Context, messages []any) error {
    log.Printf("Sending %d messages", len(messages))
    return nil
})

// After receiving response
agent.OnAfterMessage(func(ctx context.Context, response any) error {
    log.Printf("Received response")
    return nil
})

// Tool execution
agent.OnToolCall(func(ctx context.Context, name string, input json.RawMessage, output string, err error) error {
    if err != nil {
        log.Printf("Tool %s failed: %v", name, err)
    } else {
        log.Printf("Tool %s succeeded", name)
    }
    return nil
})

// Before context compaction
agent.OnBeforeCompaction(func(ctx context.Context, sessionID string) error {
    log.Printf("Context compaction starting for session %s", sessionID)
    return nil
})

// After context compaction
agent.OnAfterCompaction(func(ctx context.Context, result any) error {
    log.Printf("Context compaction completed")
    return nil
})
```

### Context Management

AgentPG includes context compaction based on patterns from Claude Code, Aider, and OpenCode:

```go
agent, _ := agentpg.New(
    drv,
    config,
    agentpg.WithAutoCompaction(true), // Default: enabled
    agentpg.WithCompactionStrategy(agentpg.HybridStrategy), // Default
)

// Automatic compaction at 85% context utilization
// - Protects last 40K tokens (OpenCode pattern)
// - Prunes tool outputs first (free, no API call)
// - Summarizes with 8-section structure (Claude Code pattern)
// - Maintains full audit trail and reversibility
```

Manual compaction control:

```go
// Disable auto-compaction
agent, _ := agentpg.New(drv, config, agentpg.WithAutoCompaction(false))

// Check if compaction is needed
stats, _ := agent.GetCompactionStats(ctx, sessionID)
if stats.ShouldCompact {
    // Manually trigger compaction
    result, _ := agent.CompactContext(ctx, sessionID)
}
```

### Extended Context

Enable 1M token context with automatic retry:

```go
agent, _ := agentpg.New(
    drv,
    config,
    agentpg.WithExtendedContext(true),
)

// If a max_tokens error occurs, the agent automatically retries
// with the anthropic-beta header for extended context
```

### Streaming Architecture

AgentPG uses **streaming internally** for all operations. The `Run()` method leverages Anthropic's streaming API under the hood, which provides:

- **Long context support** - No timeouts on large conversations
- **Better reliability** - Incremental message accumulation
- **Consistent behavior** - Same code path for all request sizes
- **Extended context handling** - Automatic retry with 1M context headers

```go
// Run() uses streaming internally
response, err := agent.Run(ctx, "Explain quantum computing")
// Internally: streams → accumulates → returns complete message

// The streaming is handled transparently:
// 1. Creates streaming request to Claude
// 2. Accumulates all content blocks as they arrive
// 3. Handles tool calls automatically
// 4. Returns complete response when done
```

**Why internal streaming?**
- Simpler API for most use cases
- Automatic tool execution loop
- Built-in retry logic and error handling
- No need for explicit event handling unless required

### Transaction Support

AgentPG provides atomic database operations through transaction support. By default, `Run()` automatically wraps all database operations in a transaction, ensuring either all messages are saved or none (on error/timeout).

```go
// Simple usage - atomic by default
response, err := agent.Run(ctx, "Hello!")
// If error occurs, all messages are rolled back automatically

// Advanced usage - combine your business logic with agent in ONE transaction
// With pgxv5 driver:
tx, err := pool.Begin(ctx)  // Use native pgx transaction
if err != nil {
    return err
}
defer tx.Rollback(ctx) // Rollback if not committed

// Your business logic in the same transaction
_, err = tx.Exec(ctx, "INSERT INTO orders (user_id, status) VALUES ($1, $2)", userID, "pending")
if err != nil {
    return err
}

// Agent operations in the same transaction
response, err := agent.RunTx(ctx, tx, "Process this order and generate a confirmation")
if err != nil {
    return err // Everything rolled back (your INSERT + agent messages)
}

// Commit all atomically - your business logic AND agent messages
return tx.Commit(ctx)

// With database/sql driver:
tx, err := db.BeginTx(ctx, nil)
// ... same pattern with *sql.Tx
response, err := agent.RunTx(ctx, tx, "Process this order")
tx.Commit()
```

**Benefits:**
- **Full atomicity** - Combine your business logic with agent operations in one transaction
- **Native transactions** - Use pgx.Tx or *sql.Tx depending on your driver
- **Type-safe** - The transaction type is inferred from your driver choice
- **Nested agent isolation** - Each nested agent manages its own independent transaction
- **No partial state** - On timeout or error, everything is rolled back cleanly


## Architecture

AgentPG follows these design principles:

- **Streaming-first** - All Claude API calls use streaming for reliability
- **Stateful** - Full conversation history persisted in PostgreSQL
- **Composable** - Agents can use other agents as tools
- **Observable** - Hooks provide visibility into all operations

## Package Structure

```
agentpg/
├── agent.go                    # Core Agent[TTx] type with generics
├── config.go                   # Configuration
├── options.go                  # Functional options
├── session.go                  # Session management
├── message.go                  # Message types
├── errors.go                   # Error handling
├── driver/                     # Database driver abstraction
│   ├── driver.go               # Driver interface
│   ├── executor.go             # Executor interfaces
│   ├── context.go              # Context injection
│   ├── pgxv5/                  # pgx/v5 driver (separate module)
│   │   ├── driver.go           # Driver implementation
│   │   └── store.go            # Storage operations
│   └── databasesql/            # database/sql driver (separate module)
│       ├── driver.go           # Driver with savepoint nesting
│       └── store.go            # Storage operations
├── tool/                       # Tool system
│   ├── tool.go                 # Tool interface
│   ├── registry.go             # Tool registry
│   └── executor.go             # Tool execution
├── storage/                    # Storage abstraction
│   ├── store.go                # Store interface
│   └── migrations/             # SQL migrations
├── streaming/                  # Streaming support
│   ├── stream.go               # Stream wrapper
│   ├── accumulator.go          # Message accumulation
│   └── event.go                # Event types
├── hooks/                      # Hook system
│   ├── hooks.go                # Hook registry
│   └── logging.go              # Built-in logging hooks
├── compaction/                 # Context management
│   ├── manager.go              # Compaction orchestration
│   ├── strategy.go             # Strategy interface
│   ├── hybrid.go               # Prune + summarize
│   ├── summarization.go        # Claude Code summarization
│   ├── partitioner.go          # Message partitioning
│   └── tokens.go               # Token counting
└── internal/                   # Internal utilities
    └── anthropic/              # Anthropic SDK adapters
```

## Roadmap

**Phase 1** ✅ - Foundation (Complete)
- Core types, storage, streaming, hooks

**Phase 2** ✅ - Execution (Complete)
- Agent.Run(), tool execution, nested agents

**Phase 3** ✅ - Context Management (Complete)
- Auto-compaction, summarization, hybrid strategies, token counting

**Phase 4** ✅ - Streaming & Hooks (Complete)
- Streaming-first architecture (all API calls use SSE), hooks, observability

**Phase 5** 📋 - Advanced Features (Planned)
- Vision support, structured outputs, batch processing

## Examples

- [`examples/basic/`](examples/basic/) - Simple agent usage and configuration
- [`examples/streaming/`](examples/streaming/) - Tools, hooks, and auto-compaction
- [`examples/custom_tools/`](examples/custom_tools/) - Tool development patterns
- [`examples/nested_agents/`](examples/nested_agents/) - Agent composition and delegation
- [`examples/context_compaction/`](examples/context_compaction/) - Context management strategies
- [`examples/extended_context/`](examples/extended_context/) - 1M token context window
- [`examples/database_sql/`](examples/database_sql/) - Using the database/sql driver
- [`examples/advanced/`](examples/advanced/) - Production patterns (multi-tenant, observability, rate limiting, etc.)

See the [examples README](examples/README.md) for detailed documentation and usage instructions.

## Contributing

Contributions are welcome! Please see the [architecture documentation](docs/architecture.md) for details on the system design.

## Credits

Built with:
- [Anthropic Go SDK](https://github.com/anthropics/anthropic-sdk-go)
- [pgx](https://github.com/jackc/pgx) - PostgreSQL driver (pgxv5 driver)
- [lib/pq](https://github.com/lib/pq) - PostgreSQL driver (databasesql driver)
- [uuid](https://github.com/google/uuid) - UUID generation
