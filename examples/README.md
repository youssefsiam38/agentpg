# AgentPG Examples

This directory contains comprehensive examples demonstrating various features of AgentPG.

## Prerequisites

Before running any example, ensure you have:

1. **PostgreSQL database** - Required for storing conversation history
2. **Anthropic API key** - Get one from https://console.anthropic.com/

Set the following environment variables:

```bash
export ANTHROPIC_API_KEY="your-api-key-here"
export DATABASE_URL="postgresql://user:password@localhost:5432/agentpg"
```

## Example Categories

### basic/
**Location**: `examples/basic/main.go`

Basic agent creation and configuration:
- Creating an AgentPG instance
- Basic configuration (model, system prompt, temperature)
- Creating and managing sessions
- Running simple queries
- Accessing response content and usage statistics

```bash
go run examples/basic/main.go
```

---

### streaming/
**Location**: `examples/streaming/main.go`

Streaming architecture, tools, and hooks:
- Demonstrates streaming-first design (all API calls use SSE internally)
- Creating custom tools (calculator)
- All 5 observability hooks
- Automatic context compaction

```bash
go run examples/streaming/main.go
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
| `01_basic_delegation/` | Basic `agent.AsToolFor()` usage - research agent delegated from main agent |
| `02_specialist_agents/` | Multiple specialist agents (coder, researcher, analyst) with own tools |
| `03_multi_level_hierarchy/` | 3-level hierarchy: manager → team leads → workers |

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
| `01_auto_compaction/` | `WithAutoCompaction`, `WithCompactionTrigger`, `WithCompactionTarget` |
| `02_manual_compaction/` | Explicit compaction control, `WithCompactionPreserveN`, `WithSummarizerModel` |
| `03_custom_strategy/` | Implement custom `compaction.Strategy` interface |
| `04_compaction_monitoring/` | `OnBeforeCompaction`, `OnAfterCompaction` hooks, metrics |

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
- `WithExtendedContext(true)` configuration
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
| `04_rate_limiting/` | `OnBeforeMessage` hook for rate limiting, token bucket pattern |
| `05_database_tool/` | Safe SQL query tool, SELECT-only validation, result formatting |
| `06_http_api_tool/` | Generic HTTP client tool, timeout handling, response parsing |
| `07_pii_filtering/` | PII detection with regex patterns, blocking/logging modes |
| `08_error_recovery/` | `WithMaxRetries`, exponential backoff, graceful degradation |

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
| Agent creation & config | basic/, all examples |
| Session management | basic/, advanced/01_multi_tenant |
| Tool interface (struct) | custom_tools/01_struct_tool |
| NewFuncTool | custom_tools/02_func_tool |
| Schema validation | custom_tools/03_schema_validation |
| Tool registry & executor | custom_tools/04_parallel_execution |
| AsToolFor() | nested_agents/01_basic_delegation |
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

### Core Options
- `WithMaxTokens(n)` - Set maximum output tokens
- `WithTemperature(t)` - Control response randomness (0.0-1.0)
- `WithTopK(k)` - Top-k sampling parameter
- `WithTopP(p)` - Nucleus sampling parameter
- `WithStopSequences(...)` - Custom stop sequences

### Compaction Options
- `WithAutoCompaction(true)` - Enable automatic context management
- `WithCompactionTrigger(0.85)` - Trigger threshold (% of context)
- `WithCompactionTarget(0.5)` - Target utilization after compaction
- `WithCompactionPreserveN(10)` - Always preserve last N messages
- `WithCompactionProtectedTokens(40000)` - Protect last N tokens
- `WithSummarizerModel("claude-3-haiku")` - Model for summarization

### Extended Context
- `WithExtendedContext(true)` - Enable 1M token context window

---

## Database Schema

AgentPG automatically manages the database schema. Tables created:
- `sessions` - Stores conversation sessions with tenant isolation
- `messages` - Stores all messages with JSONB content and usage tracking
- `compaction_events` - Audit log of compaction operations
- `message_archive` - Archived messages from compaction

---

## Hooks for Observability

All hooks are available for custom observability:
- `OnBeforeMessage(func(ctx, messages []*types.Message) error)` - Before API call
- `OnAfterMessage(func(ctx, response *types.Response) error)` - After API response
- `OnToolCall(func(ctx, toolName, input, output string, err error) error)` - Tool execution
- `OnBeforeCompaction(func(ctx, sessionID string, tokenCount int) error)` - Before compaction
- `OnAfterCompaction(func(ctx, result *compaction.CompactionResult) error)` - After compaction

---

## Troubleshooting

**Database connection errors**: Verify your DATABASE_URL is correct and PostgreSQL is running

**API errors**: Check that your ANTHROPIC_API_KEY is valid and has sufficient credits

**Import errors**: Run `go mod tidy` to ensure all dependencies are downloaded

**Permission errors**: Ensure the database user has CREATE TABLE permissions

---

## Next Steps

- Read the main [README](../README.md) for architecture details
- Check out the [compaction](../compaction) package for context management
- Explore the [tool](../tool) package for advanced tool patterns
- Review the [hooks](../hooks) package for built-in observability hooks
- See [docs/](../docs/) for detailed documentation
