# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

AgentPG is a production-grade stateful AI agent toolkit for Go. It provides persistent conversation management with PostgreSQL, automatic context compaction, a composable tool system, and multi-tenant session isolation.

**Tech Stack**: Go 1.25.4, PostgreSQL 14+, Anthropic Claude API (official Go SDK), pgx/v5

## Common Commands

```bash
# Development
make test                 # Run all tests
make test-unit           # Unit tests only (-short flag)
make test-integration    # Integration tests (requires DATABASE_URL)
make lint                # golangci-lint
make fmt                 # Format code

# Database
make docker-up           # Start PostgreSQL container
make docker-down         # Stop PostgreSQL
make migrate             # Run migrations

# Single test
go test -v -run TestFunctionName ./path/to/package
```

**Required Environment Variables:**
- `DATABASE_URL=postgres://agentpg:agentpg@localhost:5432/agentpg?sslmode=disable`
- `ANTHROPIC_API_KEY=sk-ant-...`

## Architecture

```
Agent (Orchestrator)
├── Session Manager (multi-tenant isolation, sync.RWMutex)
├── Tool System (registry → executor → validator)
├── Compaction Manager (hybrid strategy: pruning + summarization)
├── Hook System (observability: OnBeforeMessage, OnAfterMessage, OnToolCall, etc.)
└── Storage Layer (pluggable Store interface)
    └── PostgreSQL (reference implementation with pgx)
```

**Key Design Patterns:**
- **Functional Options**: `New(cfg, WithMaxTokens(4096), WithTemperature(0.7))`
- **Interface-Based Extensibility**: Store, Tool, Strategy, Hook interfaces
- **Streaming-First**: All Claude API calls use streaming with automatic accumulation

## Package Structure

| Package | Purpose |
|---------|---------|
| `agentpg` (root) | Agent orchestrator, session management, configuration |
| `compaction/` | Context management (hybrid strategy, summarization, token counting) |
| `storage/` | Pluggable persistence (Store interface, PostgreSQL impl, migrations) |
| `tool/` | Tool registry, executor, JSON Schema validation, built-in tools |
| `streaming/` | Response accumulation, event types |
| `hooks/` | Observability hooks (logging, custom) |
| `internal/` | Adapters, test utilities, validation helpers |

## Code Conventions

**Naming:**
- Constructors: `New*` prefix (`NewSession`, `NewRegistry`)
- Options: `With*` prefix (`WithMaxTokens`, `WithAutoCompaction`)
- Interfaces: noun names (`Tool`, `Store`, `Strategy`)

**Tool Interface:**
```go
type Tool interface {
    Name() string
    Description() string
    InputSchema() ToolSchema  // JSON Schema
    Execute(ctx context.Context, input json.RawMessage) (string, error)
}
```

**Error Handling:**
- Custom error types with context (`AgentError`, `ErrNoSession`)
- Wrap errors: `fmt.Errorf("context: %w", err)`
- Check with: `errors.Is(err, ErrSessionNotFound)`

**Testing:**
- Table-driven tests
- Integration tests check `DATABASE_URL` and skip if missing
- Test utilities in `internal/testutil/`

## Critical Implementation Details

**Compaction System:**
- Triggers at 85% context utilization
- Protects last 40K tokens + recent messages
- Hybrid strategy: pruning before summarization
- All operations atomic within database transaction

**Multi-Tenancy:**
- Sessions isolated by `tenant_id`
- Thread-safe with `sync.RWMutex`
- Never expose sessions across tenants

**Model Awareness:**
- Context windows auto-configured per model (200K for Claude 3.5/Opus 4.5)
- Known models: claude-3-*, claude-3.5-*, claude-opus-4.5-*

## Documentation

Detailed documentation in `/docs/`:
- `architecture.md` - System design and data flows
- `tools.md` - Tool development patterns
- `compaction.md` - Context management strategies
- `storage.md` - Database schema details
- `hooks.md` - Hook system details
