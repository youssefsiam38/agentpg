# Streaming, Tools and Hooks Example

This example demonstrates AgentPG's streaming architecture, tool system, and observability hooks.

## Streaming Architecture

AgentPG uses **streaming for all API calls**. The `Run()` method internally:
1. Opens an SSE stream to Anthropic's API
2. Processes events (`TextDelta`, `ToolUseStart`, `ContentBlockStop`, etc.) as they arrive
3. Accumulates them into a complete message via the `Accumulator`
4. Returns the final response

This streaming-first approach provides reliability for long contexts and enables automatic tool execution loops.

## Features Demonstrated

1. **Custom Tool Implementation** - Calculator tool with input validation
2. **All Five Hooks** - Complete observability into agent behavior
3. **Auto-Compaction** - Automatic context management enabled
4. **Multi-Step Tool Execution** - Agent uses tools multiple times

## Setup

```bash
# Set environment variables
export DATABASE_URL="postgres://user:password@localhost:5432/agentpg"
export ANTHROPIC_API_KEY="your-api-key"

# Run the example
go run main.go
```

## Tool Implementation

The example includes a calculator tool showing the full `tool.Tool` interface:

```go
type CalculatorTool struct{}

func (c *CalculatorTool) Name() string {
    return "calculator"
}

func (c *CalculatorTool) Description() string {
    return "Performs basic arithmetic operations"
}

func (c *CalculatorTool) InputSchema() tool.ToolSchema {
    return tool.ToolSchema{
        Type: "object",
        Properties: map[string]tool.PropertyDef{
            "operation": {
                Type:        "string",
                Description: "The operation: add, subtract, multiply, divide",
                Enum:        []string{"add", "subtract", "multiply", "divide"},
            },
            "a": {Type: "number", Description: "First number"},
            "b": {Type: "number", Description: "Second number"},
        },
        Required: []string{"operation", "a", "b"},
    }
}

func (c *CalculatorTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
    // Parse input and perform calculation
    // Return result as string
}
```

## Hook System

All five hooks are demonstrated:

### OnBeforeMessage
Called before sending messages to Claude:
```go
agent.OnBeforeMessage(func(ctx context.Context, messages []*types.Message) error {
    fmt.Printf("Sending %d messages to Claude\n", len(messages))
    return nil
})
```

### OnAfterMessage
Called after receiving Claude's response:
```go
agent.OnAfterMessage(func(ctx context.Context, response *types.Response) error {
    fmt.Printf("Usage: %d input, %d output tokens\n",
        response.Usage.InputTokens, response.Usage.OutputTokens)
    return nil
})
```

### OnToolCall
Called when a tool is executed:
```go
agent.OnToolCall(func(ctx context.Context, toolName string, input json.RawMessage, output string, err error) error {
    fmt.Printf("Tool '%s' called with: %s\n", toolName, string(input))
    if err != nil {
        fmt.Printf("Error: %v\n", err)
    } else {
        fmt.Printf("Output: %s\n", output)
    }
    return nil
})
```

### OnBeforeCompaction
Called before context compaction starts:
```go
agent.OnBeforeCompaction(func(ctx context.Context, sessionID string) error {
    fmt.Printf("Compaction starting for session %s\n", sessionID)
    return nil
})
```

### OnAfterCompaction
Called after context compaction completes:
```go
agent.OnAfterCompaction(func(ctx context.Context, result *compaction.CompactionResult) error {
    fmt.Printf("Compacted: %d -> %d tokens\n",
        result.OriginalTokens, result.CompactedTokens)
    return nil
})
```

## Example Output

```
Created session: 550e8400-e29b-41d4-a716-446655440001

=== Example 1: Tool Usage ===
[HOOK] About to send 1 messages to Claude
[HOOK] Tool 'calculator' called
[HOOK] Input: {"operation":"multiply","a":42,"b":1337}
[HOOK] Output: 56154
[HOOK] Received response with 1 content blocks
[HOOK] Usage: 312 input tokens, 89 output tokens

Agent response:
42 multiplied by 1337 equals 56,154.

=== Example 2: Multiple Calculations ===
[HOOK] About to send 3 messages to Claude
[HOOK] Tool 'calculator' called
[HOOK] Input: {"operation":"add","a":100,"b":50}
[HOOK] Output: 150
[HOOK] Tool 'calculator' called
[HOOK] Input: {"operation":"multiply","a":150,"b":2}
[HOOK] Output: 300
...

=== Registered Tools ===
- calculator

=== Demo Complete ===
```

## Architecture Notes

- **Internal Streaming**: `Run()` uses streaming internally for reliability with long contexts
- **Automatic Tool Loop**: Agent automatically executes tools and continues the conversation
- **Hook Ordering**: Hooks are called in registration order
- **Error Handling**: Return an error from a hook to abort the operation

## Key Concepts

### Tool Registration
```go
agent.RegisterTool(&CalculatorTool{})
```

### Auto-Compaction
```go
agentpg.WithAutoCompaction(true)  // Enable in options
```

### Getting Registered Tools
```go
tools := agent.GetTools()  // Returns []string of tool names
```

## Next Steps

- See `examples/basic/` for simpler agent usage
- See `examples/custom_tools/` for more tool patterns
- See `examples/context_compaction/` for compaction strategies
- See `examples/advanced/02_observability/` for production logging patterns
