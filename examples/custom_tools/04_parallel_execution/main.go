// Package main demonstrates tool usage with AgentPG.
//
// This example shows:
// - Creating custom tools with the tool.Tool interface
// - Registering tools on the AgentPG client
// - How Claude can call multiple tools in a single response
// - Tracking tool execution through call logs
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/youssefsiam38/agentpg"
	"github.com/youssefsiam38/agentpg/driver/pgxv5"
	"github.com/youssefsiam38/agentpg/tool"
)

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

	// Log the call with timestamp
	d.mu.Lock()
	d.callLog = append(d.callLog, fmt.Sprintf("%s:%s", params.Source, params.Query))
	d.fetchLog = append(d.fetchLog, time.Now())
	d.mu.Unlock()

	// Simulate different fetch times based on source
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

func (d *DataFetchTool) GetFetchTimes() []time.Time {
	d.mu.Lock()
	defer d.mu.Unlock()
	result := make([]time.Time, len(d.fetchLog))
	copy(result, d.fetchLog)
	return result
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

	// Create driver
	drv := pgxv5.New(pool)

	// Create the AgentPG client
	client, err := agentpg.NewClient(drv, &agentpg.ClientConfig{
		APIKey: apiKey,
	})
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}

	// Register data fetch tool on client
	dataFetcher := NewDataFetchTool()
	if err := client.RegisterTool(dataFetcher); err != nil {
		log.Fatalf("Failed to register tool: %v", err)
	}

	// Start the client
	if err := client.Start(ctx); err != nil {
		log.Fatalf("Failed to start client: %v", err)
	}

	// Create agent in the database (after client.Start)
	maxTokens := 2048
	agent, err := client.CreateAgent(ctx, &agentpg.AgentDefinition{
		Name:         "agent.ID",
		Description:  "A data processing assistant",
		Model:        "claude-sonnet-4-5-20250929",
		SystemPrompt: "You are a data processing assistant. Use the available tools to fetch and process data. When asked to fetch from multiple sources, call all the fetch_data tools in parallel (in the same response) to be efficient.",
		MaxTokens:    &maxTokens,
		Tools:        []string{"fetch_data"},
	})
	if err != nil {
		log.Fatalf("Failed to create agent: %v", err)
	}
	defer func() {
		if err := client.Stop(context.Background()); err != nil {
			log.Printf("Error stopping client: %v", err)
		}
	}()

	fmt.Println("=== AgentPG Tool Execution Demo ===")
	fmt.Println()
	log.Printf("Client started (instance ID: %s)", client.InstanceID())

	// Create session
	sessionID, err := client.NewSession(ctx, nil, map[string]any{
		"description": "Tool execution demonstration",
	})
	if err != nil {
		log.Fatalf("Failed to create session: %v", err)
	}

	fmt.Printf("Created session: %s\n\n", sessionID)

	// Run agent with request that triggers multiple tool calls
	// Claude may call fetch_data multiple times in a single response
	fmt.Println("Requesting data from multiple sources...")
	fmt.Println("(Claude may call the tool multiple times in parallel)")
	fmt.Println()

	start := time.Now()
	response, err := client.RunFastSync(ctx, sessionID, agent.ID,
		"Fetch the user profile from the database, the cache, and the API. The query for all should be 'user-123'. Call all three fetch_data tools in your response.", nil)
	if err != nil {
		log.Fatalf("Failed to run agent: %v", err)
	}
	totalDuration := time.Since(start)

	fmt.Println("=== Agent Response ===")
	for _, block := range response.Message.Content {
		if block.Type == agentpg.ContentTypeText {
			fmt.Println(block.Text)
		}
	}

	// Show execution statistics
	fmt.Println()
	fmt.Println("=== Execution Statistics ===")
	fmt.Printf("Total time: %v\n", totalDuration)
	fmt.Printf("Iterations: %d\n", response.IterationCount)
	fmt.Printf("Tool iterations: %d\n", response.ToolIterations)
	fmt.Printf("Input tokens: %d\n", response.Usage.InputTokens)
	fmt.Printf("Output tokens: %d\n", response.Usage.OutputTokens)

	// Show call log to demonstrate parallel execution
	fmt.Println()
	fmt.Println("=== Tool Call Log ===")
	callLog := dataFetcher.GetCallLog()
	fetchTimes := dataFetcher.GetFetchTimes()

	if len(callLog) > 0 {
		firstCall := fetchTimes[0]
		for i, call := range callLog {
			offset := fetchTimes[i].Sub(firstCall)
			fmt.Printf("%d. %s (started at +%v)\n", i+1, call, offset)
		}

		// If all calls started within a short window, they were parallel
		if len(fetchTimes) > 1 {
			lastOffset := fetchTimes[len(fetchTimes)-1].Sub(firstCall)
			if lastOffset < 50*time.Millisecond {
				fmt.Println("\nNote: All tool calls started within 50ms - executed in parallel!")
			}
		}
	}

	fmt.Println()
	fmt.Println("=== Demo Complete ===")
}
