package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/youssefsiam38/agentpg"
	"github.com/youssefsiam38/agentpg/tool"
)

// SimpleTool is a minimal tool for demonstration
type SimpleTool struct {
	name        string
	description string
	delay       time.Duration
	result      string
}

func NewSimpleTool(name, description string, delay time.Duration, result string) *SimpleTool {
	return &SimpleTool{
		name:        name,
		description: description,
		delay:       delay,
		result:      result,
	}
}

func (s *SimpleTool) Name() string        { return s.name }
func (s *SimpleTool) Description() string { return s.description }

func (s *SimpleTool) InputSchema() tool.ToolSchema {
	return tool.ToolSchema{
		Type: "object",
		Properties: map[string]tool.PropertyDef{
			"input": {
				Type:        "string",
				Description: "Input for the tool",
			},
		},
		Required: []string{"input"},
	}
}

func (s *SimpleTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var params struct {
		Input string `json:"input"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", err
	}

	// Simulate work
	time.Sleep(s.delay)

	return fmt.Sprintf("[%s] Processed '%s': %s", s.name, params.Input, s.result), nil
}

// DataFetchTool simulates fetching data from different sources
type DataFetchTool struct {
	mu       sync.Mutex
	callLog  []string
	fetchLog []time.Time
}

func NewDataFetchTool() *DataFetchTool {
	return &DataFetchTool{
		callLog:  make([]string, 0),
		fetchLog: make([]time.Time, 0),
	}
}

func (d *DataFetchTool) Name() string        { return "fetch_data" }
func (d *DataFetchTool) Description() string { return "Fetch data from a specified source" }

func (d *DataFetchTool) InputSchema() tool.ToolSchema {
	return tool.ToolSchema{
		Type: "object",
		Properties: map[string]tool.PropertyDef{
			"source": {
				Type:        "string",
				Description: "Data source to fetch from",
				Enum:        []string{"database", "api", "cache", "file"},
			},
			"query": {
				Type:        "string",
				Description: "Query or identifier for the data",
			},
		},
		Required: []string{"source", "query"},
	}
}

func (d *DataFetchTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var params struct {
		Source string `json:"source"`
		Query  string `json:"query"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", err
	}

	// Log the call
	d.mu.Lock()
	d.callLog = append(d.callLog, fmt.Sprintf("%s:%s", params.Source, params.Query))
	d.fetchLog = append(d.fetchLog, time.Now())
	d.mu.Unlock()

	// Simulate different fetch times
	var delay time.Duration
	switch params.Source {
	case "cache":
		delay = 10 * time.Millisecond
	case "database":
		delay = 50 * time.Millisecond
	case "api":
		delay = 100 * time.Millisecond
	case "file":
		delay = 30 * time.Millisecond
	}
	time.Sleep(delay)

	// Simulated data responses
	data := map[string]string{
		"database": fmt.Sprintf("DB record for '%s': {id: 123, status: 'active'}", params.Query),
		"api":      fmt.Sprintf("API response for '%s': {data: 'fetched', timestamp: '%s'}", params.Query, time.Now().Format(time.RFC3339)),
		"cache":    fmt.Sprintf("Cached value for '%s': 'cached_data_v1'", params.Query),
		"file":     fmt.Sprintf("File contents for '%s': 'file data here...'", params.Query),
	}

	return data[params.Source], nil
}

func (d *DataFetchTool) GetCallLog() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	result := make([]string, len(d.callLog))
	copy(result, d.callLog)
	return result
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

	// ==========================================================
	// Part 1: Demonstrate tool.Registry management
	// ==========================================================
	fmt.Println("=== Part 1: Registry Management ===")
	fmt.Println()

	// Create a standalone registry
	registry := tool.NewRegistry()

	// Register individual tools
	tool1 := NewSimpleTool("analyzer", "Analyze data", 10*time.Millisecond, "analysis complete")
	tool2 := NewSimpleTool("transformer", "Transform data", 15*time.Millisecond, "transformation done")
	tool3 := NewSimpleTool("validator", "Validate data", 5*time.Millisecond, "validation passed")

	if err := registry.Register(tool1); err != nil {
		log.Fatalf("Failed to register tool: %v", err)
	}

	// Register multiple tools at once
	if err := registry.RegisterAll([]tool.Tool{tool2, tool3}); err != nil {
		log.Fatalf("Failed to register tools: %v", err)
	}

	// Registry operations
	fmt.Printf("Tool count: %d\n", registry.Count())
	fmt.Printf("Has 'analyzer': %v\n", registry.Has("analyzer"))
	fmt.Printf("Has 'unknown': %v\n", registry.Has("unknown"))
	fmt.Printf("Registered tools: %v\n", registry.List())

	// Get specific tool
	if t, ok := registry.Get("analyzer"); ok {
		fmt.Printf("Got tool: %s - %s\n", t.Name(), t.Description())
	}

	// ==========================================================
	// Part 2: Demonstrate tool.Executor with different modes
	// ==========================================================
	fmt.Println()
	fmt.Println("=== Part 2: Executor Modes ===")
	fmt.Println()

	executor := tool.NewExecutor(registry)
	executor.SetDefaultTimeout(5 * time.Second)

	// Prepare tool calls
	calls := []tool.ToolCallRequest{
		{ID: "call-1", ToolName: "analyzer", Input: json.RawMessage(`{"input": "test1"}`)},
		{ID: "call-2", ToolName: "transformer", Input: json.RawMessage(`{"input": "test2"}`)},
		{ID: "call-3", ToolName: "validator", Input: json.RawMessage(`{"input": "test3"}`)},
	}

	// Sequential execution
	fmt.Println("Sequential execution:")
	start := time.Now()
	seqResults := executor.ExecuteMultiple(ctx, calls)
	seqDuration := time.Since(start)

	for i, r := range seqResults {
		if r.Error != nil {
			fmt.Printf("  %s: Error - %v\n", calls[i].ID, r.Error)
		} else {
			fmt.Printf("  %s: %s (%.0fms)\n", calls[i].ID, r.Output, float64(r.Duration.Microseconds())/1000)
		}
	}
	fmt.Printf("  Total time: %.0fms\n", float64(seqDuration.Microseconds())/1000)

	// Parallel execution
	fmt.Println("\nParallel execution:")
	start = time.Now()
	parResults := executor.ExecuteParallel(ctx, calls)
	parDuration := time.Since(start)

	for i, r := range parResults {
		if r.Error != nil {
			fmt.Printf("  %s: Error - %v\n", calls[i].ID, r.Error)
		} else {
			fmt.Printf("  %s: %s (%.0fms)\n", calls[i].ID, r.Output, float64(r.Duration.Microseconds())/1000)
		}
	}
	fmt.Printf("  Total time: %.0fms\n", float64(parDuration.Microseconds())/1000)
	fmt.Printf("  Speedup: %.2fx faster\n", float64(seqDuration)/float64(parDuration))

	// Batch execution (respects parallel flag)
	fmt.Println("\nBatch execution (parallel=true):")
	start = time.Now()
	batchResults := executor.ExecuteBatch(ctx, calls, true) // parallel=true
	batchDuration := time.Since(start)
	fmt.Printf("  Processed %d calls in %.0fms\n", len(batchResults), float64(batchDuration.Microseconds())/1000)

	// ==========================================================
	// Part 3: Use with AgentPG
	// ==========================================================
	fmt.Println()
	fmt.Println("=== Part 3: AgentPG Integration ===")
	fmt.Println()

	// Create agent with multiple tools
	agent, err := agentpg.New(
		agentpg.Config{
			DB:           pool,
			Client:       &client,
			Model:        "claude-sonnet-4-5-20250929",
			SystemPrompt: "You are a data processing assistant. Use the available tools to fetch and process data. When asked to fetch from multiple sources, call the tools efficiently.",
		},
		agentpg.WithMaxTokens(2048),
	)
	if err != nil {
		log.Fatalf("Failed to create agent: %v", err)
	}

	// Register data fetch tool
	dataFetcher := NewDataFetchTool()
	if err := agent.RegisterTool(dataFetcher); err != nil {
		log.Fatalf("Failed to register tool: %v", err)
	}

	// Create session
	sessionID, err := agent.NewSession(ctx, "1", "parallel-execution-demo", nil, map[string]any{
		"description": "Parallel execution demonstration",
	})
	if err != nil {
		log.Fatalf("Failed to create session: %v", err)
	}

	fmt.Printf("Created session: %s\n\n", sessionID)

	// Run agent with request that might trigger multiple tool calls
	fmt.Println("Requesting data from multiple sources...")
	response, err := agent.Run(ctx, "Fetch the user profile from both the database and the cache, and also get the latest data from the API. The query for all should be 'user-123'.")
	if err != nil {
		log.Fatalf("Failed to run agent: %v", err)
	}

	for _, block := range response.Message.Content {
		if block.Type == agentpg.ContentTypeText {
			fmt.Println(block.Text)
		}
	}

	// Show call log
	fmt.Println("\n=== Tool Call Log ===")
	for i, call := range dataFetcher.GetCallLog() {
		fmt.Printf("%d. %s\n", i+1, call)
	}

	// Show registered tools
	fmt.Println("\n=== Registered Tools ===")
	for _, name := range agent.GetTools() {
		fmt.Printf("- %s\n", name)
	}

	fmt.Println("\n=== Demo Complete ===")
}
