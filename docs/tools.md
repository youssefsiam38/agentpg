# Tools Guide

This guide covers building, registering, and managing tools in AgentPG.

## Overview

Tools extend your agent's capabilities by allowing it to:
- Access external APIs and data sources
- Perform calculations and data processing
- Interact with databases and file systems
- Execute code and run commands
- Delegate to other agents

## Quick Start

### Creating a Simple Tool

```go
import (
    "context"
    "encoding/json"
    "github.com/youssefsiam38/agentpg/tool"
)

// Create a tool that gets the current time
timeTool := tool.NewFuncTool(
    "get_current_time",
    "Get the current date and time",
    tool.ToolSchema{
        Type: "object",
        Properties: map[string]tool.PropertyDef{
            "timezone": {
                Type:        "string",
                Description: "Timezone (e.g., 'America/New_York')",
            },
        },
    },
    func(ctx context.Context, input json.RawMessage) (string, error) {
        var params struct {
            Timezone string `json:"timezone"`
        }
        json.Unmarshal(input, &params)

        loc := time.UTC
        if params.Timezone != "" {
            loc, _ = time.LoadLocation(params.Timezone)
        }

        return time.Now().In(loc).Format(time.RFC3339), nil
    },
)
```

### Registering Tools

```go
// At agent creation
agent, _ := agentpg.New(cfg,
    agentpg.WithTools(timeTool, searchTool, calculatorTool),
)

// Or at runtime
agent.RegisterTool(newTool)
```

---

## Tool Interface

All tools implement the `Tool` interface:

```go
type Tool interface {
    Name() string
    Description() string
    InputSchema() ToolSchema
    Execute(ctx context.Context, input json.RawMessage) (string, error)
}
```

### Name

Unique identifier for the tool. Must be:
- Alphanumeric with underscores
- Descriptive but concise
- Unique within the agent

```go
func (t *MyTool) Name() string {
    return "web_search"
}
```

### Description

Human-readable description that Claude uses to decide when to use the tool:

```go
func (t *MyTool) Description() string {
    return "Search the web for current information. Use this when you need up-to-date information that may not be in your training data."
}
```

**Tips for Good Descriptions:**
- Explain what the tool does
- Explain when to use it
- Mention any limitations

### InputSchema

JSON Schema defining the tool's parameters:

```go
func (t *MyTool) InputSchema() tool.ToolSchema {
    return tool.ToolSchema{
        Type: "object",
        Properties: map[string]tool.PropertyDef{
            "query": {
                Type:        "string",
                Description: "The search query",
            },
            "max_results": {
                Type:        "integer",
                Description: "Maximum results (1-10)",
                Minimum:     ptr(1.0),
                Maximum:     ptr(10.0),
            },
        },
        Required: []string{"query"},
    }
}
```

### Execute

The function that performs the tool's action:

```go
func (t *MyTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
    // Parse input
    var params struct {
        Query      string `json:"query"`
        MaxResults int    `json:"max_results"`
    }
    if err := json.Unmarshal(input, &params); err != nil {
        return "", fmt.Errorf("invalid input: %w", err)
    }

    // Perform action
    results, err := t.client.Search(ctx, params.Query, params.MaxResults)
    if err != nil {
        return "", err
    }

    // Return string result
    return formatResults(results), nil
}
```

---

## Schema Definition

### Property Types

| Type | Go Type | Description |
|------|---------|-------------|
| `string` | `string` | Text values |
| `number` | `float64` | Floating point numbers |
| `integer` | `int` | Whole numbers |
| `boolean` | `bool` | True/false values |
| `array` | `[]T` | Lists of items |
| `object` | `struct` | Nested objects |

### PropertyDef

```go
type PropertyDef struct {
    Type        string                 // Required: "string", "number", etc.
    Description string                 // Strongly recommended
    Enum        []string               // Allowed values (strings only)
    Minimum     *float64               // Min value (numbers)
    Maximum     *float64               // Max value (numbers)
    MinLength   *int                   // Min length (strings)
    MaxLength   *int                   // Max length (strings)
    Items       *PropertyDef           // Item schema (arrays)
    Properties  map[string]PropertyDef // Nested props (objects)
}
```

### Examples

**String with Enum:**
```go
"status": {
    Type:        "string",
    Description: "Order status",
    Enum:        []string{"pending", "processing", "shipped", "delivered"},
}
```

**Number with Range:**
```go
"temperature": {
    Type:        "number",
    Description: "Temperature in Celsius",
    Minimum:     ptr(-40.0),
    Maximum:     ptr(100.0),
}
```

**Array of Strings:**
```go
"tags": {
    Type:        "array",
    Description: "List of tags",
    Items:       &tool.PropertyDef{Type: "string"},
}
```

**Nested Object:**
```go
"address": {
    Type:        "object",
    Description: "Shipping address",
    Properties: map[string]tool.PropertyDef{
        "street": {Type: "string"},
        "city":   {Type: "string"},
        "zip":    {Type: "string"},
    },
}
```

---

## Input Validation

The tool executor validates inputs against schemas before execution:

```go
executor := tool.NewExecutor(registry)

// This happens automatically during Execute
err := executor.ValidateInput("my_tool", inputJSON)
if err != nil {
    // Validation failed:
    // - Missing required fields
    // - Wrong types
    // - Out of range values
    // - Invalid enum values
}
```

### Validation Errors

| Error | Cause |
|-------|-------|
| `missing required field` | Required field not provided |
| `expected string, got number` | Type mismatch |
| `value not in enum` | Invalid enum value |
| `value below minimum` | Number too small |
| `value above maximum` | Number too large |
| `string too short` | Below minLength |
| `string too long` | Above maxLength |
| `value must be an integer` | Float for integer type |

---

## Tool Execution

### Single Execution

```go
result := executor.Execute(ctx, "tool_name", inputJSON)
// result.Output - Tool output string
// result.Error  - Any error that occurred
// result.Duration - Execution time
```

### Parallel Execution

```go
calls := []tool.ToolCallRequest{
    {ID: "1", ToolName: "search", Input: json.RawMessage(`{"query": "weather"}`)},
    {ID: "2", ToolName: "search", Input: json.RawMessage(`{"query": "news"}`)},
}

// Execute in parallel
results := executor.ExecuteParallel(ctx, calls)
```

### Timeout Configuration

```go
executor := tool.NewExecutor(registry)
executor.SetDefaultTimeout(30 * time.Second)
```

### Execution Flow

```
1. Agent receives tool_use block from Claude
2. Executor validates input against schema
3. Tool.Execute() is called with context and input
4. Result (or error) returned to Claude as tool_result
5. Loop continues until no more tool calls
```

---

## Best Practices

### Error Handling

```go
func (t *MyTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
    // Parse input with validation
    var params struct {
        URL string `json:"url"`
    }
    if err := json.Unmarshal(input, &params); err != nil {
        return "", fmt.Errorf("invalid input: %w", err)
    }

    // Validate business logic
    if !strings.HasPrefix(params.URL, "https://") {
        return "", fmt.Errorf("URL must use HTTPS")
    }

    // Handle external errors gracefully
    result, err := t.fetch(ctx, params.URL)
    if err != nil {
        // Return user-friendly error message
        return "", fmt.Errorf("failed to fetch URL: %w", err)
    }

    return result, nil
}
```

### Context Handling

```go
func (t *MyTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
    // Respect context cancellation
    select {
    case <-ctx.Done():
        return "", ctx.Err()
    default:
    }

    // Pass context to downstream calls
    result, err := t.api.Call(ctx, params)

    // Handle timeout gracefully
    if errors.Is(err, context.DeadlineExceeded) {
        return "", fmt.Errorf("operation timed out")
    }

    return result, err
}
```

### Output Formatting

```go
func (t *MyTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
    results := fetchResults()

    // Format for readability
    var sb strings.Builder
    sb.WriteString("Search Results:\n\n")
    for i, r := range results {
        sb.WriteString(fmt.Sprintf("%d. %s\n   %s\n\n", i+1, r.Title, r.URL))
    }

    return sb.String(), nil
}
```

---

## Common Tool Patterns

### HTTP API Tool

```go
type APITool struct {
    client  *http.Client
    baseURL string
}

func (t *APITool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
    var params struct {
        Endpoint string `json:"endpoint"`
        Method   string `json:"method"`
    }
    json.Unmarshal(input, &params)

    req, _ := http.NewRequestWithContext(ctx, params.Method, t.baseURL+params.Endpoint, nil)
    resp, err := t.client.Do(req)
    if err != nil {
        return "", err
    }
    defer resp.Body.Close()

    body, _ := io.ReadAll(resp.Body)
    return string(body), nil
}
```

### Database Query Tool

```go
type QueryTool struct {
    db *sql.DB
}

func (t *QueryTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
    var params struct {
        Query  string        `json:"query"`
        Params []interface{} `json:"params"`
    }
    json.Unmarshal(input, &params)

    // Only allow SELECT queries
    if !strings.HasPrefix(strings.ToUpper(params.Query), "SELECT") {
        return "", fmt.Errorf("only SELECT queries are allowed")
    }

    rows, err := t.db.QueryContext(ctx, params.Query, params.Params...)
    if err != nil {
        return "", err
    }
    defer rows.Close()

    return formatRows(rows), nil
}
```

### Calculator Tool

```go
calculatorTool := tool.NewFuncTool(
    "calculator",
    "Perform mathematical calculations",
    tool.ToolSchema{
        Type: "object",
        Properties: map[string]tool.PropertyDef{
            "expression": {
                Type:        "string",
                Description: "Mathematical expression (e.g., '2 + 2', 'sqrt(16)')",
            },
        },
        Required: []string{"expression"},
    },
    func(ctx context.Context, input json.RawMessage) (string, error) {
        var params struct {
            Expression string `json:"expression"`
        }
        json.Unmarshal(input, &params)

        result, err := evaluate(params.Expression)
        if err != nil {
            return "", err
        }

        return fmt.Sprintf("%v", result), nil
    },
)
```

---

## Transaction Access in Tools

When agents run via `RunTx`, tools can access the native database transaction to perform their own database operations atomically with the agent's operations.

### TxFromContext

```go
import (
    "github.com/jackc/pgx/v5"
    "github.com/youssefsiam38/agentpg"
)

// Get the native transaction - panics if not available
tx := agentpg.TxFromContext[pgx.Tx](ctx)

// Safely get the transaction - returns error if not available
tx, err := agentpg.TxFromContextSafely[pgx.Tx](ctx)
if err != nil {
    // Handle case where agent was called via Run() instead of RunTx()
}
```

### When is a Transaction Available?

| Method | Transaction in Context |
|--------|----------------------|
| `agent.Run(ctx, prompt)` | Yes (auto-managed) |
| `agent.RunTx(ctx, tx, prompt)` | Yes (user-provided) |

Both `Run()` and `RunTx()` provide transaction access to tools. The difference is:
- **`Run()`** - Agent manages the transaction lifecycle (begin/commit/rollback)
- **`RunTx()`** - You control the transaction, allowing you to combine agent operations with your own database work atomically

### Example: Database Tool with Transaction

```go
type AuditLogTool struct {
    pool *pgxpool.Pool // Fallback for non-transactional use
}

func (t *AuditLogTool) Name() string        { return "audit_log" }
func (t *AuditLogTool) Description() string { return "Log an action to the audit trail" }
func (t *AuditLogTool) InputSchema() tool.ToolSchema {
    return tool.ToolSchema{
        Type: "object",
        Properties: map[string]tool.PropertyDef{
            "action":  {Type: "string", Description: "Action performed"},
            "details": {Type: "string", Description: "Action details"},
        },
        Required: []string{"action"},
    }
}

func (t *AuditLogTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
    var params struct {
        Action  string `json:"action"`
        Details string `json:"details"`
    }
    json.Unmarshal(input, &params)

    // Try to get transaction, fall back to pool
    tx, err := agentpg.TxFromContextSafely[pgx.Tx](ctx)
    if err != nil {
        // No transaction - use pool directly (auto-commit)
        _, err = t.pool.Exec(ctx,
            "INSERT INTO audit_log (action, details) VALUES ($1, $2)",
            params.Action, params.Details)
    } else {
        // Has transaction - use it (will commit/rollback with agent)
        _, err = tx.Exec(ctx,
            "INSERT INTO audit_log (action, details) VALUES ($1, $2)",
            params.Action, params.Details)
    }

    if err != nil {
        return "", fmt.Errorf("failed to log: %w", err)
    }
    return "Logged successfully", nil
}
```

### Example: Tool Requiring Transaction

```go
type TransferFundsTool struct{}

func (t *TransferFundsTool) Name() string        { return "transfer_funds" }
func (t *TransferFundsTool) Description() string { return "Transfer funds between accounts" }
func (t *TransferFundsTool) InputSchema() tool.ToolSchema {
    return tool.ToolSchema{
        Type: "object",
        Properties: map[string]tool.PropertyDef{
            "from_account": {Type: "string", Description: "Source account ID"},
            "to_account":   {Type: "string", Description: "Destination account ID"},
            "amount":       {Type: "number", Description: "Amount to transfer"},
        },
        Required: []string{"from_account", "to_account", "amount"},
    }
}

func (t *TransferFundsTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
    var params struct {
        FromAccount string  `json:"from_account"`
        ToAccount   string  `json:"to_account"`
        Amount      float64 `json:"amount"`
    }
    json.Unmarshal(input, &params)

    // This tool REQUIRES a transaction - use the panicking version
    tx := agentpg.TxFromContext[pgx.Tx](ctx)

    // Debit source account
    _, err := tx.Exec(ctx,
        "UPDATE accounts SET balance = balance - $1 WHERE id = $2",
        params.Amount, params.FromAccount)
    if err != nil {
        return "", err
    }

    // Credit destination account
    _, err = tx.Exec(ctx,
        "UPDATE accounts SET balance = balance + $1 WHERE id = $2",
        params.Amount, params.ToAccount)
    if err != nil {
        return "", err
    }

    return fmt.Sprintf("Transferred $%.2f from %s to %s",
        params.Amount, params.FromAccount, params.ToAccount), nil
}
```

**Note**: If `TransferFundsTool` is called via `Run()` instead of `RunTx()`, it will panic. This is intentional - financial operations should always be transactional.

### Using with database/sql Driver

For the database/sql driver, use `*sql.Tx`:

```go
import "database/sql"

func (t *MyTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
    tx := agentpg.TxFromContext[*sql.Tx](ctx)

    result, err := tx.ExecContext(ctx, "INSERT INTO ...")
    // ...
}
```

### Testing Tools with Transactions

When testing tools that use `TxFromContext`, you need to provide a context with a transaction:

```go
func TestOrderTool(t *testing.T) {
    ctx := context.Background()
    pool, _ := pgxpool.New(ctx, databaseURL)

    tx, _ := pool.Begin(ctx)
    defer tx.Rollback(ctx)

    // Create context with transaction using the test helper
    ctx = agentpg.WithTestTx(ctx, tx)

    tool := &OrderTool{}
    result, err := tool.Execute(ctx, json.RawMessage(`{"product_id": "123", "quantity": 5}`))

    // Assert results...
    // tx.Rollback() cleans up test data
}
```

### API Reference

```go
// ErrNoTransaction is returned when TxFromContextSafely is called
// but no transaction exists in context.
var ErrNoTransaction = errors.New("agentpg: no transaction in context, only available within agent execution")

// TxFromContext returns the native database transaction from context.
// Panics if no transaction is available.
// TTx must match your driver: pgx.Tx for pgxv5, *sql.Tx for databasesql.
func TxFromContext[TTx any](ctx context.Context) TTx

// TxFromContextSafely returns the native transaction, or error if not available.
// Use this for tools that should work both with and without transactions.
func TxFromContextSafely[TTx any](ctx context.Context) (TTx, error)

// WithTestTx creates a context with a native transaction for testing tools.
func WithTestTx[TTx any](ctx context.Context, tx TTx) context.Context
```

---

## Nested Agents as Tools

You can use one agent as a tool for another:

```go
// Create a specialized research agent
researchAgent, _ := agentpg.New(agentpg.Config{
    SystemPrompt: "You are a research specialist. Provide detailed, well-sourced answers.",
    // ...
})

// Register as a tool for the main agent
err := researchAgent.AsToolFor(mainAgent)

// Now the main agent can delegate research tasks
response, _ := mainAgent.Run(ctx, "I need detailed research on quantum computing")
// Main agent may call the research agent internally
```

### Custom Agent Tool

```go
type ResearchAgentTool struct {
    agent *agentpg.Agent
}

func (t *ResearchAgentTool) Name() string {
    return "research_agent"
}

func (t *ResearchAgentTool) Description() string {
    return "Delegate complex research tasks to a specialized research agent"
}

func (t *ResearchAgentTool) InputSchema() tool.ToolSchema {
    return tool.ToolSchema{
        Type: "object",
        Properties: map[string]tool.PropertyDef{
            "topic": {
                Type:        "string",
                Description: "Research topic",
            },
            "depth": {
                Type:        "string",
                Description: "Research depth",
                Enum:        []string{"brief", "detailed", "comprehensive"},
            },
        },
        Required: []string{"topic"},
    }
}

func (t *ResearchAgentTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
    var params struct {
        Topic string `json:"topic"`
        Depth string `json:"depth"`
    }
    json.Unmarshal(input, &params)

    prompt := fmt.Sprintf("Research: %s\nDepth: %s", params.Topic, params.Depth)
    response, err := t.agent.Run(ctx, prompt)
    if err != nil {
        return "", err
    }

    // Extract text from response
    return extractText(response.Message.Content), nil
}
```

---

## Registry Management

### Tool Registry

```go
registry := tool.NewRegistry()

// Register individual tools
registry.Register(tool1)
registry.Register(tool2)

// Register multiple at once
registry.RegisterAll([]tool.Tool{tool3, tool4, tool5})

// Check if tool exists
if registry.Has("my_tool") {
    // ...
}

// Get a tool
myTool, ok := registry.Get("my_tool")

// List all tools
names := registry.List()
count := registry.Count()
```

### Tool Lifecycle

```go
// Tools registered at agent creation
agent, _ := agentpg.New(cfg, agentpg.WithTools(initialTools...))

// Add tools later
agent.RegisterTool(newTool)

// List current tools
tools := agent.GetTools()
```

---

## See Also

- [API Reference](./api-reference.md) - Complete tool API
- [Architecture](./architecture.md) - Tool system design
- [Hooks](./hooks.md) - Tool call hooks
