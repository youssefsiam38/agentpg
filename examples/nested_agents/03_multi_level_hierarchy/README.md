# Multi-Level Hierarchy Example

This example demonstrates a three-level agent hierarchy simulating a project management structure.

## Architecture

```
                    ┌─────────────────┐
                    │ Project Manager │ ← Level 1
                    │   (Top-level)   │
                    └─────────────────┘
                            │
            ┌───────────────┴───────────────┐
            ▼                               ▼
    ┌───────────────┐               ┌───────────────┐
    │  Engineering  │               │    Design     │ ← Level 2
    │     Lead      │               │     Lead      │
    └───────────────┘               └───────────────┘
            │                               │
    ┌───────┼───────┐                       │
    ▼       ▼       ▼                       ▼
┌──────┐┌──────┐┌──────┐              ┌──────────┐
│Front ││Back  ││  DB  │              │    UX    │ ← Level 3
│ end  ││ end  ││ Spec │              │ Designer │
└──────┘└──────┘└──────┘              └──────────┘
   │       │       │                       │
   ▼       ▼       ▼                       ▼
[lint] [tests] [migrate]              [review]
```

## Hierarchy Levels

### Level 1: Project Manager
- **Role**: Top-level orchestrator
- **Reports from**: Engineering Lead, Design Lead
- **Responsibilities**:
  - Break down requests into components
  - Delegate to appropriate team leads
  - Synthesize executive summaries

### Level 2: Team Leads
**Engineering Lead**
- **Manages**: Frontend Dev, Backend Dev, DB Specialist
- **Focus**: Technical implementation and code quality

**Design Lead**
- **Manages**: UX Designer
- **Focus**: Design and accessibility

### Level 3: Workers (with Tools)
| Worker | Tool | Function |
|--------|------|----------|
| Frontend Developer | `lint_frontend` | Code quality checks |
| Backend Developer | `run_tests` | Test execution |
| Database Specialist | `run_migration` | Schema migrations |
| UX Designer | `review_design` | Accessibility review |

## Request Flow

```
User: "Check code quality and accessibility"
                    │
                    ▼
         ┌─────────────────┐
         │ Project Manager │ Breaks down request
         └────────┬────────┘
                  │
       ┌──────────┴──────────┐
       ▼                     ▼
┌─────────────┐      ┌─────────────┐
│ Engineering │      │   Design    │
│    Lead     │      │    Lead     │
└──────┬──────┘      └──────┬──────┘
       │                    │
  ┌────┼────┐               │
  ▼    ▼    ▼               ▼
Front Back  DB          UX Designer
 end   end  Spec            │
  │    │                    │
  ▼    ▼                    ▼
[lint][tests]           [review]
```

## Key Patterns

### Creating the Hierarchy
```go
// Level 3: Workers with tools
frontendDev.RegisterTool(&FrontendLintTool{})

// Level 2: Team leads manage workers
frontendDev.AsToolFor(engineeringLead)
backendDev.AsToolFor(engineeringLead)
dbSpecialist.AsToolFor(engineeringLead)

// Level 1: PM manages team leads
engineeringLead.AsToolFor(projectManager)
designLead.AsToolFor(projectManager)
```

### Delegation Chain
When the PM receives a request:
1. PM analyzes and delegates to appropriate team lead(s)
2. Team lead further delegates to specific workers
3. Workers use their tools
4. Results bubble back up the chain

## Running the Example

```bash
export ANTHROPIC_API_KEY="your-api-key"
export DATABASE_URL="postgresql://user:pass@localhost:5432/agentpg"

cd examples/nested_agents/03_multi_level_hierarchy
go run main.go
```

## Expected Output

```
Creating Level 3 workers...
Creating Level 2 team leads...
Creating Level 1 project manager...

=== Agent Hierarchy ===

Level 1 (Project Manager)
    └── Engineering Lead (Level 2)
    │       ├── Frontend Developer (Level 3) [lint_frontend]
    │       ├── Backend Developer (Level 3)  [run_tests]
    │       └── Database Specialist (Level 3) [run_migration]
    └── Design Lead (Level 2)
            └── UX Designer (Level 3)        [review_design]

Project Manager Tools:
  - agent_You_are_the_Engineer
  - agent_You_are_the_Design_T

Created session: 550e8400-e29b-41d4-a716-446655440000

=== Example 1: Full Project Status ===
Project Status Update:

**Engineering Status** (via Engineering Lead):
- Frontend: Lint passed, no ESLint warnings
- Backend: 45/45 tests passed with 87% coverage
- Database: Schema up to date

**Design Status** (via Design Lead):
- Accessibility: Score 94/100, WCAG 2.1 AA compliant

Overall: Project is on track for delivery.

=== Example 2: Engineering Focus ===
Engineering Update:
- Database migration completed (3 pending migrations applied)
- All backend tests passing (45/45, 87% coverage)

Ready for deployment.

=== Example 3: Design Focus ===
Design Review Complete:
The dashboard component passed accessibility review with a score of 94/100.
All color contrasts meet WCAG 2.1 AA standards.

=== Token Usage (Last Response) ===
Input tokens: 2156
Output tokens: 287

=== Demo Complete ===
```

## Considerations

### Token Usage
- Deep hierarchies increase total token usage
- Each delegation adds context overhead
- Consider using lighter models (Haiku) for workers

### Session Isolation
- Each agent has its own isolated session
- Tenant ID "nested" is used for nested agents
- Sessions persist between calls for context

### Best Practices

1. **Clear role definitions** - Each level has distinct responsibilities
2. **Appropriate delegation** - PM doesn't directly manage workers
3. **Synthesis at each level** - Leaders summarize before reporting up
4. **Tool placement** - Only leaf nodes (workers) have domain tools

## Use Cases

- **Project Management** - Coordinate complex multi-team projects
- **Customer Support** - Escalation chains (L1 → L2 → L3)
- **Content Creation** - Editor → Writers → Fact-checkers
- **Data Pipelines** - Orchestrator → Extractors → Transformers → Loaders

## Next Steps

- See [context_compaction](../../context_compaction/) for managing long conversations
- See [advanced/02_observability](../../advanced/02_observability/) for monitoring deep hierarchies
