# Distributed Deployment

This document covers AgentPG's multi-instance deployment capabilities.

## Overview

AgentPG provides a `Client` type that manages the lifecycle of distributed agent instances. This enables:

- **High Availability** - Multiple instances can serve requests
- **Automatic Cleanup** - Stale instances and stuck runs are cleaned up
- **Leader Election** - Coordinated cleanup via single leader
- **Real-Time Events** - PostgreSQL LISTEN/NOTIFY for instant event propagation

## Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              PostgreSQL                                      │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐  ┌───────────────────┐  │
│  │  instances  │  │   leader    │  │    runs     │  │  agents/tools     │  │
│  └─────────────┘  └─────────────┘  └─────────────┘  └───────────────────┘  │
│         ▲               ▲               ▲                    ▲              │
└─────────┼───────────────┼───────────────┼────────────────────┼──────────────┘
          │               │               │                    │
          │    LISTEN/NOTIFY              │                    │
          │               │               │                    │
┌─────────┴───────────────┴───────────────┴────────────────────┴──────────────┐
│                                                                              │
│   ┌─────────────────────────┐        ┌─────────────────────────┐            │
│   │     Instance 1          │        │     Instance 2          │            │
│   │     (Leader)            │        │     (Follower)          │            │
│   │  ┌─────────────────┐   │        │  ┌─────────────────┐   │            │
│   │  │   Heartbeat     │   │        │  │   Heartbeat     │   │            │
│   │  │   (30s)         │   │        │  │   (30s)         │   │            │
│   │  └─────────────────┘   │        │  └─────────────────┘   │            │
│   │  ┌─────────────────┐   │        │  ┌─────────────────┐   │            │
│   │  │  Leader Elector │   │        │  │  Leader Elector │   │            │
│   │  │  (owns lease)   │   │        │  │  (attempts)     │   │            │
│   │  └─────────────────┘   │        │  └─────────────────┘   │            │
│   │  ┌─────────────────┐   │        │                        │            │
│   │  │  Cleanup Svc    │   │        │  (cleanup only runs    │            │
│   │  │  (1m interval)  │   │        │   on leader)           │            │
│   │  └─────────────────┘   │        │                        │            │
│   │  ┌─────────────────┐   │        │  ┌─────────────────┐   │            │
│   │  │   Notifier      │   │        │  │   Notifier      │   │            │
│   │  │  (real-time)    │   │        │  │  (real-time)    │   │            │
│   │  └─────────────────┘   │        │  └─────────────────┘   │            │
│   └─────────────────────────┘        └─────────────────────────┘            │
│                                                                              │
└──────────────────────────────────────────────────────────────────────────────┘
```

## Quick Start

```go
package main

import (
    "context"
    "log"
    "os"
    "os/signal"
    "time"

    "github.com/jackc/pgx/v5/pgxpool"
    "github.com/youssefsiam38/agentpg"
    "github.com/youssefsiam38/agentpg/driver/pgxv5"
)

// Register agents globally (typically in init())
func init() {
    agentpg.Register(&agentpg.AgentDefinition{
        Name:         "chat",
        Description:  "General purpose chat agent",
        Model:        "claude-sonnet-4-5-20250929",
        SystemPrompt: "You are a helpful assistant.",
    })

    agentpg.RegisterTool(&MyTool{})
}

func main() {
    ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
    defer cancel()

    pool, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
    if err != nil {
        log.Fatal(err)
    }
    defer pool.Close()

    drv := pgxv5.New(pool)

    client, err := agentpg.NewClient(drv, &agentpg.ClientConfig{
        APIKey: os.Getenv("ANTHROPIC_API_KEY"),
    })
    if err != nil {
        log.Fatal(err)
    }

    if err := client.Start(ctx); err != nil {
        log.Fatal(err)
    }
    defer client.Stop(context.Background())

    // Get agent handle
    agent := client.Agent("chat")
    if agent == nil {
        log.Fatal("Agent 'chat' not registered")
    }

    // Create session and run
    sessionID, _ := agent.NewSession(ctx, "tenant1", "user123", nil, nil)
    response, err := agent.Run(ctx, sessionID, "Hello!")
    if err != nil {
        log.Fatal(err)
    }

    log.Printf("Response: %v", response)

    // Wait for shutdown signal
    <-ctx.Done()
}
```

## Configuration

### ClientConfig

```go
type ClientConfig struct {
    // APIKey is the Anthropic API key (required if Client is not provided)
    APIKey string

    // Client is an existing Anthropic client (optional, takes precedence over APIKey)
    Client *anthropic.Client

    // InstanceID is a unique identifier for this client instance (optional)
    // If not provided, a UUID will be generated
    InstanceID string

    // Hostname is the hostname for this instance (optional)
    // If not provided, os.Hostname() is used
    Hostname string

    // Metadata is additional metadata to store with this instance
    Metadata map[string]any

    // HeartbeatInterval is how often to send heartbeats (optional)
    // Default: 30 seconds
    HeartbeatInterval time.Duration

    // CleanupInterval is how often to run cleanup operations when leader (optional)
    // Default: 1 minute
    CleanupInterval time.Duration

    // StuckRunTimeout is how long a run can be in "running" state before
    // it's considered stuck and will be marked as failed (optional)
    // Default: 1 hour
    StuckRunTimeout time.Duration

    // LeaderTTL is how long a leader's lease is valid (optional)
    // Default: 30 seconds
    LeaderTTL time.Duration

    // OnError is called when background operations fail
    OnError func(err error)

    // OnBecameLeader is called when this instance becomes the leader
    OnBecameLeader func()

    // OnLostLeadership is called when this instance loses leadership
    OnLostLeadership func()
}
```

### Recommended Production Settings

```go
client, err := agentpg.NewClient(drv, &agentpg.ClientConfig{
    APIKey:            os.Getenv("ANTHROPIC_API_KEY"),
    HeartbeatInterval: 30 * time.Second,   // Default
    CleanupInterval:   1 * time.Minute,    // Default
    StuckRunTimeout:   1 * time.Hour,      // Default
    LeaderTTL:         30 * time.Second,   // Default
    OnError: func(err error) {
        // Log to your observability platform
        log.Printf("AgentPG background error: %v", err)
    },
    OnBecameLeader: func() {
        // Update metrics, enable cleanup dashboards
        log.Println("This instance is now the leader")
    },
    OnLostLeadership: func() {
        // Update metrics
        log.Println("This instance lost leadership")
    },
})
```

## Components

### Instance Registration

When `client.Start()` is called:

1. Instance is registered in `agentpg_instances` table
2. Registered agents are linked in `agentpg_instance_agents`
3. Registered tools are linked in `agentpg_instance_tools`
4. Heartbeat service starts

When `client.Stop()` is called:

1. Instance is deregistered from `agentpg_instances`
2. All linked agents/tools are automatically cleaned up (CASCADE)

### Heartbeat Service

The heartbeat service periodically updates `last_heartbeat_at` in the instances table:

- **Default interval**: 30 seconds
- **Instance TTL**: 2 minutes (instances not heartbeating for 2 minutes are considered stale)

```go
// Heartbeat runs automatically, but you can configure the interval
client, _ := agentpg.NewClient(drv, &agentpg.ClientConfig{
    HeartbeatInterval: 15 * time.Second, // More aggressive heartbeat
})
```

### Leader Election

Leader election uses a TTL-based lease stored in PostgreSQL:

1. Only one instance can be leader at a time
2. Leader must renew lease before it expires
3. If leader fails to renew, another instance can take over

```go
client, _ := agentpg.NewClient(drv, &agentpg.ClientConfig{
    LeaderTTL: 30 * time.Second, // How long the lease is valid
    OnBecameLeader: func() {
        // Called when this instance becomes leader
        // The cleanup service is automatically started
    },
    OnLostLeadership: func() {
        // Called when this instance loses leadership
        // The cleanup service is automatically stopped
    },
})
```

### Cleanup Service (Leader Only)

The cleanup service runs only on the leader instance and performs:

1. **Stale Instance Cleanup**
   - Finds instances with `last_heartbeat_at` older than 2 minutes
   - Deregisters them (triggering CASCADE delete of agent/tool links)

2. **Stuck Run Cleanup**
   - Finds runs in "running" state longer than `StuckRunTimeout`
   - Marks them as "failed" with error message "run timed out"

3. **Expired Leader Cleanup**
   - Removes expired leader entries from the `agentpg_leader` table

```go
client, _ := agentpg.NewClient(drv, &agentpg.ClientConfig{
    CleanupInterval: 30 * time.Second, // Run cleanup every 30s
    StuckRunTimeout: 30 * time.Minute, // Mark runs stuck after 30m
})
```

## Run State Machine

Runs track individual agent executions with a state machine:

```
         ┌──────────────────────────────────────┐
         │                                      │
         ▼                                      │
    ┌─────────┐     success      ┌───────────┐  │
    │ running │ ───────────────► │ completed │  │
    └─────────┘                  └───────────┘  │
         │                                      │
         │ user cancel                          │
         │ ─────────────────► ┌───────────────┐ │
         │                    │  cancelled    │ │
         │                    └───────────────┘ │
         │                                      │
         │ error/timeout                        │
         └────────────────────► ┌────────────┐  │
                               │   failed    │  │
                               └────────────┘  │
                                     │          │
                                     └──────────┘
                              (terminal states)
```

### Run Tracking

```go
// Runs are created automatically when using agent.Run()
response, err := agent.Run(ctx, sessionID, "Hello!")

// Access run information from response
run := response.Run
fmt.Printf("Run ID: %s\n", run.ID)
fmt.Printf("State: %s\n", run.State)  // "completed" or "failed"
fmt.Printf("Input Tokens: %d\n", run.InputTokens)
fmt.Printf("Output Tokens: %d\n", run.OutputTokens)
```

## Real-Time Events

AgentPG uses PostgreSQL LISTEN/NOTIFY for real-time event propagation:

### Available Events

| Event Type | Channel | Description |
|------------|---------|-------------|
| `run_state_changed` | `agentpg_run_state_changed` | Run state transitioned |
| `instance_registered` | `agentpg_instance_registered` | New instance registered |
| `instance_deregistered` | `agentpg_instance_deregistered` | Instance deregistered |
| `leader_changed` | `agentpg_leader_changed` | Leader changed |

### Subscribing to Events

```go
// The notifier is available after client.Start()
notifier := client.Notifier()

// Subscribe to run state changes
unsubscribe := notifier.Subscribe(notifier.EventRunStateChanged, func(event *notifier.Event) {
    fmt.Printf("Run %s changed state\n", event.Payload)
})
defer unsubscribe()
```

**Note**: Event subscription is only available with pgx/v5 driver (LISTEN support). The database/sql driver can send events but cannot receive them.

## Database Schema

### Distributed Tables

```sql
-- Instance registration
CREATE TABLE agentpg_instances (
    id VARCHAR(255) PRIMARY KEY,
    hostname VARCHAR(255),
    pid INTEGER,
    version VARCHAR(50),
    metadata JSONB DEFAULT '{}',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    last_heartbeat_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Leader election
CREATE TABLE agentpg_leader (
    name VARCHAR(255) PRIMARY KEY DEFAULT 'cleanup',
    leader_id VARCHAR(255) NOT NULL,
    elected_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    expires_at TIMESTAMP WITH TIME ZONE NOT NULL
);

-- Run tracking
CREATE TABLE agentpg_runs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id UUID NOT NULL REFERENCES agentpg_sessions(id),
    state VARCHAR(20) NOT NULL DEFAULT 'running',
    agent_name VARCHAR(255) NOT NULL,
    prompt TEXT NOT NULL,
    response_text TEXT,
    stop_reason VARCHAR(50),
    input_tokens INTEGER DEFAULT 0,
    output_tokens INTEGER DEFAULT 0,
    tool_iterations INTEGER DEFAULT 0,
    error_message TEXT,
    error_type VARCHAR(50),
    instance_id VARCHAR(255),
    metadata JSONB DEFAULT '{}',
    started_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    finalized_at TIMESTAMP WITH TIME ZONE
);

-- Agent definitions
CREATE TABLE agentpg_agents (
    name VARCHAR(255) PRIMARY KEY,
    description TEXT,
    model VARCHAR(255) NOT NULL,
    system_prompt TEXT,
    max_tokens INTEGER,
    temperature REAL,
    config JSONB DEFAULT '{}',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Tool definitions
CREATE TABLE agentpg_tools (
    name VARCHAR(255) PRIMARY KEY,
    description TEXT NOT NULL,
    input_schema JSONB NOT NULL,
    metadata JSONB DEFAULT '{}',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Instance-Agent links
CREATE TABLE agentpg_instance_agents (
    instance_id VARCHAR(255) REFERENCES agentpg_instances(id) ON DELETE CASCADE,
    agent_name VARCHAR(255) REFERENCES agentpg_agents(name) ON DELETE CASCADE,
    registered_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    PRIMARY KEY (instance_id, agent_name)
);

-- Instance-Tool links
CREATE TABLE agentpg_instance_tools (
    instance_id VARCHAR(255) REFERENCES agentpg_instances(id) ON DELETE CASCADE,
    tool_name VARCHAR(255) REFERENCES agentpg_tools(name) ON DELETE CASCADE,
    registered_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    PRIMARY KEY (instance_id, tool_name)
);
```

## Deployment Patterns

### Single Instance

For simple deployments, use `Client` with default settings:

```go
client, _ := agentpg.NewClient(drv, &agentpg.ClientConfig{
    APIKey: apiKey,
})
client.Start(ctx)
defer client.Stop(ctx)
```

### Multiple Instances (Kubernetes)

For Kubernetes deployments with multiple replicas:

```go
client, _ := agentpg.NewClient(drv, &agentpg.ClientConfig{
    APIKey:   apiKey,
    Hostname: os.Getenv("HOSTNAME"), // Pod name
    Metadata: map[string]any{
        "namespace": os.Getenv("POD_NAMESPACE"),
        "node":      os.Getenv("NODE_NAME"),
    },
    OnError: func(err error) {
        // Send to your logging/alerting system
        slog.Error("agentpg error", "error", err)
    },
})
```

### Health Check Endpoint

```go
func healthHandler(w http.ResponseWriter, r *http.Request) {
    status := map[string]any{
        "instance_id": client.InstanceID(),
        "is_running":  client.IsRunning(),
        "is_leader":   client.IsLeader(),
    }
    json.NewEncoder(w).Encode(status)
}
```

## Troubleshooting

### Instance Not Becoming Leader

1. Check if another instance holds the lease:
   ```sql
   SELECT * FROM agentpg_leader WHERE name = 'cleanup';
   ```

2. Check if lease is expired:
   ```sql
   SELECT *, expires_at < NOW() AS is_expired
   FROM agentpg_leader WHERE name = 'cleanup';
   ```

3. Manually expire for testing:
   ```sql
   DELETE FROM agentpg_leader WHERE name = 'cleanup';
   ```

### Stuck Runs Not Being Cleaned

1. Verify the cleanup service is running (check logs for "became leader")
2. Check `StuckRunTimeout` configuration
3. Manually check stuck runs:
   ```sql
   SELECT * FROM agentpg_runs
   WHERE state = 'running'
   AND started_at < NOW() - INTERVAL '1 hour';
   ```

### Instances Not Being Cleaned Up

1. Check heartbeat interval configuration
2. Verify stale instances:
   ```sql
   SELECT * FROM agentpg_instances
   WHERE last_heartbeat_at < NOW() - INTERVAL '2 minutes';
   ```

## Related Documentation

- [Architecture](./architecture.md) - System design
- [Deployment](./deployment.md) - Production setup
- [Storage](./storage.md) - Database schema details
