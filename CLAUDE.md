# AgentPG - Event-Driven AI Agent Framework

Event-driven Go framework for building async AI agents using PostgreSQL for state management and distribution. Provides stateful, distributed, and transaction-safe agent execution with Claude Batch API integration.

## Architecture Overview

**AgentPG Architecture**

**Layer 1: HTTP Server Layer (Optional)**
- ui.UIHandler() - /ui/* (HTMX + SSR)
- Components: Dashboard, Sessions/Runs, Chat Interface, Monitoring

**Layer 2: Workers**
- Client 1 (Worker), Client 2 (Worker), Client N (Worker)
- Deployed as k8s pods

**Layer 3: PostgreSQL Database**
- Tables: Sessions, Runs, Iterations, Tools, Messages, Instances, Agents
- LISTEN/NOTIFY channels:
  - agentpg_run_created
  - agentpg_tool_pending
  - agentpg_run_state
  - agentpg_tools_complete

**Layer 4: Claude Batch API**
- 24h processing window
- Async submission/polling


### Key Design Principles

1. **PostgreSQL as Single Source of Truth**: All state stored in PostgreSQL. No in-memory state that could be lost.
2. **Event-Driven with Polling Fallback**: Uses LISTEN/NOTIFY for real-time events, with polling as fallback.
3. **Race-Safe Distribution**: Uses `SELECT FOR UPDATE SKIP LOCKED` for safe work claiming across workers.
4. **Transaction-First**: `RunTx()` accepts user transactions for atomic operations.
5. **Database-Driven Agents**: Agents are database entities with UUID primary keys, not per-client registrations. Tools are registered per-client.
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
An `Agent` is a Claude-powered AI with a specific role, model, and system prompt. Agents are database entities identified by UUID, created via `CreateAgent()` after the client starts.
```go
agent, err := client.CreateAgent(ctx, &agentpg.AgentDefinition{
    Name:         "assistant",
    Model:        "claude-sonnet-4-5-20250929",
    SystemPrompt: "You are a helpful assistant.",
})
// Use agent.ID (uuid.UUID) when running
```

### Tool
A `Tool` is a function that agents can call, implementing the `tool.Tool` interface.
```go
type WeatherTool struct{}
func (t *WeatherTool) Name() string        { return "get_weather" }
func (t *WeatherTool) Description() string { return "Get current weather" }
func (t *WeatherTool) InputSchema() tool.ToolSchema { /* ... */ }
func (t *WeatherTool) Execute(ctx context.Context, input json.RawMessage) (string, error) { /* ... */ }
client.RegisterTool(&WeatherTool{})
```

### Session
A `Session` is a conversation context. Messages are stored per-session. Sessions use flexible metadata for app-specific fields like tenant/user identifiers.
```go
sessionID, err := client.NewSession(ctx, nil, map[string]any{
    "tenant_id": "tenant-1", "user_id": "user-123",
})
```

### Run
A `Run` is a single agent invocation. Runs are async by default and can span multiple iterations.
```go
runID, err := client.Run(ctx, sessionID, agent.ID, "Hello!", nil)  // Uses agent UUID, nil variables
response, err := client.WaitForRun(ctx, runID)

// With variables (passed to tools via context)
runID, err := client.Run(ctx, sessionID, agent.ID, "Continue the story", map[string]any{
    "story_id": "story-123",
    "user_id":  "user-456",
})
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
psql $DATABASE_URL -f storage/migrations/001_agentpg_migration.up.sql
```

### Basic Example
```go
package main

import (
    "context"; "fmt"; "log"; "os"
    "github.com/jackc/pgx/v5/pgxpool"
    "github.com/youssefsiam38/agentpg"
    "github.com/youssefsiam38/agentpg/driver/pgxv5"
)

func main() {
    ctx := context.Background()
    pool, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
    if err != nil { log.Fatal(err) }
    defer pool.Close()

    drv := pgxv5.New(pool)
    client, err := agentpg.NewClient(drv, &agentpg.ClientConfig{
        APIKey: os.Getenv("ANTHROPIC_API_KEY"),
    })
    if err != nil { log.Fatal(err) }

    if err := client.Start(ctx); err != nil { log.Fatal(err) }
    defer client.Stop(context.Background())

    // Get or create agent (idempotent - safe to call on every startup)
    agent, err := client.GetOrCreateAgent(ctx, &agentpg.AgentDefinition{
        Name:         "assistant",
        Model:        "claude-sonnet-4-5-20250929",
        SystemPrompt: "You are a helpful assistant.",
    })
    if err != nil { log.Fatal(err) }

    sessionID, _ := client.NewSession(ctx, nil, nil)
    response, err := client.RunSync(ctx, sessionID, agent.ID, "What is 2+2?", nil)
    if err != nil { log.Fatal(err) }
    fmt.Println(response.Text)
}
```

---

## Client Configuration

```go
type ClientConfig struct {
    APIKey             string        // Required. Falls back to ANTHROPIC_API_KEY env var
    Name               string        // Instance name. Defaults to hostname
    ID                 string        // Unique ID. Defaults to generated UUID
    MaxConcurrentRuns  int           // Default: 10
    MaxConcurrentTools int           // Default: 50
    BatchPollInterval  time.Duration // Default: 30s - how often to poll Claude Batch API
    RunPollInterval    time.Duration // Default: 1s - polling fallback for new runs
    ToolPollInterval   time.Duration // Default: 500ms - polling for tool executions
    HeartbeatInterval  time.Duration // Default: 15s - instance liveness
    LeaderTTL          time.Duration // Default: 30s - leader election lease
    StuckRunTimeout    time.Duration // Default: 5min - marks runs as stuck
    Logger             Logger
    AutoCompactionEnabled bool       // Default: false - enable auto compaction
    CompactionConfig   *compaction.Config
}
```

### Drivers
```go
// database/sql
db, _ := sql.Open("postgres", os.Getenv("DATABASE_URL"))
drv := databasesql.New(db)

// pgx/v5 (Recommended)
pool, _ := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
drv := pgxv5.New(pool)
```

---

## Agent Creation

Agents are database entities created via `CreateAgent()` after the client starts. They are identified by UUID primary keys.

```go
type AgentDefinition struct {
    Name         string            // Required unique identifier (display name)
    Description  string            // Shown when agent is used as tool
    Model        string            // Required Claude model ID
    SystemPrompt string            // Agent behavior definition
    Tools        []string          // Tool names this agent can use (must be registered)
    AgentIDs     []uuid.UUID       // Agent UUIDs for delegation (agent-as-tool)
    Metadata     map[string]any    // App-specific data (tenant_id, tags, etc.)
    MaxTokens    *int
    Temperature  *float64
    TopK         *int
    TopP         *float64
    Config       map[string]any
}

// Create agent - returns agent with UUID
agent, err := client.CreateAgent(ctx, &agentpg.AgentDefinition{...})
fmt.Println(agent.ID)  // uuid.UUID

// Get or create (idempotent) - returns existing or creates new
agent, err := client.GetOrCreateAgent(ctx, &agentpg.AgentDefinition{
    Name:  "assistant",
    Model: "claude-sonnet-4-5-20250929",
})

// Query agents
agents, err := client.ListAgents(ctx, nil, 100, 0)  // All agents (limit 100, offset 0)
agents, err := client.ListAgents(ctx, map[string]any{"tenant_id": "t1"}, 100, 0)  // Filter by metadata

// Get agent by ID or name
agent, err := client.GetAgentByID(ctx, agentID)
agent, err := client.GetAgentByName(ctx, "assistant", nil)  // nil = no metadata filter
```

### Model Selection
```go
"claude-opus-4-5-20251101"   // Most capable, best for complex tasks
"claude-sonnet-4-5-20250929" // Balanced performance and cost
"claude-3-5-haiku-20241022"  // Fast and cost-effective
```

---

## Tool Registration

### Tool Interface
```go
type Tool interface {
    Name() string
    Description() string
    InputSchema() ToolSchema
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
            "expression": {Type: "string", Description: "Math expression (e.g., '2 + 2')"},
        },
        Required: []string{"expression"},
    }
}

func (t *CalculatorTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
    var params struct { Expression string `json:"expression"` }
    if err := json.Unmarshal(input, &params); err != nil {
        return "", fmt.Errorf("invalid input: %w", err)
    }
    return fmt.Sprintf("Result: %v", evaluateExpression(params.Expression)), nil
}
```

### Database-Aware Tool
```go
type UserLookupTool struct { db *pgxpool.Pool }

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
    var params struct { UserID string `json:"user_id"` }
    json.Unmarshal(input, &params)
    var name, email string
    err := t.db.QueryRow(ctx, "SELECT name, email FROM users WHERE id = $1", params.UserID).Scan(&name, &email)
    if err != nil { return "", fmt.Errorf("user not found: %w", err) }
    return fmt.Sprintf("User: %s <%s>", name, email), nil
}
```

### Registering Tools and Creating Agents
```go
// Step 1: Register tools on client (before Start)
client.RegisterTool(&CalculatorTool{})
client.RegisterTool(&UserLookupTool{db: pool})
client.RegisterTool(&WeatherTool{apiKey: weatherAPIKey})

// Step 2: Start client
client.Start(ctx)

// Step 3: Create agents with allowed tools (after Start)
mathAgent, _ := client.CreateAgent(ctx, &agentpg.AgentDefinition{
    Name:  "math-assistant", Model: "claude-sonnet-4-5-20250929",
    Tools: []string{"calculator"},
})

fullAgent, _ := client.CreateAgent(ctx, &agentpg.AgentDefinition{
    Name:  "full-assistant", Model: "claude-sonnet-4-5-20250929",
    Tools: []string{"calculator", "lookup_user", "get_weather"},
})

// Step 4: Run using agent UUID
response, _ := client.RunSync(ctx, sessionID, mathAgent.ID, "What is 2+2?", nil)
```

**Important**: An agent can only use tools that are registered on the client instance AND listed in the agent's `Tools` array.

---

## Agent-as-Tool Pattern

Enables multi-level agent hierarchies where one agent can delegate to another using the `AgentIDs` field.

### Basic Delegation
```go
// Create specialist agent first (child)
researcher, _ := client.CreateAgent(ctx, &agentpg.AgentDefinition{
    Name:         "researcher",
    Description:  "Research specialist for gathering information",
    Model:        "claude-sonnet-4-5-20250929",
    SystemPrompt: "You are a research specialist.",
})

// Create manager with delegation via AgentIDs field
manager, _ := client.CreateAgent(ctx, &agentpg.AgentDefinition{
    Name:         "manager",
    Model:        "claude-sonnet-4-5-20250929",
    SystemPrompt: "You are a project manager. Delegate research tasks to your researcher.",
    AgentIDs:     []uuid.UUID{researcher.ID},
})
```

### Multi-Level Hierarchy
```go
// Level 3: Worker agents with specialized tools
frontendDev, _ := client.CreateAgent(ctx, &agentpg.AgentDefinition{
    Name: "frontend-dev", Description: "Frontend developer",
    Model: "claude-sonnet-4-5-20250929", Tools: []string{"lint"},
})
backendDev, _ := client.CreateAgent(ctx, &agentpg.AgentDefinition{
    Name: "backend-dev", Description: "Backend developer",
    Model: "claude-sonnet-4-5-20250929", Tools: []string{"test"},
})

// Level 2: Team lead - delegates to workers
techLead, _ := client.CreateAgent(ctx, &agentpg.AgentDefinition{
    Name: "tech-lead", Description: "Technical lead",
    Model: "claude-sonnet-4-5-20250929",
    AgentIDs: []uuid.UUID{frontendDev.ID, backendDev.ID},
})

// Level 1: Project manager - delegates to tech-lead
projectManager, _ := client.CreateAgent(ctx, &agentpg.AgentDefinition{
    Name: "project-manager", Model: "claude-sonnet-4-5-20250929",
    AgentIDs: []uuid.UUID{techLead.ID},
})
```

**Important**: Use `Tools` for regular tool access, `AgentIDs` for agent-as-tool delegation. Tools are validated at runtime based on client registrations.

---

## Run Variables (Tool Context)

Pass per-run variables to tools via the `variables` parameter. Tools access these via context helpers.

### Passing Variables
```go
// Pass variables when creating a run
response, _ := client.RunSync(ctx, sessionID, agent.ID, "Continue the story", map[string]any{
    "story_id": "story-123",
    "user_id":  "user-456",
    "tenant_id": "tenant-1",
})
```

### Accessing Variables in Tools
```go
import "github.com/youssefsiam38/agentpg/tool"

type StoryTool struct {
    db *pgxpool.Pool
}

func (t *StoryTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
    // Get a single variable with type safety
    storyID, ok := tool.GetVariable[string](ctx, "story_id")
    if !ok {
        return "", errors.New("story_id not provided")
    }

    // Get with default value
    maxChapters := tool.GetVariableOr[int](ctx, "max_chapters", 10)

    // Get all variables
    vars := tool.GetVariables(ctx)

    // Get full run context (includes RunID, SessionID)
    runCtx, ok := tool.GetRunContext(ctx)
    if ok {
        fmt.Printf("Run: %s, Session: %s\n", runCtx.RunID, runCtx.SessionID)
    }

    // Use in database query
    var content string
    err := t.db.QueryRow(ctx,
        "SELECT content FROM chapters WHERE story_id = $1 LIMIT $2",
        storyID, maxChapters,
    ).Scan(&content)

    return content, err
}
```

### Context Helper Functions
| Function | Description |
|----------|-------------|
| `tool.GetVariable[T](ctx, key)` | Get typed variable, returns (value, ok) |
| `tool.GetVariableOr[T](ctx, key, default)` | Get variable or default value |
| `tool.MustGetVariable[T](ctx, key)` | Get variable or panic |
| `tool.GetVariables(ctx)` | Get all variables as map |
| `tool.GetRunContext(ctx)` | Get full context (RunID, SessionID, Variables) |
| `tool.GetRunID(ctx)` | Get just the run ID |
| `tool.GetSessionID(ctx)` | Get just the session ID |

### Variable Inheritance
Variables are automatically propagated to child runs in agent-as-tool hierarchies:
```go
// Parent run with variables
response, _ := client.RunSync(ctx, sessionID, manager.ID, "Research topic X", map[string]any{
    "project_id": "proj-123",
})
// When manager delegates to researcher agent, researcher's tools also receive project_id
```

---

## Sessions and Runs

### Creating Sessions
```go
sessionID, _ := client.NewSession(ctx, nil, nil)  // Basic
sessionID, _ := client.NewSession(ctx, nil, map[string]any{"tenant_id": "t1", "user_id": "u1"})  // With metadata
childSessionID, _ := client.NewSession(ctx, &parentSessionID, nil)  // Child session
```

### Running Agents

All Run methods accept an agent UUID (not name string) and an optional variables map.

#### Batch API (Cost-Effective, Higher Latency)
```go
runID, _ := client.Run(ctx, sessionID, agent.ID, "Hello!", nil)  // Async
response, _ := client.RunSync(ctx, sessionID, agent.ID, "Hello!", nil)  // Convenience
runID, _ := client.RunTx(ctx, tx, sessionID, agent.ID, "Hello!", nil)  // With transaction
```

#### Streaming API (Real-Time, Lower Latency)
```go
runID, _ := client.RunFast(ctx, sessionID, agent.ID, "Hello!", nil)  // Async
response, _ := client.RunFastSync(ctx, sessionID, agent.ID, "Hello!", nil)  // Recommended for interactive
runID, _ := client.RunFastTx(ctx, tx, sessionID, agent.ID, "Hello!", nil)  // With transaction
```

#### With Variables
```go
// Variables are passed to tools via context during execution
vars := map[string]any{"story_id": "story-123", "tenant_id": "tenant-1"}
response, _ := client.RunSync(ctx, sessionID, agent.ID, "Continue the story", vars)
```

| Feature | Batch API (`Run*`) | Streaming API (`RunFast*`) |
|---------|-------------------|---------------------------|
| Latency | Higher (polling) | Lower (real-time) |
| Cost | 50% discount | Standard pricing |
| Best for | Background tasks, high volume | Interactive apps, chat UIs |

### Response Structure
```go
type Response struct {
    Text           string   // Final text response
    StopReason     string   // "end_turn", "max_tokens", "tool_use"
    Usage          Usage    // Token statistics
    Message        *Message
    IterationCount int      // Batch API calls made
    ToolIterations int      // Iterations with tool use
}
```

### Run States
`pending`, `batch_submitting`, `batch_pending`, `batch_processing`, `pending_tools`, `awaiting_input`, `completed` (terminal), `cancelled` (terminal), `failed` (terminal)

---

## Transaction-First API

```go
func CreateOrderWithNotification(ctx context.Context, client *agentpg.Client, pool *pgxpool.Pool, order Order, agentID uuid.UUID) error {
    tx, _ := pool.Begin(ctx)
    defer tx.Rollback(ctx)

    orderID, _ := insertOrder(ctx, tx, order)
    sessionID, _ := client.NewSessionTx(ctx, tx, nil, map[string]any{"order_id": orderID})
    runID, _ := client.RunTx(ctx, tx, sessionID, agentID,  // Use agent UUID
        fmt.Sprintf("Process order %s: %v", orderID, order),
        map[string]any{"order_id": orderID})  // Variables for tools

    tx.Commit(ctx)  // Commit first
    client.WaitForRun(ctx, runID)  // Then wait
    return nil
}
```

**No `RunSyncTx`**: Would deadlock (run not visible until tx commits, but waiting before committing).

---

## Distributed Workers

Multiple client instances process work from same database using `SELECT FOR UPDATE SKIP LOCKED`:

```go
client1, _ := agentpg.NewClient(drv, &agentpg.ClientConfig{Name: "worker", ID: "pod-1"})
client2, _ := agentpg.NewClient(drv, &agentpg.ClientConfig{Name: "worker", ID: "pod-2"})

// Both register same tools (agents are database entities, shared across instances)
for _, c := range []*agentpg.Client{client1, client2} {
    c.RegisterTool(&MyTool{})
}

// Agents are created once in database, accessible by all instances
agent, _ := client1.CreateAgent(ctx, &agentpg.AgentDefinition{Name: "assistant", ...})
// client2 can also run this agent since it's in the database
```

### Tool-Based Routing
Instances only claim work for tools they have registered:
```go
codeWorker.RegisterTool(&LintTool{})
codeWorker.RegisterTool(&TestTool{})
// Claims runs for any agent that uses "lint" or "test" tools
// Agents are database entities - any instance with the required tools can process runs
```

---

## Context Compaction

```go
client, _ := agentpg.NewClient(drv, &agentpg.ClientConfig{
    AutoCompactionEnabled: true,
    CompactionConfig: &compaction.Config{
        Strategy:         compaction.StrategyHybrid,
        Trigger:          0.85,       // 85% context usage threshold
        TargetTokens:     80000,
        PreserveLastN:    10,
        ProtectedTokens:  40000,
        MaxTokensForModel: 200000,
        SummarizerModel:  "claude-3-5-haiku-20241022",
        SummarizerMaxTokens: 4096,
    },
})
```

### Strategies
- **Hybrid** (default): Phase 1 prunes tool outputs, Phase 2 summarizes if still over target
- **Summarization**: Directly summarizes all compactable messages using Claude

### Manual Compaction
```go
needsCompaction, _ := client.NeedsCompaction(ctx, sessionID)
stats, _ := client.GetCompactionStats(ctx, sessionID)
result, _ := client.Compact(ctx, sessionID)
result, _ := client.CompactIfNeeded(ctx, sessionID)
result, _ := client.CompactWithConfig(ctx, sessionID, customConfig)
```

### Message Partitioning (Non-compactable)
| Category | Description |
|----------|-------------|
| Protected | Within last `ProtectedTokens` |
| Preserved | `is_preserved=true` |
| Recent | Last `PreserveLastN` messages |
| Summaries | `is_summary=true` |

---

## Error Handling

### Error Types
```go
var (
    ErrInvalidConfig, ErrSessionNotFound, ErrToolNotFound, ErrRunNotFound,
    ErrAgentNotFound, ErrAgentNotRegistered, ErrClientNotStarted, ErrClientAlreadyStarted,
    ErrInvalidStateTransition, ErrRunAlreadyFinalized, ErrInvalidToolSchema,
    ErrToolExecutionFailed, ErrCompactionFailed, ErrStorageError
)
```

### Tool Error Types
```go
func (t *MyTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
    if isInvalidInput(input) {
        return "", tool.ToolCancel(errors.New("invalid input"))  // No retry
    }
    if !isAuthorized(ctx) {
        return "", tool.ToolDiscard(errors.New("unauthorized"))  // Similar to cancel
    }
    if isRateLimited(err) {
        return "", tool.ToolSnooze(30*time.Second, err)  // Retry after duration, doesn't consume attempt
    }
    return "", err  // Regular error, will be retried with backoff
}
```

### Retry Configuration
```go
ToolRetryConfig: &agentpg.ToolRetryConfig{
    MaxAttempts: 2,    // Default: 2 (1 retry) for fast feedback
    Jitter:      0.0,  // Default: 0 = instant retry
},
RunRescueConfig: &agentpg.RunRescueConfig{
    RescueInterval:    time.Minute,
    RescueTimeout:     5 * time.Minute,
    MaxRescueAttempts: 3,
},
```

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
| `agentpg_instance_tools` | Which tools each instance handles |
| `agentpg_leader` | Leader election (UNLOGGED) |
| `agentpg_compaction_events` | Compaction audit trail |
| `agentpg_message_archive` | Archived compacted messages |

---

## LISTEN/NOTIFY Events

| Channel | Trigger | Payload |
|---------|---------|---------|
| `agentpg_run_created` | New pending run | run_id, session_id, agent_id, parent_run_id, depth |
| `agentpg_run_state` | Run state change | run_id, session_id, agent_id, state, previous_state |
| `agentpg_run_finalized` | Completed/failed/cancelled | run_id, session_id, state, parent_run_id |
| `agentpg_tool_pending` | New tool execution | execution_id, run_id, tool_name, is_agent_tool |
| `agentpg_tools_complete` | All tools for run done | run_id |

Polling fallback: `RunPollInterval` (1s), `ToolPollInterval` (500ms), `BatchPollInterval` (30s)

---

## Leader Election

One instance is elected leader for maintenance: cleanup stale instances, recover stuck runs, periodic maintenance.

```go
LeaderTTL: 30 * time.Second,  // Lease duration
HeartbeatInterval: 15 * time.Second,  // Refresh interval
```

---

## Monitoring

### Key Metrics
```sql
-- Active runs by state
SELECT state, COUNT(*) FROM agentpg_runs WHERE finalized_at IS NULL GROUP BY state;

-- Tool execution queue depth
SELECT tool_name, COUNT(*) FROM agentpg_tool_executions WHERE state = 'pending' GROUP BY tool_name;

-- Instance health
SELECT id, name, active_run_count, last_heartbeat_at FROM agentpg_instances ORDER BY last_heartbeat_at DESC;
```

### Logging
```go
type MyLogger struct { logger *slog.Logger }
func (l *MyLogger) Debug(msg string, args ...any) { l.logger.Debug(msg, args...) }
func (l *MyLogger) Info(msg string, args ...any)  { l.logger.Info(msg, args...) }
func (l *MyLogger) Warn(msg string, args ...any)  { l.logger.Warn(msg, args...) }
func (l *MyLogger) Error(msg string, args ...any) { l.logger.Error(msg, args...) }

client, _ := agentpg.NewClient(drv, &agentpg.ClientConfig{Logger: &MyLogger{logger: slog.Default()}})
```

---

## Admin UI

```go
uiConfig := &ui.Config{
    BasePath:           "/ui",
    PageSize:           25,
    RefreshInterval:    5 * time.Second,
    MetadataFilter:     map[string]any{"tenant_id": "my-tenant"},  // Pre-filter sessions
    MetadataFilterKeys: []string{"tenant_id", "user_id"},         // Filter dropdowns
    MetadataDisplayKeys: []string{"tenant_id", "user_id"},        // Keys to show in lists
    ReadOnly:           false,  // Set true to disable chat/write operations
}
http.Handle("/ui/", http.StripPrefix("/ui", ui.UIHandler(drv.Store(), client, uiConfig)))
```

### Pages
| Path | Description |
|------|-------------|
| `/dashboard` | Overview with stats, recent runs, active instances |
| `/sessions`, `/sessions/{id}` | Session list and detail |
| `/runs`, `/runs/{id}` | Run list and detail with iterations, tool executions |
| `/runs/{id}/conversation` | Full conversation view |
| `/tool-executions`, `/tool-executions/{id}` | Tool execution list and detail |
| `/agents` | Database agents with capable instances |
| `/instances` | Active worker instances with health status |
| `/compaction` | Compaction events history |
| `/chat`, `/chat/session/{id}` | Interactive chat interface |

---

## Testing

### Unit Testing Tools
```go
func TestMyTool(t *testing.T) {
    tool := &MyTool{}
    schema := tool.InputSchema()
    assert.Equal(t, "object", schema.Type)
    assert.Contains(t, schema.Required, "input")

    input := json.RawMessage(`{"input": "test"}`)
    result, err := tool.Execute(context.Background(), input)
    assert.NoError(t, err)
    assert.Contains(t, result, "expected output")
}
```

### Integration Testing
```go
func TestAgentRun(t *testing.T) {
    pool := setupTestDB(t)
    defer pool.Close()

    client, _ := agentpg.NewClient(pgxv5.New(pool), &agentpg.ClientConfig{
        APIKey: os.Getenv("ANTHROPIC_API_KEY"),
    })

    ctx := context.Background()
    client.Start(ctx)
    defer client.Stop(context.Background())

    // Create agent after Start
    agent, _ := client.CreateAgent(ctx, &agentpg.AgentDefinition{
        Name:         "test-assistant",
        Model:        "claude-3-5-haiku-20241022",
        SystemPrompt: "Always respond with 'OK'.",
    })

    sessionID, _ := client.NewSession(ctx, nil, nil)
    response, err := client.RunSync(ctx, sessionID, agent.ID, "Say OK", nil)
    require.NoError(t, err)
    assert.Contains(t, response.Text, "OK")
}
```

---

## Best Practices

### Agent Design
- Single responsibility per agent
- Clear, specific system prompts
- Use cheaper models for simple tasks

### Tool Design
- Validate and sanitize input
- Handle timeouts (respect context cancellation)
- Return useful errors (shown to Claude)
- Design for idempotency (tools may be retried)

### Hierarchy Design
- Limit depth (each level = batch API call)
- Clear delegation patterns
- Higher-level agents should synthesize responses

### Performance
- Use pgxpool for connection efficiency
- Tune MaxConcurrentRuns/Tools for workload
- Enable compaction for long conversations

### Security
- Use MetadataFilter for data isolation by tenant
- Use env vars for API keys
- Sanitize all user input
- Limit tool capabilities appropriately

---

## Troubleshooting

### Run Stuck in Pending
```sql
-- Check if agent exists and get its required tools
SELECT id, name, tools FROM agentpg_agents WHERE id = 'agent-uuid';

-- Check which instances have the required tools registered
SELECT it.instance_id, it.tool_name FROM agentpg_instance_tools it
WHERE it.tool_name IN (SELECT unnest(tools) FROM agentpg_agents WHERE id = 'agent-uuid');

-- Check instance health
SELECT id, last_heartbeat_at FROM agentpg_instances ORDER BY last_heartbeat_at DESC;
```

### Tool Execution Not Starting
```sql
SELECT it.instance_id, it.tool_name FROM agentpg_instance_tools it WHERE it.tool_name = 'stuck-tool';
SELECT id, tool_name, created_at FROM agentpg_tool_executions WHERE state = 'pending' ORDER BY created_at;
```

### Batch Never Completes
```sql
SELECT batch_id, batch_status, batch_poll_count, batch_expires_at FROM agentpg_iterations WHERE batch_status = 'in_progress';
```

---

## API Reference

See [Go documentation](https://pkg.go.dev/github.com/youssefsiam38/agentpg).

### Key Methods
- `NewClient()`, `Start()`, `Stop()` - Lifecycle
- `RegisterTool()` - Tool registration (before Start)
- `CreateAgent()`, `GetOrCreateAgent()`, `GetAgentByID()`, `GetAgentByName()`, `ListAgents()`, `UpdateAgent()`, `DeleteAgent()` - Agent management (after Start)
- `NewSession()`, `NewSessionTx()` - Create sessions
- `Run()`, `RunTx()`, `RunSync()` - Execute agents (Batch API, takes agent UUID and variables)
- `RunFast()`, `RunFastTx()`, `RunFastSync()` - Execute agents (Streaming API, takes agent UUID and variables)
- `tool.GetVariable()`, `tool.GetVariableOr()`, `tool.GetRunContext()` - Access run variables in tools
- `WaitForRun()`, `GetRun()`, `GetSession()` - Query state
- `Compact()`, `CompactWithConfig()`, `CompactIfNeeded()` - Manual compaction
- `NeedsCompaction()`, `GetCompactionStats()` - Compaction queries