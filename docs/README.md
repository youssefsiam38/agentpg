# AgentPG Documentation


AgentPG is a fully event-driven Go framework for building async AI agents using PostgreSQL for state management and distribution. It provides stateful, distributed, and transaction-safe agent execution with Claude API integration.

## Key Features

- **Event-Driven Architecture**: PostgreSQL LISTEN/NOTIFY for real-time coordination with polling fallback
- **Distributed Workers**: Race-safe work claiming with `SELECT FOR UPDATE SKIP LOCKED`
- **Transaction-First API**: Atomic operations with user transaction support
- **Multi-Agent Hierarchies**: Agent-as-tool pattern for complex workflows
- **Dual API Modes**: Batch API (50% cost savings) and Streaming API (real-time)
- **Context Compaction**: Automatic context window management for long conversations
- **Embedded Admin UI**: HTMX-powered dashboard for monitoring and chat

## Documentation Index

### Getting Started

| Document | Description |
|----------|-------------|
| [Getting Started](./getting-started.md) | Quick start guide with installation and first agent |
| [Configuration](./configuration.md) | All configuration options with defaults |
| [Tools Guide](./tools.md) | Building custom tools for agents |

### Core Concepts

| Document | Description |
|----------|-------------|
| [Architecture](./architecture.md) | System design, components, and data flow |
| [Distributed Workers](./distributed.md) | Multi-instance coordination and leader election |
| [Context Compaction](./compaction.md) | Managing long conversations and token limits |

### API Reference

| Document | Description |
|----------|-------------|
| [Go API Reference](./golang-api-reference.md.md) | Complete Go package documentation |
| [REST API Reference](./rest-api-reference.md) | HTTP API endpoints for the admin UI |

### Operations

| Document | Description |
|----------|-------------|
| [Deployment](./deployment.md) | Production deployment, Docker, and scaling |
| [Admin UI](./ui.md) | Web interface setup and customization |
| [Hooks](./hooks.md) | Extension points and customization |

### Development

| Document | Description |
|----------|-------------|
| [Contributing](./contributing.md) | Development setup and contribution guidelines |

## Quick Links

- **Examples**: See the [`/examples`](../examples/) directory for working code samples
- **Migrations**: Database schema in [`/storage/migrations`](../storage/migrations/)
- **Main Reference**: Comprehensive guide in [`/CLAUDE.md`](../CLAUDE.md)

## Installation

```bash
go get github.com/youssefsiam38/agentpg
```

## Minimal Example

```go
package main

import (
    "context"
    "fmt"
    "os"

    "github.com/jackc/pgx/v5/pgxpool"
    "github.com/youssefsiam38/agentpg"
    "github.com/youssefsiam38/agentpg/driver/pgxv5"
)

func main() {
    ctx := context.Background()

    // Connect to PostgreSQL
    pool, _ := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
    defer pool.Close()

    // Create driver and client
    drv := pgxv5.New(pool)
    client, _ := agentpg.NewClient(drv, &agentpg.ClientConfig{
        APIKey: os.Getenv("ANTHROPIC_API_KEY"),
    })

    // Register agent
    client.RegisterAgent(&agentpg.AgentDefinition{
        Name:         "assistant",
        Model:        "claude-sonnet-4-5-20250929",
        SystemPrompt: "You are a helpful assistant.",
    })

    // Start and run
    client.Start(ctx)
    defer client.Stop(context.Background())

    sessionID, _ := client.NewSession(ctx, "tenant-1", "user-1", nil, nil)
    response, _ := client.RunSync(ctx, sessionID, "assistant", "Hello!")
    fmt.Println(response.Text)
}
```

## License

Mozilla Public License 2.0 - see [LICENSE](../LICENSE) for details.  