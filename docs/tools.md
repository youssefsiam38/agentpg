# Tools Guide

Tools allow agents to perform actions, access external services, and interact with your application. This guide covers everything you need to build custom tools for AgentPG.

## Table of Contents

1. [Tool Interface](#tool-interface)
2. [Quick Start with FuncTool](#quick-start-with-functool)
3. [Struct-Based Tools](#struct-based-tools)
4. [Schema Design](#schema-design)
5. [Error Handling](#error-handling)
6. [Retry Configuration](#retry-configuration)
7. [Database-Aware Tools](#database-aware-tools)
8. [Run Variables (Tool Context)](#run-variables-tool-context)
9. [Best Practices](#best-practices)
10. [Examples](#examples)

---

## Tool Interface

Every tool must implement the `Tool` interface from the `tool` package:

```go
import "github.com/youssefsiam38/agentpg/tool"

type Tool interface {
    Name() string
    Description() string
    InputSchema() ToolSchema
    Execute(ctx context.Context, input json.RawMessage) (string, error)
}
```

| Method | Purpose |
|--------|---------|
| `Name()` | Unique identifier used by agents to call the tool |
| `Description()` | Explains what the tool does (shown to Claude) |
| `InputSchema()` | JSON Schema defining the parameters |
| `Execute()` | Implementation that receives JSON input and returns a string result |

---

## Quick Start with FuncTool

For simple, stateless tools, use `tool.NewFuncTool()` to avoid boilerplate:

```go
import "github.com/youssefsiam38/agentpg/tool"

func createTimeTool() tool.Tool {
    return tool.NewFuncTool(
        "get_time",                                    // name
        "Get the current time in a specified timezone", // description
        tool.ToolSchema{                               // schema
            Type: "object",
            Properties: map[string]tool.PropertyDef{
                "timezone": {
                    Type:        "string",
                    Description: "Timezone name (e.g., 'America/New_York', 'UTC')",
                },
            },
            Required: []string{"timezone"},
        },
        func(ctx context.Context, input json.RawMessage) (string, error) {
            var params struct {
                Timezone string `json:"timezone"`
            }
            if err := json.Unmarshal(input, &params); err != nil {
                return "", err
            }

            loc, err := time.LoadLocation(params.Timezone)
            if err != nil {
                return "", fmt.Errorf("invalid timezone: %w", err)
            }

            return time.Now().In(loc).Format(time.RFC3339), nil
        },
    )
}

// Register it
client.RegisterTool(createTimeTool())
```

---

## Struct-Based Tools

For tools that need state, configuration, or shared resources, use a struct:

```go
type WeatherTool struct {
    apiKey      string
    defaultUnit string
    cache       map[string]weatherData
}

func NewWeatherTool(apiKey string) *WeatherTool {
    return &WeatherTool{
        apiKey:      apiKey,
        defaultUnit: "celsius",
        cache:       make(map[string]weatherData),
    }
}

func (w *WeatherTool) Name() string {
    return "get_weather"
}

func (w *WeatherTool) Description() string {
    return "Get current weather conditions for a location"
}

func (w *WeatherTool) InputSchema() tool.ToolSchema {
    return tool.ToolSchema{
        Type: "object",
        Properties: map[string]tool.PropertyDef{
            "location": {
                Type:        "string",
                Description: "City name or coordinates",
            },
            "unit": {
                Type:        "string",
                Description: "Temperature unit",
                Enum:        []string{"celsius", "fahrenheit"},
            },
        },
        Required: []string{"location"},
    }
}

func (w *WeatherTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
    var params struct {
        Location string `json:"location"`
        Unit     string `json:"unit"`
    }
    if err := json.Unmarshal(input, &params); err != nil {
        return "", fmt.Errorf("invalid input: %w", err)
    }

    if params.Unit == "" {
        params.Unit = w.defaultUnit
    }

    // Check cache first
    if cached, ok := w.cache[params.Location]; ok {
        return formatWeather(cached, params.Unit), nil
    }

    // Fetch from API using w.apiKey
    data, err := w.fetchWeather(ctx, params.Location)
    if err != nil {
        return "", err
    }

    w.cache[params.Location] = data
    return formatWeather(data, params.Unit), nil
}
```

Register the tool:

```go
client.RegisterTool(NewWeatherTool(os.Getenv("WEATHER_API_KEY")))
```

---

## Schema Design

The `ToolSchema` defines what parameters your tool accepts. Claude uses this to understand how to call your tool.

### Basic Schema Structure

```go
type ToolSchema struct {
    Type        string                  // Must be "object"
    Properties  map[string]PropertyDef  // Parameter definitions
    Required    []string                // Required parameter names
    Description string                  // Additional context
}
```

### PropertyDef Options

```go
type PropertyDef struct {
    Type        string                  // "string", "number", "integer", "boolean", "array", "object"
    Description string                  // Explain the property

    // String constraints
    Enum        []string                // Allowed values
    MinLength   *int                    // Minimum length
    MaxLength   *int                    // Maximum length
    Pattern     string                  // Regex pattern

    // Numeric constraints
    Minimum     *float64                // Minimum value
    Maximum     *float64                // Maximum value

    // Array constraints
    Items       *PropertyDef            // Item type for arrays
    MinItems    *int
    MaxItems    *int

    // Nested object constraints
    Properties  map[string]PropertyDef
    Required    []string

    // Default value
    Default     any
}
```

### Complete Schema Example

```go
func (t *TaskTool) InputSchema() tool.ToolSchema {
    minTitleLen := 3
    maxTitleLen := 100
    minScore := 0.0
    maxScore := 100.0
    maxTags := 5

    return tool.ToolSchema{
        Type: "object",
        Properties: map[string]tool.PropertyDef{
            // String with length constraints
            "title": {
                Type:        "string",
                Description: "Task title",
                MinLength:   &minTitleLen,
                MaxLength:   &maxTitleLen,
            },
            // Enum constraint
            "priority": {
                Type:        "string",
                Description: "Task priority level",
                Enum:        []string{"low", "medium", "high", "critical"},
            },
            // Numeric range
            "score": {
                Type:        "number",
                Description: "Confidence score (0-100)",
                Minimum:     &minScore,
                Maximum:     &maxScore,
            },
            // Array of strings
            "tags": {
                Type:        "array",
                Description: "Labels for the task",
                Items:       &tool.PropertyDef{Type: "string"},
                MaxItems:    &maxTags,
            },
            // Nested object
            "assignee": {
                Type:        "object",
                Description: "Person assigned to the task",
                Properties: map[string]tool.PropertyDef{
                    "name":  {Type: "string", Description: "Full name"},
                    "email": {Type: "string", Description: "Email address"},
                },
                Required: []string{"name"},
            },
        },
        Required: []string{"title", "priority"},
    }
}
```

---

## Error Handling

Tools can return three special error types to control retry behavior:

### ToolCancel - Unrecoverable Error

Use when the error cannot be fixed by retrying (authentication failures, permission denied):

```go
import "github.com/youssefsiam38/agentpg/tool"

func (t *MyTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
    if !isAuthenticated(ctx) {
        return "", tool.ToolCancel(errors.New("authentication failed"))
    }
    // ...
}
```

### ToolDiscard - Invalid Input

Use when the input is fundamentally invalid:

```go
func (t *MyTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
    var params struct {
        UserID string `json:"user_id"`
    }
    if err := json.Unmarshal(input, &params); err != nil {
        return "", tool.ToolDiscard(fmt.Errorf("invalid JSON: %w", err))
    }
    // ...
}
```

### ToolSnooze - Temporary Delay

Use for transient failures like rate limits. Does NOT consume a retry attempt:

```go
func (t *MyTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
    result, err := callExternalAPI(ctx)
    if isRateLimited(err) {
        return "", tool.ToolSnooze(30*time.Second, err)
    }
    if err != nil {
        return "", err // Regular error - will retry
    }
    return result, nil
}
```

### Error Behavior Summary

| Error Type | Retries | Use Case |
|------------|---------|----------|
| Regular `error` | Yes (up to MaxAttempts) | Transient failures |
| `ToolCancel` | No | Unrecoverable errors |
| `ToolDiscard` | No | Invalid input |
| `ToolSnooze` | Yes (unlimited) | Rate limits, temporary unavailability |

---

## Retry Configuration

By default, tools retry instantly with 2 attempts (1 retry on failure):

```go
// Default configuration
ToolRetryConfig: &agentpg.ToolRetryConfig{
    MaxAttempts: 2,    // 1 retry
    Jitter:      0.0,  // Instant retry
}
```

For unreliable external services, configure exponential backoff:

```go
client, _ := agentpg.NewClient(drv, &agentpg.ClientConfig{
    APIKey: apiKey,
    ToolRetryConfig: &agentpg.ToolRetryConfig{
        MaxAttempts: 5,    // More attempts
        Jitter:      0.1,  // 10% jitter to prevent thundering herd
    },
})
```

**Backoff delays** (when Jitter > 0):

| Attempt | Delay |
|---------|-------|
| 1 | 1 second |
| 2 | 16 seconds |
| 3 | 81 seconds |
| 4 | 256 seconds |
| 5 | 625 seconds |

---

## Database-Aware Tools

Tools can access databases and external services via struct fields:

```go
type UserLookupTool struct {
    db *pgxpool.Pool
}

func NewUserLookupTool(db *pgxpool.Pool) *UserLookupTool {
    return &UserLookupTool{db: db}
}

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
    var params struct {
        UserID string `json:"user_id"`
    }
    if err := json.Unmarshal(input, &params); err != nil {
        return "", tool.ToolDiscard(err)
    }

    var name, email string
    err := t.db.QueryRow(ctx,
        "SELECT name, email FROM users WHERE id = $1",
        params.UserID,
    ).Scan(&name, &email)

    if errors.Is(err, pgx.ErrNoRows) {
        return "User not found", nil
    }
    if err != nil {
        return "", err // Will retry
    }

    return fmt.Sprintf("User: %s <%s>", name, email), nil
}

// Register with database connection
client.RegisterTool(NewUserLookupTool(pool))
```

---

## Run Variables (Tool Context)

Tools can access per-run variables passed when creating a run. This is useful for passing context like `storyId`, `tenantId`, `userId`, etc. without hardcoding them in the tool.

### Passing Variables

Variables are passed as the last parameter to Run methods:

```go
// Pass variables when creating a run
response, _ := client.RunSync(ctx, sessionID, agent.ID, "Continue the story", map[string]any{
    "story_id":  "story-123",
    "tenant_id": "tenant-1",
    "user_id":   "user-456",
})

// Without variables, pass nil
response, _ := client.RunSync(ctx, sessionID, agent.ID, "Hello!", nil)
```

### Accessing Variables in Tools

Use the `tool` package helpers to access variables:

```go
import "github.com/youssefsiam38/agentpg/tool"

type StoryTool struct {
    db *pgxpool.Pool
}

func (t *StoryTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
    // Get a typed variable (returns value, ok)
    storyID, ok := tool.GetVariable[string](ctx, "story_id")
    if !ok {
        return "", errors.New("story_id not provided in run variables")
    }

    // Get with default value
    maxChapters := tool.GetVariableOr[int](ctx, "max_chapters", 10)

    // Get or panic (use when variable is guaranteed)
    tenantID := tool.MustGetVariable[string](ctx, "tenant_id")

    // Get all variables
    vars := tool.GetVariables(ctx)

    // Get full run context
    runCtx, ok := tool.GetRunContext(ctx)
    if ok {
        fmt.Printf("Run: %s, Session: %s\n", runCtx.RunID, runCtx.SessionID)
    }

    // Use in database query
    var content string
    err := t.db.QueryRow(ctx,
        "SELECT content FROM chapters WHERE story_id = $1 AND tenant_id = $2 LIMIT $3",
        storyID, tenantID, maxChapters,
    ).Scan(&content)

    return content, err
}
```

### Context Helper Functions

| Function | Description |
|----------|-------------|
| `tool.GetVariable[T](ctx, key)` | Get typed variable, returns `(value, ok)` |
| `tool.GetVariableOr[T](ctx, key, default)` | Get variable or return default value |
| `tool.MustGetVariable[T](ctx, key)` | Get variable or panic if not found |
| `tool.GetVariables(ctx)` | Get all variables as `map[string]any` |
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

// When manager delegates to researcher agent via AgentIDs,
// the researcher's tools also receive project_id
```

---

## Best Practices

### Schema Design

1. **Be explicit about required fields** - List all mandatory parameters
2. **Use Enum for constrained choices** - Helps Claude make valid selections
3. **Provide clear descriptions** - Claude uses these to decide when to use the tool
4. **Use typed constraints** - Min/Max, Length limits improve reliability

### Error Handling

1. **Use ToolCancel for auth failures** - Don't waste retries on permission errors
2. **Use ToolDiscard for bad input** - Fail fast on invalid data
3. **Use ToolSnooze for rate limits** - Preserves retry attempts
4. **Return useful error messages** - They're shown to Claude

### Performance

1. **Keep tools fast** - They block during tool iterations
2. **Implement caching** - For expensive external calls
3. **Respect context cancellation** - Check `ctx.Done()` in long operations
4. **Use appropriate timeouts** - Tool execution has a 5-minute timeout

### Registration

1. **Register tools before `Start()`** - Tools can't be added after starting
2. **Assign tools explicitly** - Use the `Tools` field in AgentDefinition
3. **Share tools across agents** - Register once, reference in multiple agents

---

## Examples

The `/examples/custom_tools/` directory contains complete working examples:

| Example | Description |
|---------|-------------|
| `01_struct_tool/` | Struct-based tool with state and configuration |
| `02_func_tool/` | Quick tool creation with `NewFuncTool()` |
| `03_schema_validation/` | Advanced schema with all constraint types |
| `04_parallel_execution/` | Multiple tools executing concurrently |

The `/examples/retry_rescue/` directory covers error handling:

| Example | Description |
|---------|-------------|
| `01_instant_retry/` | Default instant retry behavior |
| `02_error_types/` | Using ToolCancel, ToolDiscard, ToolSnooze |
| `03_exponential_backoff/` | Configuring backoff for external APIs |

### Running Examples

```bash
export ANTHROPIC_API_KEY="your-api-key"
export DATABASE_URL="postgresql://user:password@localhost:5432/agentpg"

# Run any example
go run examples/custom_tools/01_struct_tool/main.go
```

---

## Next Steps

- [Configuration](./configuration.md) - Tune retry settings and concurrency
- [Architecture](./architecture.md) - Understand the tool execution flow
- [Distributed Workers](./distributed.md) - Scale tool execution across instances
