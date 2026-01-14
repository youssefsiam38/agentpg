// Package main demonstrates the Client API with specialist agents.
//
// This example shows:
// - Per-client agent and tool registration
// - Multiple specialist agents with their own tools
// - Orchestrator pattern that delegates to specialists
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

// CodeAnalysisTool is a tool that the code specialist uses
type CodeAnalysisTool struct{}

func (c *CodeAnalysisTool) Name() string        { return "analyze_code" }
func (c *CodeAnalysisTool) Description() string { return "Analyze code for patterns and issues" }

func (c *CodeAnalysisTool) InputSchema() tool.ToolSchema {
	return tool.ToolSchema{
		Type: "object",
		Properties: map[string]tool.PropertyDef{
			"code": {
				Type:        "string",
				Description: "The code to analyze",
			},
			"language": {
				Type:        "string",
				Description: "Programming language",
				Enum:        []string{"go", "python", "javascript", "typescript"},
			},
		},
		Required: []string{"code", "language"},
	}
}

func (c *CodeAnalysisTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var params struct {
		Code     string `json:"code"`
		Language string `json:"language"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", err
	}

	// Simulated analysis
	lines := strings.Count(params.Code, "\n") + 1
	funcs := strings.Count(params.Code, "func ") + strings.Count(params.Code, "def ") + strings.Count(params.Code, "function ")

	return fmt.Sprintf("Code Analysis (%s):\n- Lines: %d\n- Functions detected: %d\n- Language: %s",
		params.Language, lines, funcs, params.Language), nil
}

// DataQueryTool is a tool that the data specialist uses
type DataQueryTool struct{}

func (d *DataQueryTool) Name() string { return "query_data" }
func (d *DataQueryTool) Description() string {
	return "Query and analyze datasets (simulated)"
}

func (d *DataQueryTool) InputSchema() tool.ToolSchema {
	return tool.ToolSchema{
		Type: "object",
		Properties: map[string]tool.PropertyDef{
			"dataset": {
				Type:        "string",
				Description: "Dataset to query",
				Enum:        []string{"sales", "users", "products", "logs"},
			},
			"operation": {
				Type:        "string",
				Description: "Operation to perform",
				Enum:        []string{"count", "average", "sum", "list"},
			},
			"field": {
				Type:        "string",
				Description: "Field to operate on (optional)",
			},
		},
		Required: []string{"dataset", "operation"},
	}
}

func (d *DataQueryTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var params struct {
		Dataset   string `json:"dataset"`
		Operation string `json:"operation"`
		Field     string `json:"field"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", err
	}

	// Simulated data responses
	data := map[string]map[string]string{
		"sales": {
			"count":   "Total records: 15,432",
			"average": "Average sale: $127.50",
			"sum":     "Total revenue: $1,967,100",
			"list":    "Top 5 sales: [$450, $380, $350, $320, $299]",
		},
		"users": {
			"count":   "Total users: 5,892",
			"average": "Average age: 34.2 years",
			"sum":     "Total sessions: 89,450",
			"list":    "Recent users: [john, alice, bob, carol, dave]",
		},
		"products": {
			"count":   "Total products: 1,245",
			"average": "Average price: $45.99",
			"sum":     "Total inventory value: $572,500",
			"list":    "Top products: [Widget A, Gadget B, Tool C]",
		},
		"logs": {
			"count":   "Total log entries: 1,234,567",
			"average": "Average response time: 145ms",
			"sum":     "Total errors: 3,421",
			"list":    "Recent errors: [404, 500, 503, 401, 403]",
		},
	}

	if result, ok := data[params.Dataset][params.Operation]; ok {
		return fmt.Sprintf("Query: %s.%s\nResult: %s", params.Dataset, params.Operation, result), nil
	}

	return "No data found", nil
}

// SearchTool is a tool that the research specialist uses
type SearchTool struct{}

func (s *SearchTool) Name() string        { return "search_knowledge" }
func (s *SearchTool) Description() string { return "Search knowledge base for information" }

func (s *SearchTool) InputSchema() tool.ToolSchema {
	return tool.ToolSchema{
		Type: "object",
		Properties: map[string]tool.PropertyDef{
			"query": {
				Type:        "string",
				Description: "Search query",
			},
			"domain": {
				Type:        "string",
				Description: "Knowledge domain to search",
				Enum:        []string{"technology", "science", "business", "general"},
			},
		},
		Required: []string{"query"},
	}
}

func (s *SearchTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var params struct {
		Query  string `json:"query"`
		Domain string `json:"domain"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", err
	}

	// Simulated search results
	return fmt.Sprintf("Search results for '%s' in %s domain:\n1. Relevant article about %s\n2. Documentation for %s\n3. Tutorial on %s basics",
		params.Query, params.Domain, params.Query, params.Query, params.Query), nil
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

	// Create the pgx/v5 driver
	drv := pgxv5.New(pool)

	// Create the AgentPG client
	client, err := agentpg.NewClient(drv, &agentpg.ClientConfig{
		APIKey: apiKey,
	})
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}

	// Register tools on the client
	if err := client.RegisterTool(&CodeAnalysisTool{}); err != nil {
		log.Fatalf("Failed to register code analysis tool: %v", err)
	}
	if err := client.RegisterTool(&DataQueryTool{}); err != nil {
		log.Fatalf("Failed to register data query tool: %v", err)
	}
	if err := client.RegisterTool(&SearchTool{}); err != nil {
		log.Fatalf("Failed to register search tool: %v", err)
	}

	maxTokensSpecialist := 2048
	maxTokensOrchestrator := 2048

	// Code Specialist - focuses on code analysis
	if err := client.RegisterAgent(&agentpg.AgentDefinition{
		Name:        "code-specialist",
		Description: "Code analysis specialist",
		Model:       "claude-sonnet-4-5-20250929",
		SystemPrompt: `You are a code analysis specialist. Your role is to:
1. Analyze code for patterns, issues, and improvements
2. Use the analyze_code tool to get metrics
3. Provide actionable feedback on code quality
4. Suggest best practices

Always use your tools to back up your analysis.`,
		Tools:     []string{"analyze_code"}, // Only code analysis tool
		MaxTokens: &maxTokensSpecialist,
	}); err != nil {
		log.Fatalf("Failed to register code-specialist agent: %v", err)
	}

	// Data Specialist - focuses on data analysis
	if err := client.RegisterAgent(&agentpg.AgentDefinition{
		Name:        "data-specialist",
		Description: "Data analysis specialist",
		Model:       "claude-sonnet-4-5-20250929",
		SystemPrompt: `You are a data analysis specialist. Your role is to:
1. Query and analyze datasets using the query_data tool
2. Provide insights from data
3. Identify trends and patterns
4. Make data-driven recommendations

Always query the data before providing insights.`,
		Tools:     []string{"query_data"}, // Only data query tool
		MaxTokens: &maxTokensSpecialist,
	}); err != nil {
		log.Fatalf("Failed to register data-specialist agent: %v", err)
	}

	// Research Specialist - focuses on knowledge search
	if err := client.RegisterAgent(&agentpg.AgentDefinition{
		Name:        "research-specialist",
		Description: "Research and knowledge specialist",
		Model:       "claude-sonnet-4-5-20250929",
		SystemPrompt: `You are a research specialist. Your role is to:
1. Search the knowledge base using search_knowledge tool
2. Synthesize information from multiple sources
3. Provide comprehensive explanations
4. Cite relevant resources

Use your search tool to find relevant information.`,
		Tools:     []string{"search_knowledge"}, // Only search tool
		MaxTokens: &maxTokensSpecialist,
	}); err != nil {
		log.Fatalf("Failed to register research-specialist agent: %v", err)
	}

	// Orchestrator agent - no direct tools, delegates to specialists
	if err := client.RegisterAgent(&agentpg.AgentDefinition{
		Name:        "orchestrator",
		Description: "Orchestrator that coordinates specialists",
		Model:       "claude-sonnet-4-5-20250929",
		SystemPrompt: `You are an orchestrator that coordinates multiple specialist agents.

Available specialists:
- Code Agent: For code analysis, reviews, and programming questions
- Data Agent: For data analysis, queries, and business metrics
- Research Agent: For general research and information lookup

Delegate tasks to the appropriate specialist based on the user's request.
You can delegate to multiple specialists if the task requires different expertise.
Synthesize their responses into a cohesive answer for the user.`,
		// Orchestrator delegates to specialist agents (registered as agent tools)
		Agents:    []string{"code-specialist", "data-specialist", "research-specialist"},
		MaxTokens: &maxTokensOrchestrator,
	}); err != nil {
		log.Fatalf("Failed to register orchestrator agent: %v", err)
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

	fmt.Println("=== Specialist Agents Setup Complete ===")
	fmt.Println("- Code Specialist: analyze_code tool")
	fmt.Println("- Data Specialist: query_data tool")
	fmt.Println("- Research Specialist: search_knowledge tool")
	fmt.Println("- Orchestrator: all specialists as tools")
	fmt.Println()

	// Create session
	sessionID, err := client.NewSession(ctx, "1", "specialist-demo", nil, map[string]any{
		"description": "Specialist agents demonstration",
	})
	if err != nil {
		log.Fatalf("Failed to create session: %v", err)
	}

	fmt.Printf("Created session: %s\n\n", sessionID)

	// ==========================================================
	// Example 1: Code-related question (Code Agent)
	// ==========================================================
	fmt.Println("=== Example 1: Code Analysis ===")
	codeQuestion := `Analyze this Go code:
func fibonacci(n int) int {
    if n <= 1 {
        return n
    }
    return fibonacci(n-1) + fibonacci(n-2)
}
`
	response1, err := client.RunSync(ctx, sessionID, "orchestrator", codeQuestion)
	if err != nil {
		log.Fatalf("Failed to run agent: %v", err)
	}

	for _, block := range response1.Message.Content {
		if block.Type == agentpg.ContentTypeText {
			fmt.Println(block.Text)
		}
	}

	// ==========================================================
	// Example 2: Data question (Data Agent)
	// ==========================================================
	fmt.Println("\n=== Example 2: Data Analysis ===")
	response2, err := client.RunSync(ctx, sessionID, "orchestrator", "What are our total sales and how many users do we have?")
	if err != nil {
		log.Fatalf("Failed to run agent: %v", err)
	}

	for _, block := range response2.Message.Content {
		if block.Type == agentpg.ContentTypeText {
			fmt.Println(block.Text)
		}
	}

	// ==========================================================
	// Example 3: Research question (Research Agent)
	// ==========================================================
	fmt.Println("\n=== Example 3: Research Query ===")
	response3, err := client.RunSync(ctx, sessionID, "orchestrator", "Research best practices for API design")
	if err != nil {
		log.Fatalf("Failed to run agent: %v", err)
	}

	for _, block := range response3.Message.Content {
		if block.Type == agentpg.ContentTypeText {
			fmt.Println(block.Text)
		}
	}

	// ==========================================================
	// Example 4: Multi-specialist question
	// ==========================================================
	fmt.Println("\n=== Example 4: Multi-Specialist Query ===")
	response4, err := client.RunSync(ctx, sessionID, "orchestrator", "I'm building a new feature. Research best practices for user authentication, and also check our current user metrics to see how many users we have.")
	if err != nil {
		log.Fatalf("Failed to run agent: %v", err)
	}

	for _, block := range response4.Message.Content {
		if block.Type == agentpg.ContentTypeText {
			fmt.Println(block.Text)
		}
	}

	fmt.Println("\n=== Demo Complete ===")
}
