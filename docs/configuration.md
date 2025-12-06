# Configuration Guide

This guide covers all configuration options for AgentPG, including recommended settings for different use cases.

## Required Configuration

Every agent requires a driver and configuration:

```go
import (
    "github.com/jackc/pgx/v5/pgxpool"
    "github.com/youssefsiam38/agentpg"
    "github.com/youssefsiam38/agentpg/driver/pgxv5"
)

pool, _ := pgxpool.New(ctx, dbURL)
drv := pgxv5.New(pool)

agent, err := agentpg.New(drv, agentpg.Config{
    Client:       &client,        // Anthropic API client
    Model:        "claude-sonnet-4-5-20250929",  // Model ID
    SystemPrompt: "You are a helpful assistant.",
})
```

### Available Drivers

| Driver | Import | Transaction Type |
|--------|--------|------------------|
| pgxv5 | `github.com/youssefsiam38/agentpg/driver/pgxv5` | `pgx.Tx` |
| databasesql | `github.com/youssefsiam38/agentpg/driver/databasesql` | `*sql.Tx` |

### Database Connection

```go
import "github.com/jackc/pgx/v5/pgxpool"

// Basic connection
pool, err := pgxpool.New(ctx, "postgres://user:pass@localhost:5432/dbname")

// With configuration
config, _ := pgxpool.ParseConfig(databaseURL)
config.MaxConns = 25
config.MinConns = 5
config.MaxConnLifetime = time.Hour
config.MaxConnIdleTime = 30 * time.Minute
pool, err := pgxpool.NewWithConfig(ctx, config)
```

### Anthropic Client

```go
import "github.com/anthropics/anthropic-sdk-go"

// From environment variable (ANTHROPIC_API_KEY)
client := anthropic.NewClient()

// Explicit API key
client := anthropic.NewClient(
    option.WithAPIKey("sk-ant-..."),
)

// Custom base URL (for proxies)
client := anthropic.NewClient(
    option.WithBaseURL("https://your-proxy.com"),
)
```

### Model Selection

| Model | Context | Speed | Cost | Best For |
|-------|---------|-------|------|----------|
| `claude-sonnet-4-5-20250929` | 200K | Fast | $$ | General use, production |
| `claude-opus-4-5-20251101` | 200K | Slower | $$$$ | Complex reasoning |
| `claude-3-5-haiku-20241022` | 200K | Fastest | $ | High volume, simple tasks |

```go
// Production recommendation
Model: "claude-sonnet-4-5-20250929"

// Cost-sensitive applications
Model: "claude-3-5-haiku-20241022"

// Maximum capability
Model: "claude-opus-4-5-20251101"
```

---

## Generation Options

### Token Limits

```go
// Set maximum output tokens
agentpg.WithMaxTokens(8192)  // Default varies by model
```

**Recommendations:**
- Chat applications: 2048-4096
- Code generation: 8192-16384
- Document generation: 16384+

### Sampling Parameters

```go
// Temperature (0.0-1.0)
// Lower = more deterministic, Higher = more creative
agentpg.WithTemperature(0.7)

// Top-K sampling (integer)
// Limits token selection to top K candidates
agentpg.WithTopK(40)

// Nucleus sampling (0.0-1.0)
// Limits to tokens comprising top P probability mass
agentpg.WithTopP(0.9)
```

**Use Case Recommendations:**

| Use Case | Temperature | Notes |
|----------|-------------|-------|
| Code generation | 0.0-0.3 | Deterministic, consistent |
| Customer support | 0.3-0.5 | Balanced |
| Creative writing | 0.7-0.9 | More varied outputs |
| Brainstorming | 0.9-1.0 | Maximum creativity |

### Stop Sequences

```go
// Stop generation at specific sequences
agentpg.WithStopSequences("```", "</answer>", "END")
```

---

## Context Management

### Automatic Compaction

```go
// Enable/disable auto-compaction (default: true)
agentpg.WithAutoCompaction(true)
```

When enabled, the agent automatically compacts context when approaching the model's limit.

### Compaction Trigger

```go
// Trigger compaction at 85% of context window (default)
agentpg.WithCompactionTrigger(0.85)

// More aggressive (trigger earlier)
agentpg.WithCompactionTrigger(0.70)

// Less aggressive (maximize context usage)
agentpg.WithCompactionTrigger(0.95)
```

### Compaction Target

```go
// Target token count after compaction
agentpg.WithCompactionTarget(80000)  // Default: 40% of max

// Aggressive reduction
agentpg.WithCompactionTarget(40000)

// Preserve more context
agentpg.WithCompactionTarget(120000)
```

### Message Preservation

```go
// Always preserve last N messages (default: 10)
agentpg.WithCompactionPreserveN(10)

// For long tool chains, preserve more
agentpg.WithCompactionPreserveN(20)

// Never compact last N tokens (default: 40000)
agentpg.WithCompactionProtectedTokens(40000)
```

### Compaction Strategy

```go
// Hybrid: prune tool outputs first, then summarize (default)
agentpg.WithCompactionStrategy(agentpg.HybridStrategy)

// Summarization: always summarize
agentpg.WithCompactionStrategy(agentpg.SummarizationStrategy)
```

### Summarizer Model

```go
// Model for generating summaries (default: haiku for speed/cost)
agentpg.WithSummarizerModel("claude-3-5-haiku-20241022")

// Higher quality summaries (slower, more expensive)
agentpg.WithSummarizerModel("claude-sonnet-4-5-20250929")
```

### Extended Context

```go
// Enable 1M token context (requires beta access)
agentpg.WithExtendedContext(true)
```

---

## Tool Configuration

### Registering Tools

```go
// At creation time
agent, _ := agentpg.New(cfg,
    agentpg.WithTools(tool1, tool2, tool3),
)

// At runtime
agent.RegisterTool(newTool)
```

### Tool Iteration Limits

```go
// Maximum tool calls per Run() (default: 10)
agentpg.WithMaxToolIterations(10)

// For complex workflows
agentpg.WithMaxToolIterations(50)
```

### Tool Timeout

```go
// Set timeout for individual tool executions (default: 5 minutes)
agentpg.WithToolTimeout(5 * time.Minute)

// For quick tools
agentpg.WithToolTimeout(30 * time.Second)

// For long-running tools (e.g., nested agents, external APIs)
agentpg.WithToolTimeout(10 * time.Minute)
```

### Tool Output Handling

```go
// Preserve full tool outputs during compaction
agentpg.WithPreserveToolOutputs(true)  // Default: false

// When true: tool outputs are never summarized
// When false: tool outputs may be truncated during compaction
```

---

## Reliability Options

### Retries

```go
// Maximum retry attempts for transient failures (default: 2)
agentpg.WithMaxRetries(3)
```

Retries apply to:
- Rate limit errors (429)
- Server errors (5xx)
- Network timeouts

---

## Configuration Profiles

### Production Chat Application

```go
agent, _ := agentpg.New(cfg,
    agentpg.WithMaxTokens(4096),
    agentpg.WithTemperature(0.5),
    agentpg.WithAutoCompaction(true),
    agentpg.WithCompactionTrigger(0.85),
    agentpg.WithCompactionPreserveN(10),
    agentpg.WithMaxRetries(3),
)
```

### Code Generation Agent

```go
agent, _ := agentpg.New(cfg,
    agentpg.WithMaxTokens(16384),
    agentpg.WithTemperature(0.1),
    agentpg.WithTools(codeTools...),
    agentpg.WithMaxToolIterations(20),
    agentpg.WithPreserveToolOutputs(true),
)
```

### High-Volume Support Bot

```go
agent, _ := agentpg.New(agentpg.Config{
    Model: "claude-3-5-haiku-20241022",  // Fast, cheap
    // ...
},
    agentpg.WithMaxTokens(1024),
    agentpg.WithTemperature(0.3),
    agentpg.WithCompactionTrigger(0.70),  // Aggressive compaction
    agentpg.WithCompactionTarget(40000),
)
```

### Long-Running Research Agent

```go
agent, _ := agentpg.New(agentpg.Config{
    Model: "claude-opus-4-5-20251101",  // Maximum capability
    // ...
},
    agentpg.WithMaxTokens(16384),
    agentpg.WithExtendedContext(true),  // 1M context
    agentpg.WithMaxToolIterations(100),
    agentpg.WithCompactionPreserveN(50),
)
```

---

## Environment Variables

AgentPG doesn't read environment variables directly, but common patterns:

```bash
# Database
export DATABASE_URL="postgres://user:pass@localhost:5432/agentpg"

# Anthropic API
export ANTHROPIC_API_KEY="sk-ant-..."
```

```go
import "os"

pool, _ := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
client := anthropic.NewClient()  // Reads ANTHROPIC_API_KEY automatically
```

---

## Validation

Configuration is validated at agent creation:

```go
agent, err := agentpg.New(drv, cfg, opts...)
if err != nil {
    // Handle invalid configuration
    // Common errors:
    // - "Anthropic client is required"
    // - "Model is required"
    // - "SystemPrompt is required"
    // - "threshold must be between 0 and 1"
}
```

---

## See Also

- [Architecture](./architecture.md) - How configuration affects system behavior
- [Compaction](./compaction.md) - Deep dive into context management
- [Tools](./tools.md) - Tool configuration details
- [Deployment](./deployment.md) - Production configuration recommendations
