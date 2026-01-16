# AgentPG - Event-Driven AI Agent Framework

AgentPG is a fully event-driven Go framework for building async AI agents using PostgreSQL for state management and distribution. It provides stateful, distributed, and transaction-safe agent execution with Claude Batch API integration.

## Table of Contents

1. [Architecture Overview](#architecture-overview)
2. [Core Concepts](#core-concepts)
3. [Getting Started](#getting-started)
4. [Client Configuration](#client-configuration)
5. [Agent Registration](#agent-registration)
6. [Tool Registration](#tool-registration)
7. [Agent-as-Tool Pattern](#agent-as-tool-pattern)
8. [Sessions and Runs](#sessions-and-runs)
9. [Transaction-First API](#transaction-first-api)
10. [Distributed Workers](#distributed-workers)
11. [Claude Batch API Integration](#claude-batch-api-integration)
12. [Multi-Iteration Runs](#multi-iteration-runs)
13. [Context Compaction](#context-compaction)
14. [Error Handling](#error-handling)
15. [Database Schema](#database-schema)
16. [LISTEN/NOTIFY Events](#listennotify-events)
17. [Leader Election](#leader-election)
18. [Monitoring and Observability](#monitoring-and-observability)
19. [Testing](#testing)

---

## Architecture Overview

```
┌──────────────────────────────────────────────────────────────────────────────────┐
│                               AgentPG Architecture                                │
├──────────────────────────────────────────────────────────────────────────────────┤
│                                                                                   │
│  ┌────────────────────────────────────────────────────────────────────────────┐  │
│  │                      HTTP Server Layer (Optional)                           │  │
│  │  ┌─────────────────────────────────────────────────────────────────────┐   │  │
│  │  │                      ui.UIHandler()                                  │   │  │
│  │  │  /ui/* (HTMX + SSR)                                                  │   │  │
│  │  │  • Dashboard                    • Sessions/Runs                      │   │  │
│  │  │  • Chat Interface               • Monitoring                         │   │  │
│  │  └───────────────────────────────────┬─────────────────────────────────┘   │  │
│  │                                      │ (optional: chat via client)         │  │
│  └──────────────────────────────────────┼─────────────────────────────────────┘  │
│                                         │                                        │
│  ┌──────────────────────────────────────┴─────────────────────────────────────┐  │
│  │                                                                             │  │
│  │  ┌──────────────┐    ┌──────────────┐    ┌──────────────┐                  │  │
│  │  │   Client 1   │    │   Client 2   │    │   Client N   │   (k8s pods)     │  │
│  │  │  (Worker)    │    │  (Worker)    │    │  (Worker)    │                  │  │
│  │  └──────┬───────┘    └──────┬───────┘    └──────┬───────┘                  │  │
│  │         │                   │                   │                           │  │
│  │         └───────────────────┼───────────────────┘                           │  │
│  │                             │                                               │  │
│  └─────────────────────────────┼───────────────────────────────────────────────┘  │
│                                │                                                  │
│                                ▼                                                  │
│  ┌─────────────────────────────────────────────────────────────────────────────┐ │
│  │                         PostgreSQL Database                                  │ │
│  │  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐  ┌────────────┐          │ │
│  │  │   Sessions  │  │    Runs     │  │ Iterations  │  │   Tools    │          │ │
│  │  └─────────────┘  └─────────────┘  └─────────────┘  └────────────┘          │ │
│  │  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐  ┌────────────┐          │ │
│  │  │  Messages   │  │Tool Executions│ │  Instances │  │   Agents   │          │ │
│  │  └─────────────┘  └─────────────┘  └─────────────┘  └────────────┘          │ │
│  │                                                                              │ │
│  │  LISTEN/NOTIFY Channels:                                                    │ │
│  │  • agentpg_run_created    • agentpg_tool_pending                            │ │
│  │  • agentpg_run_state      • agentpg_tools_complete                          │ │
│  │  • agentpg_run_finalized                                                    │ │
│  └─────────────────────────────────────────────────────────────────────────────┘ │
│                                │                                                  │
│                                ▼                                                  │
│  ┌─────────────────────────────────────────────────────────────────────────────┐ │
│  │                       Claude Batch API                                       │ │
│  │  • 24-hour processing window                                                │ │
│  │  • Async submission and polling                                             │ │
│  │  • Cost-effective for high-volume                                           │ │
│  └─────────────────────────────────────────────────────────────────────────────┘ │
│                                                                                   │
└──────────────────────────────────────────────────────────────────────────────────┘
```

### Key Design Principles

1. **PostgreSQL as Single Source of Truth**: All state is stored in PostgreSQL. No in-memory state that could be lost.

2. **Event-Driven with Polling Fallback**: Uses LISTEN/NOTIFY for real-time events, with polling as fallback for reliability.

3. **Race-Safe Distribution**: Uses `SELECT FOR UPDATE SKIP LOCKED` for safe work claiming across distributed workers.

4. **Transaction-First**: `RunTx()` accepts user transactions for atomic operations.

5. **Per-Client Registration**: No global state. All agents/tools registered on client instances.

6. **Multi-Level Agent Hierarchies**: Agents can be tools for other agents (PM → Lead → Worker pattern).

---

## Core Concepts

### Client

A `Client` represents a single worker instance. Multiple clients can run across different processes/pods, sharing work from the same database.

```go
client, err := agentpg.NewClient(driver, &agentpg.ClientConfig{
    APIKey: os.Getenv("ANTHROPIC_API_KEY"),
    Name:   "worker-1",
})
```

### Agent

An `Agent` is a Claude-powered AI with a specific role, model, and system prompt. Agents are registered per-client.

```go
client.RegisterAgent(&agentpg.AgentDefinition{
    Name:         "assistant",
    Model:        "claude-sonnet-4-5-20250929",
    SystemPrompt: "You are a helpful assistant.",
})
```

### Tool

A `Tool` is a function that agents can call. Tools implement the `tool.Tool` interface.

```go
type WeatherTool struct{}

func (t *WeatherTool) Name() string        { return "get_weather" }
func (t *WeatherTool) Description() string { return "Get current weather" }
func (t *WeatherTool) InputSchema() tool.ToolSchema { /* ... */ }
func (t *WeatherTool) Execute(ctx context.Context, input json.RawMessage) (string, error) { /* ... */ }

client.RegisterTool(&WeatherTool{})
```

### Session

A `Session` is a conversation context. Messages are stored per-session, enabling multi-turn conversations.

```go
sessionID, err := client.NewSession(ctx, "tenant-1", "user-123", nil, nil)
```

### Run

A `Run` is a single agent invocation. Runs are async by default and can span multiple iterations (batch API calls).

```go
runID, err := client.Run(ctx, sessionID, "assistant", "Hello!")
response, err := client.WaitForRun(ctx, runID)
```

### Iteration

An `Iteration` is a single Claude Batch API call within a run. A run may have multiple iterations:

```
User Prompt → Iteration 1 (tool_use) → Execute Tools → Iteration 2 (tool_use) → Execute Tools → Iteration 3 (end_turn)
```

---

## Getting Started

### Installation

```bash
go get github.com/youssefsiam38/agentpg
```

### Database Setup

Run the migration to create the schema:

```bash
psql $DATABASE_URL -f storage/migrations/001_agentpg_migration.up.sql
```

### Basic Example

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

    // Connect to PostgreSQL
    pool, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
    if err != nil {
        log.Fatal(err)
    }
    defer pool.Close()

    // Create driver and client
    drv := pgxv5.New(pool)
    client, err := agentpg.NewClient(drv, &agentpg.ClientConfig{
        APIKey: os.Getenv("ANTHROPIC_API_KEY"),
    })
    if err != nil {
        log.Fatal(err)
    }

    // Register agent
    client.RegisterAgent(&agentpg.AgentDefinition{
        Name:         "assistant",
        Model:        "claude-sonnet-4-5-20250929",
        SystemPrompt: "You are a helpful assistant.",
    })

    // Start client (begins processing)
    if err := client.Start(ctx); err != nil {
        log.Fatal(err)
    }
    defer client.Stop(context.Background())

    // Create session and run
    sessionID, _ := client.NewSession(ctx, "tenant-1", "demo", nil, nil)
    response, err := client.RunSync(ctx, sessionID, "assistant", "What is 2+2?")
    if err != nil {
        log.Fatal(err)
    }

    fmt.Println(response.Text)
}
```

---

## Client Configuration

### ClientConfig Options

```go
type ClientConfig struct {
    // APIKey is the Anthropic API key (required).
    // Falls back to ANTHROPIC_API_KEY environment variable.
    APIKey string

    // Name identifies this service instance.
    // Defaults to hostname.
    Name string

    // ID is the unique identifier for this client.
    // Defaults to a generated UUID.
    // Must be unique across all running instances.
    ID string

    // MaxConcurrentRuns limits concurrent run processing.
    // Defaults to 10.
    MaxConcurrentRuns int

    // MaxConcurrentTools limits concurrent tool executions.
    // Defaults to 50.
    MaxConcurrentTools int

    // BatchPollInterval is how often to poll Claude Batch API.
    // Defaults to 30 seconds.
    BatchPollInterval time.Duration

    // RunPollInterval is polling fallback interval for new runs.
    // Defaults to 1 second.
    RunPollInterval time.Duration

    // ToolPollInterval is polling interval for tool executions.
    // Defaults to 500 milliseconds.
    ToolPollInterval time.Duration

    // HeartbeatInterval for instance liveness.
    // Defaults to 15 seconds.
    HeartbeatInterval time.Duration

    // LeaderTTL is the leader election lease duration.
    // Defaults to 30 seconds.
    LeaderTTL time.Duration

    // StuckRunTimeout marks runs as stuck after this duration.
    // Defaults to 5 minutes.
    StuckRunTimeout time.Duration

    // Logger for structured logging.
    Logger Logger

    // AutoCompactionEnabled enables automatic context compaction in workers.
    // When enabled, workers check if compaction is needed after each run
    // completes and trigger compaction if the context exceeds the threshold.
    // Defaults to false (manual compaction only).
    AutoCompactionEnabled bool

    // CompactionConfig is the configuration for context compaction.
    // If nil, default compaction configuration is used.
    // Only used if AutoCompactionEnabled is true or when calling Compact() manually.
    CompactionConfig *compaction.Config
}
```

### Using database/sql Driver

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

### Using pgx/v5 Driver (Recommended)

```go
import (
    "github.com/jackc/pgx/v5/pgxpool"
    "github.com/youssefsiam38/agentpg/driver/pgxv5"
)

pool, _ := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
drv := pgxv5.New(pool)
client, _ := agentpg.NewClient(drv, config)
```

---

## Agent Registration

### AgentDefinition

```go
type AgentDefinition struct {
    // Name is the unique identifier (required).
    Name string

    // Description is shown when agent is used as tool.
    Description string

    // Model is the Claude model ID (required).
    Model string

    // SystemPrompt defines the agent's behavior.
    SystemPrompt string

    // Tools is the list of tool names this agent can use.
    // Only tools listed here will be available to the agent.
    // Must reference tools registered via client.RegisterTool().
    Tools []string

    // Agents is the list of agent names this agent can delegate to.
    // Listed agents become available as tools to this agent.
    // Enables multi-level agent hierarchies (PM → Lead → Worker pattern).
    Agents []string

    // MaxTokens limits response length.
    MaxTokens *int

    // Temperature controls randomness (0.0 to 1.0).
    Temperature *float64

    // TopK limits token selection.
    TopK *int

    // TopP (nucleus sampling) limits cumulative probability.
    TopP *float64

    // Config holds additional settings as JSON.
    Config map[string]any
}
```

### Registering Multiple Agents

```go
// Register all agents before calling Start()
agents := []*agentpg.AgentDefinition{
    {
        Name:         "researcher",
        Model:        "claude-sonnet-4-5-20250929",
        SystemPrompt: "You are a research assistant.",
    },
    {
        Name:         "writer",
        Model:        "claude-sonnet-4-5-20250929",
        SystemPrompt: "You are a technical writer.",
    },
    {
        Name:         "reviewer",
        Model:        "claude-opus-4-5-20251101",
        SystemPrompt: "You are a code reviewer.",
    },
}

for _, agent := range agents {
    if err := client.RegisterAgent(agent); err != nil {
        log.Fatalf("Failed to register %s: %v", agent.Name, err)
    }
}
```

### Model Selection

```go
// Available models with their characteristics
var models = map[string]string{
    "claude-opus-4-5-20251101":   "Most capable, best for complex tasks",
    "claude-sonnet-4-5-20250929": "Balanced performance and cost",
    "claude-3-5-haiku-20241022":  "Fast and cost-effective",
}

// Use Haiku for simple tasks
client.RegisterAgent(&agentpg.AgentDefinition{
    Name:  "quick-assistant",
    Model: "claude-3-5-haiku-20241022",
    // ...
})

// Use Opus for complex reasoning
client.RegisterAgent(&agentpg.AgentDefinition{
    Name:  "expert-analyst",
    Model: "claude-opus-4-5-20251101",
    // ...
})
```

---

## Tool Registration

### Tool Interface

```go
type Tool interface {
    // Name returns the tool's unique identifier.
    Name() string

    // Description explains what the tool does (shown to Claude).
    Description() string

    // InputSchema returns the JSON Schema for the tool's input.
    InputSchema() ToolSchema

    // Execute runs the tool with the given input.
    Execute(ctx context.Context, input json.RawMessage) (string, error)
}

type ToolSchema struct {
    Type       string                  // Must be "object"
    Properties map[string]PropertyDef
    Required   []string
}

type PropertyDef struct {
    Type        string
    Description string
    Enum        []string // Optional: allowed values
}
```

### Simple Tool Example

```go
type CalculatorTool struct{}

func (t *CalculatorTool) Name() string        { return "calculator" }
func (t *CalculatorTool) Description() string { return "Perform arithmetic calculations" }

func (t *CalculatorTool) InputSchema() tool.ToolSchema {
    return tool.ToolSchema{
        Type: "object",
        Properties: map[string]tool.PropertyDef{
            "expression": {
                Type:        "string",
                Description: "Mathematical expression to evaluate (e.g., '2 + 2')",
            },
        },
        Required: []string{"expression"},
    }
}

func (t *CalculatorTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
    var params struct {
        Expression string `json:"expression"`
    }
    if err := json.Unmarshal(input, &params); err != nil {
        return "", fmt.Errorf("invalid input: %w", err)
    }

    // Evaluate expression (simplified)
    result := evaluateExpression(params.Expression)
    return fmt.Sprintf("Result: %v", result), nil
}
```

### Database-Aware Tool (Transaction Context)

```go
type UserLookupTool struct {
    db *pgxpool.Pool
}

func (t *UserLookupTool) Name() string        { return "lookup_user" }
func (t *UserLookupTool) Description() string { return "Look up user information by ID" }

func (t *UserLookupTool) InputSchema() tool.ToolSchema {
    return tool.ToolSchema{
        Type: "object",
        Properties: map[string]tool.PropertyDef{
            "user_id": {Type: "string", Description: "The user's ID"},
        },
        Required: []string{"user_id"},
    }
}

func (t *UserLookupTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
    var params struct {
        UserID string `json:"user_id"`
    }
    json.Unmarshal(input, &params)

    var name, email string
    err := t.db.QueryRow(ctx,
        "SELECT name, email FROM users WHERE id = $1",
        params.UserID,
    ).Scan(&name, &email)

    if err != nil {
        return "", fmt.Errorf("user not found: %w", err)
    }

    return fmt.Sprintf("User: %s <%s>", name, email), nil
}
```

### Tool with Enum Constraints

```go
type PriorityTool struct{}

func (t *PriorityTool) InputSchema() tool.ToolSchema {
    return tool.ToolSchema{
        Type: "object",
        Properties: map[string]tool.PropertyDef{
            "task": {Type: "string", Description: "Task description"},
            "priority": {
                Type:        "string",
                Description: "Task priority level",
                Enum:        []string{"low", "medium", "high", "critical"},
            },
        },
        Required: []string{"task", "priority"},
    }
}
```

### Registering Tools and Assigning to Agents

Tools must be:
1. Registered on the client via `client.RegisterTool()`
2. Listed in the agent's `Tools` array to be accessible

```go
// Step 1: Register tools on the client
client.RegisterTool(&CalculatorTool{})
client.RegisterTool(&UserLookupTool{db: pool})
client.RegisterTool(&WeatherTool{apiKey: weatherAPIKey})

// Step 2: Register agents with their allowed tools
client.RegisterAgent(&agentpg.AgentDefinition{
    Name:         "math-assistant",
    Model:        "claude-sonnet-4-5-20250929",
    SystemPrompt: "You are a math assistant.",
    Tools:        []string{"calculator"},  // Only has calculator
})

client.RegisterAgent(&agentpg.AgentDefinition{
    Name:         "full-assistant",
    Model:        "claude-sonnet-4-5-20250929",
    SystemPrompt: "You are a helpful assistant.",
    Tools:        []string{"calculator", "lookup_user", "get_weather"},  // Has all tools
})

client.RegisterAgent(&agentpg.AgentDefinition{
    Name:         "simple-assistant",
    Model:        "claude-sonnet-4-5-20250929",
    SystemPrompt: "You are a simple chat assistant.",
    // No Tools field = no tool access
})
```

**Important**: An agent can only use tools that are:
- Registered on the client
- Listed in the agent's `Tools` array

If a tool is registered but not in the agent's `Tools` array, that agent cannot use it.

---

## Agent-as-Tool Pattern

The agent-as-tool pattern enables multi-level agent hierarchies where one agent can delegate to another. Delegation is defined at the agent level using the `Agents` field in `AgentDefinition`.

### Basic Delegation

```go
// Register the specialist agent first (child agent)
client.RegisterAgent(&agentpg.AgentDefinition{
    Name:         "researcher",
    Description:  "Research specialist for gathering information",
    Model:        "claude-sonnet-4-5-20250929",
    SystemPrompt: "You are a research specialist.",
})

// Register the manager with delegation to researcher via Agents field
client.RegisterAgent(&agentpg.AgentDefinition{
    Name:         "manager",
    Model:        "claude-sonnet-4-5-20250929",
    SystemPrompt: "You are a project manager. Delegate research tasks to your researcher.",
    Agents:       []string{"researcher"},  // researcher becomes a callable tool
})

// When manager needs research, it can call the researcher agent as a tool
// The system automatically:
// 1. Creates a child run for researcher
// 2. Waits for researcher to complete
// 3. Returns researcher's response as the tool result
```

### Multi-Level Hierarchy

```go
// Register tools first
client.RegisterTool(&LintTool{})
client.RegisterTool(&TestTool{})

// Level 3: Worker agents with specialized tools
client.RegisterAgent(&agentpg.AgentDefinition{
    Name:         "frontend-dev",
    Description:  "Frontend developer for React/TypeScript",
    Model:        "claude-sonnet-4-5-20250929",
    SystemPrompt: "You are a frontend developer...",
    Tools:        []string{"lint"},  // Only has lint tool
})

client.RegisterAgent(&agentpg.AgentDefinition{
    Name:         "backend-dev",
    Description:  "Backend developer for Go/APIs",
    Model:        "claude-sonnet-4-5-20250929",
    SystemPrompt: "You are a backend developer...",
    Tools:        []string{"test"},  // Only has test tool
})

// Level 2: Team lead - delegates to workers via Agents field
client.RegisterAgent(&agentpg.AgentDefinition{
    Name:         "tech-lead",
    Description:  "Technical lead coordinating developers",
    Model:        "claude-sonnet-4-5-20250929",
    SystemPrompt: "You coordinate frontend and backend work...",
    Agents:       []string{"frontend-dev", "backend-dev"},  // Workers as callable tools
})

// Level 1: Project manager - delegates to tech-lead via Agents field
client.RegisterAgent(&agentpg.AgentDefinition{
    Name:         "project-manager",
    Model:        "claude-sonnet-4-5-20250929",
    SystemPrompt: "You are the project manager...",
    Agents:       []string{"tech-lead"},  // Tech-lead as callable tool
})

// Now project-manager can delegate to tech-lead,
// who can further delegate to frontend-dev or backend-dev
```

**Important**: Agent delegation is now scoped at the agent level:
- Use `Tools` field for regular tool access
- Use `Agents` field for agent-as-tool delegation
- Both are validated at `Start()` time to ensure referenced tools/agents exist

### How Agent-as-Tool Works Internally

```
1. PM receives: "Check frontend code quality"

2. PM calls tech-lead tool:
   - Parent run state: pending_tools
   - Tool execution created: is_agent_tool=true, agent_name="tech-lead"

3. Tool worker claims execution:
   - Creates child run for tech-lead
   - Child run: parent_run_id=PM's run, depth=1
   - Tool execution: child_run_id set, state=running

4. Tech-lead run processes:
   - May call frontend-dev tool (creates depth=2 child)
   - Eventually completes with response

5. Database trigger fires:
   - When child run completes, trigger updates parent tool execution
   - tool_output = child's response_text
   - state = completed

6. Parent run continues:
   - All tools complete → pg_notify('agentpg_tools_complete')
   - Next iteration with tool_results
   - PM synthesizes and responds
```

### Depth Tracking

```sql
-- Runs table tracks hierarchy depth
SELECT id, agent_name, depth, parent_run_id
FROM agentpg_runs
WHERE session_id = '...'
ORDER BY created_at;

-- Results:
-- id       | agent_name      | depth | parent_run_id
-- run-pm   | project-manager | 0     | NULL
-- run-lead | tech-lead       | 1     | run-pm
-- run-fe   | frontend-dev    | 2     | run-lead
```

---

## Sessions and Runs

### Creating Sessions

```go
// Basic session
sessionID, err := client.NewSession(ctx, "tenant-1", "user-123", nil, nil)

// Session with metadata
sessionID, err := client.NewSession(ctx, "tenant-1", "user-123", nil, map[string]any{
    "user_name": "John Doe",
    "plan":      "premium",
})

// Child session (for nested agents)
childSessionID, err := client.NewSession(ctx, "tenant-1", "child-session", &parentSessionID, nil)
```

### Running Agents

AgentPG provides two API modes for running agents:

#### Batch API (Cost-Effective, Higher Latency)

```go
// Async run using Batch API (returns immediately)
runID, err := client.Run(ctx, sessionID, "assistant", "Hello!")

// Wait for completion
response, err := client.WaitForRun(ctx, runID)

// Sync run using Batch API (convenience wrapper)
response, err := client.RunSync(ctx, sessionID, "assistant", "Hello!")

// With transaction support
runID, err := client.RunTx(ctx, tx, sessionID, "assistant", "Hello!")
```

#### Streaming API (Real-Time, Lower Latency)

```go
// Async run using Streaming API (returns immediately)
runID, err := client.RunFast(ctx, sessionID, "assistant", "Hello!")

// Wait for completion
response, err := client.WaitForRun(ctx, runID)

// Sync run using Streaming API (recommended for interactive use)
response, err := client.RunFastSync(ctx, sessionID, "assistant", "Hello!")

// With transaction support
runID, err := client.RunFastTx(ctx, tx, sessionID, "assistant", "Hello!")
```

#### Choosing Between Batch and Streaming

| Feature | Batch API (`Run*`) | Streaming API (`RunFast*`) |
|---------|-------------------|---------------------------|
| Latency | Higher (polling) | Lower (real-time) |
| Cost | 50% discount | Standard pricing |
| Best for | Background tasks, high volume | Interactive apps, chat UIs |
| Timeout | 24 hours | Connection-based |

### Response Structure

```go
type Response struct {
    // Text is the final text response
    Text string

    // StopReason: "end_turn", "max_tokens", "tool_use"
    StopReason string

    // Usage contains token statistics
    Usage Usage

    // Message is the full message with content blocks
    Message *Message

    // IterationCount is how many batch API calls were made
    IterationCount int

    // ToolIterations is iterations that involved tool use
    ToolIterations int
}

type Usage struct {
    InputTokens              int
    OutputTokens             int
    CacheCreationInputTokens int
    CacheReadInputTokens     int
}
```

### Querying Run State

```go
// Get current run state
run, err := client.GetRun(ctx, runID)
if err != nil {
    log.Fatal(err)
}

fmt.Printf("State: %s\n", run.State)
fmt.Printf("Iterations: %d\n", run.IterationCount)
fmt.Printf("Tokens: %d in, %d out\n", run.InputTokens, run.OutputTokens)

if run.State == agentpg.RunStateFailed {
    fmt.Printf("Error: %s - %s\n", run.ErrorType, run.ErrorMessage)
}
```

### Run States

```go
const (
    RunStatePending         = "pending"          // Waiting for worker
    RunStateBatchSubmitting = "batch_submitting" // Submitting to Batch API
    RunStateBatchPending    = "batch_pending"    // Batch submitted, waiting
    RunStateBatchProcessing = "batch_processing" // Claude processing
    RunStatePendingTools    = "pending_tools"    // Waiting for tool executions
    RunStateAwaitingInput   = "awaiting_input"   // Needs continuation
    RunStateCompleted       = "completed"        // Terminal: success
    RunStateCancelled       = "cancelled"        // Terminal: cancelled
    RunStateFailed          = "failed"           // Terminal: error
)
```

---

## Transaction-First API

AgentPG provides transaction-aware methods for atomic operations.

### Why Transaction-First?

```go
// Scenario: Create order and notify AI assistant atomically
func CreateOrderWithNotification(ctx context.Context, client *agentpg.Client, pool *pgxpool.Pool, order Order) error {
    tx, err := pool.Begin(ctx)
    if err != nil {
        return err
    }
    defer tx.Rollback(ctx)

    // Insert order in transaction
    orderID, err := insertOrder(ctx, tx, order)
    if err != nil {
        return err
    }

    // Create session in same transaction
    sessionID, err := client.NewSessionTx(ctx, tx, "tenant-1", fmt.Sprintf("order-%s", orderID), nil, nil)
    if err != nil {
        return err
    }

    // Create run in same transaction
    // Run won't be visible to workers until transaction commits
    runID, err := client.RunTx(ctx, tx, sessionID, "order-processor",
        fmt.Sprintf("Process order %s: %v", orderID, order))
    if err != nil {
        return err
    }

    // Commit - now everything is visible atomically
    if err := tx.Commit(ctx); err != nil {
        return err
    }

    // Wait for processing (OUTSIDE transaction)
    response, err := client.WaitForRun(ctx, runID)
    return err
}
```

### Available Transaction Methods

```go
// Session creation in transaction
sessionID, err := client.NewSessionTx(ctx, tx, tenantID, userID, parentSessionID, metadata)

// Run creation in transaction
runID, err := client.RunTx(ctx, tx, sessionID, agentName, prompt)
```

### Important: No RunSyncTx

There is intentionally **no `RunSyncTx`** method because it would deadlock:

```go
// THIS WOULD DEADLOCK - DON'T DO THIS
func BadExample(ctx context.Context, tx pgx.Tx) {
    runID, _ := client.RunTx(ctx, tx, sessionID, "agent", "prompt")

    // DEADLOCK: Run is not visible until tx commits,
    // but we're waiting for it before committing
    response, _ := client.WaitForRun(ctx, runID)

    tx.Commit(ctx) // Never reached
}

// CORRECT PATTERN
func GoodExample(ctx context.Context, pool *pgxpool.Pool) {
    tx, _ := pool.Begin(ctx)
    runID, _ := client.RunTx(ctx, tx, sessionID, "agent", "prompt")
    tx.Commit(ctx) // Commit first

    response, _ := client.WaitForRun(ctx, runID) // Then wait
}
```

---

## Distributed Workers

### How Distribution Works

Multiple client instances can process work from the same database:

```go
// Instance 1 (pod-1)
client1, _ := agentpg.NewClient(drv, &agentpg.ClientConfig{
    APIKey: apiKey,
    Name:   "worker",
    ID:     "pod-1",
})

// Instance 2 (pod-2)
client2, _ := agentpg.NewClient(drv, &agentpg.ClientConfig{
    APIKey: apiKey,
    Name:   "worker",
    ID:     "pod-2",
})

// Both register the same agents and tools
for _, c := range []*agentpg.Client{client1, client2} {
    c.RegisterAgent(&agentpg.AgentDefinition{Name: "assistant", ...})
    c.RegisterTool(&MyTool{})
}
```

### Race-Safe Claiming

Work is claimed using `SELECT FOR UPDATE SKIP LOCKED`:

```sql
-- Stored procedure for claiming runs
CREATE FUNCTION agentpg_claim_runs(p_instance_id TEXT, p_max_count INTEGER)
RETURNS SETOF agentpg_runs AS $$
BEGIN
    RETURN QUERY
    WITH claimable AS (
        SELECT r.id FROM agentpg_runs r
        WHERE r.state = 'pending'
          AND r.claimed_by_instance_id IS NULL
          AND EXISTS (
              SELECT 1 FROM agentpg_instance_agents ia
              WHERE ia.instance_id = p_instance_id
                AND ia.agent_name = r.agent_name
          )
        ORDER BY r.created_at ASC
        LIMIT p_max_count
        FOR UPDATE OF r SKIP LOCKED  -- Race-safe!
    )
    UPDATE agentpg_runs r
    SET claimed_by_instance_id = p_instance_id,
        claimed_at = NOW(),
        state = 'batch_submitting'
    FROM claimable c WHERE r.id = c.id
    RETURNING r.*;
END;
$$ LANGUAGE plpgsql;
```

### Capability-Based Routing

Instances only claim work they can handle:

```go
// Specialized worker for code tools only
codeWorker, _ := agentpg.NewClient(drv, &agentpg.ClientConfig{
    Name: "code-worker",
})
codeWorker.RegisterAgent(&agentpg.AgentDefinition{Name: "code-assistant", ...})
codeWorker.RegisterTool(&LintTool{})
codeWorker.RegisterTool(&TestTool{})
// This worker only claims runs for "code-assistant" agent
// and tool executions for "lint" and "test" tools

// General worker
generalWorker, _ := agentpg.NewClient(drv, &agentpg.ClientConfig{
    Name: "general-worker",
})
generalWorker.RegisterAgent(&agentpg.AgentDefinition{Name: "assistant", ...})
generalWorker.RegisterTool(&WeatherTool{})
// This worker claims different work
```

### Instance Health and Cleanup

```go
// Heartbeat keeps instance alive
// Default: every 15 seconds

// When instance dies without graceful shutdown:
// 1. Heartbeat stops
// 2. Leader detects stale instance (no heartbeat for TTL)
// 3. Leader deletes stale instance
// 4. Trigger marks orphaned work as failed
// 5. Failed tool executions may be retried
```

---

## Claude Batch API Integration

AgentPG uses Claude's Batch API for cost-effective, async processing.

### Batch API Flow

```
1. Run created (state: pending)
           ↓
2. Worker claims run (state: batch_submitting)
           ↓
3. Worker submits to Batch API (state: batch_pending)
           ↓
4. Batch Poller polls status (state: batch_processing)
           ↓
5. Batch completes (state: completed/pending_tools/failed)
```

### Iteration Tracking

Each batch API call is tracked as an iteration:

```sql
SELECT
    i.iteration_number,
    i.trigger_type,
    i.batch_id,
    i.batch_status,
    i.stop_reason,
    i.has_tool_use,
    i.input_tokens,
    i.output_tokens
FROM agentpg_iterations i
WHERE i.run_id = 'run-123'
ORDER BY i.iteration_number;

-- iteration_number | trigger_type  | stop_reason | has_tool_use
-- 1                | user_prompt   | tool_use    | true
-- 2                | tool_results  | tool_use    | true
-- 3                | tool_results  | end_turn    | false
```

### Batch Polling Configuration

```go
client, _ := agentpg.NewClient(drv, &agentpg.ClientConfig{
    // How often to poll Claude for batch status
    BatchPollInterval: 30 * time.Second,

    // Minimum gap between polls for same batch
    // (handled internally by batch poller)
})
```

### Batch Expiration

Claude batches expire after 24 hours. AgentPG tracks this:

```sql
-- batch_expires_at is set when batch is submitted
UPDATE agentpg_iterations
SET batch_expires_at = NOW() + INTERVAL '24 hours'
WHERE id = 'iter-123';

-- Expired batches result in failed runs
```

---

## Multi-Iteration Runs

A single run can involve multiple Claude API calls (iterations).

### Example Flow

```
User: "Find weather in NYC and send email summary"

Iteration 1:
  Input: User prompt
  Output: tool_use [get_weather(city="NYC")]

  [Execute get_weather tool]

Iteration 2:
  Input: tool_result [Weather: 72°F, sunny]
  Output: tool_use [send_email(to="user@example.com", body="...")]

  [Execute send_email tool]

Iteration 3:
  Input: tool_result [Email sent successfully]
  Output: end_turn "I've checked the weather and sent you an email..."
```

### Database State During Multi-Iteration

```sql
-- Run progresses through states
-- pending → batch_submitting → batch_pending → batch_processing → pending_tools
--        ↑_____________________________________________________|
--        (loop until no more tool_use)

-- Each iteration creates records
INSERT INTO agentpg_iterations (run_id, iteration_number, trigger_type, ...)
VALUES
    ('run-1', 1, 'user_prompt', ...),
    ('run-1', 2, 'tool_results', ...),
    ('run-1', 3, 'tool_results', ...);

-- Tool executions link to iterations
INSERT INTO agentpg_tool_executions (run_id, iteration_id, tool_name, ...)
VALUES
    ('run-1', 'iter-1', 'get_weather', ...),
    ('run-1', 'iter-2', 'send_email', ...);
```

### Tracking Progress

```go
// Run includes iteration counts
run, _ := client.GetRun(ctx, runID)
fmt.Printf("Iteration: %d/%d\n", run.CurrentIteration, run.IterationCount)
fmt.Printf("Tool iterations: %d\n", run.ToolIterations)
```

---

## Context Compaction

Long conversations can exceed Claude's context window (200K tokens). AgentPG provides automatic and manual compaction via the `compaction` package.

### Configuration

```go
import "github.com/youssefsiam38/agentpg/compaction"

// Enable auto-compaction in ClientConfig
client, _ := agentpg.NewClient(drv, &agentpg.ClientConfig{
    APIKey: apiKey,
    AutoCompactionEnabled: true,  // Trigger compaction after each run
    CompactionConfig: &compaction.Config{
        Strategy:            compaction.StrategyHybrid, // or StrategySummarization
        Trigger:             0.85,                      // 85% context usage threshold
        TargetTokens:        80000,                     // Target after compaction
        PreserveLastN:       10,                        // Always keep last 10 messages
        ProtectedTokens:     40000,                     // Never touch last 40K tokens
        MaxTokensForModel:   200000,                    // Claude's context window
        SummarizerModel:     "claude-3-5-haiku-20241022",
        SummarizerMaxTokens: 4096,
        PreserveToolOutputs: false,                     // Prune tool outputs in hybrid mode
        UseTokenCountingAPI: true,                      // Use Claude's token counting API
    },
})
```

### Compaction Strategies

#### Hybrid Strategy (Default)

Two-phase approach that minimizes API costs:

1. **Phase 1: Prune Tool Outputs** - Replaces tool outputs with `[TOOL OUTPUT PRUNED]` placeholders (free, no API call)
2. **Phase 2: Summarize** - If still over target, uses Claude to summarize remaining messages

```go
CompactionConfig: &compaction.Config{
    Strategy:            compaction.StrategyHybrid,
    PreserveToolOutputs: false,  // Enable pruning
}
```

#### Summarization Strategy

Directly summarizes all compactable messages using Claude's streaming API. Creates a structured 9-section summary (Claude Code pattern).

```go
CompactionConfig: &compaction.Config{
    Strategy: compaction.StrategySummarization,
}
```

### Manual Compaction

```go
// Check if compaction is needed
needsCompaction, err := client.NeedsCompaction(ctx, sessionID)

// Get detailed statistics
stats, err := client.GetCompactionStats(ctx, sessionID)
fmt.Printf("Usage: %.1f%% (%d tokens)\n", stats.UsagePercent*100, stats.TotalTokens)
fmt.Printf("Messages: %d total, %d compactable\n", stats.TotalMessages, stats.CompactableMessages)

// Manual compaction
result, err := client.Compact(ctx, sessionID)
fmt.Printf("Reduced: %d -> %d tokens\n", result.OriginalTokens, result.CompactedTokens)

// Compact only if needed
result, err := client.CompactIfNeeded(ctx, sessionID)

// Compact with custom config (one-off override)
result, err := client.CompactWithConfig(ctx, sessionID, &compaction.Config{
    Strategy:     compaction.StrategySummarization,
    TargetTokens: 50000,
})
```

### Message Partitioning

Messages are partitioned into mutually exclusive categories:

| Category | Description | Compactable |
|----------|-------------|-------------|
| Protected | Within last `ProtectedTokens` (40K default) | No |
| Preserved | Marked `is_preserved=true` | No |
| Recent | Last `PreserveLastN` messages (10 default) | No |
| Summaries | Previous compaction summaries (`is_summary=true`) | No |
| Compactable | Everything else | Yes |

### Preserved Messages

Mark important messages to never be compacted:

```sql
-- Messages with is_preserved=true are never removed
UPDATE agentpg_messages
SET is_preserved = true
WHERE id = 'important-message-id';
```

### Compaction Result

```go
type Result struct {
    EventID             uuid.UUID     // ID of the compaction event record
    Strategy            Strategy      // Strategy that was used
    OriginalTokens      int           // Token count before compaction
    CompactedTokens     int           // Token count after compaction
    MessagesRemoved     int           // Number of messages archived
    PreservedMessageIDs []uuid.UUID   // IDs of preserved messages
    SummaryCreated      bool          // Whether a summary was created
    Duration            time.Duration // How long compaction took
}
```

### Compaction Statistics

```go
type Stats struct {
    SessionID           uuid.UUID
    TotalMessages       int
    TotalTokens         int
    UsagePercent        float64  // Percentage of context window used
    CompactionCount     int      // Times this session has been compacted
    PreservedMessages   int      // Non-compactable message count
    SummaryMessages     int      // Summary messages from previous compactions
    CompactableMessages int      // Messages eligible for compaction
    NeedsCompaction     bool     // Whether compaction should be triggered
}
```

### Database Tables

```sql
-- Track compaction history
SELECT
    strategy,
    original_tokens,
    compacted_tokens,
    messages_removed,
    duration_ms
FROM agentpg_compaction_events
WHERE session_id = 'session-123'
ORDER BY created_at DESC;

-- Retrieve archived messages (for potential recovery)
SELECT original_message
FROM agentpg_message_archive
WHERE session_id = 'session-123'
ORDER BY archived_at DESC;
```

### Token Counting

The compaction package uses Claude's token counting API with a fallback:

1. **Primary**: `client.Messages.CountTokens()` API for accurate counts
2. **Fallback**: Character-based approximation (~4 characters per token)

---

## Error Handling

### Error Types

```go
var (
    ErrInvalidConfig          = errors.New("invalid configuration")
    ErrSessionNotFound        = errors.New("session not found")
    ErrToolNotFound           = errors.New("tool not found")
    ErrRunNotFound            = errors.New("run not found")
    ErrAgentNotFound          = errors.New("agent not found")
    ErrAgentNotRegistered     = errors.New("agent not registered")
    ErrClientNotStarted       = errors.New("client not started")
    ErrClientAlreadyStarted   = errors.New("client already started")
    ErrInvalidStateTransition = errors.New("invalid state transition")
    ErrRunAlreadyFinalized    = errors.New("run already finalized")
    ErrInvalidToolSchema      = errors.New("invalid tool schema")
    ErrToolExecutionFailed    = errors.New("tool execution failed")
    ErrCompactionFailed       = errors.New("context compaction failed")
    ErrStorageError           = errors.New("storage operation failed")
)
```

### Structured Errors

```go
// AgentError provides context
type AgentError struct {
    Op        string         // Operation that failed
    Err       error          // Underlying error
    SessionID string         // Session ID if applicable
    Context   map[string]any // Additional context
}

// Usage
if err != nil {
    var agentErr *agentpg.AgentError
    if errors.As(err, &agentErr) {
        log.Printf("Operation %s failed: %v", agentErr.Op, agentErr.Err)
        log.Printf("Session: %s", agentErr.SessionID)
        log.Printf("Context: %v", agentErr.Context)
    }
}
```

### Run Failure Handling

```go
response, err := client.RunSync(ctx, sessionID, "assistant", prompt)
if err != nil {
    // Check if run exists but failed
    if errors.Is(err, agentpg.ErrRunNotFound) {
        log.Println("Run was not created")
        return
    }

    // Get run details for debugging
    run, _ := client.GetRun(ctx, runID)
    if run != nil && run.State == agentpg.RunStateFailed {
        log.Printf("Run failed: %s - %s", run.ErrorType, run.ErrorMessage)

        switch run.ErrorType {
        case "batch_error":
            // Claude API error
        case "tool_error":
            // Tool execution failed
        case "timeout":
            // Run exceeded time limit
        case "instance_disconnected":
            // Worker died during processing
        }
    }
}
```

### Tool Error Handling

```go
func (t *MyTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
    // Check context cancellation
    if err := ctx.Err(); err != nil {
        return "", fmt.Errorf("cancelled: %w", err)
    }

    // Validate input
    var params struct {
        Required string `json:"required"`
    }
    if err := json.Unmarshal(input, &params); err != nil {
        return "", fmt.Errorf("invalid input: %w", err)
    }

    if params.Required == "" {
        return "", errors.New("required field is empty")
    }

    // Execute with timeout
    ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
    defer cancel()

    result, err := doWork(ctx, params)
    if err != nil {
        // Return error - will be passed to Claude as tool_result with is_error=true
        return "", fmt.Errorf("work failed: %w", err)
    }

    return result, nil
}
```

### Retry and Rescue System

AgentPG provides a robust retry and rescue system (inspired by River) to ensure runs and tool executions never get stuck in non-terminal states.

#### Tool Error Types

Tools can return special error types to control retry behavior:

```go
import "github.com/youssefsiam38/agentpg/tool"

func (t *MyTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
    // Cancel immediately - no retry, permanent failure
    if isInvalidInput(input) {
        return "", tool.ToolCancel(errors.New("invalid input format"))
    }

    // Discard permanently - similar to cancel, for unrecoverable errors
    if !isAuthorized(ctx) {
        return "", tool.ToolDiscard(errors.New("unauthorized"))
    }

    // Snooze - retry after duration, does NOT consume an attempt
    if isRateLimited(err) {
        return "", tool.ToolSnooze(30*time.Second, err)
    }

    // Regular error - will be retried with exponential backoff
    result, err := doWork(ctx)
    if err != nil {
        return "", err  // Will retry up to MaxAttempts times
    }

    return result, nil
}
```

#### Retry Configuration

```go
client, _ := agentpg.NewClient(drv, &agentpg.ClientConfig{
    // Tool retry configuration (defaults are optimized for snappy UX)
    ToolRetryConfig: &agentpg.ToolRetryConfig{
        MaxAttempts: 2,    // Default: 2 attempts (1 retry) for fast feedback
        Jitter:      0.0,  // Default: 0 = instant retry, no delay
    },

    // Run rescue configuration
    RunRescueConfig: &agentpg.RunRescueConfig{
        RescueInterval:    time.Minute,      // Default: check every 1 minute
        RescueTimeout:     5 * time.Minute,  // Default: runs stuck > 5 min are rescued
        MaxRescueAttempts: 3,                // Default: max 3 rescue attempts
    },
})
```

#### Instant Retry (Default)

By default, tool retries happen **instantly** with no delay for a snappy user experience:
- `MaxAttempts: 2` = 1 immediate retry on failure
- `Jitter: 0.0` = no delay between retries

#### Exponential Backoff (Opt-in)

For tools that need backoff (e.g., external APIs with rate limits), set `Jitter > 0` to enable River's attempt^4 formula:

```go
ToolRetryConfig: &agentpg.ToolRetryConfig{
    MaxAttempts: 5,    // More attempts for unreliable services
    Jitter:      0.1,  // Enable backoff with 10% jitter
},
```

| Attempt | Delay |
|---------|-------|
| 1 | 1 second |
| 2 | 16 seconds |
| 3 | 81 seconds |
| 4 | 256 seconds |
| 5 | 625 seconds |

Jitter (±10%) prevents thundering herd effects when multiple tools retry simultaneously.

#### Snooze vs Retry

- **Retry**: Regular errors consume an attempt. After `MaxAttempts`, the execution fails permanently.
- **Snooze**: Does NOT consume an attempt. Useful for rate limits, temporary unavailability. Unlimited snoozes allowed.

#### Agent-as-Tool Failures

Agent-as-tool (child run) failures are NOT retried. When a child agent fails, the parent run receives the error as a tool result, allowing the parent agent to handle it appropriately.

#### Run Rescue

The rescuer worker (running on the leader instance only) periodically checks for stuck runs:

- Runs stuck in non-terminal states (`batch_submitting`, `batch_pending`, `batch_processing`, `streaming`, `pending_tools`) longer than `RescueTimeout` are reset to `pending` state
- After `MaxRescueAttempts`, the run is marked as `failed` with error type `rescue_failed`
- Rescue tracking fields: `rescue_attempts`, `last_rescue_at`

#### Database Fields

Tool executions track:
- `scheduled_at`: When the execution becomes eligible for claiming
- `snooze_count`: Number of times snoozed (informational)
- `last_error`: Error message from the last failed attempt

Runs track:
- `rescue_attempts`: Number of times rescued
- `last_rescue_at`: Timestamp of last rescue

---

## Database Schema

### Core Tables

| Table | Purpose |
|-------|---------|
| `agentpg_sessions` | Conversation contexts with multi-tenant isolation |
| `agentpg_runs` | Agent run executions with hierarchy support |
| `agentpg_iterations` | Each batch API call within a run |
| `agentpg_messages` | Conversation messages |
| `agentpg_content_blocks` | Normalized message content |
| `agentpg_tool_executions` | Pending/completed tool work |
| `agentpg_agents` | Agent definitions |
| `agentpg_tools` | Tool definitions |

### Infrastructure Tables

| Table | Purpose |
|-------|---------|
| `agentpg_instances` | Running worker instances (UNLOGGED) |
| `agentpg_instance_agents` | Which agents each instance handles |
| `agentpg_instance_tools` | Which tools each instance handles |
| `agentpg_leader` | Leader election (UNLOGGED) |
| `agentpg_compaction_events` | Compaction audit trail |
| `agentpg_message_archive` | Archived compacted messages |

### Key Relationships

```sql
-- Hierarchical runs
agentpg_runs.parent_run_id → agentpg_runs.id
agentpg_runs.parent_tool_execution_id → agentpg_tool_executions.id

-- Multi-iteration tracking
agentpg_iterations.run_id → agentpg_runs.id
agentpg_runs.current_iteration_id → agentpg_iterations.id

-- Tool execution to run
agentpg_tool_executions.run_id → agentpg_runs.id
agentpg_tool_executions.iteration_id → agentpg_iterations.id
agentpg_tool_executions.child_run_id → agentpg_runs.id (for agent-as-tool)

-- Session hierarchy
agentpg_sessions.parent_session_id → agentpg_sessions.id
```

### Enum Types

```sql
-- Run states
CREATE TYPE agentpg_run_state AS ENUM(
    'pending', 'batch_submitting', 'batch_pending', 'batch_processing',
    'pending_tools', 'awaiting_input', 'completed', 'cancelled', 'failed'
);

-- Batch status
CREATE TYPE agentpg_batch_status AS ENUM(
    'in_progress', 'canceling', 'ended'
);

-- Tool execution states
CREATE TYPE agentpg_tool_execution_state AS ENUM(
    'pending', 'running', 'completed', 'failed', 'skipped'
);

-- Content types
CREATE TYPE agentpg_content_type AS ENUM(
    'text', 'tool_use', 'tool_result', 'image', 'document',
    'thinking', 'server_tool_use', 'web_search_result'
);

-- Message roles
CREATE TYPE agentpg_message_role AS ENUM(
    'user', 'assistant', 'system'
);
```

---

## LISTEN/NOTIFY Events

AgentPG uses PostgreSQL LISTEN/NOTIFY for real-time event distribution.

### Channels

| Channel | Trigger | Payload |
|---------|---------|---------|
| `agentpg_run_created` | New pending run | `{run_id, session_id, agent_name, parent_run_id, depth}` |
| `agentpg_run_state` | Run state change | `{run_id, session_id, agent_name, state, previous_state, parent_run_id}` |
| `agentpg_run_finalized` | Run completed/failed/cancelled | `{run_id, session_id, state, parent_run_id, parent_tool_execution_id}` |
| `agentpg_tool_pending` | New tool execution | `{execution_id, run_id, tool_name, is_agent_tool, agent_name}` |
| `agentpg_tools_complete` | All tools for run done | `{run_id}` |

### Subscribing to Events

```go
// Internal implementation
func (w *Worker) subscribeToNotifications(ctx context.Context) {
    conn, _ := pool.Acquire(ctx)
    defer conn.Release()

    conn.Exec(ctx, "LISTEN agentpg_run_created")
    conn.Exec(ctx, "LISTEN agentpg_tool_pending")

    for {
        notification, _ := conn.Conn().WaitForNotification(ctx)

        switch notification.Channel {
        case "agentpg_run_created":
            w.handleRunCreated(notification.Payload)
        case "agentpg_tool_pending":
            w.handleToolPending(notification.Payload)
        }
    }
}
```

### Polling Fallback

When LISTEN/NOTIFY is unavailable (connection issues), workers fall back to polling:

```go
// RunWorker polls every RunPollInterval (default: 1s)
// ToolWorker polls every ToolPollInterval (default: 500ms)
// BatchPoller polls every BatchPollInterval (default: 30s)
```

---

## Leader Election

One instance is elected leader for maintenance tasks.

### Leader Responsibilities

- Clean up stale instances (no heartbeat)
- Recover stuck runs (claimed too long)
- Run periodic maintenance

### Election Mechanism

```sql
-- Single-row table
CREATE UNLOGGED TABLE agentpg_leader (
    name TEXT PRIMARY KEY DEFAULT 'default' CHECK (name = 'default'),
    leader_id TEXT NOT NULL,
    elected_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL
);

-- Attempt to become leader
INSERT INTO agentpg_leader (leader_id, elected_at, expires_at)
VALUES ('instance-1', NOW(), NOW() + INTERVAL '30 seconds')
ON CONFLICT (name) DO UPDATE
SET leader_id = 'instance-1',
    elected_at = NOW(),
    expires_at = NOW() + INTERVAL '30 seconds'
WHERE agentpg_leader.expires_at < NOW();

-- Refresh lease (must be current leader)
UPDATE agentpg_leader
SET expires_at = NOW() + INTERVAL '30 seconds'
WHERE leader_id = 'instance-1';
```

### Leader TTL Configuration

```go
client, _ := agentpg.NewClient(drv, &agentpg.ClientConfig{
    LeaderTTL: 30 * time.Second, // Lease duration
    HeartbeatInterval: 15 * time.Second, // Refresh interval
})
```

---

## Monitoring and Observability

### Key Metrics

```sql
-- Active runs by state
SELECT state, COUNT(*)
FROM agentpg_runs
WHERE finalized_at IS NULL
GROUP BY state;

-- Tool execution queue depth
SELECT tool_name, COUNT(*)
FROM agentpg_tool_executions
WHERE state = 'pending'
GROUP BY tool_name;

-- Iteration counts per run
SELECT
    r.id,
    r.agent_name,
    r.iteration_count,
    r.tool_iterations,
    r.input_tokens + r.output_tokens as total_tokens
FROM agentpg_runs r
WHERE r.created_at > NOW() - INTERVAL '1 hour';

-- Instance health
SELECT
    id,
    name,
    active_run_count,
    active_tool_count,
    last_heartbeat_at,
    NOW() - last_heartbeat_at as time_since_heartbeat
FROM agentpg_instances
ORDER BY last_heartbeat_at DESC;
```

### Logging

```go
// Implement Logger interface
type MyLogger struct {
    logger *slog.Logger
}

func (l *MyLogger) Debug(msg string, args ...any) { l.logger.Debug(msg, args...) }
func (l *MyLogger) Info(msg string, args ...any)  { l.logger.Info(msg, args...) }
func (l *MyLogger) Warn(msg string, args ...any)  { l.logger.Warn(msg, args...) }
func (l *MyLogger) Error(msg string, args ...any) { l.logger.Error(msg, args...) }

client, _ := agentpg.NewClient(drv, &agentpg.ClientConfig{
    Logger: &MyLogger{logger: slog.Default()},
})
```

### Health Checks

```go
// Check if client is healthy
func healthCheck(client *agentpg.Client) bool {
    // Client is started
    if client.InstanceID() == "" {
        return false
    }

    // Can query database
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()

    _, err := client.GetSession(ctx, uuid.Nil) // Will return ErrSessionNotFound
    return err == agentpg.ErrSessionNotFound // Error is expected, but DB is reachable
}
```

---

## Testing

### Unit Testing Tools

```go
func TestMyTool(t *testing.T) {
    tool := &MyTool{}

    // Test schema
    schema := tool.InputSchema()
    assert.Equal(t, "object", schema.Type)
    assert.Contains(t, schema.Required, "input")

    // Test execution
    input := json.RawMessage(`{"input": "test"}`)
    result, err := tool.Execute(context.Background(), input)

    assert.NoError(t, err)
    assert.Contains(t, result, "expected output")
}
```

### Integration Testing

```go
func TestAgentRun(t *testing.T) {
    // Setup test database
    pool := setupTestDB(t)
    defer pool.Close()

    drv := pgxv5.New(pool)
    client, _ := agentpg.NewClient(drv, &agentpg.ClientConfig{
        APIKey: os.Getenv("ANTHROPIC_API_KEY"),
    })

    client.RegisterAgent(&agentpg.AgentDefinition{
        Name:         "test-assistant",
        Model:        "claude-3-5-haiku-20241022", // Use fast model for tests
        SystemPrompt: "You are a test assistant. Always respond with 'OK'.",
    })

    ctx := context.Background()
    client.Start(ctx)
    defer client.Stop(context.Background())

    sessionID, _ := client.NewSession(ctx, "test", "test-session", nil, nil)
    response, err := client.RunSync(ctx, sessionID, "test-assistant", "Say OK")

    require.NoError(t, err)
    assert.Contains(t, response.Text, "OK")
}
```

### Mocking the Batch API

```go
type MockBatchAPI struct {
    responses map[string]*worker.BatchResultResponse
}

func (m *MockBatchAPI) SubmitBatch(ctx context.Context, req worker.BatchRequest) (string, string, error) {
    batchID := "mock-batch-" + uuid.New().String()
    requestID := req.CustomID
    return batchID, requestID, nil
}

func (m *MockBatchAPI) GetBatchStatus(ctx context.Context, batchID string) (*worker.BatchStatusResponse, error) {
    return &worker.BatchStatusResponse{
        ID:     batchID,
        Status: worker.BatchStatusEnded,
    }, nil
}

func (m *MockBatchAPI) GetBatchResult(ctx context.Context, batchID, requestID string) (*worker.BatchResultResponse, error) {
    return m.responses[requestID], nil
}
```

---

### API Modes

AgentPG supports two API modes for executing agents:

#### Batch API (Cost-Effective)

```go
// Run() creates pending run, returns immediately
runID, _ := client.Run(ctx, sessionID, "agent", prompt)

// Wait for batch processing (polls until complete)
response, _ := client.WaitForRun(ctx, runID)

// Or use convenience wrapper
response, _ := client.RunSync(ctx, sessionID, "agent", prompt)

// With transaction support
runID, _ := client.RunTx(ctx, tx, sessionID, "agent", prompt)
```

#### Streaming API (Real-Time)

```go
// RunFast() uses streaming API for lower latency
runID, _ := client.RunFast(ctx, sessionID, "agent", prompt)

// Wait for streaming to complete
response, _ := client.WaitForRun(ctx, runID)

// Or use convenience wrapper (recommended for interactive apps)
response, _ := client.RunFastSync(ctx, sessionID, "agent", prompt)

// With transaction support
runID, _ := client.RunFastTx(ctx, tx, sessionID, "agent", prompt)
```

**When to use which:**
- **Batch API**: Background processing, high volume, cost-sensitive (50% discount)
- **Streaming API**: Interactive apps, chat interfaces, real-time responses

### Database Migration

```bash
# Backup existing data
pg_dump $DATABASE_URL > backup.sql

# Run down migration to remove old schema
psql $DATABASE_URL -f storage/migrations/001_agentpg_migration.down.sql

# Run up migration for new schema
psql $DATABASE_URL -f storage/migrations/001_agentpg_migration.up.sql

# Migrate existing sessions/messages if needed
# (Custom migration script based on your data)
```

---

## Best Practices

### Agent Design

1. **Single Responsibility**: Each agent should have a focused role
2. **Clear System Prompts**: Be specific about capabilities and limitations
3. **Appropriate Model Selection**: Use cheaper models for simple tasks

### Tool Design

1. **Validate Input**: Always validate and sanitize tool input
2. **Handle Timeouts**: Respect context cancellation
3. **Return Useful Errors**: Error messages are shown to Claude
4. **Idempotency**: Tools may be retried on failure

### Hierarchy Design

1. **Limit Depth**: Deep hierarchies add latency (each level = batch API call)
2. **Clear Delegation**: Make it obvious when to delegate
3. **Aggregation**: Higher-level agents should synthesize responses

### Performance

1. **Connection Pooling**: Use pgxpool for connection efficiency
2. **Batch Size**: Tune claim batch sizes for your workload
3. **Concurrency**: Set appropriate MaxConcurrentRuns/Tools
4. **Compaction**: Enable for long conversations

### Security

1. **Tenant Isolation**: Always filter by tenant_id
2. **API Key Protection**: Use environment variables
3. **Input Validation**: Sanitize all user input
4. **Tool Permissions**: Limit tool capabilities appropriately

---

## Troubleshooting

### Run Stuck in Pending

```sql
-- Check if any workers have capability
SELECT ia.instance_id, ia.agent_name
FROM agentpg_instance_agents ia
WHERE ia.agent_name = 'stuck-agent';

-- Check worker health
SELECT id, last_heartbeat_at
FROM agentpg_instances
ORDER BY last_heartbeat_at DESC;
```

### Tool Execution Not Starting

```sql
-- Check tool registration
SELECT it.instance_id, it.tool_name
FROM agentpg_instance_tools it
WHERE it.tool_name = 'stuck-tool';

-- Check pending executions
SELECT id, tool_name, created_at
FROM agentpg_tool_executions
WHERE state = 'pending'
ORDER BY created_at;
```

### Batch Never Completes

```sql
-- Check iteration status
SELECT
    i.batch_id,
    i.batch_status,
    i.batch_poll_count,
    i.batch_last_poll_at,
    i.batch_expires_at
FROM agentpg_iterations i
WHERE i.batch_status = 'in_progress'
ORDER BY i.created_at DESC;

-- Check if batch expired
SELECT * FROM agentpg_iterations
WHERE batch_expires_at < NOW()
  AND batch_status = 'in_progress';
```

### High Memory Usage

- Reduce MaxConcurrentRuns/MaxConcurrentTools
- Enable compaction for long sessions
- Check for message accumulation in sessions

---

## API Reference

See the [Go documentation](https://pkg.go.dev/github.com/youssefsiam38/agentpg) for complete API reference.

### Key Types

- `Client[TTx]` - Main client type
- `ClientConfig` - Client configuration
- `AgentDefinition` - Agent configuration
- `Response` - Run result
- `Run` - Run state
- `Session` - Session state
- `Message` - Conversation message
- `ContentBlock` - Message content

### Key Methods

- `NewClient()` - Create client
- `RegisterAgent()` - Register agent
- `RegisterTool()` - Register tool
- `RegisterAgentAsTool()` - Create agent hierarchy
- `Start()` / `Stop()` - Lifecycle
- `NewSession()` / `NewSessionTx()` - Create sessions
- `Run()` / `RunTx()` / `RunSync()` - Execute agents (Batch API)
- `RunFast()` / `RunFastTx()` / `RunFastSync()` - Execute agents (Streaming API)
- `WaitForRun()` - Wait for completion
- `GetRun()` / `GetSession()` - Query state
- `Compact()` / `CompactWithConfig()` - Manual compaction
- `CompactIfNeeded()` - Conditional compaction
- `NeedsCompaction()` - Check compaction threshold
- `GetCompactionStats()` - Get session compaction statistics

---

## Admin UI

AgentPG includes an embedded admin UI for monitoring and managing agents. The UI is built with HTMX + Tailwind CSS and provides server-side rendering for a fast, responsive experience.

### Overview

The `ui` package provides a handler function:

| Handler | Description |
|---------|-------------|
| `ui.UIHandler()` | SSR frontend with HTMX + Tailwind |

### Basic Setup

```go
import (
    "net/http"
    "github.com/youssefsiam38/agentpg/ui"
)

// Create driver and client as usual
drv := pgxv5.New(pool)
client, _ := agentpg.NewClient(drv, clientConfig)

// Mount UI at /ui/
uiConfig := &ui.Config{
    BasePath:        "/ui",
    PageSize:        25,
    RefreshInterval: 5 * time.Second,
}
http.Handle("/ui/", http.StripPrefix("/ui", ui.UIHandler(drv.Store(), client, uiConfig)))
```

### Configuration Options

```go
type Config struct {
    // BasePath is the URL prefix where the UI is mounted.
    // For example, if mounted at "/ui/", set BasePath to "/ui".
    // All navigation links will be prefixed with this path.
    BasePath string

    // TenantID filters data to a single tenant.
    // If empty, shows all tenants (admin mode) with a tenant selector.
    TenantID string

    // ReadOnly disables write operations (chat, session creation).
    // Useful for monitoring-only deployments.
    ReadOnly bool

    // Logger for structured logging.
    Logger Logger

    // RefreshInterval for SSE updates and auto-refresh.
    // Defaults to 5 seconds.
    RefreshInterval time.Duration

    // PageSize for pagination.
    // Defaults to 25.
    PageSize int
}
```

### Admin Mode vs Single-Tenant Mode

```go
// Admin mode: shows all tenants with a selector
adminConfig := &ui.Config{
    BasePath: "/admin",
    // TenantID is empty = admin mode
}

// Single-tenant mode: filters to one tenant only
tenantConfig := &ui.Config{
    BasePath: "/ui",
    TenantID: "tenant-123",  // Only shows this tenant's data
}
```

### Read-Only Monitoring Mode

```go
// Full admin UI with chat
fullUIConfig := &ui.Config{
    BasePath: "/ui",
}
http.Handle("/ui/", http.StripPrefix("/ui", ui.UIHandler(store, client, fullUIConfig)))

// Read-only monitoring (no chat, no write operations)
monitorConfig := &ui.Config{
    BasePath: "/monitor",
    ReadOnly: true,  // Disables chat and session creation
}
// Pass nil for client when ReadOnly is true
http.Handle("/monitor/", http.StripPrefix("/monitor", ui.UIHandler(store, nil, monitorConfig)))
```

### Adding Middleware

Wrap handlers externally using standard Go patterns:

```go
// Single middleware
cfg := &ui.Config{BasePath: "/ui"}
http.Handle("/ui/", http.StripPrefix("/ui", authMiddleware(ui.UIHandler(store, client, cfg))))

// Multiple middlewares chained
handler := authMiddleware(loggingMiddleware(rateLimitMiddleware(ui.UIHandler(store, client, cfg))))
http.Handle("/ui/", http.StripPrefix("/ui", handler))

// Using justinas/alice
chain := alice.New(authMiddleware, loggingMiddleware)
http.Handle("/ui/", http.StripPrefix("/ui", chain.Then(ui.UIHandler(store, client, cfg))))

// Using chi router
r.Route("/ui", func(r chi.Router) {
    r.Use(authMiddleware)
    r.Use(loggingMiddleware)
    r.Mount("/", ui.UIHandler(store, client, cfg))
})
```

### Frontend Pages

The UI provides these pages:

| Path | Description |
|------|-------------|
| `/` | Redirects to dashboard |
| `/dashboard` | Overview with stats, recent runs, active instances |
| `/sessions` | Session list with pagination and filtering |
| `/sessions/{id}` | Session detail with runs, messages, token usage |
| `/runs` | Run list with state filtering |
| `/runs/{id}` | Run detail with iterations, tool executions, messages |
| `/runs/{id}/conversation` | Full conversation view for a run |
| `/tool-executions` | Tool execution list with state filtering |
| `/tool-executions/{id}` | Tool execution detail with input/output |
| `/agents` | Registered agents across all instances |
| `/instances` | Active worker instances with health status |
| `/compaction` | Compaction events history |
| `/chat` | Interactive chat interface |
| `/chat/session/{id}` | Chat with existing session |

### Chat Interface

The chat interface allows real-time interaction with agents:

```go
// Chat is enabled when:
// 1. ReadOnly is false (default)
// 2. A client is provided to UIHandler

// Full UI with chat
http.Handle("/ui/", http.StripPrefix("/ui", ui.UIHandler(store, client, config)))

// To disable chat, either:
// Option 1: Set ReadOnly to true
config.ReadOnly = true

// Option 2: Pass nil for client
http.Handle("/ui/", http.StripPrefix("/ui", ui.UIHandler(store, nil, config)))
```

Chat features:
- Create new sessions with custom user IDs
- Select from registered agents
- Send messages and see real-time responses
- View tool executions inline
- Automatic polling for run completion

### Complete Example

```go
package main

import (
    "log"
    "net/http"
    "os"
    "time"

    "github.com/jackc/pgx/v5/pgxpool"
    "github.com/youssefsiam38/agentpg"
    "github.com/youssefsiam38/agentpg/driver/pgxv5"
    "github.com/youssefsiam38/agentpg/ui"
)

func main() {
    ctx := context.Background()

    // Connect to PostgreSQL
    pool, _ := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
    defer pool.Close()

    // Create driver and client
    drv := pgxv5.New(pool)
    client, _ := agentpg.NewClient(drv, &agentpg.ClientConfig{
        APIKey: os.Getenv("ANTHROPIC_API_KEY"),
    })

    // Register agents and tools
    client.RegisterAgent(&agentpg.AgentDefinition{
        Name:         "assistant",
        Model:        "claude-sonnet-4-5-20250929",
        SystemPrompt: "You are a helpful assistant.",
    })

    client.Start(ctx)
    defer client.Stop(context.Background())

    // Setup HTTP handlers
    mux := http.NewServeMux()

    // Full admin UI with chat
    fullConfig := &ui.Config{
        BasePath:        "/ui",
        PageSize:        25,
        RefreshInterval: 5 * time.Second,
    }
    mux.Handle("/ui/", http.StripPrefix("/ui", ui.UIHandler(drv.Store(), client, fullConfig)))

    // Read-only monitoring (separate endpoint)
    monitorConfig := &ui.Config{
        BasePath: "/monitor",
        ReadOnly: true,
        PageSize: 50,
    }
    mux.Handle("/monitor/", http.StripPrefix("/monitor", ui.UIHandler(drv.Store(), nil, monitorConfig)))

    log.Println("Server starting on :8080")
    log.Println("  /ui/      - Admin UI with chat")
    log.Println("  /monitor/ - Read-only monitoring")
    http.ListenAndServe(":8080", mux)
}
```
