# Distributed Workers

AgentPG provides a fully distributed, event-driven worker architecture where multiple client instances can process work from a single PostgreSQL database.

## Table of Contents

- [Overview](#overview)
- [Instance Registration](#instance-registration)
- [Work Claiming](#work-claiming)
- [Worker Types](#worker-types)
- [LISTEN/NOTIFY Events](#listennotify-events)
- [Leader Election](#leader-election)
- [Run Rescue System](#run-rescue-system)
- [Configuration](#configuration)

---

## Overview

### Key Principles

1. **PostgreSQL as Single Source of Truth**: All state stored in PostgreSQL. No in-memory state that could be lost.
2. **Event-Driven with Polling Fallback**: Uses LISTEN/NOTIFY for real-time events, with polling as fallback.
3. **Race-Safe Distribution**: Uses `SELECT FOR UPDATE SKIP LOCKED` for safe work claiming.
4. **Capability-Based Routing**: Instances only claim work they can handle.
5. **Leader Election**: Single leader handles maintenance tasks.

### Architecture

```
┌─────────────────────┐     ┌─────────────────────┐     ┌─────────────────────┐
│     Worker Pod 1    │     │     Worker Pod 2    │     │     Worker Pod N    │
│  Agents: assistant  │     │  Agents: code-agent │     │  Agents: all        │
│  Tools: weather     │     │  Tools: lint, test  │     │  Tools: all         │
└──────────┬──────────┘     └──────────┬──────────┘     └──────────┬──────────┘
           │                           │                           │
           └───────────────────────────┼───────────────────────────┘
                                       │
                                       ▼
                         ┌─────────────────────────────┐
                         │    PostgreSQL Database      │
                         │  ┌───────────────────────┐  │
                         │  │  Work Queues (Tables) │  │
                         │  │  - agentpg_runs       │  │
                         │  │  - agentpg_tool_exec  │  │
                         │  └───────────────────────┘  │
                         │  ┌───────────────────────┐  │
                         │  │   LISTEN/NOTIFY       │  │
                         │  │  - run_created        │  │
                         │  │  - tool_pending       │  │
                         │  └───────────────────────┘  │
                         └─────────────────────────────┘
```

---

## Instance Registration

When a client starts (`client.Start()`), it registers itself in the database.

### Registration Process

1. **Register Instance**: Creates record in `agentpg_instances` (UNLOGGED table)
2. **Sync Capabilities**: Registers agents/tools in linking tables
3. **Start Workers**: Begins background processing loops

```go
client, _ := agentpg.NewClient(drv, &agentpg.ClientConfig{
    APIKey: apiKey,
    Name:   "worker-1",         // Logical name (can be shared)
    ID:     "pod-abc-123",      // Unique identifier
})

// Register capabilities
client.RegisterAgent(&agentpg.AgentDefinition{
    Name:  "assistant",
    Model: "claude-sonnet-4-5-20250929",
})
client.RegisterTool(&MyTool{})

// Start (registers instance and begins processing)
client.Start(ctx)
```

### Instance Tables

```sql
-- Instance record
agentpg_instances (UNLOGGED)
├── id                   -- Unique identifier
├── name                 -- Logical name
├── hostname, pid        -- System info
├── max_concurrent_runs  -- Capacity
├── max_concurrent_tools -- Capacity
├── active_run_count     -- Current load
├── active_tool_count    -- Current load
└── last_heartbeat_at    -- Liveness

-- Capability mappings
agentpg_instance_agents (UNLOGGED)
├── instance_id
└── agent_name

agentpg_instance_tools (UNLOGGED)
├── instance_id
└── tool_name
```

### Heartbeat Loop

Each instance sends heartbeats to prove liveness:

```go
// Default: every 15 seconds
HeartbeatInterval: 15 * time.Second

// If no heartbeat for InstanceTTL, instance is considered dead
InstanceTTL: 60 * time.Second
```

### Graceful Shutdown

When `client.Stop()` is called:
1. Releases leadership (if leader)
2. Unregisters instance
3. Database trigger marks orphaned work as failed

---

## Work Claiming

AgentPG uses `SELECT FOR UPDATE SKIP LOCKED` for race-safe work distribution.

### How SKIP LOCKED Works

```sql
-- Multiple workers can run this simultaneously
-- Each gets different rows, no blocking
WITH claimable AS (
    SELECT id FROM agentpg_runs
    WHERE state = 'pending'
      AND claimed_by_instance_id IS NULL
    ORDER BY created_at ASC
    LIMIT 5
    FOR UPDATE SKIP LOCKED  -- Magic happens here
)
UPDATE agentpg_runs
SET claimed_by_instance_id = 'my-instance-id',
    claimed_at = NOW(),
    state = 'batch_submitting'
FROM claimable
WHERE agentpg_runs.id = claimable.id
RETURNING *;
```

**Key behaviors:**
- `FOR UPDATE` locks rows atomically
- `SKIP LOCKED` skips rows locked by other transactions
- Multiple workers run in parallel without blocking
- Each worker gets different work items

### Capability-Based Routing

Instances only claim work they can handle:

```sql
-- Run claiming checks instance has agent capability
WHERE EXISTS (
    SELECT 1 FROM agentpg_instance_agents ia
    WHERE ia.instance_id = 'my-instance'
      AND ia.agent_name = r.agent_name
)

-- Tool claiming checks instance has tool capability
WHERE EXISTS (
    SELECT 1 FROM agentpg_instance_tools it
    WHERE it.instance_id = 'my-instance'
      AND it.tool_name = te.tool_name
)
```

### Specialized Workers

```go
// Code worker - only handles code-related work
codeWorker.RegisterAgent(&agentpg.AgentDefinition{Name: "code-assistant"})
codeWorker.RegisterTool(&LintTool{})
codeWorker.RegisterTool(&TestTool{})
// Only claims: runs for "code-assistant", tools "lint" and "test"

// General worker - handles everything else
generalWorker.RegisterAgent(&agentpg.AgentDefinition{Name: "assistant"})
generalWorker.RegisterTool(&WeatherTool{})
// Only claims: runs for "assistant", tool "get_weather"
```

---

## Worker Types

AgentPG runs multiple background worker goroutines.

### Run Worker (Batch API)

Processes runs using Claude Batch API (50% cost discount).

```
pending → batch_submitting → batch_pending → batch_processing
    ↓ (batch completes)
completed / pending_tools / failed
```

**Flow:**
1. Claims pending batch runs
2. Builds messages from session history
3. Submits to Claude Batch API
4. Creates iteration record with batch_id
5. Updates run state to `batch_pending`

### Streaming Worker

Processes runs using Claude Streaming API (real-time).

```
pending → streaming → completed / pending_tools / failed
```

**Flow:**
1. Claims pending streaming runs
2. Opens streaming connection to Claude
3. Accumulates response in real-time
4. Creates assistant message on completion

### Tool Worker

Executes tool calls from agent responses.

**Flow:**
1. Claims pending tool executions
2. For regular tools: calls `tool.Execute()`
3. For agent tools: creates child run and waits
4. Marks execution as completed/failed
5. When all tools done: creates tool_result message

### Batch Poller

Polls Claude Batch API for completion status.

**Flow:**
1. Finds iterations with `batch_status = 'in_progress'`
2. Calls Claude API to check status
3. On completion: fetches results, processes response
4. Creates assistant message with content blocks
5. Creates tool executions if tool_use blocks present

### Rescuer (Leader Only)

Recovers stuck runs (runs on leader instance only).

**Flow:**
1. Finds runs stuck in non-terminal states
2. Resets them to `pending` for reprocessing
3. After max attempts: marks as `failed`

---

## LISTEN/NOTIFY Events

PostgreSQL NOTIFY channels provide real-time event signaling.

### Channels

| Channel | Trigger | Payload |
|---------|---------|---------|
| `agentpg_run_created` | New pending run | `{run_id, session_id, agent_name, run_mode, depth}` |
| `agentpg_run_state` | Run state change | `{run_id, session_id, agent_name, state, previous_state}` |
| `agentpg_run_finalized` | Run completed/failed | `{run_id, session_id, state, parent_tool_execution_id}` |
| `agentpg_tool_pending` | New tool execution | `{execution_id, run_id, tool_name, is_agent_tool}` |
| `agentpg_tools_complete` | All tools done | `{run_id}` |

### Event-Driven Flow

```
User creates run
       ↓
pg_notify('agentpg_run_created')
       ↓
Workers receive notification
       ↓
Matching worker claims run immediately
       ↓
(No polling delay!)
```

### Fallback Polling

If LISTEN/NOTIFY fails (connection issues), workers fall back to polling:

```go
RunPollInterval:  1 * time.Second      // Run claiming
ToolPollInterval: 500 * time.Millisecond  // Tool claiming
BatchPollInterval: 30 * time.Second    // Batch status
```

### External Event Consumers

External systems can listen to events:

```go
// External monitoring service
conn.Exec(ctx, "LISTEN agentpg_run_finalized")

for {
    notif, _ := conn.WaitForNotification(ctx)
    // Handle run completion externally
    handleRunCompleted(notif.Payload)
}
```

---

## Leader Election

One instance is elected leader for maintenance tasks.

### Leader Table

```sql
agentpg_leader (UNLOGGED)
├── name       -- 'default' (single row)
├── leader_id  -- Instance ID
├── elected_at -- When elected
└── expires_at -- Lease expiration
```

### Election Mechanism

```sql
-- Attempt to become leader
INSERT INTO agentpg_leader (leader_id, elected_at, expires_at)
VALUES ('instance-1', NOW(), NOW() + INTERVAL '30 seconds')
ON CONFLICT (name) DO UPDATE
SET leader_id = 'instance-1',
    elected_at = NOW(),
    expires_at = NOW() + INTERVAL '30 seconds'
WHERE agentpg_leader.expires_at < NOW();  -- Only if expired

-- Refresh lease (must be current leader)
UPDATE agentpg_leader
SET expires_at = NOW() + INTERVAL '30 seconds'
WHERE leader_id = 'instance-1';
```

### Leader Responsibilities

Only the leader runs:

1. **Stale Instance Cleanup**
   - Finds instances with expired heartbeats
   - Deletes them (triggers orphan cleanup)
   - Marks their work as failed

2. **Stuck Run Rescue**
   - Finds runs stuck longer than timeout
   - Resets them for reprocessing
   - Marks as failed after max attempts

### Configuration

```go
LeaderTTL: 30 * time.Second  // Lease duration
// Refresh happens at LeaderTTL/2 (every 15 seconds)
```

---

## Run Rescue System

Ensures runs never stay stuck permanently.

### Stuck Run Detection

A run is considered stuck if:
- In non-terminal state (`batch_submitting`, `batch_pending`, `batch_processing`, `streaming`, `pending_tools`)
- Claimed more than `RescueTimeout` ago (default: 5 minutes)
- Has no pending/running tool executions

### Rescue Process

```
Stuck run detected
       ↓
rescue_attempts < MaxRescueAttempts?
       ↓
Yes: Reset to 'pending', increment rescue_attempts
No:  Mark as 'failed' with error_type='rescue_failed'
```

### Configuration

```go
RunRescueConfig: &agentpg.RunRescueConfig{
    RescueInterval:    time.Minute,      // Check frequency
    RescueTimeout:     5 * time.Minute,  // Stuck threshold
    MaxRescueAttempts: 3,                // Max retries
}
```

### Tool Retry

Tool executions have their own retry mechanism:

```go
ToolRetryConfig: &agentpg.ToolRetryConfig{
    MaxAttempts: 2,    // 1 initial + 1 retry
    Jitter:      0.0,  // 0 = instant retry (default)
}
```

**Error handling options:**
- Regular error: retries up to MaxAttempts
- `tool.ToolCancel(err)`: permanent failure, no retry
- `tool.ToolDiscard(err)`: permanent failure, invalid input
- `tool.ToolSnooze(duration, err)`: retry after delay, doesn't consume attempt

---

## Configuration

### Distributed Worker Settings

```go
type ClientConfig struct {
    // Instance identification
    Name string  // Logical name (can be shared across pods)
    ID   string  // Unique identifier (must be unique per pod)

    // Concurrency limits
    MaxConcurrentRuns          int  // Batch runs (default: 10)
    MaxConcurrentStreamingRuns int  // Streaming runs (default: 5)
    MaxConcurrentTools         int  // Tool executions (default: 50)

    // Polling intervals (fallback when LISTEN/NOTIFY unavailable)
    RunPollInterval   time.Duration  // Run claiming (default: 1s)
    ToolPollInterval  time.Duration  // Tool claiming (default: 500ms)
    BatchPollInterval time.Duration  // Batch status (default: 30s)

    // Instance health
    HeartbeatInterval time.Duration  // Liveness (default: 15s)
    InstanceTTL       time.Duration  // Stale threshold (default: 60s)
    CleanupInterval   time.Duration  // Cleanup frequency (default: 1min)

    // Leader election
    LeaderTTL time.Duration  // Lease duration (default: 30s)

    // Run rescue
    RunRescueConfig *RunRescueConfig

    // Tool retry
    ToolRetryConfig *ToolRetryConfig
}
```

### Scaling Recommendations

**Horizontal Scaling:**
```go
// Add more worker pods with same configuration
// Work automatically distributes via SKIP LOCKED
client, _ := agentpg.NewClient(drv, &agentpg.ClientConfig{
    Name: "worker",              // Same name
    ID:   os.Getenv("POD_NAME"), // Unique per pod
})
```

**Specialized Workers:**
```go
// High-priority worker (faster polling)
highPriority, _ := agentpg.NewClient(drv, &agentpg.ClientConfig{
    RunPollInterval:  100 * time.Millisecond,
    ToolPollInterval: 50 * time.Millisecond,
})

// Cost-optimized worker (less aggressive)
costOptimized, _ := agentpg.NewClient(drv, &agentpg.ClientConfig{
    RunPollInterval:  5 * time.Second,
    ToolPollInterval: 1 * time.Second,
})
```

### Database Considerations

**Connection Pool Sizing:**
```go
// Each worker needs connections for:
// - Main queries
// - LISTEN/NOTIFY listener
// - Concurrent tool executions

// Recommended: max_connections >= workers * (2 + MaxConcurrentTools/10)
```

**UNLOGGED Tables:**
Instance tables use UNLOGGED for performance:
- `agentpg_instances`
- `agentpg_instance_agents`
- `agentpg_instance_tools`
- `agentpg_leader`

These are regenerated when workers restart, so WAL durability is not needed.
