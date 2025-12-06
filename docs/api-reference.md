# API Reference

Complete API documentation for AgentPG.

## Table of Contents

- [Agent](#agent)
- [Configuration](#configuration)
- [Sessions](#sessions)
- [Messages](#messages)
- [Tools](#tools)
- [Hooks](#hooks)
- [Storage](#storage)
- [Errors](#errors)

---

## Agent

### Creating an Agent

```go
func New[TTx any](drv driver.Driver[TTx], cfg Config, opts ...Option) (*Agent[TTx], error)
```

Creates a new agent with the required driver, configuration, and optional settings. The transaction type is automatically inferred from the driver.

**Parameters:**
- `drv` - Database driver (pgxv5 or databasesql)
- `cfg` - Required configuration (client, model, system prompt)
- `opts` - Zero or more functional options

**Example:**
```go
import (
    "github.com/anthropics/anthropic-sdk-go"
    "github.com/jackc/pgx/v5/pgxpool"
    "github.com/youssefsiam38/agentpg"
    "github.com/youssefsiam38/agentpg/driver/pgxv5"
)

pool, _ := pgxpool.New(ctx, databaseURL)
drv := pgxv5.New(pool)
client := anthropic.NewClient()

agent, err := agentpg.New(drv, agentpg.Config{
    Client:       &client,
    Model:        "claude-sonnet-4-5-20250929",
    SystemPrompt: "You are a helpful assistant.",
},
    agentpg.WithMaxTokens(4096),
    agentpg.WithTemperature(0.7),
)
```

### Agent Methods

#### Run

```go
func (a *Agent) Run(ctx context.Context, prompt string) (*Response, error)
```

Sends a message to the agent and returns the response. Automatically handles:
- **Transaction management** - All database operations are atomic (commit on success, rollback on error)
- Message persistence
- Tool execution loops
- Context compaction (if enabled)

**Parameters:**
- `ctx` - Context for cancellation and timeouts
- `prompt` - User message text

**Returns:**
- `*Response` - Contains the assistant's message, stop reason, and token usage
- `error` - Any error that occurred

**Example:**
```go
response, err := agent.Run(ctx, "What is the weather like?")
if err != nil {
    log.Fatal(err)
}
fmt.Println(response.Message.Content)
```

#### RunTx

```go
func (a *Agent[TTx]) RunTx(ctx context.Context, tx TTx, prompt string) (*Response, error)
```

Sends a message to the agent within a user-managed transaction. The caller is responsible for committing or rolling back the transaction. This allows you to combine your own database operations with agent operations in a single atomic transaction.

**Parameters:**
- `ctx` - Context for cancellation and timeouts
- `tx` - Native transaction (`pgx.Tx` for pgxv5 driver, `*sql.Tx` for databasesql driver)
- `prompt` - User message text

**Returns:**
- `*Response` - Contains the assistant's message, stop reason, and token usage
- `error` - Any error that occurred

**Example:**
```go
// Start transaction using the PostgreSQL pool
tx, err := pool.Begin(ctx)
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
response, err := agent.RunTx(ctx, tx, "Process this order")
if err != nil {
    return err // Everything rolled back
}

// Commit all atomically
return tx.Commit(ctx)
```

#### NewSession

```go
func (a *Agent[TTx]) NewSession(ctx context.Context, tenantID, identifier string, parentSessionID *string, metadata map[string]any) (string, error)
```

Creates a new conversation session.

**Parameters:**
- `tenantID` - Tenant identifier for multi-tenancy
- `identifier` - Custom session identifier (e.g., user ID)
- `parentSessionID` - Optional parent session ID (for nested agents)
- `metadata` - Optional key-value metadata

**Returns:**
- `string` - The new session's UUID
- `error` - Any error that occurred

**Example:**
```go
sessionID, err := agent.NewSession(ctx, "company-abc", "user-123", nil, map[string]any{
    "user_name": "Alice",
    "plan": "premium",
})
```

#### LoadSession

```go
func (a *Agent) LoadSession(ctx context.Context, sessionID string) error
```

Loads an existing session by its UUID.

**Example:**
```go
err := agent.LoadSession(ctx, "550e8400-e29b-41d4-a716-446655440000")
```

#### LoadSessionByIdentifier

```go
func (a *Agent) LoadSessionByIdentifier(ctx context.Context, tenantID, identifier string) error
```

Loads a session by tenant ID and identifier.

**Example:**
```go
err := agent.LoadSessionByIdentifier(ctx, "company-abc", "user-123")
```

#### CurrentSession

```go
func (a *Agent) CurrentSession() string
```

Returns the currently active session ID (thread-safe).

#### GetModel

```go
func (a *Agent) GetModel() string
```

Returns the model being used by this agent.

**Example:**
```go
model := agent.GetModel()
fmt.Printf("Using model: %s\n", model)
```

#### GetSystemPrompt

```go
func (a *Agent) GetSystemPrompt() string
```

Returns the system prompt configured for this agent.

#### GetSession

```go
func (a *Agent) GetSession(ctx context.Context, sessionID string) (*SessionInfo, error)
```

Returns the full session object for the specified session ID.

**Parameters:**
- `ctx` - Context for cancellation and timeouts
- `sessionID` - The UUID of the session to retrieve

**Returns:**
- `*SessionInfo` - Session details including ID, tenant, metadata, message count, etc.
- `error` - Any error that occurred

#### GetMessages

```go
func (a *Agent) GetMessages(ctx context.Context) ([]*Message, error)
```

Returns all messages in the current session.

#### GetCompactionStats

```go
func (a *Agent) GetCompactionStats(ctx context.Context) (*compaction.CompactionStats, error)
```

Returns statistics about the current session's context usage and compaction state.

**Returns:**
- `*CompactionStats` - Contains current tokens, max tokens, utilization percentage, message count, etc.
- `error` - Any error that occurred

**Example:**
```go
stats, err := agent.GetCompactionStats(ctx)
if err != nil {
    log.Fatal(err)
}
fmt.Printf("Context utilization: %.1f%% (%d/%d tokens)\n",
    stats.UtilizationPct, stats.CurrentTokens, stats.MaxTokens)
fmt.Printf("Should compact: %v\n", stats.ShouldCompact)
```

#### Compact

```go
func (a *Agent) Compact(ctx context.Context) (*compaction.CompactionResult, error)
```

Manually triggers context compaction for the current session. Automatically wraps execution in a transaction for atomicity.

**Returns:**
- `*CompactionResult` - Contains compaction details (original/compacted tokens, messages removed, summary, etc.)
- `error` - Any error that occurred

**Example:**
```go
// Check if compaction would be beneficial
stats, _ := agent.GetCompactionStats(ctx)
fmt.Printf("Current utilization: %.1f%%\n", stats.UtilizationPct)

// Manually trigger compaction
result, err := agent.Compact(ctx)
if err != nil {
    log.Fatal(err)
}

if result != nil {
    fmt.Printf("Compacted: %d -> %d tokens (%.1f%% reduction)\n",
        result.OriginalTokens,
        result.CompactedTokens,
        100.0*(1.0-float64(result.CompactedTokens)/float64(result.OriginalTokens)))
    fmt.Printf("Messages removed: %d\n", result.MessagesRemoved)
}
```

#### CompactTx

```go
func (a *Agent[TTx]) CompactTx(ctx context.Context, tx TTx) (*compaction.CompactionResult, error)
```

Manually triggers context compaction within an existing transaction. The caller is responsible for committing or rolling back the transaction. This allows combining compaction with other database operations atomically.

**Parameters:**
- `ctx` - Context for cancellation and timeouts
- `tx` - Native transaction (`pgx.Tx` for pgxv5 driver, `*sql.Tx` for databasesql driver)

**Returns:**
- `*CompactionResult` - Contains compaction details
- `error` - Any error that occurred

**Example:**
```go
// Start transaction
tx, err := pool.Begin(ctx)
if err != nil {
    return err
}
defer tx.Rollback(ctx)

// Compact the session
result, err := agent.CompactTx(ctx, tx)
if err != nil {
    return err
}

// Your business logic in the same transaction
if result != nil {
    _, err = tx.Exec(ctx,
        "INSERT INTO compaction_log (session_id, tokens_saved) VALUES ($1, $2)",
        sessionID, result.OriginalTokens - result.CompactedTokens)
    if err != nil {
        return err
    }
}

// Commit all atomically
return tx.Commit(ctx)
```

#### RegisterTool

```go
func (a *Agent) RegisterTool(t tool.Tool) error
```

Registers a tool with the agent at runtime.

#### GetTools

```go
func (a *Agent) GetTools() []string
```

Returns the names of all registered tools.

#### AsToolFor

```go
func (a *Agent) AsToolFor(parent *Agent) error
```

Registers this agent as a tool that can be called by another agent.

**Example:**
```go
// Create a specialized research agent
researchAgent, _ := agentpg.New(cfg,
    agentpg.WithSystemPrompt("You are a research specialist."),
)

// Register as a tool for the main agent
err := researchAgent.AsToolFor(mainAgent)
```

#### Close

```go
func (a *Agent) Close() error
```

Closes the agent and releases database connections.

---

## Configuration

### Config (Required)

```go
type Config struct {
    Client       *anthropic.Client  // Anthropic API client
    Model        string             // Model ID (e.g., "claude-sonnet-4-5-20250929")
    SystemPrompt string             // System prompt for the agent
}
```

**Note:** The database connection is provided via the driver, not in Config.

### Options (Functional)

| Option | Description | Default |
|--------|-------------|---------|
| `WithMaxTokens(n int64)` | Maximum tokens to generate | Model-specific |
| `WithTemperature(t float64)` | Sampling temperature (0.0-1.0) | API default |
| `WithTopK(k int64)` | Top-K sampling | API default |
| `WithTopP(p float64)` | Nucleus sampling | API default |
| `WithStopSequences(...string)` | Custom stop sequences | None |
| `WithTools(...tool.Tool)` | Register tools | None |
| `WithAutoCompaction(bool)` | Enable auto-compaction | `true` |
| `WithCompactionStrategy(s)` | Compaction strategy | `"hybrid"` |
| `WithCompactionTrigger(float64)` | Trigger threshold (0.0-1.0) | `0.85` |
| `WithCompactionTarget(int)` | Target tokens after compaction | 40% of max |
| `WithCompactionPreserveN(int)` | Always preserve last N messages | `10` |
| `WithCompactionProtectedTokens(int)` | Never compact last N tokens | `40000` |
| `WithSummarizerModel(string)` | Model for summarization | `"claude-3-5-haiku-20241022"` |
| `WithMaxContextTokens(int)` | Override model's context window | Model-specific |
| `WithMaxToolIterations(int)` | Max tool calls per Run | `10` |
| `WithToolTimeout(time.Duration)` | Timeout for tool executions | `5m` |
| `WithExtendedContext(bool)` | Enable 1M token context | `false` |
| `WithMaxRetries(int)` | Max API retry attempts | `2` |
| `WithPreserveToolOutputs(bool)` | Keep full tool outputs | `false` |

### Known Models

```go
var KnownModels = map[string]ModelInfo{
    "claude-sonnet-4-5-20250929":   {MaxContextTokens: 200000, DefaultMaxTokens: 16384},
    "claude-opus-4-5-20251101":   {MaxContextTokens: 200000, DefaultMaxTokens: 16384},
    "claude-3-5-sonnet-20241022": {MaxContextTokens: 200000, DefaultMaxTokens: 8192},
    "claude-3-5-haiku-20241022":  {MaxContextTokens: 200000, DefaultMaxTokens: 8192},
    "claude-3-opus-20240229":     {MaxContextTokens: 200000, DefaultMaxTokens: 4096},
    "claude-3-sonnet-20240229":   {MaxContextTokens: 200000, DefaultMaxTokens: 4096},
    "claude-3-haiku-20240307":    {MaxContextTokens: 200000, DefaultMaxTokens: 4096},
}
```

---

## Messages

### Message Type

```go
type Message struct {
    ID          string                 // UUID
    SessionID   string                 // Parent session UUID
    Role        Role                   // "user", "assistant", or "system"
    Content     []ContentBlock         // Content blocks
    TokenCount  int                    // Estimated token count
    Metadata    map[string]any         // Custom metadata
    IsPreserved bool                   // Protected from compaction
    IsSummary   bool                   // Is a compaction summary
    CreatedAt   time.Time
    UpdatedAt   time.Time
}
```

### Roles

```go
const (
    RoleUser      Role = "user"
    RoleAssistant Role = "assistant"
    RoleSystem    Role = "system"
)
```

### Content Blocks

```go
type ContentBlock struct {
    Type         ContentType            // Block type
    Text         string                 // For text blocks
    ToolUseID    string                 // For tool_use blocks
    ToolName     string                 // Tool name
    ToolInput    map[string]any         // Tool parameters
    ToolInputRaw json.RawMessage        // Raw JSON input
    ToolResultID string                 // For tool_result blocks
    ToolContent  string                 // Tool output
    IsError      bool                   // Tool error flag
    Source       *ImageSource           // For image blocks
    Document     *DocumentSource        // For document blocks
}
```

### Content Types

```go
const (
    ContentTypeText       ContentType = "text"
    ContentTypeToolUse    ContentType = "tool_use"
    ContentTypeToolResult ContentType = "tool_result"
    ContentTypeImage      ContentType = "image"
    ContentTypeDocument   ContentType = "document"
)
```

### Helper Functions

```go
// Create a new message
func NewMessage(sessionID string, role Role, content []ContentBlock) *Message

// Create a user message with text
func NewUserMessage(sessionID string, text string) *Message

// Create an assistant message
func NewAssistantMessage(sessionID string, content []ContentBlock) *Message

// Create content blocks
func NewTextBlock(text string) ContentBlock
func NewToolUseBlock(id, name string, input map[string]any) ContentBlock
func NewToolResultBlock(toolUseID string, content string, isError bool) ContentBlock
```

### Response Type

```go
type Response struct {
    Message    *Message  // The assistant's response message
    StopReason string    // Why generation stopped
    Usage      *Usage    // Token usage statistics
}

type Usage struct {
    InputTokens  int
    OutputTokens int
}
```

---

## Tools

### Tool Interface

```go
type Tool interface {
    Name() string
    Description() string
    InputSchema() ToolSchema
    Execute(ctx context.Context, input json.RawMessage) (string, error)
}
```

### ToolSchema

```go
type ToolSchema struct {
    Type        string                    // Must be "object"
    Properties  map[string]PropertyDef    // Parameter definitions
    Required    []string                  // Required parameter names
}

type PropertyDef struct {
    Type        string         // "string", "number", "integer", "boolean", "array", "object"
    Description string         // Parameter description
    Enum        []string       // Allowed values (for strings)
    Minimum     *float64       // Min value (for numbers)
    Maximum     *float64       // Max value (for numbers)
    MinLength   *int           // Min length (for strings)
    MaxLength   *int           // Max length (for strings)
    Items       *PropertyDef   // Item schema (for arrays)
    Properties  map[string]PropertyDef  // Nested properties (for objects)
}
```

### FuncTool

Create tools from functions:

```go
func NewFuncTool(
    name string,
    description string,
    schema ToolSchema,
    fn func(ctx context.Context, input json.RawMessage) (string, error),
) Tool
```

**Example:**
```go
searchTool := tool.NewFuncTool(
    "web_search",
    "Search the web for information",
    tool.ToolSchema{
        Type: "object",
        Properties: map[string]tool.PropertyDef{
            "query": {
                Type:        "string",
                Description: "The search query",
            },
            "max_results": {
                Type:        "integer",
                Description: "Maximum results to return",
                Minimum:     ptr(1.0),
                Maximum:     ptr(10.0),
            },
        },
        Required: []string{"query"},
    },
    func(ctx context.Context, input json.RawMessage) (string, error) {
        var params struct {
            Query      string `json:"query"`
            MaxResults int    `json:"max_results"`
        }
        json.Unmarshal(input, &params)

        // Perform search...
        return results, nil
    },
)
```

### Registry

```go
// Create a registry
registry := tool.NewRegistry()

// Register tools
registry.Register(myTool)
registry.RegisterAll([]tool.Tool{tool1, tool2})

// Check registration
exists := registry.Has("tool_name")
tool, ok := registry.Get("tool_name")

// List all tools
names := registry.List()
count := registry.Count()
```

### Executor

```go
// Create executor
executor := tool.NewExecutor(registry)

// Set timeout
executor.SetDefaultTimeout(30 * time.Second)

// Execute single tool
result := executor.Execute(ctx, "tool_name", input)

// Execute multiple tools (sequential)
results := executor.ExecuteMultiple(ctx, calls)

// Execute multiple tools (parallel)
results := executor.ExecuteParallel(ctx, calls)

// Execute batch with strategy
results := executor.ExecuteBatch(ctx, calls, parallel)
```

---

## Hooks

Register callbacks for agent lifecycle events:

```go
import (
    "github.com/youssefsiam38/agentpg/compaction"
    "github.com/youssefsiam38/agentpg/types"
)

// Before sending messages to Claude
agent.OnBeforeMessage(func(ctx context.Context, messages []*types.Message) error {
    log.Printf("Sending %d messages", len(messages))
    return nil
})

// After receiving response
agent.OnAfterMessage(func(ctx context.Context, response *types.Response) error {
    log.Printf("Received response with %d content blocks", len(response.Message.Content))
    return nil
})

// When a tool is called
agent.OnToolCall(func(ctx context.Context, name string, input json.RawMessage, output string, err error) error {
    log.Printf("Tool %s called", name)
    return nil
})

// Before compaction
agent.OnBeforeCompaction(func(ctx context.Context, sessionID string) error {
    log.Printf("Compacting session %s", sessionID)
    return nil
})

// After compaction
agent.OnAfterCompaction(func(ctx context.Context, result *compaction.CompactionResult) error {
    log.Printf("Compaction complete: %d tokens -> %d tokens", result.OriginalTokens, result.CompactedTokens)
    return nil
})
```

---

## Storage

### Store Interface

```go
type Store interface {
    // Session operations
    CreateSession(ctx context.Context, tenantID, identifier string, metadata map[string]any) (string, error)
    GetSession(ctx context.Context, id string) (*Session, error)
    GetSessionByTenantAndIdentifier(ctx context.Context, tenantID, identifier string) (*Session, error)
    GetSessionsByTenant(ctx context.Context, tenantID string) ([]*Session, error)
    UpdateSessionCompactionCount(ctx context.Context, sessionID string) error

    // Message operations
    SaveMessage(ctx context.Context, msg *Message) error
    GetMessages(ctx context.Context, sessionID string) ([]*Message, error)
    DeleteMessages(ctx context.Context, ids []string) error
    GetSessionTokenCount(ctx context.Context, sessionID string) (int, error)

    // Compaction operations
    SaveCompactionEvent(ctx context.Context, event *CompactionEvent) error
    GetCompactionHistory(ctx context.Context, sessionID string) ([]*CompactionEvent, error)
    ArchiveMessages(ctx context.Context, eventID, sessionID string, messages []*Message) error

    // Lifecycle
    Close() error
}
```

### Transaction Support

```go
type TxStore interface {
    Store
    BeginTx(ctx context.Context) (Tx, error)
}

type Tx interface {
    Store
    Commit(ctx context.Context) error
    Rollback(ctx context.Context) error
}
```

### Drivers

The storage layer is accessed through drivers:

```go
// pgxv5 driver (recommended)
import "github.com/youssefsiam38/agentpg/driver/pgxv5"
drv := pgxv5.New(pool)  // pool is *pgxpool.Pool
store := drv.GetStore()

// database/sql driver
import "github.com/youssefsiam38/agentpg/driver/databasesql"
drv := databasesql.New(db)  // db is *sql.DB
store := drv.GetStore()
```

---

## Errors

### Error Types

```go
var (
    ErrNoSession         = errors.New("no active session")
    ErrSessionNotFound   = errors.New("session not found")
    ErrInvalidConfig     = errors.New("invalid configuration")
    ErrInvalidToolSchema = errors.New("invalid tool schema")
    ErrToolNotFound      = errors.New("tool not found")
)
```

### AgentError

```go
type AgentError struct {
    Operation string         // Operation that failed
    SessionID string         // Session ID (if applicable)
    Err       error          // Underlying error
    Context   map[string]any // Additional context
}

// Create errors
err := NewAgentError("Run", underlyingErr)
err := NewAgentErrorWithSession("Run", sessionID, underlyingErr)

// Add context
err = err.WithContext("key", value)

// Check error types
if errors.Is(err, ErrNoSession) {
    // Handle missing session
}
```

---

## Quick Reference

### Minimal Example

```go
pool, _ := pgxpool.New(ctx, "postgres://...")
drv := pgxv5.New(pool)
client := anthropic.NewClient()

agent, _ := agentpg.New(drv, agentpg.Config{
    Client:       &client,
    Model:        "claude-sonnet-4-5-20250929",
    SystemPrompt: "You are helpful.",
})

agent.NewSession(ctx, "tenant", "user", nil, nil)
response, _ := agent.Run(ctx, "Hello!")
for _, block := range response.Message.Content {
    if block.Type == agentpg.ContentTypeText {
        fmt.Println(block.Text)
    }
}
```

### With Tools

```go
agent, _ := agentpg.New(cfg, agentpg.WithTools(myTool))
```

### Resume Session

```go
agent.LoadSession(ctx, sessionID)
// or
agent.LoadSessionByIdentifier(ctx, "tenant", "user")
```
