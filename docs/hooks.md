# Hooks and Extension System

AgentPG provides multiple extension mechanisms for customizing and extending framework behavior.

## Table of Contents

- [Extension Interfaces](#extension-interfaces)
- [Event System (LISTEN/NOTIFY)](#event-system-listennotify)
- [Metadata Fields](#metadata-fields)
- [Tool Error Handling](#tool-error-handling)
- [Configuration Hooks](#configuration-hooks)
- [Future: Lifecycle Hooks](#future-lifecycle-hooks)

---

## Extension Interfaces

AgentPG uses interface-based extension for core components.

### Tool Interface

The primary extension mechanism for custom functionality.

```go
type Tool interface {
    Name() string
    Description() string
    InputSchema() tool.ToolSchema
    Execute(ctx context.Context, input json.RawMessage) (string, error)
}
```

**Example:**

```go
type WeatherTool struct {
    apiKey string
}

func (t *WeatherTool) Name() string        { return "get_weather" }
func (t *WeatherTool) Description() string { return "Get current weather for a city" }

func (t *WeatherTool) InputSchema() tool.ToolSchema {
    return tool.ToolSchema{
        Type: "object",
        Properties: map[string]tool.PropertyDef{
            "city": {Type: "string", Description: "City name"},
        },
        Required: []string{"city"},
    }
}

func (t *WeatherTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
    var params struct {
        City string `json:"city"`
    }
    json.Unmarshal(input, &params)

    // Call weather API
    weather, err := t.fetchWeather(params.City)
    if err != nil {
        return "", err  // Error shown to Claude as tool_result with is_error=true
    }
    return weather, nil
}

// Register
client.RegisterTool(&WeatherTool{apiKey: os.Getenv("WEATHER_API_KEY")})
```

### Logger Interface

Custom logging implementations.

```go
type Logger interface {
    Debug(msg string, args ...any)
    Info(msg string, args ...any)
    Warn(msg string, args ...any)
    Error(msg string, args ...any)
}
```

**Example with slog:**

```go
type SlogLogger struct {
    logger *slog.Logger
}

func (l *SlogLogger) Debug(msg string, args ...any) { l.logger.Debug(msg, args...) }
func (l *SlogLogger) Info(msg string, args ...any)  { l.logger.Info(msg, args...) }
func (l *SlogLogger) Warn(msg string, args ...any)  { l.logger.Warn(msg, args...) }
func (l *SlogLogger) Error(msg string, args ...any) { l.logger.Error(msg, args...) }

// Use
client, _ := agentpg.NewClient(drv, &agentpg.ClientConfig{
    Logger: &SlogLogger{logger: slog.Default()},
})
```

### Driver Interface

Custom database driver implementations.

```go
type Driver[TTx any] interface {
    Store() Store[TTx]
    Listener() Listener
    BeginTx(ctx context.Context) (TTx, error)
    CommitTx(ctx context.Context, tx TTx) error
    RollbackTx(ctx context.Context, tx TTx) error
    Close() error
}
```

**Built-in implementations:**
- `pgxv5.New(pool)` - pgx/v5 (recommended)
- `databasesql.New(db)` - database/sql

### Compaction Strategy

Custom context compaction strategies.

```go
type StrategyExecutor interface {
    Name() compaction.Strategy
    Execute(ctx context.Context, partition *MessagePartition) (*StrategyResult, error)
}
```

**Built-in strategies:**
- `StrategySummarization` - Claude-based summarization
- `StrategyHybrid` - Prune tool outputs first, then summarize

**Override with custom config:**

```go
result, err := client.CompactWithConfig(ctx, sessionID, &compaction.Config{
    Strategy:     compaction.StrategySummarization,
    TargetTokens: 50000,
})
```

---

## Event System (LISTEN/NOTIFY)

PostgreSQL NOTIFY channels fire events at lifecycle milestones.

### Available Channels

| Channel | Event | Payload |
|---------|-------|---------|
| `agentpg_run_created` | New run submitted | `{run_id, session_id, agent_name, run_mode, depth}` |
| `agentpg_run_state` | Run state changes | `{run_id, session_id, agent_name, state, previous_state}` |
| `agentpg_run_finalized` | Run completed/failed/cancelled | `{run_id, session_id, state, parent_tool_execution_id}` |
| `agentpg_tool_pending` | New tool execution pending | `{execution_id, run_id, tool_name, is_agent_tool}` |
| `agentpg_tools_complete` | All tools for run completed | `{run_id}` |

### External Event Consumer

Subscribe to events from external services:

```go
import "github.com/lib/pq"

// Create listener
listener := pq.NewListener(connStr, 10*time.Second, time.Minute, nil)
listener.Listen("agentpg_run_finalized")

// Process events
for {
    select {
    case notif := <-listener.Notify:
        var payload struct {
            RunID     string `json:"run_id"`
            SessionID string `json:"session_id"`
            State     string `json:"state"`
        }
        json.Unmarshal([]byte(notif.Extra), &payload)

        // Custom handling
        if payload.State == "completed" {
            sendWebhook(payload)
            updateExternalSystem(payload)
        }
    }
}
```

### Use Cases

- **Monitoring**: Track agent execution metrics
- **Webhooks**: Notify external systems on completion
- **Audit Logging**: Record all run state transitions
- **Custom Analytics**: Build usage dashboards

---

## Metadata Fields

Multiple entity types support arbitrary JSON metadata.

### Available Metadata Fields

```go
// Session metadata
session, _ := client.NewSession(ctx, "tenant", "user", nil, map[string]any{
    "request_id":   "req-123",
    "user_tier":    "premium",
    "source":       "web",
})

// Run metadata (via CreateRunParams)
type CreateRunParams struct {
    Metadata map[string]any
}

// Message metadata (via CreateMessageParams)
type CreateMessageParams struct {
    Metadata map[string]any
}

// Instance metadata (via RegisterInstanceParams)
type RegisterInstanceParams struct {
    Metadata map[string]any
}
```

### Query Metadata

```sql
-- Find premium user sessions
SELECT * FROM agentpg_sessions
WHERE metadata->>'user_tier' = 'premium';

-- Find runs with specific request ID
SELECT * FROM agentpg_runs r
JOIN agentpg_sessions s ON r.session_id = s.id
WHERE s.metadata->>'request_id' = 'req-123';
```

---

## Tool Error Handling

Tools can control retry behavior through error types.

### Error Types

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
        return "", err  // Retries up to MaxAttempts
    }

    return result, nil
}
```

### Error Behavior

| Error Type | Retries | Consumes Attempt | Use Case |
|------------|---------|------------------|----------|
| Regular error | Yes | Yes | Transient failures |
| `ToolCancel` | No | N/A | Invalid input, auth failure |
| `ToolDiscard` | No | N/A | Malformed data |
| `ToolSnooze` | Yes (after delay) | No | Rate limits, temporary unavailability |

### Retry Configuration

```go
ToolRetryConfig: &agentpg.ToolRetryConfig{
    MaxAttempts: 2,    // 1 initial + 1 retry (default)
    Jitter:      0.0,  // 0 = instant retry (default)
}

// With exponential backoff
ToolRetryConfig: &agentpg.ToolRetryConfig{
    MaxAttempts: 5,
    Jitter:      0.1,  // Enables River's attempt^4 formula
}
// Delays: 1s, 16s, 81s, 256s, 625s (with 10% jitter)
```

---

## Configuration Hooks

### Timing Configuration

```go
type ClientConfig struct {
    // Polling intervals
    BatchPollInterval time.Duration  // Batch API status (default: 30s)
    RunPollInterval   time.Duration  // Run claiming (default: 1s)
    ToolPollInterval  time.Duration  // Tool claiming (default: 500ms)

    // Heartbeat
    HeartbeatInterval time.Duration  // Instance liveness (default: 15s)

    // Leadership
    LeaderTTL time.Duration  // Leader lease duration (default: 30s)

    // Cleanup
    InstanceTTL     time.Duration  // Stale instance timeout (default: 60s)
    CleanupInterval time.Duration  // Cleanup frequency (default: 1min)
}
```

### Concurrency Configuration

```go
type ClientConfig struct {
    MaxConcurrentRuns          int  // Batch runs (default: 10)
    MaxConcurrentStreamingRuns int  // Streaming runs (default: 5)
    MaxConcurrentTools         int  // Tool executions (default: 50)
}
```

### Compaction Configuration

```go
type ClientConfig struct {
    AutoCompactionEnabled bool              // Auto-compact after runs (default: false)
    CompactionConfig      *compaction.Config
}

type compaction.Config struct {
    Strategy            Strategy  // StrategyHybrid or StrategySummarization
    Trigger             float64   // Context usage threshold (default: 0.85)
    TargetTokens        int       // Target after compaction (default: 80000)
    PreserveLastN       int       // Keep last N messages (default: 10)
    ProtectedTokens     int       // Never compact last N tokens (default: 40000)
    SummarizerModel     string    // Summarization model
    PreserveToolOutputs bool      // Keep tool outputs in hybrid mode
}
```

---

## Future: Lifecycle Hooks

AgentPG currently does **not** have explicit lifecycle hooks, but the following extension points are planned for future releases.

### Proposed Run Hooks

```go
// Future API - not yet implemented
type RunHook interface {
    OnRunCreated(ctx context.Context, run *Run) error
    OnRunStateChanged(ctx context.Context, run *Run, prevState RunState) error
    OnRunCompleted(ctx context.Context, run *Run) error
    OnRunFailed(ctx context.Context, run *Run, err error) error
}

// Usage
client.RegisterRunHook(&MyRunHook{})
```

### Proposed Tool Hooks

```go
// Future API - not yet implemented
type ToolHook interface {
    OnBeforeExecute(ctx context.Context, exec *ToolExecution) error
    OnAfterExecute(ctx context.Context, exec *ToolExecution, output string, err error) error
    OnToolError(ctx context.Context, exec *ToolExecution, err error) error
}

// Usage
client.RegisterToolHook(&MyToolHook{})
```

### Proposed Message Hooks

```go
// Future API - not yet implemented
type MessageHook interface {
    OnMessageCreated(ctx context.Context, msg *Message) error
    OnMessageUpdated(ctx context.Context, msg *Message) error
    OnMessageCompacted(ctx context.Context, archived []*Message) error
}

// Usage
client.RegisterMessageHook(&MyMessageHook{})
```

### Current Workaround

Until lifecycle hooks are implemented, use external LISTEN/NOTIFY consumers:

```go
// External hook implementation via PostgreSQL events
listener.Listen("agentpg_run_state")
listener.Listen("agentpg_run_finalized")

for notif := range listener.Notify {
    switch notif.Channel {
    case "agentpg_run_state":
        // Pseudo OnRunStateChanged
        handleStateChange(notif.Payload)
    case "agentpg_run_finalized":
        // Pseudo OnRunCompleted/OnRunFailed
        handleRunFinalized(notif.Payload)
    }
}
```

---

## Best Practices

### For Custom Business Logic

Implement the `Tool` interface:

```go
client.RegisterTool(&MyBusinessLogicTool{
    db:     database,
    cache:  redis,
    logger: logger,
})
```

### For Monitoring

Subscribe to PostgreSQL LISTEN/NOTIFY events externally:

```go
listener.Listen("agentpg_run_finalized")
// Send metrics to Prometheus, Datadog, etc.
```

### For Custom Logging

Implement the `Logger` interface:

```go
client, _ := agentpg.NewClient(drv, &agentpg.ClientConfig{
    Logger: &MyCustomLogger{},
})
```

### For New Databases

Implement the `Driver` interface (see `driver/pgxv5` for reference).

### For Context Management

Override with `CompactWithConfig()` for custom compaction behavior:

```go
client.CompactWithConfig(ctx, sessionID, &compaction.Config{
    Strategy:     compaction.StrategySummarization,
    TargetTokens: 50000,
})
```

### For Application Context

Use metadata fields on sessions, runs, and messages:

```go
client.NewSession(ctx, "tenant", "user", nil, map[string]any{
    "correlation_id": requestID,
    "user_metadata":  userInfo,
})
```

### For Retry Logic

Configure `ToolRetryConfig` and `RunRescueConfig`:

```go
client, _ := agentpg.NewClient(drv, &agentpg.ClientConfig{
    ToolRetryConfig: &agentpg.ToolRetryConfig{
        MaxAttempts: 3,
        Jitter:      0.1,
    },
    RunRescueConfig: &agentpg.RunRescueConfig{
        RescueTimeout:     10 * time.Minute,
        MaxRescueAttempts: 5,
    },
})
```
