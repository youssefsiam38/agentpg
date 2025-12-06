# Struct-Based Tool Example

This example demonstrates how to implement the `tool.Tool` interface using a struct-based approach.

## When to Use Struct-Based Tools

Use this pattern when your tool needs:
- **Internal state** (API keys, database connections, configuration)
- **Complex initialization** (loading data, setting up connections)
- **Shared resources** between methods
- **Testability** (easy to mock and inject dependencies)

## Tool Interface

```go
type Tool interface {
    Name() string                                                    // Tool identifier
    Description() string                                             // Human-readable description
    InputSchema() ToolSchema                                         // JSON Schema for parameters
    Execute(ctx context.Context, input json.RawMessage) (string, error)  // Execution logic
}
```

## Key Patterns Demonstrated

### 1. Constructor Pattern
```go
func NewWeatherTool() *WeatherTool {
    return &WeatherTool{
        defaultUnit: "celsius",
        locations:   loadLocations(),
    }
}
```

### 2. Input Schema Definition
```go
func (w *WeatherTool) InputSchema() tool.ToolSchema {
    return tool.ToolSchema{
        Type: "object",
        Properties: map[string]tool.PropertyDef{
            "city": {
                Type:        "string",
                Description: "City name",
            },
            "unit": {
                Type: "string",
                Enum: []string{"celsius", "fahrenheit"},
            },
        },
        Required: []string{"city"},
    }
}
```

### 3. Input Parsing and Validation
```go
func (w *WeatherTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
    var params struct {
        City string `json:"city"`
        Unit string `json:"unit"`
    }
    if err := json.Unmarshal(input, &params); err != nil {
        return "", fmt.Errorf("invalid input: %w", err)
    }
    // ... validation and logic
}
```

## Running the Example

```bash
export ANTHROPIC_API_KEY="your-api-key"
export DATABASE_URL="postgresql://user:pass@localhost:5432/agentpg"

cd examples/custom_tools/01_struct_tool
go run main.go
```

## Expected Output

```
Created session: 550e8400-e29b-41d4-a716-446655440000

=== Example 1: Basic Weather Query ===
The weather in Tokyo is currently sunny with a temperature of 28.0°C and 70% humidity.

=== Example 2: Weather with Fahrenheit ===
In New York, it's currently 72.5°F (22.5°C) with partly cloudy conditions and 65% humidity.

=== Example 3: Unknown City ===
The weather in Reykjavik shows a temperature of 18.3°C with cloudy conditions.

=== Registered Tools ===
- get_weather

=== Demo Complete ===
```

## Next Steps

- See [02_func_tool](../02_func_tool/) for a simpler approach to tool creation
- See [03_schema_validation](../03_schema_validation/) for advanced input validation
