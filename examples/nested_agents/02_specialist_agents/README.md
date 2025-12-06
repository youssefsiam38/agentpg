# Specialist Agents Example

This example demonstrates multiple specialist agents, each with their own tools, coordinated by an orchestrator agent.

## Architecture

```
                    ┌──────────────┐
                    │ Orchestrator │
                    │    Agent     │
                    └──────────────┘
                          │
          ┌───────────────┼───────────────┐
          ▼               ▼               ▼
    ┌──────────┐    ┌──────────┐    ┌──────────┐
    │   Code   │    │   Data   │    │ Research │
    │  Agent   │    │  Agent   │    │  Agent   │
    └──────────┘    └──────────┘    └──────────┘
          │               │               │
          ▼               ▼               ▼
    ┌──────────┐    ┌──────────┐    ┌──────────┐
    │ analyze_ │    │  query_  │    │ search_  │
    │   code   │    │   data   │    │knowledge │
    └──────────┘    └──────────┘    └──────────┘
```

## Specialist Agents

### Code Agent
- **Focus**: Code analysis and programming
- **Tool**: `analyze_code` - Analyzes code patterns and metrics
- **Use cases**: Code reviews, refactoring suggestions, bug detection

### Data Agent
- **Focus**: Data analysis and business metrics
- **Tool**: `query_data` - Queries datasets for insights
- **Use cases**: Sales reports, user analytics, trend analysis

### Research Agent
- **Focus**: Information retrieval and synthesis
- **Tool**: `search_knowledge` - Searches knowledge bases
- **Use cases**: Best practices, documentation, learning resources

## Key Patterns

### Specialists with Their Own Tools
```go
codeAgent, _ := agentpg.New(agentpg.Config{
    SystemPrompt: "You are a code analysis specialist...",
    ...
})
codeAgent.RegisterTool(&CodeAnalysisTool{})
```

### Orchestrator Registration
```go
orchestrator, _ := agentpg.New(agentpg.Config{
    SystemPrompt: "You are an orchestrator that coordinates specialists...",
    ...
})

codeAgent.AsToolFor(orchestrator)
dataAgent.AsToolFor(orchestrator)
researchAgent.AsToolFor(orchestrator)
```

### Multi-Specialist Queries
The orchestrator can delegate to multiple specialists in a single request:
```go
orchestrator.Run(ctx, "Research authentication best practices and check our user metrics")
// Calls both Research Agent and Data Agent
```

## Running the Example

```bash
export ANTHROPIC_API_KEY="your-api-key"
export DATABASE_URL="postgresql://user:pass@localhost:5432/agentpg"

cd examples/nested_agents/02_specialist_agents
go run main.go
```

## Expected Output

```
=== Orchestrator Tools ===
- agent_You_are_a_code_analys
- agent_You_are_a_data_analys
- agent_You_are_a_research_sp

Created session: 550e8400-e29b-41d4-a716-446655440000

=== Example 1: Code Analysis ===
I've analyzed the fibonacci function. Here's my assessment:

The code implements a recursive fibonacci algorithm. While correct, it has
exponential time complexity O(2^n) due to redundant calculations...
[Includes metrics from analyze_code tool]

=== Example 2: Data Analysis ===
Based on our data analysis:
- Total sales revenue: $1,967,100
- Total users: 5,892
[Synthesized from query_data tool results]

=== Example 3: Research Query ===
Here are the best practices for API design based on my research:
1. RESTful principles...
2. Versioning strategies...
[Includes search_knowledge results]

=== Example 4: Multi-Specialist Query ===
I consulted both specialists:

From Research: Best practices for user authentication include...
From Data: Current user base is 5,892 users...

Combined recommendation: Given our user base size...

=== Demo Complete ===
```

## When to Use Multiple Specialists

| Scenario | Specialists Involved |
|----------|---------------------|
| "Review this code" | Code Agent only |
| "Show me sales data" | Data Agent only |
| "Research microservices" | Research Agent only |
| "Optimize our API based on usage patterns" | Data + Code Agents |
| "Build auth feature with best practices" | Research + Data Agents |

## Best Practices

1. **Clear domain separation** - Each specialist has distinct expertise
2. **Specialist-specific tools** - Tools match the specialist's domain
3. **Orchestrator guidance** - Clear instructions on when to delegate
4. **Tool reuse** - Specialists can share similar tools if needed

## Next Steps

- See [03_multi_level_hierarchy](../03_multi_level_hierarchy/) for deeper nesting
- See [advanced/02_observability](../../advanced/02_observability/) for monitoring nested calls
