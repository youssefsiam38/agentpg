# Basic Agent Example

This example demonstrates the simplest use case of the AgentPG package.

## Setup

1. Apply database migrations:

```bash
# Using psql
psql -U myuser -d mydb -f ../../storage/migrations/001_agentpg_migration.up.sql
```

2. Set environment variables:

```bash
export ANTHROPIC_API_KEY="your-api-key"
export DATABASE_URL="postgresql://user:password@localhost:5432/dbname?sslmode=disable"
```

3. Run the example:

```bash
go run main.go
```

## What This Example Shows

- Creating an agent with required configuration
- Using functional options (WithMaxTokens, WithTemperature)
- Creating a new session
- Running the agent with a simple prompt
- Accessing the response content and usage stats

## Output

You should see:
- The session ID
- The agent's response
- Token usage statistics
- Stop reason

## Key Features Demonstrated

1. **Streaming-first**: Run() uses streaming internally for long context support
2. **Automatic session management**: The agent manages conversation history
3. **PostgreSQL persistence**: All messages are saved to the database
4. **Simple API**: Just create, configure, and run

## Next Steps

- See `examples/custom_tools/` for tool usage
- See `examples/nested_agents/` for agent-as-tool patterns
- See `examples/streaming/` for explicit streaming events
