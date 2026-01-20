# AgentPG Golang API Reference

Complete API reference for the AgentPG framework.

## Table of Contents

- [Core Package](#core-package)
- [Tool Package](#tool-package)
- [Driver Package](#driver-package)
- [Compaction Package](#compaction-package)
- [UI Package](#ui-package)

---

## Core Package

`github.com/youssefsiam38/agentpg`

### Client Type

```go
type Client[TTx any] struct
```

Generic client type with database transaction support. The type parameter `TTx` represents the native transaction type (e.g., `pgx.Tx`, `*sql.Tx`).

### Client Lifecycle

#### NewClient

```go
func NewClient[TTx any](drv driver.Driver[TTx], config *ClientConfig) (*Client[TTx], error)
```

Creates a new AgentPG client. Tools must be registered before calling `Start()`. Agents are created via `GetOrCreateAgent()` after `Start()`.

#### Start

```go
func (c *Client[TTx]) Start(ctx context.Context) error
```

Initializes client and begins background processing:
- Validates agent/tool references
- Registers instance in database
- Starts background workers (run worker, streaming worker, tool worker, batch poller, rescuer)

#### Stop

```go
func (c *Client[TTx]) Stop(ctx context.Context) error
```

Gracefully shuts down the client:
- Cancels background tasks with context timeout
- Releases leadership if acquired
- Closes driver listener

#### InstanceID

```go
func (c *Client[TTx]) InstanceID() string
```

Returns unique identifier for this client instance.

#### Config

```go
func (c *Client[TTx]) Config() *ClientConfig
```

Returns the client configuration.

---

### Tool Registration and Agent Management

#### RegisterTool

```go
func (c *Client[TTx]) RegisterTool(t tool.Tool) error
```

Registers a tool with the client. Must be called before `Start()`.

#### GetOrCreateAgent

```go
func (c *Client[TTx]) GetOrCreateAgent(ctx context.Context, def *AgentDefinition) (*Agent, error)
```

Creates a new agent in the database or returns existing agent if one with the same name already exists. Must be called after `Start()`. This operation is idempotent and safe to call on every startup.

Returns an `*Agent` with the database UUID that should be used when creating runs.

#### CreateAgent

```go
func (c *Client[TTx]) CreateAgent(ctx context.Context, def *AgentDefinition) (*Agent, error)
```

Creates a new agent in the database. Returns error if agent with same name already exists. Must be called after `Start()`.

#### GetAgent

```go
func (c *Client[TTx]) GetAgent(ctx context.Context, id uuid.UUID) (*Agent, error)
```

Returns agent by UUID from the database.

#### GetAgentByName

```go
func (c *Client[TTx]) GetAgentByName(ctx context.Context, name string) (*Agent, error)
```

Returns agent by name from the database.

#### GetTool

```go
func (c *Client[TTx]) GetTool(name string) tool.Tool
```

Returns registered tool by name, or nil if not found.

---

### Session Management

#### NewSession

```go
func (c *Client[TTx]) NewSession(ctx context.Context, parentSessionID *uuid.UUID, metadata map[string]any) (uuid.UUID, error)
```

Creates a new conversation session.

| Parameter | Description |
|-----------|-------------|
| `parentSessionID` | For nested/child sessions (optional) |
| `metadata` | Arbitrary JSON metadata for app-specific fields (tenant_id, user_id, etc.) |

**Example:**
```go
sessionID, _ := client.NewSession(ctx, nil, map[string]any{
    "tenant_id": "tenant-123",
    "user_id":   "user-456",
})
```

#### NewSessionTx

```go
func (c *Client[TTx]) NewSessionTx(ctx context.Context, tx TTx, parentSessionID *uuid.UUID, metadata map[string]any) (uuid.UUID, error)
```

Creates session within existing transaction. Session not visible until transaction commits.

#### GetSession

```go
func (c *Client[TTx]) GetSession(ctx context.Context, id uuid.UUID) (*Session, error)
```

Retrieves session by ID.

---

### Run Execution (Batch API)

50% cost discount, higher latency. Best for background tasks.

#### Run

```go
func (c *Client[TTx]) Run(ctx context.Context, sessionID uuid.UUID, agentID uuid.UUID, prompt string, variables map[string]any) (uuid.UUID, error)
```

Creates async run using Claude Batch API. Returns immediately with run ID.

| Parameter | Description |
|-----------|-------------|
| `sessionID` | Session UUID |
| `agentID` | Agent UUID (from `GetOrCreateAgent`) |
| `prompt` | User message |
| `variables` | Per-run variables accessible to tools via context (or `nil`) |

#### RunTx

```go
func (c *Client[TTx]) RunTx(ctx context.Context, tx TTx, sessionID uuid.UUID, agentID uuid.UUID, prompt string, variables map[string]any) (uuid.UUID, error)
```

Creates run within transaction. Run not visible to workers until transaction commits.

#### RunSync

```go
func (c *Client[TTx]) RunSync(ctx context.Context, sessionID uuid.UUID, agentID uuid.UUID, prompt string, variables map[string]any) (*Response, error)
```

Convenience wrapper: creates run and waits for completion. **Do NOT use inside transaction (deadlock risk).**

---

### Run Execution (Streaming API)

Real-time responses, standard pricing. Best for interactive applications.

#### RunFast

```go
func (c *Client[TTx]) RunFast(ctx context.Context, sessionID uuid.UUID, agentID uuid.UUID, prompt string, variables map[string]any) (uuid.UUID, error)
```

Creates async run using Claude Streaming API. Returns immediately with run ID.

#### RunFastTx

```go
func (c *Client[TTx]) RunFastTx(ctx context.Context, tx TTx, sessionID uuid.UUID, agentID uuid.UUID, prompt string, variables map[string]any) (uuid.UUID, error)
```

Creates streaming run within transaction.

#### RunFastSync

```go
func (c *Client[TTx]) RunFastSync(ctx context.Context, sessionID uuid.UUID, agentID uuid.UUID, prompt string, variables map[string]any) (*Response, error)
```

Convenience wrapper for streaming. Recommended for interactive applications.

---

### Run Status and Completion

#### WaitForRun

```go
func (c *Client[TTx]) WaitForRun(ctx context.Context, runID uuid.UUID) (*Response, error)
```

Waits for run to complete and returns response. Works with both Batch and Streaming modes.

#### GetRun

```go
func (c *Client[TTx]) GetRun(ctx context.Context, id uuid.UUID) (*Run, error)
```

Retrieves run state by ID.

---

### Context Compaction

#### Compact

```go
func (c *Client[TTx]) Compact(ctx context.Context, sessionID uuid.UUID) (*compaction.Result, error)
```

Performs context compaction on session. Replaces older messages with structured summary.

#### CompactWithConfig

```go
func (c *Client[TTx]) CompactWithConfig(ctx context.Context, sessionID uuid.UUID, cfg *compaction.Config) (*compaction.Result, error)
```

Compaction with custom one-off configuration.

#### NeedsCompaction

```go
func (c *Client[TTx]) NeedsCompaction(ctx context.Context, sessionID uuid.UUID) (bool, error)
```

Checks if session exceeds compaction threshold.

#### GetCompactionStats

```go
func (c *Client[TTx]) GetCompactionStats(ctx context.Context, sessionID uuid.UUID) (*compaction.Stats, error)
```

Returns compaction statistics for session.

#### CompactIfNeeded

```go
func (c *Client[TTx]) CompactIfNeeded(ctx context.Context, sessionID uuid.UUID) (*compaction.Result, error)
```

Conditionally performs compaction. Returns nil result if not needed.

---

## Configuration Types

### ClientConfig

```go
type ClientConfig struct {
    // Required
    APIKey string  // Anthropic API key (fallback: ANTHROPIC_API_KEY env var)

    // Instance identification
    Name string    // Service instance identifier (default: hostname)
    ID   string    // Unique instance identifier (default: UUID)

    // Concurrency limits
    MaxConcurrentRuns          int  // Batch run concurrency (default: 10)
    MaxConcurrentStreamingRuns int  // Streaming run concurrency (default: 5)
    MaxConcurrentTools         int  // Tool execution concurrency (default: 50)

    // Polling intervals
    BatchPollInterval time.Duration  // Batch API status poll (default: 30s)
    RunPollInterval   time.Duration  // Run claiming poll (default: 1s)
    ToolPollInterval  time.Duration  // Tool execution poll (default: 500ms)

    // Instance health
    HeartbeatInterval time.Duration  // Liveness heartbeat (default: 15s)
    InstanceTTL       time.Duration  // Stale instance timeout (default: 60s)
    CleanupInterval   time.Duration  // Cleanup job frequency (default: 1min)

    // Leadership
    LeaderTTL       time.Duration  // Leader election lease (default: 30s)
    StuckRunTimeout time.Duration  // Run rescue timeout (default: 5min)

    // Extensions
    Logger                Logger              // Structured logger (optional)
    AutoCompactionEnabled bool                // Auto-compact after runs (default: false)
    CompactionConfig      *compaction.Config  // Custom compaction config
    ToolRetryConfig       *ToolRetryConfig    // Tool retry behavior
    RunRescueConfig       *RunRescueConfig    // Run rescue behavior
}
```

#### DefaultConfig

```go
func DefaultConfig() *ClientConfig
```

Returns new config with all defaults set.

### ToolRetryConfig

```go
type ToolRetryConfig struct {
    MaxAttempts int      // Maximum execution attempts (default: 2)
    Jitter      float64  // Backoff jitter 0.0-1.0 (default: 0.0 = instant retry)
}
```

#### DefaultToolRetryConfig

```go
func DefaultToolRetryConfig() *ToolRetryConfig
```

#### NextRetryDelay

```go
func (c *ToolRetryConfig) NextRetryDelay(attemptCount int) time.Duration
```

Calculates delay before next retry. Uses River's attempt^4 formula if Jitter > 0.

### RunRescueConfig

```go
type RunRescueConfig struct {
    RescueInterval    time.Duration  // Check frequency (default: 1min)
    RescueTimeout     time.Duration  // Stuck threshold (default: 5min)
    MaxRescueAttempts int            // Max attempts before failure (default: 3)
}
```

---

## Data Types

### AgentDefinition

Used to define an agent when calling `GetOrCreateAgent()` or `CreateAgent()`.

```go
type AgentDefinition struct {
    Name         string          // Unique identifier (required)
    Description  string          // Description (shown when used as tool)
    Model        string          // Claude model ID (required)
    SystemPrompt string          // Agent behavior instructions
    Tools        []string        // Tool names agent can use
    AgentIDs     []uuid.UUID     // Agent UUIDs for delegation (agent-as-tool)
    MaxTokens    *int            // Response length limit
    Temperature  *float64        // Randomness 0.0-1.0
    TopK         *int            // Token selection limit
    TopP         *float64        // Nucleus sampling probability
    Config       map[string]any  // Additional settings
}
```

### Agent

Represents a database agent entity returned from `GetOrCreateAgent()`.

```go
type Agent struct {
    ID           uuid.UUID       // Database primary key (use this for runs)
    Name         string          // Unique identifier
    Description  string          // Description (shown when used as tool)
    Model        string          // Claude model ID
    SystemPrompt string          // Agent behavior instructions
    Tools        []string        // Tool names agent can use
    AgentIDs     []uuid.UUID     // Agent UUIDs for delegation
    MaxTokens    *int            // Response length limit
    Temperature  *float64        // Randomness 0.0-1.0
    TopK         *int            // Token selection limit
    TopP         *float64        // Nucleus sampling probability
    Config       map[string]any  // Additional settings
    CreatedAt    time.Time
    UpdatedAt    time.Time
}
```

#### AllToolNames

```go
func (a *Agent) AllToolNames() []string
```

Returns all tool names (both regular and agent-as-tool).

### Session

```go
type Session struct {
    ID              uuid.UUID
    ParentSessionID *uuid.UUID
    Depth           int
    Metadata        map[string]any  // App-specific fields (tenant_id, user_id, etc.)
    CompactionCount int
    CreatedAt       time.Time
    UpdatedAt       time.Time
}
```

### Run

```go
type Run struct {
    ID                      uuid.UUID
    SessionID               uuid.UUID
    AgentID                 uuid.UUID        // Agent UUID (foreign key)
    RunMode                 RunMode          // "batch" or "streaming"
    ParentRunID             *uuid.UUID
    ParentToolExecutionID   *uuid.UUID
    Depth                   int
    State                   RunState
    PreviousState           *RunState
    Prompt                  string
    CurrentIteration        int
    CurrentIterationID      *uuid.UUID
    ResponseText            *string
    StopReason              *string
    InputTokens             int
    OutputTokens            int
    CacheCreationInputTokens int
    CacheReadInputTokens    int
    IterationCount          int
    ToolIterations          int
    ErrorMessage            *string
    ErrorType               *string
    CreatedByInstanceID     *string
    ClaimedByInstanceID     *string
    ClaimedAt               *time.Time
    RescueAttempts          int
    LastRescueAt            *time.Time
    Metadata                map[string]any
    CreatedAt               time.Time
    StartedAt               *time.Time
    FinalizedAt             *time.Time
}
```

#### Usage

```go
func (r *Run) Usage() Usage
```

Returns cumulative token usage.

### Response

```go
type Response struct {
    Text           string    // Final text response
    StopReason     string    // Reason run stopped
    Usage          Usage     // Token statistics
    Message        *Message  // Full final message
    IterationCount int       // Number of API calls
    ToolIterations int       // Iterations with tool_use
}
```

### Usage

```go
type Usage struct {
    InputTokens              int
    OutputTokens             int
    CacheCreationInputTokens int
    CacheReadInputTokens     int
}
```

#### Methods

```go
func (u Usage) Add(other Usage) Usage  // Combines two Usage values
func (u Usage) Total() int              // Returns total tokens (input + output)
```

### Message

```go
type Message struct {
    ID          uuid.UUID
    SessionID   uuid.UUID
    RunID       *uuid.UUID
    Role        MessageRole      // "user", "assistant", "system"
    Content     []ContentBlock
    Usage       Usage
    IsPreserved bool             // Never compact this message
    IsSummary   bool             // This is a compaction summary
    Metadata    map[string]any
    CreatedAt   time.Time
    UpdatedAt   time.Time
}
```

### ContentBlock

```go
type ContentBlock struct {
    Type               string          // text, tool_use, tool_result, etc.
    Text               string          // For text/thinking blocks
    ToolUseID          string          // ID of tool_use block
    ToolName           string          // Name of tool being called
    ToolInput          json.RawMessage // Tool input
    ToolResultForUseID string          // ID of tool_use this is result for
    ToolContent        string          // Tool output text
    IsError            bool            // Tool execution failed
    Source             json.RawMessage // Media/document source
    SearchResults      json.RawMessage // Web search results
    Metadata           map[string]any
}
```

### Iteration

```go
type Iteration struct {
    ID                       uuid.UUID
    RunID                    uuid.UUID
    IterationNumber          int
    IsStreaming              bool
    BatchID                  *string
    BatchRequestID           *string
    BatchStatus              *BatchStatus
    BatchSubmittedAt         *time.Time
    BatchCompletedAt         *time.Time
    BatchExpiresAt           *time.Time
    BatchPollCount           int
    BatchLastPollAt          *time.Time
    StreamingStartedAt       *time.Time
    StreamingCompletedAt     *time.Time
    TriggerType              string        // "user_prompt", "tool_results", "continuation"
    RequestMessageIDs        []uuid.UUID
    StopReason               *string
    ResponseMessageID        *uuid.UUID
    HasToolUse               bool
    ToolExecutionCount       int
    InputTokens              int
    OutputTokens             int
    CacheCreationInputTokens int
    CacheReadInputTokens     int
    ErrorMessage             *string
    ErrorType                *string
    CreatedAt                time.Time
    StartedAt                *time.Time
    CompletedAt              *time.Time
}
```

### ToolExecution

```go
type ToolExecution struct {
    ID                  uuid.UUID
    RunID               uuid.UUID
    IterationID         uuid.UUID
    State               ToolExecutionState  // pending, running, completed, failed, skipped
    ToolUseID           string
    ToolName            string
    ToolInput           json.RawMessage
    IsAgentTool         bool
    AgentID             *uuid.UUID          // Agent UUID for agent-as-tool
    ChildRunID          *uuid.UUID
    ToolOutput          *string
    IsError             bool
    ErrorMessage        *string
    ClaimedByInstanceID *string
    ClaimedAt           *time.Time
    AttemptCount        int
    MaxAttempts         int
    ScheduledAt         time.Time
    SnoozeCount         int
    LastError           *string
    CreatedAt           time.Time
    StartedAt           *time.Time
    CompletedAt         *time.Time
}
```

### Instance

```go
type Instance struct {
    ID                 string
    Name               string
    Hostname           *string
    PID                *int
    Version            *string
    MaxConcurrentRuns  int
    MaxConcurrentTools int
    ActiveRunCount     int
    ActiveToolCount    int
    Metadata           map[string]any
    CreatedAt          time.Time
    LastHeartbeatAt    time.Time
}
```

### CompactionEvent

```go
type CompactionEvent struct {
    ID                  uuid.UUID
    SessionID           uuid.UUID
    Strategy            string
    OriginalTokens      int
    CompactedTokens     int
    MessagesRemoved     int
    SummaryContent      *string
    PreservedMessageIDs []uuid.UUID
    ModelUsed           *string
    DurationMS          *int
    CreatedAt           time.Time
}
```

---

## Enums and Constants

### RunState

```go
type RunState string

const (
    RunStatePending         RunState = "pending"          // Waiting for worker
    RunStateBatchSubmitting RunState = "batch_submitting" // Submitting to Batch API
    RunStateBatchPending    RunState = "batch_pending"    // Submitted, awaiting processing
    RunStateBatchProcessing RunState = "batch_processing" // Claude processing
    RunStateStreaming       RunState = "streaming"        // Streaming API processing
    RunStatePendingTools    RunState = "pending_tools"    // Waiting for tool executions
    RunStateAwaitingInput   RunState = "awaiting_input"   // Needs continuation
    RunStateCompleted       RunState = "completed"        // Terminal: success
    RunStateCancelled       RunState = "cancelled"        // Terminal: cancelled
    RunStateFailed          RunState = "failed"           // Terminal: error
)

func (s RunState) IsTerminal() bool
func (s RunState) String() string
```

### RunMode

```go
type RunMode string

const (
    RunModeBatch     RunMode = "batch"     // Batch API (50% cost discount)
    RunModeStreaming RunMode = "streaming" // Streaming API (real-time)
)
```

### ToolExecutionState

```go
type ToolExecutionState string

const (
    ToolStatePending   ToolExecutionState = "pending"
    ToolStateRunning   ToolExecutionState = "running"
    ToolStateCompleted ToolExecutionState = "completed"
    ToolStateFailed    ToolExecutionState = "failed"
    ToolStateSkipped   ToolExecutionState = "skipped"
)
```

### MessageRole

```go
type MessageRole string

const (
    MessageRoleUser      MessageRole = "user"
    MessageRoleAssistant MessageRole = "assistant"
    MessageRoleSystem    MessageRole = "system"
)
```

### BatchStatus

```go
type BatchStatus string

const (
    BatchStatusInProgress BatchStatus = "in_progress"
    BatchStatusCanceling  BatchStatus = "canceling"
    BatchStatusEnded      BatchStatus = "ended"
)
```

### Content Types

```go
const (
    ContentTypeText            = "text"
    ContentTypeToolUse         = "tool_use"
    ContentTypeToolResult      = "tool_result"
    ContentTypeImage           = "image"
    ContentTypeDocument        = "document"
    ContentTypeThinking        = "thinking"
    ContentTypeServerToolUse   = "server_tool_use"
    ContentTypeWebSearchResult = "web_search_result"
)
```

### Trigger Types

```go
const (
    TriggerTypeUserPrompt   = "user_prompt"
    TriggerTypeToolResults  = "tool_results"
    TriggerTypeContinuation = "continuation"
)
```

### LISTEN/NOTIFY Channels

```go
const (
    ChannelRunCreated    = "agentpg_run_created"
    ChannelRunState      = "agentpg_run_state"
    ChannelRunFinalized  = "agentpg_run_finalized"
    ChannelToolPending   = "agentpg_tool_pending"
    ChannelToolsComplete = "agentpg_tools_complete"
)
```

---

## Error Types

### Sentinel Errors

```go
var (
    ErrInvalidConfig          = errors.New("invalid configuration")
    ErrSessionNotFound        = errors.New("session not found")
    ErrRunNotFound            = errors.New("run not found")
    ErrAgentNotFound          = errors.New("agent not found")
    ErrToolNotFound           = errors.New("tool not found")
    ErrIterationNotFound      = errors.New("iteration not found")
    ErrToolExecutionNotFound  = errors.New("tool execution not found")
    ErrAgentNotRegistered     = errors.New("agent not registered")
    ErrToolNotRegistered      = errors.New("tool not registered")
    ErrClientNotStarted       = errors.New("client not started")
    ErrClientAlreadyStarted   = errors.New("client already started")
    ErrClientStopping         = errors.New("client is stopping")
    ErrInvalidStateTransition = errors.New("invalid state transition")
    ErrRunAlreadyFinalized    = errors.New("run already finalized")
    ErrRunCancelled           = errors.New("run cancelled")
    ErrInvalidToolSchema      = errors.New("invalid tool schema")
    ErrToolExecutionFailed    = errors.New("tool execution failed")
    ErrBatchAPIError          = errors.New("batch API error")
    ErrBatchExpired           = errors.New("batch expired")
    ErrBatchFailed            = errors.New("batch failed")
    ErrStorageError           = errors.New("storage operation failed")
    ErrInstanceDisconnected   = errors.New("instance disconnected")
    ErrInstanceNotFound       = errors.New("instance not found")
    ErrCompactionFailed       = errors.New("context compaction failed")
)
```

### AgentError

```go
type AgentError struct {
    Op        string         // Operation that failed
    Err       error          // Underlying error
    SessionID string         // Session ID if applicable
    RunID     string         // Run ID if applicable
    Context   map[string]any // Additional key-value context
}

func NewAgentError(op string, err error) *AgentError
func (e *AgentError) Error() string
func (e *AgentError) Unwrap() error
func (e *AgentError) WithSession(sessionID string) *AgentError
func (e *AgentError) WithRun(runID string) *AgentError
func (e *AgentError) WithContext(key string, value any) *AgentError
func WrapError(op string, err error) error
```

---

## Tool Package

`github.com/youssefsiam38/agentpg/tool`

### Tool Interface

```go
type Tool interface {
    Name() string
    Description() string
    InputSchema() ToolSchema
    Execute(ctx context.Context, input json.RawMessage) (string, error)
}
```

### Tool Schema Types

```go
type ToolSchema struct {
    Type        string                  // Must be "object"
    Properties  map[string]PropertyDef
    Required    []string
    Description string
}

type PropertyDef struct {
    Type             string
    Description      string
    Enum             []string
    Default          any
    Minimum          *float64
    Maximum          *float64
    ExclusiveMinimum *float64
    ExclusiveMaximum *float64
    MinLength        *int
    MaxLength        *int
    Pattern          string
    Items            *PropertyDef
    MinItems         *int
    MaxItems         *int
    Properties       map[string]PropertyDef
    Required         []string
}

func (s *ToolSchema) Validate() error
func (s *ToolSchema) ToJSON() map[string]any
func (p *PropertyDef) ToJSON() map[string]any
```

### FuncTool Helper

```go
type FuncTool struct{}

func NewFuncTool(name, description string, schema ToolSchema, execute func(context.Context, json.RawMessage) (string, error)) *FuncTool
func (t *FuncTool) Name() string
func (t *FuncTool) Description() string
func (t *FuncTool) InputSchema() ToolSchema
func (t *FuncTool) Execute(ctx context.Context, input json.RawMessage) (string, error)
```

### Tool Error Types

```go
// Cancel - Immediate failure, no retry
type ToolCancelError struct{ err error }
func ToolCancel(err error) error

// Discard - Permanent failure, invalid input
type ToolDiscardError struct{ err error }
func ToolDiscard(err error) error

// Snooze - Retry after duration WITHOUT consuming attempt
type ToolSnoozeError struct {
    Duration time.Duration
    err      error
}
func ToolSnooze(duration time.Duration, err error) error

// Type checks
func IsToolCancel(err error) bool
func IsToolDiscard(err error) bool
func IsToolSnooze(err error) bool
func GetSnoozeDuration(err error) (time.Duration, bool)
```

### ToolResult

```go
type ToolResult struct {
    ToolUseID string  // ID from tool_use block
    Content   string  // Tool output text
    IsError   bool    // Execution failed
}
```

### Run Context Helpers

Tools can access per-run variables and context information using these helpers:

```go
// RunContext contains run information passed to tools
type RunContext struct {
    RunID     uuid.UUID
    SessionID uuid.UUID
    Variables map[string]any
}

// Context enrichment (used internally by tool_worker)
func WithRunContext(ctx context.Context, rc RunContext) context.Context

// Get full run context
func GetRunContext(ctx context.Context) (RunContext, bool)

// Type-safe variable access
func GetVariable[T any](ctx context.Context, key string) (T, bool)
func GetVariableOr[T any](ctx context.Context, key string, defaultValue T) T
func MustGetVariable[T any](ctx context.Context, key string) T  // Panics if not found

// Get all variables
func GetVariables(ctx context.Context) map[string]any

// Convenience getters
func GetRunID(ctx context.Context) (uuid.UUID, bool)
func GetSessionID(ctx context.Context) (uuid.UUID, bool)
```

**Usage in tools:**

```go
func (t *MyTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
    // Get typed variable
    storyID, ok := tool.GetVariable[string](ctx, "story_id")
    if !ok {
        return "", errors.New("story_id required")
    }

    // Get with default
    maxItems := tool.GetVariableOr[int](ctx, "max_items", 10)

    // Get run/session IDs
    runID, _ := tool.GetRunID(ctx)
    sessionID, _ := tool.GetSessionID(ctx)

    return fmt.Sprintf("Processing story %s in run %s", storyID, runID), nil
}
```

---

## Driver Package

`github.com/youssefsiam38/agentpg/driver`

### Driver Interface

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

### Listener Interface

```go
type Listener interface {
    Listen(ctx context.Context, channels ...string) error
    Notifications() <-chan Notification
    Close() error
}

type Notification struct {
    Channel string
    Payload string
}
```

### Store Interface

```go
type Store[TTx any] interface {
    // Session operations
    CreateSession(ctx context.Context, params CreateSessionParams) (uuid.UUID, error)
    CreateSessionTx(ctx context.Context, tx TTx, params CreateSessionParams) (uuid.UUID, error)
    GetSession(ctx context.Context, id uuid.UUID) (*Session, error)
    UpdateSession(ctx context.Context, id uuid.UUID, updates map[string]any) error
    ListSessions(ctx context.Context, params ListSessionsParams) ([]*Session, int, error)
    ListTenants(ctx context.Context) ([]TenantInfo, error)

    // Run operations
    CreateRun(ctx context.Context, params CreateRunParams) (uuid.UUID, error)
    CreateRunTx(ctx context.Context, tx TTx, params CreateRunParams) (uuid.UUID, error)
    GetRun(ctx context.Context, id uuid.UUID) (*Run, error)
    UpdateRun(ctx context.Context, id uuid.UUID, updates map[string]any) error
    UpdateRunState(ctx context.Context, id uuid.UUID, state RunState) error
    ClaimRuns(ctx context.Context, instanceID string, maxCount int, runMode *RunMode) ([]*Run, error)
    GetRunsBySession(ctx context.Context, sessionID uuid.UUID) ([]*Run, error)
    ListRuns(ctx context.Context, params ListRunsParams) ([]*Run, int, error)

    // Iteration operations
    CreateIteration(ctx context.Context, params CreateIterationParams) (uuid.UUID, error)
    GetIteration(ctx context.Context, id uuid.UUID) (*Iteration, error)
    UpdateIteration(ctx context.Context, id uuid.UUID, updates map[string]any) error
    GetIterationsForPoll(ctx context.Context, instanceID string, limit int) ([]*Iteration, error)
    GetIterationsByRun(ctx context.Context, runID uuid.UUID) ([]*Iteration, error)

    // Tool execution operations
    CreateToolExecution(ctx context.Context, params CreateToolExecutionParams) (uuid.UUID, error)
    CreateToolExecutions(ctx context.Context, params []CreateToolExecutionParams) ([]uuid.UUID, error)
    CreateToolExecutionsAndUpdateRunState(ctx context.Context, runID uuid.UUID, params []CreateToolExecutionParams) ([]uuid.UUID, error)
    GetToolExecution(ctx context.Context, id uuid.UUID) (*ToolExecution, error)
    UpdateToolExecution(ctx context.Context, id uuid.UUID, updates map[string]any) error
    ClaimToolExecutions(ctx context.Context, instanceID string, maxCount int, toolNames, agentNames []string) ([]*ToolExecution, error)
    CompleteToolExecution(ctx context.Context, id uuid.UUID, output string, isError bool) error
    GetToolExecutionsByRun(ctx context.Context, runID uuid.UUID) ([]*ToolExecution, error)
    GetToolExecutionsByIteration(ctx context.Context, iterationID uuid.UUID) ([]*ToolExecution, error)
    GetPendingToolExecutionsForRun(ctx context.Context, runID uuid.UUID) ([]*ToolExecution, error)
    ListToolExecutions(ctx context.Context, params ListToolExecutionsParams) ([]*ToolExecution, int, error)

    // Tool retry operations
    RetryToolExecution(ctx context.Context, id uuid.UUID, err error, delay time.Duration) error
    SnoozeToolExecution(ctx context.Context, id uuid.UUID, duration time.Duration, err error) error
    DiscardToolExecution(ctx context.Context, id uuid.UUID, err error) error

    // Tool completion
    CompleteToolsAndContinueRun(ctx context.Context, runID uuid.UUID, toolResults []ToolResult) error

    // Run rescue
    GetStuckRuns(ctx context.Context, timeout time.Duration, maxRescueAttempts int) ([]*Run, error)
    RescueRun(ctx context.Context, runID uuid.UUID) error

    // Message operations
    CreateMessage(ctx context.Context, params CreateMessageParams) (uuid.UUID, error)
    GetMessage(ctx context.Context, id uuid.UUID) (*Message, error)
    GetMessages(ctx context.Context, sessionID uuid.UUID, limit, offset int) ([]*Message, error)
    GetMessagesByRun(ctx context.Context, runID uuid.UUID) ([]*Message, error)
    GetMessagesForRunContext(ctx context.Context, sessionID uuid.UUID, excludeChildRunIDs []uuid.UUID) ([]*Message, error)
    GetMessagesWithRunInfo(ctx context.Context, sessionID uuid.UUID, limit, offset int) ([]*Message, error)
    UpdateMessage(ctx context.Context, id uuid.UUID, updates map[string]any) error
    DeleteMessage(ctx context.Context, id uuid.UUID) error

    // Content block operations
    CreateContentBlocks(ctx context.Context, messageID uuid.UUID, blocks []ContentBlock) error
    GetContentBlocks(ctx context.Context, messageID uuid.UUID) ([]ContentBlock, error)

    // Instance operations
    RegisterInstance(ctx context.Context, params RegisterInstanceParams) error
    UnregisterInstance(ctx context.Context, instanceID string) error
    UpdateHeartbeat(ctx context.Context, instanceID string) error
    GetInstance(ctx context.Context, id string) (*Instance, error)
    ListInstances(ctx context.Context) ([]*Instance, error)
    GetStaleInstances(ctx context.Context, ttl time.Duration) ([]*Instance, error)
    DeleteStaleInstances(ctx context.Context, ttl time.Duration) (int, error)
    GetInstanceActiveCounts(ctx context.Context, instanceID string) (runs int, tools int, err error)
    GetAllInstanceActiveCounts(ctx context.Context) (map[string]struct{ Runs, Tools int }, error)

    // Instance capability
    RegisterInstanceAgent(ctx context.Context, instanceID, agentName string) error
    RegisterInstanceTool(ctx context.Context, instanceID, toolName string) error
    UnregisterInstanceAgent(ctx context.Context, instanceID, agentName string) error
    UnregisterInstanceTool(ctx context.Context, instanceID, toolName string) error
    GetInstanceAgents(ctx context.Context, instanceID string) ([]string, error)
    GetInstanceTools(ctx context.Context, instanceID string) ([]string, error)

    // Leader election
    TryAcquireLeader(ctx context.Context, instanceID string, ttl time.Duration) (bool, error)
    RefreshLeader(ctx context.Context, instanceID string, ttl time.Duration) error
    GetLeader(ctx context.Context) (string, error)
    IsLeader(ctx context.Context, instanceID string) (bool, error)
    ReleaseLeader(ctx context.Context, instanceID string) error

    // Agent operations
    UpsertAgent(ctx context.Context, agent *AgentDefinition) error
    GetAgent(ctx context.Context, name string) (*AgentDefinition, error)
    DeleteAgent(ctx context.Context, name string) error
    ListAgents(ctx context.Context) ([]*AgentDefinition, error)

    // Tool operations
    UpsertTool(ctx context.Context, tool *ToolDefinition) error
    GetTool(ctx context.Context, name string) (*ToolDefinition, error)
    DeleteTool(ctx context.Context, name string) error
    ListTools(ctx context.Context) ([]*ToolDefinition, error)

    // Compaction operations
    CreateCompactionEvent(ctx context.Context, params CreateCompactionEventParams) (uuid.UUID, error)
    ArchiveMessage(ctx context.Context, sessionID, messageID uuid.UUID, originalMessage json.RawMessage) error
    GetCompactionEvents(ctx context.Context, sessionID uuid.UUID, limit, offset int) ([]*CompactionEvent, error)
    GetCompactionStats(ctx context.Context, sessionID uuid.UUID) (*CompactionStats, error)
}
```

### Driver Implementations

```go
// pgx/v5 (recommended)
import "github.com/youssefsiam38/agentpg/driver/pgxv5"
drv := pgxv5.New(pool)  // pool is *pgxpool.Pool

// database/sql
import "github.com/youssefsiam38/agentpg/driver/databasesql"
drv := databasesql.New(db)  // db is *sql.DB
```

---

## Compaction Package

`github.com/youssefsiam38/agentpg/compaction`

### Strategy

```go
type Strategy string

const (
    StrategySummarization Strategy = "summarization"  // Claude-based summarization
    StrategyHybrid        Strategy = "hybrid"         // Prune tool outputs first, then summarize
)
```

### Config

```go
type Config struct {
    Strategy            Strategy       // Default: StrategyHybrid
    Trigger             float64        // Context usage threshold 0.0-1.0 (default: 0.85)
    TargetTokens        int            // Target tokens after compaction (default: 80000)
    PreserveLastN       int            // Minimum recent messages to preserve (default: 10)
    ProtectedTokens     int            // Final tokens never compacted (default: 40000)
    SummarizerModel     string         // Model for summarization (default: "claude-3-5-haiku-20241022")
    SummarizerMaxTokens int            // Max summarization response tokens (default: 4096)
    MaxTokensForModel   int            // Context window size (default: 200000)
    PreserveToolOutputs bool           // Keep tool outputs in hybrid (default: false)
    UseTokenCountingAPI bool           // Use Claude token counting (default: true)
}

func DefaultConfig() *Config
func (c *Config) Validate() error
func (c *Config) ApplyDefaults()
func (c *Config) TriggerThreshold() int  // Returns absolute token count
```

### Compactor

```go
type Compactor[TTx any] struct{}

func New[TTx any](store driver.Store[TTx], anthropic *anthropic.Client, config *Config, logger Logger) *Compactor[TTx]
func (comp *Compactor[TTx]) Compact(ctx context.Context, sessionID uuid.UUID) (*Result, error)
func (comp *Compactor[TTx]) CompactIfNeeded(ctx context.Context, sessionID uuid.UUID) (*Result, error)
func (comp *Compactor[TTx]) NeedsCompaction(ctx context.Context, sessionID uuid.UUID) (bool, error)
func (comp *Compactor[TTx]) GetStats(ctx context.Context, sessionID uuid.UUID) (*Stats, error)
```

### Result

```go
type Result struct {
    EventID             uuid.UUID
    Strategy            Strategy
    OriginalTokens      int
    CompactedTokens     int
    MessagesRemoved     int
    PreservedMessageIDs []uuid.UUID
    SummaryCreated      bool
    Duration            time.Duration
}
```

### Stats

```go
type Stats struct {
    SessionID           uuid.UUID
    TotalMessages       int
    TotalTokens         int
    UsagePercent        float64
    CompactionCount     int
    PreservedMessages   int
    SummaryMessages     int
    CompactableMessages int
    NeedsCompaction     bool
}
```

---

## UI Package

`github.com/youssefsiam38/agentpg/ui`

### Config

```go
type Config struct {
    BasePath            string           // URL prefix for UI mounting
    MetadataFilter      map[string]any   // Filter sessions by metadata key-value pairs
    MetadataDisplayKeys []string         // Metadata keys to show in session lists
    MetadataFilterKeys  []string         // Metadata keys for filter dropdowns
    ReadOnly            bool             // Disable write operations
    Logger              Logger
    RefreshInterval     time.Duration    // SSE update frequency (default: 5s)
    PageSize            int              // Pagination size (default: 25)
}

func DefaultConfig() *Config
```

### Handlers

```go
func UIHandler[TTx any](store driver.Store[TTx], client *agentpg.Client[TTx], cfg *Config) http.Handler
```

---

## Helper Functions

```go
func Ptr[T any](v T) *T                // Returns pointer to value
func Deref[T any](p *T) T              // Dereferences pointer or returns zero value
func DerefOr[T any](p *T, def T) T     // Dereferences pointer or returns default
```
