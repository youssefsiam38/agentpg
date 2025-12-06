# Nested Agents Examples

This directory contains examples demonstrating agent composition and delegation patterns in AgentPG.

## Examples

| Example | Description |
|---------|-------------|
| [01_basic_delegation](./01_basic_delegation/) | Basic `AsToolFor()` pattern |
| [02_specialist_agents](./02_specialist_agents/) | Multiple specialist agents with their own tools |
| [03_multi_level_hierarchy](./03_multi_level_hierarchy/) | Three-level agent hierarchy |

## Key Concept: Agent as Tool

AgentPG allows you to register one agent as a tool for another using `AsToolFor()`:

```go
researchAgent.AsToolFor(mainAgent)
```

This creates a tool that:
- Accepts `task` and optional `context` parameters
- Creates a dedicated session for the nested agent
- Returns the nested agent's text response

## Learning Path

1. Start with **01_basic_delegation** to understand the AsToolFor pattern
2. Move to **02_specialist_agents** for multiple coordinated agents
3. Explore **03_multi_level_hierarchy** for complex orchestration

## Prerequisites

- PostgreSQL database running
- Environment variables set:
  - `ANTHROPIC_API_KEY`
  - `DATABASE_URL`

## Use Cases

- **Task Delegation** - Main agent delegates specific tasks to specialists
- **Expertise Isolation** - Different agents for code, research, analysis
- **Complex Workflows** - Multi-step processes with handoffs
- **Modular Design** - Compose agents like building blocks

## Running Examples

```bash
cd examples/nested_agents/01_basic_delegation
go run main.go
```
