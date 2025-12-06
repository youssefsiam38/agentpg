# Database Query Tool Example

This example demonstrates a safe SQL query tool that allows Claude to query a database.

## Features

- **Read-Only Queries**: Only SELECT statements allowed
- **Table Whitelist**: Restrict queries to specific tables
- **Row Limits**: Automatic LIMIT clause for safety
- **Keyword Blocking**: Prevents dangerous SQL keywords

## Security Layers

### 1. SELECT-Only Validation
```go
if !strings.HasPrefix(queryUpper, "SELECT") {
    return "", fmt.Errorf("only SELECT queries are allowed")
}
```

### 2. Dangerous Keyword Blocking
```go
dangerous := []string{"INSERT", "UPDATE", "DELETE", "DROP", "ALTER", "CREATE", "TRUNCATE", "GRANT", "REVOKE"}
for _, keyword := range dangerous {
    if strings.Contains(queryUpper, keyword) {
        return "", fmt.Errorf("query contains forbidden keyword: %s", keyword)
    }
}
```

### 3. Table Whitelist
```go
dbTool := NewDatabaseQueryTool(pool, []string{"demo_products", "demo_categories"})
```

### 4. Automatic Row Limits
```go
if !strings.Contains(queryUpper, "LIMIT") {
    query = fmt.Sprintf("%s LIMIT %d", query, d.maxRows)
}
```

## Tool Schema

```go
func (d *DatabaseQueryTool) InputSchema() tool.ToolSchema {
    return tool.ToolSchema{
        Type: "object",
        Properties: map[string]tool.PropertyDef{
            "query": {
                Type:        "string",
                Description: "SQL SELECT query to execute. Must be a SELECT statement only.",
            },
        },
        Required: []string{"query"},
    }
}
```

## Sample Conversation

```
User: What products do we have in the Electronics category?

Agent: [Uses query_database tool]
Query: SELECT * FROM demo_products WHERE category = 'Electronics'

Result:
[
  {"id": 1, "name": "Laptop Pro", "category": "Electronics", "price": 1299.99, "stock": 50},
  {"id": 2, "name": "Wireless Mouse", "category": "Electronics", "price": 29.99, "stock": 200},
  {"id": 5, "name": "Monitor 27\"", "category": "Electronics", "price": 399.99, "stock": 75}
]

Agent: We have 3 products in the Electronics category: Laptop Pro ($1,299.99),
Wireless Mouse ($29.99), and a 27" Monitor ($399.99).
```

## Blocked Queries

```
BLOCKED: DELETE FROM demo_products...
  Reason: query contains forbidden keyword: DELETE

BLOCKED: DROP TABLE demo_products...
  Reason: query contains forbidden keyword: DROP

BLOCKED: SELECT * FROM users...
  Reason: query must reference one of the allowed tables: demo_products
```

## Production Considerations

1. **Use Prepared Statements**: For any user-provided values
2. **Query Timeout**: Set context timeout for long queries
3. **Result Size Limits**: Truncate large result sets
4. **Audit Logging**: Log all queries for security review
5. **Read Replica**: Point tool at read replica to protect primary

## Database User Permissions

In production, use a database user with minimal permissions:

```sql
CREATE USER agent_reader WITH PASSWORD 'secure_password';
GRANT SELECT ON demo_products TO agent_reader;
-- No INSERT, UPDATE, DELETE permissions
```

## Running

```bash
export ANTHROPIC_API_KEY="your-api-key"
export DATABASE_URL="postgres://user:pass@localhost:5432/agentpg"

go run main.go
```
