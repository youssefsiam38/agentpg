// Package agentpg provides a production-grade, stateful AI agent toolkit for Go.
//
// AgentPG is opinionated (Anthropic + PostgreSQL + pgx), modular, and designed for
// building AI agents that can handle long-context operations with automatic context
// compaction, tool support, and nested agent composition.
//
// # Key Features
//
//   - Stateful conversations with PostgreSQL persistence
//   - Automatic context compaction using production patterns from Claude Code, Aider, and OpenCode
//   - Tool system with simple interface-based API
//   - Nested agents (agents as tools) with automatic integration
//   - Streaming-first architecture for long context support
//   - Extended context support (1M tokens) via beta headers
//   - Hooks for observability and debugging
//
// # Quick Start
//
// Create an agent with required configuration:
//
//	pool, _ := pgxpool.New(ctx, connString)
//	agent, err := agentpg.New(
//	    agentpg.Config{
//	        DB:           pool,
//	        Client:       anthropic.NewClient(),
//	        Model:        "claude-sonnet-4-5-20250929",
//	        SystemPrompt: "You are a helpful coding assistant",
//	    },
//	    agentpg.WithMaxTokens(4096),
//	    agentpg.WithAutoCompaction(true),
//	)
//
// Run the agent (streaming under the hood):
//
//	// For single-tenant apps, use "1" as tenant_id
//	sessionID, _ := agent.NewSession(ctx, "1", "user-123", nil, nil)
//	response, _ := agent.Run(ctx, "Help me build a REST API")
//
// # Custom Tools
//
// Implement the Tool interface:
//
//	type MyTool struct{}
//
//	func (t *MyTool) Name() string { return "my_tool" }
//	func (t *MyTool) Description() string { return "Does something useful" }
//	func (t *MyTool) InputSchema() agentpg.ToolSchema {
//	    return agentpg.ToolSchema{
//	        Type: "object",
//	        Properties: map[string]agentpg.PropertyDef{
//	            "param": {Type: "string", Description: "A parameter"},
//	        },
//	        Required: []string{"param"},
//	    }
//	}
//	func (t *MyTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
//	    // Tool implementation
//	    return "result", nil
//	}
//
// # Nested Agents
//
// Create specialist agents and register them as tools:
//
//	dbAgent, _ := agentpg.New(config)
//	orchestrator, _ := agentpg.New(config)
//	dbAgent.AsToolFor(orchestrator)
//
// The orchestrator can now call the database agent automatically.
//
// # Context Compaction
//
// Context is automatically compacted when reaching 85% of the model's context window.
// The package uses production patterns including:
//   - Hybrid strategy: prune tool outputs first, then summarize if needed
//   - Protection of last 40K tokens
//   - Preservation of critical information (file paths, code, errors, user corrections)
//   - 8-section structured summarization
//
// Manual compaction control:
//
//	agent, _ := agentpg.New(config, agentpg.WithAutoCompaction(false))
//	stats, _ := agent.GetContextStats(ctx)
//	if stats.UtilizationPercent > 80 {
//	    result, _ := agent.CompactContext(ctx)
//	}
//
// # Extended Context
//
// Enable 1M token context with automatic fallback:
//
//	agent, _ := agentpg.New(
//	    config,
//	    agentpg.WithExtendedContext(true),
//	)
//
// The agent will automatically retry with the extended context beta header
// if a max_tokens error occurs.
package agentpg
