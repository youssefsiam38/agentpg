# AgentPG Architecture

This document provides an in-depth technical overview of AgentPG's architecture, covering the system design, component interactions, data model, and distributed processing mechanisms.

## Table of Contents

1. [High-Level Overview](#high-level-overview)
2. [Directory Structure](#directory-structure)
3. [Core Components](#core-components)
4. [Data Model](#data-model)
5. [Worker System](#worker-system)
6. [Distributed Execution](#distributed-execution)
7. [State Machines](#state-machines)
8. [Event System](#event-system)
9. [Retry and Rescue](#retry-and-rescue)
10. [Context Compaction](#context-compaction)

---

## High-Level Overview

AgentPG is a fully event-driven Go framework for building async AI agents using PostgreSQL as the single source of truth for state management.

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
│  │                        Worker Instances (k8s pods)                          │  │
│  │  ┌──────────────┐    ┌──────────────┐    ┌──────────────┐                  │  │
│  │  │   Client 1   │    │   Client 2   │    │   Client N   │                  │  │
│  │  │  (Worker)    │    │  (Worker)    │    │  (Worker)    │                  │  │
│  │  │              │    │              │    │              │                  │  │
│  │  │ • runWorker  │    │ • runWorker  │    │ • runWorker  │                  │  │
│  │  │ • streaming  │    │ • streaming  │    │ • streaming  │                  │  │
│  │  │ • toolWorker │    │ • toolWorker │    │ • toolWorker │                  │  │
│  │  │ • batchPoll  │    │ • batchPoll  │    │ • batchPoll  │                  │  │
│  │  │ • rescuer    │    │ • rescuer    │    │ • rescuer    │                  │  │
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
│  │                       Claude API (Batch & Streaming)                         │ │
│  │  • Batch API: 24-hour processing, 50% cost discount                         │ │
│  │  • Streaming API: Real-time responses, lower latency                        │ │
│  └─────────────────────────────────────────────────────────────────────────────┘ │
│                                                                                   │
└──────────────────────────────────────────────────────────────────────────────────┘
```

### Key Design Principles

| Principle | Description |
|-----------|-------------|
| **PostgreSQL as Single Source of Truth** | All state is stored in PostgreSQL. No in-memory state that could be lost on crash. |
| **Event-Driven with Polling Fallback** | Uses LISTEN/NOTIFY for real-time events, with polling as fallback for reliability. |
| **Race-Safe Distribution** | Uses `SELECT FOR UPDATE SKIP LOCKED` for safe work claiming across distributed workers. |
| **Transaction-First** | `RunTx()` accepts user transactions for atomic operations. |
| **Database-Driven Agents** | Agents are database entities with UUID primary keys. Tools registered per-client. |
| **Multi-Level Agent Hierarchies** | Agents can be tools for other agents (PM → Lead → Worker pattern). |

---

## Directory Structure

```
agentpg/
├── client.go                 # Main Client orchestration (~1,300 lines)
├── types.go                  # Core data types (Run, Message, Response, etc.)
├── config.go                 # ClientConfig with sensible defaults
├── constants.go              # Enum constants (RunState, RunMode, ToolState)
├── errors.go                 # Sentinel errors and AgentError type
│
├── run_worker.go             # Batch API processor (~360 lines)
├── streaming_worker.go       # Streaming API processor (~570 lines)
├── tool_worker.go            # Tool executor (~380 lines)
├── batch_poller.go           # Batch API status poller (~500 lines)
├── rescuer.go                # Stuck run recovery (~110 lines)
│
├── driver/                   # Database driver abstraction
│   ├── driver.go             # Driver[TTx] interface definition
│   ├── pgxv5/                # pgx/v5 driver implementation
│   │   ├── driver.go
│   │   ├── store.go          # SQL implementations for Store interface
│   │   └── listener.go       # LISTEN/NOTIFY implementation
│   └── databasesql/          # database/sql driver implementation
│       ├── driver.go
│       ├── store.go
│       └── listener.go
│
├── storage/
│   └── migrations/           # PostgreSQL schema migrations
│       ├── 001_agentpg_migration.up.sql
│       └── 001_agentpg_migration.down.sql
│
├── tool/                     # Tool framework
│   ├── tool.go               # Tool interface & ToolSchema
│   └── errors.go             # Tool-specific errors (ToolCancel, ToolSnooze, etc.)
│
├── compaction/               # Context window management
│   ├── compaction.go         # Main Compactor logic
│   ├── config.go             # Compaction configuration
│   ├── strategy.go           # Strategy interface
│   ├── hybrid.go             # Hybrid strategy (prune + summarize)
│   ├── summarization.go      # Summarization strategy
│   ├── message_partition.go  # Message categorization
│   ├── token_counter.go      # Token counting logic
│   └── prompt.go             # Summarization prompts
│
├── ui/                       # Admin UI (HTMX + Tailwind SSR)
│   ├── handler.go            # UIHandler entry point
│   ├── config.go             # UI configuration
│   ├── frontend/             # Web UI with HTMX
│   │   ├── handlers.go
│   │   ├── router.go
│   │   └── render.go
│   └── service/              # Business logic layer
│       └── ...
│
└── examples/                 # Usage examples
    ├── admin_ui/
    ├── basic/
    ├── distributed/
    ├── nested_agents/
    └── context_compaction/
```

---

## Core Components

### Client[TTx any]

The main orchestrator that manages agent/tool registration, worker lifecycle, and run coordination.

```go
type Client[TTx any] struct {
    driver           driver.Driver[TTx]     // DB abstraction
    config           *ClientConfig          // Configuration
    anthropic        anthropic.Client       // Claude API client

    tools  map[string]tool.Tool             // Per-client tool registry

    runWorker        *runWorker[TTx]        // Batch processor
    streamingWorker  *streamingWorker[TTx]  // Streaming processor
    toolWorker       *toolWorker[TTx]       // Tool executor
    batchPoller      *batchPoller[TTx]      // Batch status poller
    rescuer          *rescuer[TTx]          // Stuck run recovery

    compactor        *compaction.Compactor  // Context compaction
    runWaiters map[uuid.UUID][]chan *Run    // Completion signaling
}
```

**Key Responsibilities:**
- Register tools (pre-Start) and create/get agents via database (post-Start)
- Create sessions and initiate runs using agent UUIDs
- Coordinate background workers
- Handle run completion waiting and signaling
- Leadership tracking for maintenance tasks

### Driver[TTx] Interface

Abstraction layer for database connectivity with two implementations:

```go
type Driver[TTx any] interface {
    BeginTx(ctx context.Context) (TTx, error)
    CommitTx(ctx context.Context, tx TTx) error
    RollbackTx(ctx context.Context, tx TTx) error
    Store() Store[TTx]
    Listener() Listener
}
```

**Implementations:**
- **pgxv5**: Uses `github.com/jackc/pgx/v5/pgxpool` (recommended)
- **databasesql**: Uses standard `database/sql` with `github.com/lib/pq`

### Store[TTx] Interface

Comprehensive data access interface with 100+ methods:

| Domain | Examples |
|--------|----------|
| Sessions | CreateSession, GetSession, ListSessions |
| Runs | CreateRun, ClaimRuns, UpdateRunState, GetStuckRuns |
| Iterations | CreateIteration, GetIterationsForPoll |
| Tool Executions | ClaimToolExecutions, RetryToolExecution, SnoozeToolExecution |
| Messages | CreateMessage, GetMessagesForRunContext |
| Instances | RegisterInstance, UpdateHeartbeat, GetStaleInstances |
| Leadership | TryAcquireLeader, RefreshLeader, IsLeader |
| Compaction | CreateCompactionEvent, ArchiveMessage |

### Tool Interface

```go
type Tool interface {
    Name() string
    Description() string
    InputSchema() ToolSchema
    Execute(ctx context.Context, input json.RawMessage) (string, error)
}
```

---

## Data Model

### Entity Relationship Diagram

```
┌─────────────────┐
│    Sessions     │
│  (tenant_id)    │◄──────────────────────────────┐
└────────┬────────┘                               │
         │ 1:N                                    │
         ▼                                        │
┌─────────────────┐                               │
│      Runs       │◄─────────────┐                │
│  (agent_name)   │              │ parent_run_id  │
│  (run_mode)     │──────────────┘                │
└────────┬────────┘                               │
         │ 1:N                                    │
         ▼                                        │
┌─────────────────┐      ┌──────────────────────┐ │
│   Iterations    │      │   Tool Executions    │ │
│  (batch_id)     │◄─────│   (tool_name)        │ │
│  (is_streaming) │      │   (child_run_id) ────┼─┤
└────────┬────────┘      └──────────────────────┘ │
         │ response_message_id                    │
         ▼                                        │
┌─────────────────┐                               │
│    Messages     │───────────────────────────────┘
│   (role)        │
└────────┬────────┘
         │ 1:N
         ▼
┌─────────────────┐
│ Content Blocks  │
│  (type)         │
│  (tool_use_id)  │
└─────────────────┘
```

### Core Tables

| Table | Purpose |
|-------|---------|
| `agentpg_sessions` | Conversation contexts with multi-tenant isolation |
| `agentpg_runs` | Agent run executions with hierarchy support |
| `agentpg_iterations` | Each batch/streaming API call within a run |
| `agentpg_messages` | Conversation messages |
| `agentpg_content_blocks` | Normalized message content (text, tool_use, tool_result) |
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

### Enum Types

```sql
-- Run modes (which API to use)
CREATE TYPE agentpg_run_mode AS ENUM ('batch', 'streaming');

-- Run lifecycle states
CREATE TYPE agentpg_run_state AS ENUM (
    'pending', 'batch_submitting', 'batch_pending', 'batch_processing',
    'streaming', 'pending_tools', 'awaiting_input',
    'completed', 'cancelled', 'failed'
);

-- Tool execution states
CREATE TYPE agentpg_tool_execution_state AS ENUM (
    'pending', 'running', 'completed', 'failed', 'skipped'
);

-- Message content types
CREATE TYPE agentpg_content_type AS ENUM (
    'text', 'tool_use', 'tool_result', 'image', 'document',
    'thinking', 'server_tool_use', 'web_search_result'
);
```

### Key Relationships

```sql
-- Hierarchical runs (agent-as-tool)
agentpg_runs.parent_run_id → agentpg_runs.id
agentpg_runs.parent_tool_execution_id → agentpg_tool_executions.id

-- Multi-iteration tracking
agentpg_iterations.run_id → agentpg_runs.id
agentpg_runs.current_iteration_id → agentpg_iterations.id

-- Tool execution to child run
agentpg_tool_executions.child_run_id → agentpg_runs.id

-- Session hierarchy
agentpg_sessions.parent_session_id → agentpg_sessions.id
```

---

## Worker System

AgentPG implements a multi-worker architecture with five specialized background workers:

```
┌─────────────────────────────────────────────────────────────┐
│                    Client Instance                          │
│  ┌──────────────────────────────────────────────────────┐   │
│  │  Background Workers (5 goroutines)                   │   │
│  │  • runWorker       - Batch API processing            │   │
│  │  • streamingWorker - Streaming API processing        │   │
│  │  • toolWorker      - Tool execution                  │   │
│  │  • batchPoller     - Batch status polling            │   │
│  │  • rescuer         - Stuck run recovery (leader)     │   │
│  │                                                        │   │
│  │  System Workers (3 goroutines)                        │   │
│  │  • heartbeatLoop   - Instance liveness               │   │
│  │  • leaderLoop      - Leader election                 │   │
│  │  • cleanupLoop     - Stale cleanup (leader)          │   │
│  └──────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────┘
```

### Worker Descriptions

| Worker | Purpose | Default Interval |
|--------|---------|------------------|
| **runWorker** | Claims pending batch runs, submits to Claude Batch API | 1 second (poll) |
| **streamingWorker** | Claims pending streaming runs, processes in real-time | 1 second (poll) |
| **toolWorker** | Claims and executes tool executions | 500ms (poll) |
| **batchPoller** | Polls Claude Batch API for status updates | 30 seconds |
| **rescuer** | Recovers runs stuck in non-terminal states (leader only) | 1 minute |

### Batch vs Streaming API

| Feature | Batch API (`Run*`) | Streaming API (`RunFast*`) |
|---------|-------------------|---------------------------|
| Worker | runWorker + batchPoller | streamingWorker |
| Latency | Higher (polling) | Lower (real-time) |
| Cost | 50% discount | Standard pricing |
| Best for | Background tasks, high volume | Interactive apps, chat UIs |
| Timeout | 24 hours | Connection-based |
| State flow | pending → batch_* → pending_tools → completed | pending → streaming → pending_tools → completed |

### Configuration Defaults

```go
DefaultMaxConcurrentRuns            = 10    // Batch API runs
DefaultMaxConcurrentStreamingRuns   = 5     // Streaming (holds connections)
DefaultMaxConcurrentTools           = 50    // Tool executions
DefaultRunPollInterval              = 1 * time.Second
DefaultToolPollInterval             = 500 * time.Millisecond
DefaultBatchPollInterval            = 30 * time.Second
DefaultHeartbeatInterval            = 15 * time.Second
DefaultLeaderTTL                    = 30 * time.Second
DefaultStuckRunTimeout              = 5 * time.Minute
```

---

## Distributed Execution

### Race-Safe Work Claiming

Work is claimed using PostgreSQL's `SELECT FOR UPDATE SKIP LOCKED`:

```sql
-- Stored procedure: agentpg_claim_runs
WITH claimable AS (
    SELECT r.id
    FROM agentpg_runs r
    WHERE r.state = 'pending'
      AND r.claimed_by_instance_id IS NULL
      AND (p_run_mode IS NULL OR r.run_mode = p_run_mode)
      -- Capability check
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
    state = CASE
        WHEN r.run_mode = 'batch' THEN 'batch_submitting'
        WHEN r.run_mode = 'streaming' THEN 'streaming'
    END
FROM claimable c WHERE r.id = c.id
RETURNING r.*;
```

**How SKIP LOCKED works:**
1. `FOR UPDATE` locks selected rows
2. `SKIP LOCKED` skips already-locked rows (non-blocking)
3. Multiple workers query simultaneously; each gets only unlocked rows
4. State transitions atomically in the UPDATE clause
5. Prevents double-claiming without blocking

### Capability-Based Routing

Instances only claim work they can handle:

```go
// Code-specialized worker
codeWorker.RegisterTool(&LintTool{})
codeWorker.RegisterTool(&TestTool{})
codeWorker.Start(ctx)
codeAgent, _ := codeWorker.GetOrCreateAgent(ctx, &AgentDefinition{
    Name:  "code-assistant",
    Model: "claude-sonnet-4-5-20250929",
    Tools: []string{"lint", "test"},
})
// Only claims runs for "code-assistant" and tools "lint"/"test"

// General worker
generalWorker.RegisterTool(&WeatherTool{})
generalWorker.Start(ctx)
assistant, _ := generalWorker.GetOrCreateAgent(ctx, &AgentDefinition{
    Name:  "assistant",
    Model: "claude-sonnet-4-5-20250929",
    Tools: []string{"get_weather"},
})
// Claims different work
```

### Leader Election

Single-leader coordination for maintenance tasks:

```sql
-- agentpg_leader table (single row)
CREATE UNLOGGED TABLE agentpg_leader (
    name TEXT PRIMARY KEY DEFAULT 'default',
    leader_id TEXT NOT NULL,
    elected_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL
);

-- Election: INSERT ON CONFLICT with TTL check
INSERT INTO agentpg_leader (leader_id, elected_at, expires_at)
VALUES ('instance-1', NOW(), NOW() + INTERVAL '30 seconds')
ON CONFLICT (name) DO UPDATE
SET leader_id = 'instance-1',
    elected_at = NOW(),
    expires_at = NOW() + INTERVAL '30 seconds'
WHERE agentpg_leader.expires_at < NOW();
```

**Leader responsibilities:**
- Clean up stale instances (no heartbeat for > 2 minutes)
- Run stuck run rescuer
- Execute periodic maintenance

### Instance Health

```go
// Heartbeat mechanism
// Every 15 seconds: UPDATE instances SET last_heartbeat_at = NOW()

// Cleanup (leader only)
// Every 1 minute: DELETE instances WHERE last_heartbeat_at < NOW() - '2 minutes'
// Trigger: agentpg_cleanup_orphaned_work marks orphaned runs/tools as failed
```

---

## State Machines

### Run State Flow (Batch API)

```
                    ┌─────────────────────────────────────────────────┐
                    │                                                 │
                    ▼                                                 │
┌─────────┐   ┌─────────────────┐   ┌─────────────────┐   ┌───────────────────┐
│ pending │──▶│ batch_submitting│──▶│  batch_pending  │──▶│ batch_processing  │
└─────────┘   └─────────────────┘   └─────────────────┘   └─────────┬─────────┘
     ▲              (worker claims)       (batch created)           │
     │                                                              │
     │         ┌────────────────────────────────────────────────────┤
     │         │                                                    │
     │         ▼                                                    ▼
     │   ┌───────────────┐                              ┌───────────────────┐
     └───│ pending_tools │                              │     completed     │
         │  (tool_use)   │                              │    (end_turn)     │
         └───────────────┘                              └───────────────────┘
                 │
                 │ (tools complete)
                 │
                 └──────────▶ (next iteration)
```

### Run State Flow (Streaming API)

```
┌─────────┐   ┌───────────┐   ┌───────────────┐   ┌───────────────────┐
│ pending │──▶│ streaming │──▶│ pending_tools │──▶│     completed     │
└─────────┘   └───────────┘   └───────┬───────┘   └───────────────────┘
                                      │
                                      │ (tools complete)
                                      │
     ┌────────────────────────────────┘
     │
     ▼
┌─────────┐
│ pending │ (next iteration)
└─────────┘
```

### Tool Execution State Flow

```
┌─────────┐   ┌─────────┐   ┌───────────────────────────────┐
│ pending │──▶│ running │──▶│ completed │ failed │ skipped │
└─────────┘   └─────────┘   └───────────────────────────────┘
                   │
                   │ (on error)
                   │
                   ▼
              ┌─────────┐
              │ pending │ (retry with scheduled_at)
              └─────────┘
```

---

## Event System

### LISTEN/NOTIFY Channels

| Channel | Trigger | Payload |
|---------|---------|---------|
| `agentpg_run_created` | New pending run | `{run_id, session_id, agent_name, run_mode, parent_run_id, depth}` |
| `agentpg_run_state` | Run state change | `{run_id, session_id, agent_name, state, previous_state, parent_run_id}` |
| `agentpg_run_finalized` | Run completed/failed/cancelled | `{run_id, session_id, state, parent_run_id, parent_tool_execution_id}` |
| `agentpg_tool_pending` | New tool execution | `{execution_id, run_id, tool_name, is_agent_tool, agent_name}` |
| `agentpg_tools_complete` | All tools for run done | `{run_id}` |

### Database Triggers

```sql
-- Notify on run creation
CREATE TRIGGER agentpg_run_created_trigger
AFTER INSERT ON agentpg_runs
FOR EACH ROW
WHEN (NEW.state = 'pending')
EXECUTE FUNCTION agentpg_notify_run_created();

-- Notify on state change
CREATE TRIGGER agentpg_run_state_trigger
AFTER UPDATE OF state ON agentpg_runs
FOR EACH ROW
WHEN (OLD.state IS DISTINCT FROM NEW.state)
EXECUTE FUNCTION agentpg_notify_run_state_change();

-- Handle child run completion (agent-as-tool)
CREATE TRIGGER agentpg_child_run_complete_trigger
AFTER UPDATE OF state ON agentpg_runs
FOR EACH ROW
WHEN (NEW.parent_tool_execution_id IS NOT NULL
      AND NEW.state IN ('completed', 'cancelled', 'failed'))
EXECUTE FUNCTION agentpg_handle_child_run_complete();
```

### Polling Fallback

When LISTEN/NOTIFY is unavailable, workers fall back to polling:
- runWorker/streamingWorker: every 1 second
- toolWorker: every 500ms
- batchPoller: every 30 seconds

---

## Retry and Rescue

### Tool Retry Configuration

```go
type ToolRetryConfig struct {
    MaxAttempts int     // Default: 2 (1 retry)
    Jitter      float64 // Default: 0.0 (instant retry)
}
```

**Default behavior: Instant retries** (2 attempts, no delay) for fast user feedback.

**Opt-in exponential backoff** (set `Jitter > 0`):

| Attempt | Delay (Jitter=0.1) |
|---------|---------------------|
| 1 | 1 second |
| 2 | 16 seconds |
| 3 | 81 seconds |
| 4 | 256 seconds |
| 5 | 625 seconds |

### Special Error Types

```go
// Cancel immediately - no retry, permanent failure
return tool.ToolCancel(errors.New("invalid input"))

// Discard permanently - similar to cancel
return tool.ToolDiscard(errors.New("unauthorized"))

// Snooze - retry after duration, does NOT consume an attempt
return tool.ToolSnooze(30*time.Second, errors.New("rate limited"))

// Regular error - retries with backoff until MaxAttempts
return fmt.Errorf("temporary failure: %w", err)
```

### Stuck Run Rescue

The rescuer worker (leader-only) handles runs stuck in non-terminal states:

```go
// Runs every RescueInterval (default: 1 minute)
// Finds runs:
// - In non-terminal state (batch_*, streaming, pending_tools)
// - claimed_at > StuckRunTimeout (default: 5 minutes)
// - rescue_attempts < MaxRescueAttempts (default: 3)
// - No pending/running tool executions

// Action:
if run.RescueAttempts >= MaxRescueAttempts {
    // Mark as failed with error_type='rescue_failed'
} else {
    // Reset to 'pending' state for reprocessing
}
```

### Orphaned Work Cleanup

When an instance dies without graceful shutdown:

```sql
-- Trigger: agentpg_cleanup_orphaned_work
-- On DELETE from agentpg_instances:
-- 1. Mark claimed runs as failed
-- 2. Mark running tool executions as failed (may be retried)
```

---

## Context Compaction

Long conversations can exceed Claude's 200K token context window. AgentPG provides automatic and manual compaction.

### Compaction Strategies

| Strategy | Description |
|----------|-------------|
| **Hybrid** (default) | Phase 1: Prune tool outputs (free). Phase 2: Summarize if still over target. |
| **Summarization** | Directly summarize all compactable messages using Claude. |

### Message Partitioning

Messages are categorized into mutually exclusive groups:

| Category | Compactable | Description |
|----------|-------------|-------------|
| Protected | No | Within last `ProtectedTokens` (40K default) |
| Preserved | No | Marked `is_preserved=true` |
| Recent | No | Last `PreserveLastN` messages (10 default) |
| Summaries | No | Previous compaction summaries |
| Compactable | Yes | Everything else |

### Configuration

```go
CompactionConfig: &compaction.Config{
    Strategy:            compaction.StrategyHybrid,
    Trigger:             0.85,      // 85% context usage threshold
    TargetTokens:        80000,     // Target after compaction
    PreserveLastN:       10,        // Keep last 10 messages
    ProtectedTokens:     40000,     // Never touch last 40K tokens
    MaxTokensForModel:   200000,    // Claude's context window
    SummarizerModel:     "claude-3-5-haiku-20241022",
    PreserveToolOutputs: false,     // Prune tool outputs in hybrid mode
    UseTokenCountingAPI: true,      // Use Claude's token counting API
}
```

### Usage

```go
// Auto-compaction (after each run completes)
client, _ := agentpg.NewClient(drv, &agentpg.ClientConfig{
    AutoCompactionEnabled: true,
    CompactionConfig:      &compaction.Config{...},
})

// Manual compaction
result, err := client.Compact(ctx, sessionID)
result, err := client.CompactIfNeeded(ctx, sessionID)

// Check statistics
stats, err := client.GetCompactionStats(ctx, sessionID)
// stats.UsagePercent, stats.TotalTokens, stats.CompactableMessages
```

---

## Atomic Operations

Critical operations use stored procedures for atomicity:

### CreateToolExecutionsAndUpdateRunState

Creates tool executions and updates run state in a single transaction:

```sql
-- Prevents partial state on crash:
-- If crash between tool creation and state update,
-- run stays in pending_tools with tools created → rescuer handles
```

### CompleteToolsAndContinueRun

Records tool results and restarts run in a single transaction:

```sql
-- Creates tool_result message
-- Transitions run to 'pending' for next iteration
-- Both succeed or both rollback
```

---

## Performance Characteristics

### Throughput

- **Tool execution**: 50 concurrent (default) = ~5000 tools/sec if 10ms each
- **Run processing**: 10 batch + 5 streaming = parallelized by nature
- **No single bottleneck**: Distributed via SQL claiming with SKIP LOCKED

### Latency

- **Batch API**: Up to 30s polling delay + Claude's 24h window
- **Streaming API**: Real-time (connection-based latency only)
- **Tool execution**: Parallel (all tools execute simultaneously per iteration)

### Scalability

- **Horizontal**: Add more client instances sharing the same database
- **Automatic work distribution**: SKIP LOCKED ensures fair distribution
- **No queue management**: PostgreSQL handles work distribution natively

### Reliability

- **Stuck run rescue**: 3 attempts before permanent failure
- **Tool retry**: 2 attempts by default (instant or exponential backoff)
- **Heartbeat detection**: 2-minute timeout before cleanup
- **Notification fallback**: Polling guarantees work is never lost

---

## Key Files Reference

| File | Lines | Purpose |
|------|-------|---------|
| `client.go` | ~1,300 | Main client, lifecycle, coordination |
| `run_worker.go` | ~360 | Batch API run processing |
| `streaming_worker.go` | ~570 | Streaming API run processing |
| `tool_worker.go` | ~380 | Tool execution with retry |
| `batch_poller.go` | ~500 | Batch API status polling |
| `rescuer.go` | ~110 | Stuck run recovery |
| `driver/driver.go` | ~400 | Driver and Store interfaces |
| `storage/migrations/*.sql` | ~1,800 | Database schema |
| `compaction/*.go` | ~800 | Context compaction |
| `ui/*.go` | ~1,500 | Admin UI |
