# Function-Based Tool Example

This example demonstrates how to create tools using `tool.NewFuncTool()` for simpler, inline tool definitions.

## When to Use Function-Based Tools

Use this pattern when:
- Your tool logic is **simple and stateless**
- You don't need to share state between tool calls
- You want **quick, inline tool definitions**
- The tool is **self-contained**

## NewFuncTool Signature

```go
func NewFuncTool(
    name string,
    description string,
    schema ToolSchema,
    fn func(context.Context, json.RawMessage) (string, error),
) Tool
```

## Key Patterns Demonstrated

### 1. Simple Tool Creation
```go
timeTool := tool.NewFuncTool(
    "get_time",                          // Name
    "Get the current time in a timezone", // Description
    tool.ToolSchema{                      // Schema
        Type: "object",
        Properties: map[string]tool.PropertyDef{
            "timezone": {Type: "string", Description: "..."},
        },
        Required: []string{"timezone"},
    },
    func(ctx context.Context, input json.RawMessage) (string, error) {
        // Implementation
    },
)
```

### 2. Multiple Tools
```go
// Register multiple func-based tools
agent.RegisterTool(timeTool)
agent.RegisterTool(dateDiffTool)
```

### 3. Inline Input Parsing
```go
var params struct {
    Timezone string `json:"timezone"`
    Format   string `json:"format"`
}
if err := json.Unmarshal(input, &params); err != nil {
    return "", fmt.Errorf("invalid input: %w", err)
}
```

## Tools Created in This Example

| Tool | Description |
|------|-------------|
| `get_time` | Get current time in a timezone with 12h/24h format |
| `calculate_date_diff` | Calculate difference between two dates |

## Running the Example

```bash
export ANTHROPIC_API_KEY="your-api-key"
export DATABASE_URL="postgresql://user:pass@localhost:5432/agentpg"

cd examples/custom_tools/02_func_tool
go run main.go
```

## Expected Output

```
Created session: 550e8400-e29b-41d4-a716-446655440000

=== Example 1: Get Time in Tokyo ===
The current time in Tokyo (JST) is 14:30:25 on Friday, December 6, 2024.

=== Example 2: Time in 12-hour Format ===
It's currently 12:30:25 AM in New York (EST).

=== Example 3: Date Difference ===
The difference between 2024-01-01 and 2024-12-31 is 52.1 weeks (365 days).

=== Registered Tools ===
- get_time
- calculate_date_diff

=== Demo Complete ===
```

## Comparison: Struct vs Function-Based Tools

| Aspect | Struct-Based | Function-Based |
|--------|--------------|----------------|
| State | Can hold internal state | Stateless |
| Complexity | More boilerplate | Less boilerplate |
| Testing | Easier to mock | Inline functions harder to test |
| Use Case | Complex, stateful tools | Simple, one-off tools |

## Next Steps

- See [01_struct_tool](../01_struct_tool/) for stateful tool patterns
- See [03_schema_validation](../03_schema_validation/) for advanced input validation
