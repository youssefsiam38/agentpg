# AgentPG Architecture

This document provides a comprehensive overview of AgentPG's architecture for CTOs, architects, and maintainers evaluating or working with the framework.

## Executive Summary

AgentPG is a stateful AI agent framework that solves three core problems:

1. **Conversation Persistence** - Automatic storage and retrieval of conversation history in PostgreSQL
2. **Context Management** - Intelligent compaction when conversations exceed model context limits
3. **Tool Orchestration** - Type-safe tool execution with parallel processing support

## System Overview

### Single-Instance Architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│                         Application Layer                            │
│                    (Your Go Application Code)                        │
└─────────────────────────────────────────────────────────────────────┘
                                  │
                                  ▼
┌─────────────────────────────────────────────────────────────────────┐
│                            AgentPG Core                              │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐  ┌────────────┐ │
│  │   Agent     │  │   Session   │  │  Streaming  │  │   Hooks    │ │
│  │  Manager    │  │  Manager    │  │   Handler   │  │  System    │ │
│  └─────────────┘  └─────────────┘  └─────────────┘  └────────────┘ │
└─────────────────────────────────────────────────────────────────────┘
         │                  │                              │
         ▼                  ▼                              ▼
┌─────────────────┐  ┌─────────────────┐  ┌─────────────────────────┐
│  Tool System    │  │ Compaction      │  │    Storage Layer        │
│ ┌─────────────┐ │  │ ┌─────────────┐ │  │  ┌───────────────────┐  │
│ │  Registry   │ │  │ │  Manager    │ │  │  │  Store Interface  │  │
│ ├─────────────┤ │  │ ├─────────────┤ │  │  ├───────────────────┤  │
│ │  Executor   │ │  │ │  Strategies │ │  │  │ PostgresStore     │  │
│ ├─────────────┤ │  │ ├─────────────┤ │  │  │ (+ Transactions)  │  │
│ │  Validator  │ │  │ │ Partitioner │ │  │  └───────────────────┘  │
│ └─────────────┘ │  │ └─────────────┘ │  └─────────────────────────┘
└─────────────────┘  └─────────────────┘              │
         │                   │                        ▼
         ▼                   ▼              ┌─────────────────────┐
┌─────────────────────────────────────────┐ │    PostgreSQL       │
│           Anthropic API                  │ │  ┌───────────────┐ │
│  (Claude Models via Official SDK)        │ │  │   sessions    │ │
└─────────────────────────────────────────┘ │  │   messages    │ │
                                            │  │ compaction_*  │ │
                                            │  │ message_arch* │ │
                                            │  └───────────────┘ │
                                            └─────────────────────┘
```

### Multi-Instance Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              PostgreSQL                                      │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐  ┌───────────────────┐  │
│  │  instances  │  │   leader    │  │    runs     │  │  agents/tools     │  │
│  │  sessions   │  │             │  │  messages   │  │  instance_agents  │  │
│  └─────────────┘  └─────────────┘  └─────────────┘  └───────────────────┘  │
└─────────────────────────────────────────────────────────────────────────────┘
          ▲               ▲               ▲                    ▲
          │    LISTEN/NOTIFY              │                    │
          │               │               │                    │
┌─────────┴───────────────┴───────────────┴────────────────────┴──────────────┐
│                                                                              │
│   ┌─────────────────────────┐        ┌─────────────────────────┐            │
│   │     Instance 1          │        │     Instance 2          │            │
│   │     (Leader)            │        │     (Follower)          │            │
│   │  ┌─────────────────┐   │        │  ┌─────────────────┐   │            │
│   │  │     Client      │   │        │  │     Client      │   │            │
│   │  │  ┌───────────┐  │   │        │  │  ┌───────────┐  │   │            │
│   │  │  │ Heartbeat │  │   │        │  │  │ Heartbeat │  │   │            │
│   │  │  │ Elector   │  │   │        │  │  │ Elector   │  │   │            │
│   │  │  │ Cleanup   │  │   │        │  │  │           │  │   │            │
│   │  │  │ Notifier  │  │   │        │  │  │ Notifier  │  │   │            │
│   │  │  └───────────┘  │   │        │  │  └───────────┘  │   │            │
│   │  │  ┌───────────┐  │   │        │  │  ┌───────────┐  │   │            │
│   │  │  │   Agents  │  │   │        │  │  │   Agents  │  │   │            │
│   │  │  └───────────┘  │   │        │  │  └───────────┘  │   │            │
│   │  └─────────────────┘   │        │  └─────────────────┘   │            │
│   └─────────────────────────┘        └─────────────────────────┘            │
│                                                                              │
└──────────────────────────────────────────────────────────────────────────────┘
```

## Core Components

### 1. Agent (`agent.go`)

The central orchestrator that coordinates all subsystems.

**Responsibilities:**
- Manages conversation flow with Claude API
- Coordinates tool execution loops
- Triggers compaction when context limits approach
- Provides thread-safe session management

**Key Design Decisions:**
- **Thread-Safety**: Uses `sync.RWMutex` for concurrent session access
- **Functional Options**: Configuration via `WithX()` pattern for extensibility
- **Model-Aware Defaults**: Automatically configures context limits based on selected model

```go
type Agent struct {
    mu             sync.RWMutex    // Thread-safe session access
    client         *anthropic.Client
    store          storage.Store
    config         *internalConfig
    currentSession *storage.Session
    hooks          []hooks.Hook
}
```

### 2. Session Management (`session.go`)

Handles conversation lifecycle and persistence.

**Key Features:**
- Create new sessions with tenant isolation
- Load existing sessions by ID or identifier
- Retrieve message history with content conversion
- Track session metadata and compaction count

**Multi-Tenancy Model:**
```
Tenant A                    Tenant B
├── Session 1              ├── Session 1
│   ├── Message 1          │   ├── Message 1
│   └── Message 2          │   └── Message 2
└── Session 2              └── Session 2
```

### 3. Storage Layer (`storage/`)

Pluggable persistence with PostgreSQL as the reference implementation.

**Interface Design:**
```go
type Store interface {
    // Session operations
    CreateSession(ctx, tenantID, identifier, metadata) (string, error)
    GetSession(ctx, id) (*Session, error)
    GetSessionByTenantAndIdentifier(ctx, tenant, identifier) (*Session, error)
    GetSessionsByTenant(ctx, tenantID) ([]*Session, error)

    // Message operations
    SaveMessage(ctx, *Message) error
    GetMessages(ctx, sessionID) ([]*Message, error)
    DeleteMessages(ctx, ids []string) error
    GetSessionTokenCount(ctx, sessionID) (int, error)

    // Compaction operations
    SaveCompactionEvent(ctx, *CompactionEvent) error
    GetCompactionHistory(ctx, sessionID) ([]*CompactionEvent, error)
    ArchiveMessages(ctx, eventID, sessionID, messages) error
}

type TxStore interface {
    Store
    BeginTx(ctx) (Tx, error)  // Transaction support
}
```

**PostgreSQL Implementation:**
- Connection pooling via `pgxpool`
- JSONB for flexible content storage
- Partial indexes for preserved/summary messages
- Cascade deletes for referential integrity

### 4. Tool System (`tool/`)

Type-safe tool definition and parallel execution.

**Components:**

| Component | Purpose |
|-----------|---------|
| `Registry` | Tool registration and lookup |
| `Executor` | Parallel/sequential execution with timeouts |
| `Validator` | JSON Schema validation for inputs |

**Execution Flow:**
```
1. Agent receives tool_use from Claude
2. Executor.ExecuteParallel() spawns goroutines
3. Each goroutine: validate → execute → collect result
4. Results returned to Claude as tool_result
5. Loop continues until no more tool calls
```

**Thread Safety:**
```go
func (e *Executor) ExecuteParallel(ctx, calls) []*ExecuteResult {
    results := make([]*ExecuteResult, len(calls))
    var wg sync.WaitGroup
    wg.Add(len(calls))
    for i, call := range calls {
        go func(idx int, c ToolCallRequest) {
            defer wg.Done()
            results[idx] = e.Execute(ctx, c.ToolName, c.Input)
        }(i, call)
    }
    wg.Wait()
    return results
}
```

### 5. Compaction System (`compaction/`)

Intelligent context window management for long conversations.

**Problem Solved:**
Claude models have finite context windows (e.g., 200K tokens). Long conversations must be compressed without losing critical information.

**Architecture:**
```
┌──────────────────────────────────────────────────────┐
│                  Compaction Manager                   │
│  - Monitors token usage                              │
│  - Triggers compaction at threshold (default: 85%)  │
│  - Coordinates strategy execution                    │
└──────────────────────────────────────────────────────┘
                          │
                          ▼
┌──────────────────────────────────────────────────────┐
│                    Partitioner                        │
│  - Identifies compactable vs protected messages      │
│  - Preserves: system prompts, recent N messages      │
│  - Protects: messages marked is_preserved=true       │
└──────────────────────────────────────────────────────┘
                          │
                          ▼
┌──────────────────────────────────────────────────────┐
│                    Strategies                         │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐  │
│  │Summarization│  │   Hybrid    │  │   Custom    │  │
│  │  (Default)  │  │             │  │             │  │
│  └─────────────┘  └─────────────┘  └─────────────┘  │
└──────────────────────────────────────────────────────┘
```

**Summarization Strategy:**
1. Partition messages into compactable/protected sets
2. Send compactable messages to summarizer model
3. Generate concise summary preserving key information
4. Archive original messages (for potential rollback)
5. Replace with summary message
6. Record compaction event for audit

**Transaction Safety:**
Compaction operations are wrapped in database transactions to ensure atomicity. If any step fails, the entire operation rolls back.

### 6. Streaming (`streaming/`)

Real-time response delivery for better UX.

**Components:**
- `Accumulator`: Aggregates streaming chunks into complete messages
- `Event`: Typed events (ContentBlockStart, ContentBlockDelta, etc.)
- Handler callbacks: `OnToken`, `OnComplete`, `OnToolUse`, `OnError`

**Event Flow:**
```
Claude API SSE Stream
        │
        ▼
┌───────────────────┐
│   Accumulator     │
│   - Buffers text  │
│   - Tracks state  │
│   - Emits events  │
└───────────────────┘
        │
        ▼
┌───────────────────┐
│  Stream Handler   │
│  - OnToken()      │
│  - OnToolUse()    │
│  - OnComplete()   │
└───────────────────┘
```

### 7. Hooks System (`hooks/`)

Extensibility points for cross-cutting concerns.

**Available Hooks:**
```go
type Hook interface {
    BeforeRun(ctx, input string) error
    AfterRun(ctx, input, output string, err error)
    BeforeToolCall(ctx, toolName string, input json.RawMessage) error
    AfterToolCall(ctx, toolName string, input json.RawMessage, output string, err error)
}
```

**Use Cases:**
- Logging and observability
- Rate limiting
- Input/output validation
- Metrics collection
- Audit trails

## Data Flow

### Standard Request Flow

```
1. User calls agent.Run(ctx, "Hello")
2. Agent checks current session (thread-safe read)
3. Agent loads message history from PostgreSQL
4. Agent checks token count, triggers compaction if needed
5. Agent sends request to Claude API with:
   - System prompt
   - Message history
   - Available tools
6. Claude responds (possibly with tool calls)
7. If tool calls present:
   a. Executor validates inputs
   b. Executor runs tools in parallel
   c. Tool results added to conversation
   d. Loop back to step 5
8. Final response saved to PostgreSQL
9. Response returned to user
```

### Compaction Flow

```
1. Agent detects tokens > threshold (85% of max)
2. CompactionManager.Compact() called
3. Partitioner separates messages:
   - Protected: system, recent N, marked preserved
   - Compactable: everything else
4. Strategy processes compactable messages
5. Within transaction:
   a. Archive original messages
   b. Delete originals from messages table
   c. Insert summary message
   d. Record compaction event
6. Transaction commits (or rolls back on error)
7. Updated history used for next request
```

## Design Principles

### 1. Separation of Concerns
Each package has a single responsibility:
- `agent.go`: Orchestration
- `session.go`: Session lifecycle
- `storage/`: Persistence
- `tool/`: Tool execution
- `compaction/`: Context management

### 2. Driver-Based Design
Database access is abstracted via drivers with type-safe transactions:
```go
// pgxv5 driver for pgx/v5 users
drv := pgxv5.New(pool)
agent, _ := agentpg.New(drv, agentpg.Config{...})

// database/sql driver for standard library users
drv := databasesql.New(db)
agent, _ := agentpg.New(drv, agentpg.Config{...})
```

### 3. Functional Options
Configuration is composable and type-safe:
```go
agent, _ := agentpg.New(drv, agentpg.Config{
    Client:       &client,
    Model:        "claude-sonnet-4-5-20250929",
    SystemPrompt: "You are helpful.",
},
    agentpg.WithMaxTokens(4096),
    agentpg.WithCompactionTrigger(0.8),
)
```

### 4. Fail-Safe Defaults
- Model-aware context limits
- Sensible compaction thresholds
- Automatic timeout handling
- Graceful degradation

### 5. Observability
- Hook system for logging/metrics
- Compaction event audit trail
- Message archive for debugging

## Performance Characteristics

### Database Operations
| Operation | Complexity | Index Used |
|-----------|------------|------------|
| Get session by ID | O(1) | Primary key |
| Get session by tenant+identifier | O(1) | Composite index |
| Get messages for session | O(n) | Session+created_at index |
| Save message | O(1) | Upsert |
| Delete messages | O(k) | Primary key batch |

### Memory Usage
- Messages loaded on-demand (not cached in agent)
- Streaming uses bounded buffers
- Tool execution results collected, then released

### Concurrency
- Thread-safe session switching
- Parallel tool execution with configurable concurrency
- Connection pool for database (configurable pool size)

## Security Considerations

See [Security](./security.md) for detailed analysis. Key points:
- API keys should use environment variables
- SQL injection prevented via parameterized queries
- Tool inputs validated against schemas
- Multi-tenant isolation via tenant_id filtering

## Scalability

### Horizontal Scaling
- Stateless agent instances (state in PostgreSQL)
- Multiple application instances can share the database
- Connection pooling handles concurrent access

### Vertical Scaling
- Compaction reduces storage growth
- Archive tables can be partitioned by date
- Indexes optimized for common query patterns

## Distributed Components

### 8. Client (`client.go`)

The Client manages multi-instance deployment lifecycle.

**Responsibilities:**
- Instance registration and deregistration
- Background service coordination (heartbeat, cleanup, election)
- Agent/tool registry management
- Graceful shutdown orchestration

```go
type Client[TTx any] struct {
    driver          driver.Driver[TTx]
    store           storage.Store
    anthropicClient *anthropic.Client
    config          *ClientConfig
    instanceID      string

    // Background services
    heartbeat *maintenance.Heartbeat
    cleanup   *maintenance.Cleanup
    elector   *leadership.Elector
    notif     *notifier.Notifier

    // State
    started  atomic.Bool
    isLeader atomic.Bool
}
```

### 9. Leadership (`leadership/`)

TTL-based leader election using PostgreSQL.

**Election Flow:**
```
1. Instance attempts to acquire lease (INSERT with TTL)
2. If successful, becomes leader and starts cleanup service
3. Leader must renew lease before expiry (UPDATE expires_at)
4. If renewal fails, leadership is lost
5. Other instances can acquire expired leases
```

**Key Design Decisions:**
- Single leader for cleanup prevents duplicate work
- TTL-based lease allows automatic failover
- No external coordinator required (PostgreSQL only)

### 10. Maintenance (`maintenance/`)

Background services for instance health and cleanup.

**Heartbeat Service:**
- Periodically updates `last_heartbeat_at` in database
- Default interval: 30 seconds
- Failure to heartbeat marks instance as stale

**Cleanup Service (Leader Only):**
- Removes stale instances (no heartbeat for 2 minutes)
- Marks stuck runs as failed (running > timeout)
- Cleans up expired leader entries

### 11. Run State Machine (`runstate/`)

Tracks individual agent executions with validated state transitions.

```go
// Valid transitions
RunStateRunning → RunStateCompleted  // Success
RunStateRunning → RunStateCancelled  // User cancel
RunStateRunning → RunStateFailed     // Error or timeout

// Invalid transitions (return error)
RunStateCompleted → RunStateRunning  // Terminal state
RunStateFailed → RunStateCompleted   // Terminal state
```

### 12. Notifier (`notifier/`)

Real-time event propagation via PostgreSQL LISTEN/NOTIFY.

**Event Types:**
| Event | Description |
|-------|-------------|
| `run_state_changed` | Run completed/failed/cancelled |
| `instance_registered` | New instance joined |
| `instance_deregistered` | Instance left |
| `leader_changed` | Leadership changed |

**Architecture:**
```
Database Trigger → NOTIFY channel → Listener → Notifier → Subscriber Handlers
```

## Future Architecture Considerations

### Planned Improvements
1. **Pluggable LLM Providers** - Support for OpenAI, local models
2. **Caching Layer** - Redis integration for session caching
3. **Event Sourcing** - Full message history reconstruction
4. **Async Tool Execution** - Background job support

### Extension Points
- Custom storage backends (implement `Store` interface)
- Custom compaction strategies (implement `Strategy` interface)
- Custom hooks (implement `Hook` interface)

## Related Documentation

- [Storage](./storage.md) - Database schema details
- [Compaction](./compaction.md) - Strategy configuration
- [Tools](./tools.md) - Building custom tools
- [Security](./security.md) - Security model
- [Deployment](./deployment.md) - Production setup
- [Distributed](./distributed.md) - Multi-instance deployment
