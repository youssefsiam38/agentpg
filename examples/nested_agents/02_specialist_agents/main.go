package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
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
	ctx := context.Background()

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

	// Create Anthropic client
	client := anthropic.NewClient(option.WithAPIKey(apiKey))

	// Create driver
	drv := pgxv5.New(pool)

	// ==========================================================
	// Create SPECIALIST AGENTS with their own tools
	// ==========================================================

	// Code Specialist - focuses on code analysis
	codeAgent, err := agentpg.New(
		drv,
		agentpg.Config{
			Client: &client,
			Model:  "claude-sonnet-4-5-20250929",
			SystemPrompt: `You are a code analysis specialist. Your role is to:
1. Analyze code for patterns, issues, and improvements
2. Use the analyze_code tool to get metrics
3. Provide actionable feedback on code quality
4. Suggest best practices

Always use your tools to back up your analysis.`,
		},
		agentpg.WithMaxTokens(2048),
	)
	if err != nil {
		log.Fatalf("Failed to create code agent: %v", err)
	}

	// Register code analysis tool
	if err := codeAgent.RegisterTool(&CodeAnalysisTool{}); err != nil {
		log.Fatalf("Failed to register tool: %v", err)
	}

	// Data Specialist - focuses on data analysis
	dataAgent, err := agentpg.New(
		drv,
		agentpg.Config{
			Client: &client,
			Model:  "claude-sonnet-4-5-20250929",
			SystemPrompt: `You are a data analysis specialist. Your role is to:
1. Query and analyze datasets using the query_data tool
2. Provide insights from data
3. Identify trends and patterns
4. Make data-driven recommendations

Always query the data before providing insights.`,
		},
		agentpg.WithMaxTokens(2048),
	)
	if err != nil {
		log.Fatalf("Failed to create data agent: %v", err)
	}

	// Register data query tool
	if err := dataAgent.RegisterTool(&DataQueryTool{}); err != nil {
		log.Fatalf("Failed to register tool: %v", err)
	}

	// Research Specialist - focuses on knowledge search
	researchAgent, err := agentpg.New(
		drv,
		agentpg.Config{
			Client: &client,
			Model:  "claude-sonnet-4-5-20250929",
			SystemPrompt: `You are a research specialist. Your role is to:
1. Search the knowledge base using search_knowledge tool
2. Synthesize information from multiple sources
3. Provide comprehensive explanations
4. Cite relevant resources

Use your search tool to find relevant information.`,
		},
		agentpg.WithMaxTokens(2048),
	)
	if err != nil {
		log.Fatalf("Failed to create research agent: %v", err)
	}

	// Register search tool
	if err := researchAgent.RegisterTool(&SearchTool{}); err != nil {
		log.Fatalf("Failed to register tool: %v", err)
	}

	// ==========================================================
	// Create ORCHESTRATOR agent
	// ==========================================================
	orchestrator, err := agentpg.New(
		drv,
		agentpg.Config{
			Client: &client,
			Model:  "claude-sonnet-4-5-20250929",
			SystemPrompt: `You are an orchestrator that coordinates multiple specialist agents.

Available specialists:
- Code Agent: For code analysis, reviews, and programming questions
- Data Agent: For data analysis, queries, and business metrics
- Research Agent: For general research and information lookup

Delegate tasks to the appropriate specialist based on the user's request.
You can delegate to multiple specialists if the task requires different expertise.
Synthesize their responses into a cohesive answer for the user.`,
		},
		agentpg.WithMaxTokens(2048),
	)
	if err != nil {
		log.Fatalf("Failed to create orchestrator: %v", err)
	}

	// Register all specialists as tools for the orchestrator
	if err := codeAgent.AsToolFor(orchestrator); err != nil {
		log.Fatalf("Failed to register code agent: %v", err)
	}
	if err := dataAgent.AsToolFor(orchestrator); err != nil {
		log.Fatalf("Failed to register data agent: %v", err)
	}
	if err := researchAgent.AsToolFor(orchestrator); err != nil {
		log.Fatalf("Failed to register research agent: %v", err)
	}

	// Show registered tools
	fmt.Println("=== Orchestrator Tools ===")
	for _, name := range orchestrator.GetTools() {
		fmt.Printf("- %s\n", name)
	}
	fmt.Println()

	// Create session
	sessionID, err := orchestrator.NewSession(ctx, "1", "specialist-demo", nil, map[string]any{
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
	response1, err := orchestrator.Run(ctx, codeQuestion)
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
	response2, err := orchestrator.Run(ctx, "What are our total sales and how many users do we have?")
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
	response3, err := orchestrator.Run(ctx, "Research best practices for API design")
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
	response4, err := orchestrator.Run(ctx, "I'm building a new feature. Research best practices for user authentication, and also check our current user metrics to see how many users we have.")
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
