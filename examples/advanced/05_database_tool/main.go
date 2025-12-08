// Package main demonstrates the Client API with database query tool.
//
// This example shows:
// - Safe database query tool with SQL injection protection
// - Table whitelist and query validation
// - Read-only query enforcement
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/youssefsiam38/agentpg"
	"github.com/youssefsiam38/agentpg/driver/pgxv5"
	"github.com/youssefsiam38/agentpg/tool"
)

// DatabaseQueryTool allows the agent to query the database safely
type DatabaseQueryTool struct {
	pool          *pgxpool.Pool
	allowedTables []string
	maxRows       int
}

func NewDatabaseQueryTool(pool *pgxpool.Pool, allowedTables []string) *DatabaseQueryTool {
	return &DatabaseQueryTool{
		pool:          pool,
		allowedTables: allowedTables,
		maxRows:       100, // Safety limit
	}
}

func (d *DatabaseQueryTool) Name() string {
	return "query_database"
}

func (d *DatabaseQueryTool) Description() string {
	return fmt.Sprintf("Execute a read-only SQL query. Allowed tables: %s. Maximum %d rows returned.",
		strings.Join(d.allowedTables, ", "), d.maxRows)
}

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

func (d *DatabaseQueryTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var params struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}

	// Validate query is SELECT only
	query := strings.TrimSpace(params.Query)
	queryUpper := strings.ToUpper(query)

	if !strings.HasPrefix(queryUpper, "SELECT") {
		return "", fmt.Errorf("only SELECT queries are allowed")
	}

	// Check for dangerous keywords
	dangerous := []string{"INSERT", "UPDATE", "DELETE", "DROP", "ALTER", "CREATE", "TRUNCATE", "GRANT", "REVOKE"}
	for _, keyword := range dangerous {
		if strings.Contains(queryUpper, keyword) {
			return "", fmt.Errorf("query contains forbidden keyword: %s", keyword)
		}
	}

	// Check that query only references allowed tables
	tableFound := false
	for _, table := range d.allowedTables {
		if strings.Contains(strings.ToLower(query), strings.ToLower(table)) {
			tableFound = true
			break
		}
	}
	if !tableFound && len(d.allowedTables) > 0 {
		return "", fmt.Errorf("query must reference one of the allowed tables: %s", strings.Join(d.allowedTables, ", "))
	}

	// Add LIMIT if not present
	if !strings.Contains(queryUpper, "LIMIT") {
		query = fmt.Sprintf("%s LIMIT %d", query, d.maxRows)
	}

	// Execute query
	rows, err := d.pool.Query(ctx, query)
	if err != nil {
		return "", fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	// Get column names
	fieldDescs := rows.FieldDescriptions()
	columns := make([]string, len(fieldDescs))
	for i, fd := range fieldDescs {
		columns[i] = string(fd.Name)
	}

	// Collect results
	var results []map[string]interface{}
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			return "", fmt.Errorf("error reading row: %w", err)
		}

		row := make(map[string]interface{})
		for i, col := range columns {
			row[col] = values[i]
		}
		results = append(results, row)
	}

	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("error iterating rows: %w", err)
	}

	// Format results
	if len(results) == 0 {
		return "No rows returned.", nil
	}

	output, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return "", fmt.Errorf("error formatting results: %w", err)
	}

	return fmt.Sprintf("Query returned %d row(s):\n%s", len(results), string(output)), nil
}

// Register agent at package initialization.
func init() {
	maxTokens := 1024
	agentpg.MustRegister(&agentpg.AgentDefinition{
		Name:        "database-tool-demo",
		Description: "Data analyst assistant with database access",
		Model:       "claude-sonnet-4-5-20250929",
		SystemPrompt: `You are a helpful data analyst assistant. You have access to a product database.
Use the query_database tool to answer questions about products.
The demo_products table has columns: id, name, category, price, stock, created_at.`,
		MaxTokens: &maxTokens,
	})
}

func main() {
	// Create a context that cancels on SIGINT/SIGTERM
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Get environment variables
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		log.Fatal("ANTHROPIC_API_KEY environment variable is required")
	}

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Fatal("DATABASE_URL environment variable is required")
	}

	// Create PostgreSQL connection pool
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer pool.Close()

	// ==========================================================
	// Setup: Create sample table for demo
	// ==========================================================

	fmt.Println("=== Database Query Tool Example ===")
	fmt.Println()

	// Create a sample table
	_, err = pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS demo_products (
			id SERIAL PRIMARY KEY,
			name VARCHAR(100) NOT NULL,
			category VARCHAR(50),
			price DECIMAL(10,2),
			stock INT,
			created_at TIMESTAMP DEFAULT NOW()
		)
	`)
	if err != nil {
		log.Fatalf("Failed to create table: %v", err)
	}

	// Insert sample data (ignore errors if already exists)
	sampleProducts := []struct {
		name     string
		category string
		price    float64
		stock    int
	}{
		{"Laptop Pro", "Electronics", 1299.99, 50},
		{"Wireless Mouse", "Electronics", 29.99, 200},
		{"Office Chair", "Furniture", 249.99, 30},
		{"Standing Desk", "Furniture", 599.99, 15},
		{"Monitor 27\"", "Electronics", 399.99, 75},
	}

	for _, p := range sampleProducts {
		pool.Exec(ctx, `
			INSERT INTO demo_products (name, category, price, stock)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT DO NOTHING
		`, p.name, p.category, p.price, p.stock)
	}

	fmt.Println("Sample table 'demo_products' created with data.")
	fmt.Println()

	// ==========================================================
	// Create agent with database tool
	// ==========================================================

	// Create the pgx/v5 driver
	drv := pgxv5.New(pool)

	// Create the AgentPG client
	client, err := agentpg.NewClient(drv, &agentpg.ClientConfig{
		APIKey: apiKey,
	})
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}

	// Start the client
	if err := client.Start(ctx); err != nil {
		log.Fatalf("Failed to start client: %v", err)
	}
	defer func() {
		if err := client.Stop(context.Background()); err != nil {
			log.Printf("Error stopping client: %v", err)
		}
	}()

	log.Printf("Client started (instance ID: %s)", client.InstanceID())

	// Get the agent
	agent := client.Agent("database-tool-demo")
	if agent == nil {
		log.Fatal("Agent 'database-tool-demo' not found")
	}

	// Register database tool with allowed tables (runtime registration for stateful tool)
	dbTool := NewDatabaseQueryTool(pool, []string{"demo_products"})
	if err := agent.RegisterTool(dbTool); err != nil {
		log.Fatalf("Failed to register tool: %v", err)
	}

	// Create session
	sessionID, err := agent.NewSession(ctx, "1", "db-tool-demo", nil, nil)
	if err != nil {
		log.Fatalf("Failed to create session: %v", err)
	}
	fmt.Printf("Session: %s\n\n", sessionID[:8]+"...")

	// ==========================================================
	// Demo queries
	// ==========================================================

	queries := []string{
		"What products do we have in the Electronics category?",
		"What is our most expensive product?",
		"How many items do we have in stock across all products?",
	}

	for i, query := range queries {
		fmt.Printf("=== Query %d ===\n", i+1)
		fmt.Printf("User: %s\n\n", query)

		response, err := agent.Run(ctx, sessionID, query)
		if err != nil {
			log.Printf("Error: %v\n\n", err)
			continue
		}

		for _, block := range response.Message.Content {
			if block.Type == agentpg.ContentTypeText {
				fmt.Printf("Agent: %s\n", block.Text)
			}
		}
		fmt.Println()
	}

	// ==========================================================
	// Demo: Security - blocked queries
	// ==========================================================

	fmt.Println("=== Security Demo ===")
	fmt.Println()

	// Try to execute a write query (should be blocked)
	fmt.Println("Testing tool security directly:")

	dangerousQueries := []string{
		"DELETE FROM demo_products WHERE id = 1",
		"DROP TABLE demo_products",
		"SELECT * FROM users", // Not in allowed tables
	}

	for _, q := range dangerousQueries {
		input, _ := json.Marshal(map[string]string{"query": q})
		result, err := dbTool.Execute(ctx, input)
		if err != nil {
			fmt.Printf("  BLOCKED: %s\n    Reason: %v\n", q[:30]+"...", err)
		} else {
			fmt.Printf("  ALLOWED: %s -> %s\n", q, result)
		}
	}

	// ==========================================================
	// Summary
	// ==========================================================

	fmt.Println()
	fmt.Println("=== Database Tool Safety Features ===")
	fmt.Println("1. SELECT-only queries (no INSERT, UPDATE, DELETE)")
	fmt.Println("2. Allowed tables whitelist")
	fmt.Println("3. Automatic LIMIT clause")
	fmt.Println("4. Dangerous keyword blocking")
	fmt.Println()
	fmt.Println("=== Demo Complete ===")
}
