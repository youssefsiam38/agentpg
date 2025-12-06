# Parallel Execution Example

This example demonstrates the tool registry and executor patterns in AgentPG, including parallel tool execution.

## Features Demonstrated

1. **tool.Registry** - Managing collections of tools
2. **tool.Executor** - Different execution modes
3. **Parallel Execution** - Running multiple tools concurrently
4. **Timeout Configuration** - Setting execution timeouts

## Registry Operations

```go
// Create registry
registry := tool.NewRegistry()

// Register single tool
registry.Register(myTool)

// Register multiple tools
registry.RegisterAll([]tool.Tool{tool1, tool2, tool3})

// Query registry
registry.Count()           // Number of registered tools
registry.Has("toolName")   // Check if tool exists
registry.Get("toolName")   // Get specific tool
registry.List()            // List all tool names
```

## Executor Modes

### 1. Single Execution
```go
executor := tool.NewExecutor(registry)
result := executor.Execute(ctx, "toolName", input)
```

### 2. Sequential Execution (ExecuteMultiple)
```go
calls := []tool.ToolCallRequest{
    {ID: "1", ToolName: "tool1", Input: input1},
    {ID: "2", ToolName: "tool2", Input: input2},
}
results := executor.ExecuteMultiple(ctx, calls)
```

### 3. Parallel Execution (ExecuteParallel)
```go
results := executor.ExecuteParallel(ctx, calls)
```

### 4. Batch Execution (configurable)
```go
results := executor.ExecuteBatch(ctx, calls, true)  // parallel=true
results := executor.ExecuteBatch(ctx, calls, false) // parallel=false
```

## ExecuteResult Structure

```go
type ExecuteResult struct {
    ID       string        // Request ID
    ToolName string        // Tool that was executed
    Input    json.RawMessage
    Output   string        // Tool output
    Error    error         // Any error
    Duration time.Duration // Execution time
}
```

## Timeout Configuration

```go
executor.SetDefaultTimeout(5 * time.Second)
```

## Performance Comparison

```
Sequential execution:
  call-1: result (10ms)
  call-2: result (15ms)
  call-3: result (5ms)
  Total time: 30ms

Parallel execution:
  call-1: result (10ms)
  call-2: result (15ms)
  call-3: result (5ms)
  Total time: 15ms
  Speedup: 2.00x faster
```

## Running the Example

```bash
export ANTHROPIC_API_KEY="your-api-key"
export DATABASE_URL="postgresql://user:pass@localhost:5432/agentpg"

cd examples/custom_tools/04_parallel_execution
go run main.go
```

## Expected Output

```
=== Part 1: Registry Management ===

Tool count: 3
Has 'analyzer': true
Has 'unknown': false
Registered tools: [analyzer transformer validator]
Got tool: analyzer - Analyze data

=== Part 2: Executor Modes ===

Sequential execution:
  call-1: [analyzer] Processed 'test1': analysis complete (10ms)
  call-2: [transformer] Processed 'test2': transformation done (15ms)
  call-3: [validator] Processed 'test3': validation passed (5ms)
  Total time: 30ms

Parallel execution:
  call-1: [analyzer] Processed 'test1': analysis complete (10ms)
  call-2: [transformer] Processed 'test2': transformation done (15ms)
  call-3: [validator] Processed 'test3': validation passed (5ms)
  Total time: 15ms
  Speedup: 2.00x faster

Batch execution (parallel=true):
  Processed 3 calls in 16ms

=== Part 3: AgentPG Integration ===

Created session: 550e8400-e29b-41d4-a716-446655440000

Requesting data from multiple sources...
I've fetched the data for user-123 from all three sources...

=== Tool Call Log ===
1. database:user-123
2. cache:user-123
3. api:user-123

=== Registered Tools ===
- fetch_data

=== Demo Complete ===
```

## When to Use Parallel Execution

| Scenario | Recommended Mode |
|----------|------------------|
| Independent data fetches | Parallel |
| Order-dependent operations | Sequential |
| Rate-limited APIs | Sequential/Batch |
| High-throughput processing | Parallel with timeout |

## Best Practices

1. **Use parallel for independent operations** - No shared state or dependencies
2. **Set appropriate timeouts** - Prevent hanging on slow tools
3. **Handle errors gracefully** - Check each result for errors
4. **Consider rate limits** - External APIs may require sequential calls
5. **Log execution times** - Monitor for performance issues

## Next Steps

- See [nested_agents](../../nested_agents/) for agent delegation patterns
- See [advanced/02_observability](../../advanced/02_observability/) for monitoring tool calls
