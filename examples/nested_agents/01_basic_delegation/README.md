# Basic Delegation Example

This example demonstrates the fundamental `AsToolFor()` pattern for agent delegation.

## Concept

The main agent can delegate tasks to a specialist agent by using it as a tool:

```go
researchAgent.AsToolFor(mainAgent)
```

When registered, the specialist becomes a callable tool with:
- **Name**: Auto-generated from system prompt (e.g., `agent_You_are_a_research_spec`)
- **Parameters**: `task` (required) and `context` (optional)

## How It Works

```
User Request
     │
     ▼
┌─────────────┐
│ Main Agent  │ ─── Decides if delegation is needed
└─────────────┘
     │
     │ (calls agent tool)
     ▼
┌─────────────┐
│  Research   │ ─── Processes the delegated task
│   Agent     │
└─────────────┘
     │
     │ (returns result)
     ▼
┌─────────────┐
│ Main Agent  │ ─── Summarizes and presents to user
└─────────────┘
```

## Key Code

### Creating Agents
```go
// Specialist agent with focused expertise
researchAgent, _ := agentpg.New(agentpg.Config{
    SystemPrompt: "You are a research specialist...",
    ...
})

// Main agent that orchestrates
mainAgent, _ := agentpg.New(agentpg.Config{
    SystemPrompt: "You are a helpful assistant that can delegate research tasks...",
    ...
})
```

### Registering Agent as Tool
```go
researchAgent.AsToolFor(mainAgent)
```

### Tool Schema (Auto-Generated)
```json
{
    "type": "object",
    "properties": {
        "task": {
            "type": "string",
            "description": "The task or question to delegate to this agent"
        },
        "context": {
            "type": "string",
            "description": "Additional context for the task (optional)"
        }
    },
    "required": ["task"]
}
```

## Session Management

Each nested agent gets its own dedicated session:
- Tenant ID: `"nested"`
- Identifier: Agent tool name
- Isolated conversation history

## Running the Example

```bash
export ANTHROPIC_API_KEY="your-api-key"
export DATABASE_URL="postgresql://user:pass@localhost:5432/agentpg"

cd examples/nested_agents/01_basic_delegation
go run main.go
```

## Expected Output

```
=== Main Agent Tools ===
- agent_You_are_a_research_spec

Created session: 550e8400-e29b-41d4-a716-446655440000

=== Example 1: Simple Question (No Delegation) ===
2 + 2 equals 4.

=== Example 2: Research Question (With Delegation) ===
Based on my research, here's how neural networks learn:

Neural networks learn through a process called backpropagation...
[Summarized research findings]

=== Example 3: Research with Specific Context ===
Here's a comparison of SQL vs NoSQL databases:

SQL databases are ideal for...
NoSQL databases excel when...
[Detailed comparison]

=== Token Usage (Last Response) ===
Input tokens: 1523
Output tokens: 487

=== Demo Complete ===
```

## When to Delegate

| Scenario | Delegation? | Reason |
|----------|-------------|--------|
| "What is 2+2?" | No | Simple, direct answer |
| "Research quantum computing" | Yes | Requires deep analysis |
| "Summarize this text" | Maybe | Depends on complexity |
| "Analyze code patterns" | Yes | Specialist expertise |

## Best Practices

1. **Clear system prompts** - Define when the main agent should delegate
2. **Focused specialists** - Each specialist should have a clear domain
3. **Don't over-delegate** - Simple tasks should be handled directly
4. **Monitor token usage** - Delegation increases total tokens used

## Next Steps

- See [02_specialist_agents](../02_specialist_agents/) for multiple specialists
- See [03_multi_level_hierarchy](../03_multi_level_hierarchy/) for deeper hierarchies
