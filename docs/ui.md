# Admin UI

AgentPG includes an embedded admin UI for monitoring and managing agents. The UI is built with HTMX + Tailwind CSS and provides server-side rendering for a fast, responsive experience.

## Overview

The `ui` package provides three handler functions:

| Handler | Description |
|---------|-------------|
| `ui.UIHandler()` | SSR frontend with HTMX + Tailwind |
| `ui.APIHandler()` | REST API with JSON responses |
| `ui.Handler()` | Combined handler (UI + API under one path) |

## Quick Start

```go
import (
    "net/http"
    "time"
    "github.com/youssefsiam38/agentpg/ui"
)

// Create driver and client
drv := pgxv5.New(pool)
client, _ := agentpg.NewClient(drv, clientConfig)

// Mount UI at /ui/
uiConfig := &ui.Config{
    BasePath:        "/ui",
    PageSize:        25,
    RefreshInterval: 5 * time.Second,
}
http.Handle("/ui/", http.StripPrefix("/ui", ui.UIHandler(drv.Store(), client, uiConfig)))
```

## Configuration

```go
type Config struct {
    // BasePath is the URL prefix where the UI is mounted.
    // All navigation links will be prefixed with this path.
    BasePath string

    // TenantID filters data to a single tenant.
    // If empty, shows all tenants (admin mode) with a tenant selector.
    TenantID string

    // ReadOnly disables write operations (chat, session creation).
    // Useful for monitoring-only deployments.
    ReadOnly bool

    // Logger for structured logging.
    Logger Logger

    // RefreshInterval for SSE updates and auto-refresh.
    // Defaults to 5 seconds.
    RefreshInterval time.Duration

    // PageSize for pagination.
    // Defaults to 25.
    PageSize int
}
```

## Operating Modes

### Admin Mode

Shows all tenants with a tenant selector dropdown:

```go
adminConfig := &ui.Config{
    BasePath: "/admin",
    // TenantID is empty = admin mode
}
```

### Single-Tenant Mode

Filters all data to one tenant only:

```go
tenantConfig := &ui.Config{
    BasePath: "/ui",
    TenantID: "tenant-123",
}
```

### Read-Only Mode

Disables chat and session creation for monitoring-only deployments:

```go
monitorConfig := &ui.Config{
    BasePath: "/monitor",
    ReadOnly: true,
}
// Pass nil for client when ReadOnly is true
http.Handle("/monitor/", http.StripPrefix("/monitor", ui.UIHandler(store, nil, monitorConfig)))
```

## Pages

| Path | Description |
|------|-------------|
| `/` | Redirects to dashboard |
| `/dashboard` | Overview with stats, active runs, instances |
| `/sessions` | Session list with pagination and filtering |
| `/sessions/{id}` | Session detail with runs, messages, token usage |
| `/runs` | Run list with state and agent filtering |
| `/runs/{id}` | Run detail with iterations, tool executions |
| `/runs/{id}/conversation` | Full conversation view for a run |
| `/tool-executions` | Tool execution list with state filtering |
| `/tool-executions/{id}` | Tool execution detail with input/output |
| `/agents` | Registered agents across all instances |
| `/instances` | Active worker instances with health status |
| `/compaction` | Compaction events history |
| `/chat` | Interactive chat interface |
| `/chat/session/{id}` | Chat with existing session |

## Chat Interface

The chat interface allows real-time interaction with agents:

```go
// Chat is enabled when:
// 1. ReadOnly is false (default)
// 2. A client is provided to UIHandler
http.Handle("/ui/", http.StripPrefix("/ui", ui.UIHandler(store, client, config)))
```

### Features

- Create new sessions with custom user IDs
- Select from registered agents
- Send messages and see real-time responses
- View tool executions inline during processing
- Automatic polling for run completion
- Two view modes:
  - **Top Level**: Only root agent messages (nested agents hidden)
  - **Hierarchy**: All messages grouped by run with depth indicators

### Hierarchical View

For multi-agent orchestration, the hierarchy view shows:

- Collapsible run groups with depth indicators (L0 Root, L1, L2, etc.)
- Agent name badges and state badges with spinners
- Nested child groups for agent-as-tool patterns
- Real-time tool execution status during pending_tools state

## Dashboard

The dashboard provides a system overview:

- **Session Stats**: Total sessions, active sessions, sessions today
- **Run Stats**: Total, active, pending, completed (24h), failed (24h)
- **Tool Stats**: Pending tools, running tools, failed (24h)
- **Instance Info**: Active instances, leader instance
- **Token Usage**: Total tokens (24h), average per run
- **Performance**: Average duration, success rate, iterations per run
- **Recent Activity**: Recent runs, tool errors, sessions

Auto-refresh updates stats at the configured interval.

## Adding Middleware

Wrap handlers using standard Go patterns:

```go
// Single middleware
http.Handle("/ui/", http.StripPrefix("/ui", authMiddleware(ui.UIHandler(store, client, cfg))))

// Multiple middlewares chained
handler := authMiddleware(loggingMiddleware(rateLimitMiddleware(ui.UIHandler(store, client, cfg))))
http.Handle("/ui/", http.StripPrefix("/ui", handler))

// Using chi router
r.Route("/ui", func(r chi.Router) {
    r.Use(authMiddleware)
    r.Use(loggingMiddleware)
    r.Mount("/", ui.UIHandler(store, client, cfg))
})
```

## Combined Handler

For simpler setups, use the combined handler that mounts both API and UI:

```go
// Mounts both:
// - /admin/api/* - REST API
// - /admin/*     - Frontend UI
mux.Handle("/admin/", http.StripPrefix("/admin", ui.Handler(store, client, config)))
```

## Complete Example

```go
package main

import (
    "context"
    "log"
    "net/http"
    "os"
    "time"

    "github.com/jackc/pgx/v5/pgxpool"
    "github.com/youssefsiam38/agentpg"
    "github.com/youssefsiam38/agentpg/driver/pgxv5"
    "github.com/youssefsiam38/agentpg/ui"
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

    // Register agents
    client.RegisterAgent(&agentpg.AgentDefinition{
        Name:         "assistant",
        Model:        "claude-sonnet-4-5-20250929",
        SystemPrompt: "You are a helpful assistant.",
    })

    client.Start(ctx)
    defer client.Stop(context.Background())

    mux := http.NewServeMux()

    // Full admin UI with chat
    fullConfig := &ui.Config{
        BasePath:        "/ui",
        PageSize:        25,
        RefreshInterval: 5 * time.Second,
    }
    mux.Handle("/ui/", http.StripPrefix("/ui", ui.UIHandler(drv.Store(), client, fullConfig)))

    // REST API
    mux.Handle("/api/", http.StripPrefix("/api", ui.APIHandler(drv.Store(), fullConfig)))

    // Read-only monitoring (separate endpoint)
    monitorConfig := &ui.Config{
        BasePath: "/monitor",
        ReadOnly: true,
        PageSize: 50,
    }
    mux.Handle("/monitor/", http.StripPrefix("/monitor", ui.UIHandler(drv.Store(), nil, monitorConfig)))

    log.Println("Server starting on :8080")
    log.Println("  /ui/      - Admin UI with chat")
    log.Println("  /api/     - REST API")
    log.Println("  /monitor/ - Read-only monitoring")
    http.ListenAndServe(":8080", mux)
}
```

## Technology Stack

- **Rendering**: Go templates with server-side rendering
- **Interactivity**: HTMX 2.0 (from CDN)
- **Styling**: Tailwind CSS (from CDN with typography plugin)
- **Icons**: Inline SVGs
- **Markdown**: goldmark parser + bluemonday sanitizer
- **JavaScript**: Minimal (~170 lines for utilities)

### JavaScript Features

The minimal JavaScript (`app.js`) provides:

- Auto-scroll on message arrival
- Clear input after send
- Loading indicators
- Keyboard shortcuts (Ctrl/Cmd + Enter to send)
- Time formatting utilities
- Copy-to-clipboard
- Global utilities via `window.AgentPG`

## Security

### Input Validation

- Form parsing with explicit field extraction
- UUID validation for all IDs
- Alphanumeric validation for user IDs and agent names
- Max length constraints (256 for user ID, 128 for agent name)
- Bounds checking on pagination parameters

### Output Sanitization

- Markdown parsing with bluemonday UGC policy
- HTML escaping via Go templates
- Safe HTML template function
- Code highlighting classes allowed

### Tenant Isolation

- TenantID filters all queries
- Admin mode explicitly manages tenant access
- No cross-tenant data leakage
