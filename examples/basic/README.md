# Basic Examples

This directory contains basic examples demonstrating core AgentPG features.

## Examples

### 01_simple_chat

The simplest AgentPG example - a single agent with no tools.

```bash
go run ./01_simple_chat/
```

**Shows:**
- Agent registration with `client.RegisterAgent()`
- Client creation and startup with `client.Start()`
- Session creation and running prompts with `client.RunSync()`

### 02_shared_tools

Demonstrates sharing tools across multiple agents.

```bash
go run ./02_shared_tools/
```

**Shows:**
- Per-client tool registration with `client.RegisterTool()`
- Referencing tools by name in `AgentDefinition.Tools`
- Different agents with different tool subsets:
  - `general-assistant`: all tools (get_time, calculator, get_weather)
  - `math-tutor`: calculator + get_time
  - `weather-bot`: get_weather + get_time
- Client lifecycle with `client.Start()` and `client.Stop()`

## Setup

1. Set environment variables:

```bash
export ANTHROPIC_API_KEY="your-api-key"
export DATABASE_URL="postgresql://user:password@localhost:5432/dbname?sslmode=disable"
```

2. Apply migrations (if not already done):

```bash
psql $DATABASE_URL -f ../../storage/migrations/001_agentpg_migration.up.sql
```

3. Run an example:

```bash
go run ./01_simple_chat/
go run ./02_shared_tools/
```

## Key Patterns

### New Per-Client Registration Pattern

```go
// Create client
client, err := agentpg.NewClient(drv, &agentpg.ClientConfig{
    APIKey: os.Getenv("ANTHROPIC_API_KEY"),
})

// Register agents BEFORE calling Start()
client.RegisterAgent(&agentpg.AgentDefinition{
    Name:         "assistant",
    Model:        "claude-sonnet-4-5-20250929",
    SystemPrompt: "You are a helpful assistant.",
    Tools:        []string{"calculator", "get_time"}, // Optional: only if tools needed
})

// Register tools BEFORE calling Start()
client.RegisterTool(&CalculatorTool{})
client.RegisterTool(&GetTimeTool{})

// Start client (begins processing)
if err := client.Start(ctx); err != nil {
    log.Fatal(err)
}
defer client.Stop(context.Background())

// Create session and run
sessionID, _ := client.NewSession(ctx, nil, nil)
response, err := client.RunSync(ctx, sessionID, "assistant", "What is 2+2?")
```

## Next Steps

- See `examples/nested_agents/` for agent-as-tool patterns
- See `examples/distributed/` for multi-worker setups
- See `examples/extended_context/` for context compaction
