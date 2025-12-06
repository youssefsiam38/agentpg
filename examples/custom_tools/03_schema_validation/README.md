# Schema Validation Example

This example demonstrates advanced JSON Schema validation features using `tool.PropertyDef` constraints.

## PropertyDef Constraints

The `tool.PropertyDef` struct supports these validation constraints:

| Constraint | Type | Description |
|------------|------|-------------|
| `Type` | string | JSON type: "string", "number", "boolean", "array", "object" |
| `Description` | string | Human-readable description for Claude |
| `Enum` | []string | Restrict to specific values |
| `Minimum` | *float64 | Minimum value for numbers |
| `Maximum` | *float64 | Maximum value for numbers |
| `MinLength` | *int | Minimum length for strings |
| `MaxLength` | *int | Maximum length for strings |
| `Items` | *PropertyDef | Schema for array items |
| `Properties` | map[string]PropertyDef | Nested object properties |

## Schema Examples in This Demo

### 1. String with Length Constraints
```go
"title": {
    Type:        "string",
    Description: "Task title (3-100 characters)",
    MinLength:   &minTitleLen,  // 3
    MaxLength:   &maxTitleLen,  // 100
},
```

### 2. Enum Constraint
```go
"priority": {
    Type:        "string",
    Description: "Task priority level",
    Enum:        []string{"low", "medium", "high", "critical"},
},
```

### 3. Number with Min/Max
```go
"score": {
    Type:        "number",
    Description: "Task importance score from 0 to 100",
    Minimum:     &minScore,  // 0.0
    Maximum:     &maxScore,  // 100.0
},
```

### 4. Array with Items Schema
```go
"tags": {
    Type:        "array",
    Description: "List of tags for categorization",
    Items: &tool.PropertyDef{
        Type:        "string",
        Description: "A single tag",
    },
},
```

### 5. Nested Object
```go
"assignee": {
    Type:        "object",
    Description: "Person assigned to this task",
    Properties: map[string]tool.PropertyDef{
        "name": {
            Type:        "string",
            Description: "Assignee's full name",
        },
        "email": {
            Type:        "string",
            Description: "Assignee's email address",
        },
    },
},
```

### 6. Required Fields
```go
Required: []string{"title", "priority"},
```

## Tools Created

| Tool | Description |
|------|-------------|
| `create_task` | Create tasks with all validation constraints |
| `list_tasks` | List tasks with optional priority filter |

## Running the Example

```bash
export ANTHROPIC_API_KEY="your-api-key"
export DATABASE_URL="postgresql://user:pass@localhost:5432/agentpg"

cd examples/custom_tools/03_schema_validation
go run main.go
```

## Expected Output

```
Created session: 550e8400-e29b-41d4-a716-446655440000

=== Example 1: Full Task Creation ===
Task #1 created successfully!
- Title: Implement user authentication
- Priority: high
- Score: 85.0
- Tags: security, backend, urgent
- Assignee: John Smith <john@example.com>
- Description: Implementing OAuth2 for user authentication

=== Example 2: Minimal Task ===
Task #2 created successfully!
- Title: Update documentation
- Priority: low
- Score: 0.0

=== Example 3: Critical Task with Tags ===
Task #3 created successfully!
- Title: Fix production database issue
- Priority: critical
- Score: 100.0
- Tags: production, database, emergency

=== Example 4: List All Tasks ===
Tasks:

#1: Implement user authentication
    Priority: high | Score: 85.0
    Tags: security, backend, urgent

#2: Update documentation
    Priority: low | Score: 0.0

#3: Fix production database issue
    Priority: critical | Score: 100.0
    Tags: production, database, emergency

Total: 3 task(s)

=== Example 5: Filter by Priority ===
Tasks:

#3: Fix production database issue
    Priority: critical | Score: 100.0
    Tags: production, database, emergency

Total: 1 task(s)

=== Registered Tools ===
- create_task
- list_tasks

=== Demo Complete ===
```

## Validation Flow

1. **Schema Definition** - Define constraints in `InputSchema()`
2. **Claude Interpretation** - Claude reads the schema and formats input accordingly
3. **Server-Side Validation** - Additional validation in `Execute()` for edge cases
4. **Error Handling** - Return clear error messages for invalid input

## Best Practices

1. **Always set `Type`** - Required for Claude to understand the parameter
2. **Use `Description`** - Helps Claude understand context
3. **Use `Enum` for fixed choices** - Prevents invalid values
4. **Set `Required` array** - Explicitly mark required fields
5. **Validate in Execute()** - Double-check constraints server-side

## Next Steps

- See [04_parallel_execution](../04_parallel_execution/) for registry and execution patterns
