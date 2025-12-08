# Basic Examples

This directory contains basic examples demonstrating core AgentPG features.

## Examples

### 01_simple_chat

The simplest AgentPG example - a single agent with no tools.

```bash
go run ./01_simple_chat/
```

**Shows:**
- Agent registration with `agentpg.MustRegister()`
- Client creation with `agentpg.NewClient()`
- Session creation and running prompts

### 02_shared_tools

Demonstrates sharing tools across multiple agents.

```bash
go run ./02_shared_tools/
```

**Shows:**
- Global tool registration with `agentpg.MustRegisterTool()`
- Referencing tools by name in `AgentDefinition.Tools`
- Different agents with different tool subsets:
  - `general-assistant`: all tools (get_time, calculator, get_weather)
  - `math-tutor`: calculator + get_time
  - `weather-bot`: get_weather + get_time

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

## Next Steps

- See `examples/custom_tools/` for more tool patterns
- See `examples/nested_agents/` for agent-as-tool patterns
- See `examples/streaming/` for streaming responses
