# Configuration Guide

This guide covers all configuration options available in AgentPG, including client settings, compaction, UI, and tool retry behavior.

## Table of Contents

1. [ClientConfig](#clientconfig)
2. [ToolRetryConfig](#toolretryconfig)
3. [RunRescueConfig](#runrescueconfig)
4. [CompactionConfig](#compactionconfig)
5. [UI Config](#ui-config)
6. [AgentDefinition](#agentdefinition)
7. [Tool Schema](#tool-schema)
8. [Environment Variables](#environment-variables)
9. [Configuration Examples](#configuration-examples)

---

## ClientConfig

The main configuration struct for the AgentPG client. Pass this to `agentpg.NewClient()`.

### Core Fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `APIKey` | `string` | `ANTHROPIC_API_KEY` env var | Anthropic API key (required). Falls back to environment variable if not set. |
| `Name` | `string` | Hostname | Identifies the service instance for logging and debugging. |
| `ID` | `string` | Generated UUID | Unique identifier for client instance. Must be unique across all running instances. |

### Concurrency Control

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `MaxConcurrentRuns` | `int` | `10` | Limits concurrent batch run processing. |
| `MaxConcurrentStreamingRuns` | `int` | `5` | Limits concurrent streaming run processing. Lower than batch since streaming holds connections longer. |
| `MaxConcurrentTools` | `int` | `50` | Limits concurrent tool executions. |

### Polling Intervals

These are fallback intervals when LISTEN/NOTIFY is unavailable.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `BatchPollInterval` | `time.Duration` | `30s` | How often to poll Claude Batch API for status. |
| `RunPollInterval` | `time.Duration` | `1s` | Polling fallback interval for new runs. |
| `ToolPollInterval` | `time.Duration` | `500ms` | Polling interval for tool executions. |

### Health & Maintenance

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `HeartbeatInterval` | `time.Duration` | `15s` | How often instances send heartbeat to indicate liveness. |
| `LeaderTTL` | `time.Duration` | `30s` | Leader election lease duration. |
| `StuckRunTimeout` | `time.Duration` | `5m` | Marks runs as stuck after this duration without progress. |
| `InstanceTTL` | `time.Duration` | `2m` | How long an instance can go without heartbeat before cleanup. Should be > 2x HeartbeatInterval. |
| `CleanupInterval` | `time.Duration` | `1m` | How often to run cleanup jobs. |

### Advanced Features

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `Logger` | `Logger` | `nil` | For structured logging. Compatible with `slog.Logger`. |
| `AutoCompactionEnabled` | `bool` | `false` | Enables automatic context compaction after each run. |
| `CompactionConfig` | `*compaction.Config` | `nil` | Configuration for context compaction. Uses defaults if nil. |
| `ToolRetryConfig` | `*ToolRetryConfig` | `nil` | Configures tool execution retry behavior. |
| `RunRescueConfig` | `*RunRescueConfig` | `nil` | Configures run rescue behavior for stuck runs. |

---

## ToolRetryConfig

Controls tool execution retry behavior using River's attempt^4 formula for exponential backoff.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `MaxAttempts` | `int` | `2` | Maximum number of execution attempts. After this, the tool is marked permanently failed. |
| `Jitter` | `float64` | `0.0` | Adds randomness to prevent thundering herd. Range: 0.0-1.0. Default (0.0) = instant retry. |

### Retry Behavior

**Default (Jitter=0.0):** Instant retries with no delay for snappy user experience.

**With Jitter > 0:** Uses River's attempt^4 formula:

| Attempt | Delay |
|---------|-------|
| 1 | ~1 second |
| 2 | ~16 seconds |
| 3 | ~81 seconds |
| 4 | ~256 seconds |
| 5 | ~625 seconds |

### Special Error Types

Tools can return special error types to control retry behavior:

```go
import "github.com/youssefsiam38/agentpg/tool"

func (t *MyTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
    // Cancel immediately - no retry, permanent failure
    if isInvalidInput(input) {
        return "", tool.ToolCancel(errors.New("invalid input format"))
    }

    // Discard permanently - similar to cancel
    if !isAuthorized(ctx) {
        return "", tool.ToolDiscard(errors.New("unauthorized"))
    }

    // Snooze - retry after duration, does NOT consume an attempt
    if isRateLimited(err) {
        return "", tool.ToolSnooze(30*time.Second, err)
    }

    // Regular error - will be retried
    return "", errors.New("recoverable error")
}
```

---

## RunRescueConfig

Configures rescue behavior for runs stuck in non-terminal states.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `RescueInterval` | `time.Duration` | `1m` | How often the leader checks for stuck runs. |
| `RescueTimeout` | `time.Duration` | `5m` | How long a run can be stuck before rescue. |
| `MaxRescueAttempts` | `int` | `3` | Maximum times a run can be rescued before permanent failure. |

### Monitored States

The rescuer monitors runs stuck in these non-terminal states:
- `batch_submitting`
- `batch_pending`
- `batch_processing`
- `streaming`
- `pending_tools`

---

## CompactionConfig

Handles automatic or manual context compaction for long conversations exceeding token limits.

### Strategy Options

| Strategy | Description | Cost | Use Case |
|----------|-------------|------|----------|
| `StrategyHybrid` | Prunes tool outputs first, then summarizes if needed | Low | Default, cost-effective |
| `StrategySummarization` | Directly summarizes using Claude | Medium | When you want full summaries |

### Configuration Fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `Strategy` | `Strategy` | `StrategyHybrid` | Compaction strategy to use. |
| `Trigger` | `float64` | `0.85` | Context usage threshold (0.0-1.0) that triggers compaction. |
| `TargetTokens` | `int` | `80000` | Target token count after compaction. |
| `PreserveLastN` | `int` | `10` | Minimum number of recent messages to always preserve. |
| `ProtectedTokens` | `int` | `40000` | Token count at end of context that is never summarized. |
| `SummarizerModel` | `string` | `claude-3-5-haiku-20241022` | Claude model to use for summarization. |
| `SummarizerMaxTokens` | `int` | `4096` | Maximum tokens for summarization response. |
| `MaxTokensForModel` | `int` | `200000` | Maximum context window for target model. |
| `PreserveToolOutputs` | `bool` | `false` | If false, tool outputs are replaced with `[TOOL OUTPUT PRUNED]`. |
| `UseTokenCountingAPI` | `bool` | `true` | Use Claude's token counting API for accurate counts. |

### Message Partitioning

Messages are categorized for compaction:

| Category | Compactable | Condition |
|----------|-------------|-----------|
| Protected | No | Within last `ProtectedTokens` |
| Preserved | No | Marked with `is_preserved=true` |
| Recent | No | Last `PreserveLastN` messages |
| Summaries | No | Previous compaction summaries |
| Compactable | Yes | Everything else |

---

## UI Config

Configures the web UI handlers.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `BasePath` | `string` | `""` | URL prefix where UI is mounted. E.g., "/ui". |
| `MetadataFilter` | `map[string]any` | `nil` | Filters sessions by metadata key-value pairs. |
| `MetadataDisplayKeys` | `[]string` | `nil` | Metadata keys to show in session lists. |
| `MetadataFilterKeys` | `[]string` | `nil` | Metadata keys to show filter dropdowns for. |
| `ReadOnly` | `bool` | `false` | Disables write operations (chat, session creation). |
| `RefreshInterval` | `time.Duration` | `5s` | For SSE updates and auto-refresh. |
| `PageSize` | `int` | `25` | Items per page for pagination. |
| `Logger` | `Logger` | `nil` | For structured logging. |

### Filtering Options

**Show All Sessions with Filter Dropdowns:**
- Set `MetadataFilterKeys` to enable filter dropdowns
- Users can filter by metadata fields in the UI
- Full read-write access (if `ReadOnly=false`)

**Pre-filter by Metadata:**
- Set `MetadataFilter` to restrict to specific metadata values
- Pre-configured filters cannot be overridden by query params
- Full read-write access (if `ReadOnly=false`)

**Read-Only Monitoring (`ReadOnly = true`):**
- Disables chat interface
- No session creation
- No write operations
- Can be used with `nil` client in `UIHandler`

---

## AgentDefinition

Defines an agent's configuration for registration with the client.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `Name` | `string` | Yes | Unique identifier for the agent. |
| `Description` | `string` | No | Shown when agent is used as a tool. |
| `Model` | `string` | Yes | Claude model ID. |
| `SystemPrompt` | `string` | No | Defines agent's behavior and role. |
| `Tools` | `[]string` | No | List of tool names this agent can use. |
| `Agents` | `[]string` | No | List of agent names this agent can delegate to. |
| `MaxTokens` | `*int` | No | Limits response length. |
| `Temperature` | `*float64` | No | Controls randomness (0.0-1.0). |
| `TopK` | `*int` | No | Limits token selection. |
| `TopP` | `*float64` | No | Nucleus sampling limit. |
| `Config` | `map[string]any` | No | Additional settings as JSON. |

### Available Models

| Model | Description |
|-------|-------------|
| `claude-opus-4-5-20251101` | Most capable, best for complex tasks |
| `claude-sonnet-4-5-20250929` | Balanced performance and cost |
| `claude-3-5-haiku-20241022` | Fast and cost-effective |

---

## Tool Schema

Tools use JSON Schema for input validation.

### ToolSchema Structure

```go
type ToolSchema struct {
    Type        string                    // Must be "object"
    Properties  map[string]PropertyDef    // Parameter definitions
    Required    []string                  // Required parameter names
    Description string                    // Additional context
}
```

### PropertyDef Fields

| Field | Type | Description |
|-------|------|-------------|
| `Type` | `string` | "string", "number", "integer", "boolean", "array", "object" |
| `Description` | `string` | Explains the property |
| `Enum` | `[]string` | Restrict to allowed values |
| `Default` | `any` | Default value |
| `Minimum`, `Maximum` | `*float64` | Numeric bounds |
| `MinLength`, `MaxLength` | `*int` | String length constraints |
| `Pattern` | `string` | Regex pattern for strings |
| `Items` | `*PropertyDef` | Array item schema |
| `Properties`, `Required` | Maps/Slices | For nested objects |

---

## Environment Variables

| Variable | Required | Description |
|----------|----------|-------------|
| `ANTHROPIC_API_KEY` | Yes* | Anthropic API key. *Only if not set in `ClientConfig.APIKey`. |
| `DATABASE_URL` | Yes | PostgreSQL connection string. |

### Connection String Examples

```bash
# Development
DATABASE_URL="postgres://agentpg:agentpg@localhost:5432/agentpg?sslmode=disable"

# Production with SSL
DATABASE_URL="postgres://user:pass@prod-db.example.com:5432/agentpg?sslmode=require"
```

---

## Configuration Examples

### Minimal Configuration

```go
client, _ := agentpg.NewClient(drv, &agentpg.ClientConfig{
    APIKey: os.Getenv("ANTHROPIC_API_KEY"),
})
```

### Production Configuration

```go
client, _ := agentpg.NewClient(drv, &agentpg.ClientConfig{
    APIKey:                     os.Getenv("ANTHROPIC_API_KEY"),
    Name:                       "worker-1",
    ID:                         "worker-1-uuid",
    MaxConcurrentRuns:          20,
    MaxConcurrentStreamingRuns: 10,
    MaxConcurrentTools:         100,
    BatchPollInterval:          30 * time.Second,
    HeartbeatInterval:          15 * time.Second,
    Logger:                     slog.Default(),
    AutoCompactionEnabled:      true,
    CompactionConfig: &compaction.Config{
        Strategy:     compaction.StrategyHybrid,
        Trigger:      0.85,
        TargetTokens: 80000,
    },
    ToolRetryConfig: &agentpg.ToolRetryConfig{
        MaxAttempts: 3,
        Jitter:      0.1,
    },
})
```

### UI Configuration - Full Admin

```go
uiConfig := &ui.Config{
    BasePath:        "/ui",
    PageSize:        25,
    RefreshInterval: 5 * time.Second,
    Logger:          slog.Default(),
}
http.Handle("/ui/", http.StripPrefix("/ui", ui.UIHandler(store, client, uiConfig)))
```

### UI Configuration - Filtered by Metadata

```go
uiConfig := &ui.Config{
    BasePath: "/tenant/ui",
    MetadataFilter: map[string]any{
        "tenant_id": "tenant-123",
    },
    PageSize: 50,
}
```

### UI Configuration - Read-Only Monitoring

```go
monitorConfig := &ui.Config{
    BasePath:        "/monitor",
    ReadOnly:        true,
    PageSize:        50,
    RefreshInterval: 10 * time.Second,
}
http.Handle("/monitor/", http.StripPrefix("/monitor", ui.UIHandler(store, nil, monitorConfig)))
```

### Compaction Configuration

```go
compactionConfig := &compaction.Config{
    Strategy:            compaction.StrategyHybrid,
    Trigger:             0.85,
    TargetTokens:        80000,
    PreserveLastN:       10,
    ProtectedTokens:     40000,
    SummarizerModel:     "claude-3-5-haiku-20241022",
    SummarizerMaxTokens: 4096,
    MaxTokensForModel:   200000,
    PreserveToolOutputs: false,
    UseTokenCountingAPI: true,
}
```

---

## Default Values Reference

### ClientConfig Defaults

```go
const (
    DefaultMaxConcurrentRuns          = 10
    DefaultMaxConcurrentStreamingRuns = 5
    DefaultMaxConcurrentTools         = 50
    DefaultBatchPollInterval          = 30 * time.Second
    DefaultRunPollInterval            = 1 * time.Second
    DefaultToolPollInterval           = 500 * time.Millisecond
    DefaultHeartbeatInterval          = 15 * time.Second
    DefaultLeaderTTL                  = 30 * time.Second
    DefaultStuckRunTimeout            = 5 * time.Minute
    DefaultInstanceTTL                = 2 * time.Minute
    DefaultCleanupInterval            = 1 * time.Minute
)
```

### ToolRetryConfig Defaults

```go
const (
    DefaultToolRetryMaxAttempts = 2   // 2 attempts (1 retry)
    DefaultToolRetryJitter      = 0.0 // Instant retry
)
```

### RunRescueConfig Defaults

```go
const (
    DefaultRescueInterval    = 1 * time.Minute
    DefaultRescueTimeout     = 5 * time.Minute
    DefaultMaxRescueAttempts = 3
)
```

### CompactionConfig Defaults

```go
const (
    DefaultStrategy            = StrategyHybrid
    DefaultTrigger             = 0.85
    DefaultTargetTokens        = 80000
    DefaultPreserveLastN       = 10
    DefaultProtectedTokens     = 40000
    DefaultSummarizerModel     = "claude-3-5-haiku-20241022"
    DefaultSummarizerMaxTokens = 4096
    DefaultMaxTokensForModel   = 200000
    DefaultPreserveToolOutputs = false
    DefaultUseTokenCountingAPI = true
)
```

### UI Config Defaults

```go
const (
    DefaultRefreshInterval = 5 * time.Second
    DefaultPageSize        = 25
)
```
