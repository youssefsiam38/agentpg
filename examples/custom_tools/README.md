# Custom Tools Examples

This directory contains examples demonstrating how to create and use custom tools with AgentPG.

## Examples

| Example | Description |
|---------|-------------|
| [01_struct_tool](./01_struct_tool/) | Implement the Tool interface with a struct-based tool |
| [02_func_tool](./02_func_tool/) | Create tools using `NewFuncTool()` for simple cases |
| [03_schema_validation](./03_schema_validation/) | Advanced JSON Schema with constraints (enum, min/max, arrays) |
| [04_parallel_execution](./04_parallel_execution/) | Tool registry and parallel tool execution |

## Learning Path

1. Start with **01_struct_tool** to understand the Tool interface
2. Move to **02_func_tool** for simpler tool creation patterns
3. Explore **03_schema_validation** for input validation features
4. Learn **04_parallel_execution** for advanced registry and execution patterns

## Tool Interface

All tools must implement the `tool.Tool` interface:

```go
type Tool interface {
    Name() string
    Description() string
    InputSchema() ToolSchema
    Execute(ctx context.Context, input json.RawMessage) (string, error)
}
```

## Prerequisites

- PostgreSQL database running
- Environment variables set:
  - `ANTHROPIC_API_KEY`
  - `DATABASE_URL`

## Running Examples

```bash
cd examples/custom_tools/01_struct_tool
go run main.go
```
