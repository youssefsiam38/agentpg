# AgentPG Examples

This directory contains comprehensive examples demonstrating various features of AgentPG.

## Client API

AgentPG provides a **Client API** for multi-instance deployment with per-client registration:

- **Per-Client Registration**: Register agents and tools on each client instance
- **Instance Management**: Automatic heartbeats and instance tracking
- **Leader Election**: Coordinated cleanup via single leader
- **Multi-Instance**: Run multiple instances for high availability

### Recommended Pattern

```go
func main() {
    // Create client
    client, _ := agentpg.NewClient(drv, &agentpg.ClientConfig{
        APIKey: apiKey,
    })

    // Register agents on client (no global state)
    client.RegisterAgent(&agentpg.AgentDefinition{
        Name:         "chat",
        Model:        "claude-sonnet-4-5-20250929",
        SystemPrompt: "You are a helpful assistant",
    })

    // Register tools on client
    client.RegisterTool(&CalculatorTool{})

    // Start client (begins background services)
    client.Start(ctx)
    defer client.Stop(ctx)

    // Create session and run
    sessionID, _ := client.NewSession(ctx, "tenant1", "user1", nil, nil)
    response, _ := client.RunSync(ctx, sessionID, "chat", "Hello!")
}
```

---

## Prerequisites

Before running any example, ensure you have:

1. **PostgreSQL database** - Required for storing conversation history
2. **Anthropic API key** - Get one from https://console.anthropic.com/

Set the following environment variables:

```bash
export ANTHROPIC_API_KEY="your-api-key-here"
export DATABASE_URL="postgresql://user:password@localhost:5432/agentpg"
```

---

## Example Categories

### basic/
**Location**: `examples/basic/`

**Client API** - The recommended pattern for new projects:
- Per-client agent registration with `client.RegisterAgent()`
- Per-client tool registration with `client.RegisterTool()`
- Session and run management via client methods

```bash
go run examples/basic/01_simple_chat/main.go
go run examples/basic/02_shared_tools/main.go
```

---

### distributed/
**Location**: `examples/distributed/main.go`

**Multi-Instance Deployment** - Full example with all features:
- Per-client agent and tool registration
- Client lifecycle (Start/Stop)
- Leader election callbacks
- Instance metadata
- Tool usage with calculator

```bash
go run examples/distributed/main.go
```

---

### database_sql/
**Location**: `examples/database_sql/main.go`

**database/sql Driver** - Using standard library:
- Standard library database/sql package
- Compatible with any database/sql driver (lib/pq, pgx stdlib)
- Transaction support with RunTx

```bash
go run examples/database_sql/main.go
```

---

### custom_tools/
Comprehensive tool development patterns.

| Example | Description |
|---------|-------------|
| `01_struct_tool/` | Full `tool.Tool` interface with struct, internal state, error handling |
| `02_func_tool/` | Quick tool creation with `tool.NewFuncTool()` |
| `03_schema_validation/` | All schema constraints: Enum, Min/Max, MinLength/MaxLength, Items, nested Properties |
| `04_parallel_execution/` | Tool registry, executor with `ExecuteParallel` and `ExecuteBatch` |

```bash
# Run any example:
go run examples/custom_tools/01_struct_tool/main.go
go run examples/custom_tools/02_func_tool/main.go
go run examples/custom_tools/03_schema_validation/main.go
go run examples/custom_tools/04_parallel_execution/main.go
```

---

### nested_agents/
Agent delegation and orchestration patterns.

| Example | Description |
|---------|-------------|
| `01_basic_delegation/` | Basic agent delegation - research agent delegated from main agent using `Agents` field |
| `02_specialist_agents/` | Multiple specialist agents (coder, researcher, analyst) with own tools |
| `03_multi_level_hierarchy/` | 3-level hierarchy: manager -> team leads -> workers |

```bash
go run examples/nested_agents/01_basic_delegation/main.go
go run examples/nested_agents/02_specialist_agents/main.go
go run examples/nested_agents/03_multi_level_hierarchy/main.go
```

---

### context_compaction/
Context management and compaction strategies.

| Example | Description |
|---------|-------------|
| `01_auto_compaction/` | Auto compaction via AgentDefinition.Config |
| `02_manual_compaction/` | Explicit compaction control with `Compact()` method |
| `03_custom_strategy/` | Implement custom `compaction.Strategy` interface |
| `04_compaction_monitoring/` | Compaction hooks and metrics |

```bash
go run examples/context_compaction/01_auto_compaction/main.go
go run examples/context_compaction/02_manual_compaction/main.go
go run examples/context_compaction/03_custom_strategy/main.go
go run examples/context_compaction/04_compaction_monitoring/main.go
```

---

### extended_context/
**Location**: `examples/extended_context/main.go`

Extended context window (1M tokens):
- Extended context via AgentDefinition.Config
- Automatic fallback and retry logic
- Processing very long documents

```bash
go run examples/extended_context/main.go
```

---

### advanced/
Production-ready patterns and integrations.

| Example | Description |
|---------|-------------|
| `01_multi_tenant/` | HTTP server, tenant isolation, session management per user |
| `02_observability/` | All 5 hooks with structured logging, metrics simulation |
| `03_cost_tracking/` | Token-to-cost calculation, per-session tracking, budget alerts |
| `04_rate_limiting/` | OnBeforeMessage hook for rate limiting, token bucket pattern |
| `05_database_tool/` | Safe SQL query tool, SELECT-only validation, result formatting |
| `06_http_api_tool/` | Generic HTTP client tool, timeout handling, response parsing |
| `07_pii_filtering/` | PII detection with regex patterns, blocking/logging modes |
| `08_error_recovery/` | Error handling patterns, graceful degradation |

```bash
go run examples/advanced/01_multi_tenant/main.go
go run examples/advanced/02_observability/main.go
go run examples/advanced/03_cost_tracking/main.go
go run examples/advanced/04_rate_limiting/main.go
go run examples/advanced/05_database_tool/main.go
go run examples/advanced/06_http_api_tool/main.go
go run examples/advanced/07_pii_filtering/main.go
go run examples/advanced/08_error_recovery/main.go
```

---

## Feature Coverage Matrix

| Feature | Example Location |
|---------|------------------|
| **Per-Client Registration** | all examples |
| **Multi-instance** | distributed/ |
| **Leader election** | distributed/ |
| Agent creation & config | all examples |
| Session management | basic/, advanced/01_multi_tenant |
| Tool interface (struct) | custom_tools/01_struct_tool, distributed/ |
| NewFuncTool | custom_tools/02_func_tool |
| Schema validation | custom_tools/03_schema_validation |
| Tool registry & executor | custom_tools/04_parallel_execution |
| Agent delegation (Agents field) | nested_agents/01_basic_delegation |
| Specialist agents | nested_agents/02_specialist_agents |
| Multi-level hierarchy | nested_agents/03_multi_level_hierarchy |
| Auto compaction | context_compaction/01_auto_compaction |
| Manual compaction | context_compaction/02_manual_compaction |
| Custom strategy | context_compaction/03_custom_strategy |
| Compaction hooks | context_compaction/04_compaction_monitoring |
| Extended context (1M) | extended_context/ |
| Multi-tenant | advanced/01_multi_tenant |
| Observability hooks | advanced/02_observability |
| Cost tracking | advanced/03_cost_tracking |
| Rate limiting | advanced/04_rate_limiting |
| Database tool | advanced/05_database_tool |
| HTTP API tool | advanced/06_http_api_tool |
| PII filtering | advanced/07_pii_filtering |
| Error recovery | advanced/08_error_recovery |

---

## Configuration Options

### Client Config

```go
client, _ := agentpg.NewClient(drv, &agentpg.ClientConfig{
    APIKey:            apiKey,
    ID:                "instance-1",        // Auto-generated if not provided
    Name:              "my-server",         // os.Hostname() if not provided
    Metadata:          map[string]any{...}, // Custom instance metadata
    HeartbeatInterval: 15 * time.Second,    // Default: 15s
    LeaderTTL:         30 * time.Second,    // Default: 30s
    StuckRunTimeout:   5 * time.Minute,     // Default: 5m
    BatchPollInterval: 30 * time.Second,    // Default: 30s
    RunPollInterval:   1 * time.Second,     // Default: 1s
    ToolPollInterval:  500 * time.Millisecond, // Default: 500ms
    MaxConcurrentRuns: 10,                  // Default: 10
    MaxConcurrentTools: 50,                 // Default: 50
    OnError:           func(err error) { ... },
    OnBecameLeader:    func() { ... },
    OnLostLeadership:  func() { ... },
})
```

### Agent Definition

```go
client.RegisterAgent(&agentpg.AgentDefinition{
    Name:         "chat",
    Description:  "A helpful chat agent",
    Model:        "claude-sonnet-4-5-20250929",
    SystemPrompt: "You are a helpful assistant",
    MaxTokens:    &maxTokens,
    Temperature:  &temp,
    Config:       map[string]any{
        "auto_compaction": true,
        "extended_context": true,
    },
})
```

### Agent-as-Tool (Hierarchies)

```go
// Register child agent first
client.RegisterAgent(&agentpg.AgentDefinition{
    Name:         "worker",
    Model:        "claude-sonnet-4-5-20250929",
    SystemPrompt: "You are a worker agent...",
    // ... other config
})

// Register parent agent with Agents field to delegate to child
client.RegisterAgent(&agentpg.AgentDefinition{
    Name:         "manager",
    Model:        "claude-sonnet-4-5-20250929",
    SystemPrompt: "You are a manager agent...",
    Agents:       []string{"worker"},  // worker becomes a callable tool for manager
    // ... other config
})
```

---

## Database Schema

AgentPG requires the database schema. Run the migration:

```bash
psql $DATABASE_URL -f storage/migrations/001_agentpg_migration.up.sql
```

### Core Tables
- `agentpg_sessions` - Stores conversation sessions with tenant isolation
- `agentpg_runs` - Agent run executions with hierarchy support
- `agentpg_iterations` - Each batch API call within a run
- `agentpg_messages` - Conversation messages
- `agentpg_tool_executions` - Tool execution tracking

### Distributed Tables
- `agentpg_instances` - Instance registration and heartbeats
- `agentpg_leader` - Leader election state
- `agentpg_agents` - Agent definitions
- `agentpg_tools` - Tool definitions

---

## Troubleshooting

**Database connection errors**: Verify your DATABASE_URL is correct and PostgreSQL is running

**API errors**: Check that your ANTHROPIC_API_KEY is valid and has sufficient credits

**Import errors**: Run `go mod tidy` to ensure all dependencies are downloaded

**Permission errors**: Ensure the database user has CREATE TABLE permissions

**Instance not starting**: Check that the database has been migrated to the latest schema

---

## Next Steps

- Read the main [README](../README.md) for architecture details
- Check out the [compaction](../compaction) package for context management
- Explore the [tool](../tool) package for advanced tool patterns
- See [docs/](../docs/) for detailed documentation
