// Package main demonstrates the default instant retry behavior.
//
// This example shows:
// - Default retry configuration (2 attempts, instant retry)
// - Tool that fails sometimes but succeeds on retry
// - Snappy user experience with no delay between retries
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand/v2"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/youssefsiam38/agentpg"
	"github.com/youssefsiam38/agentpg/driver/pgxv5"
	"github.com/youssefsiam38/agentpg/tool"
)

// FlakyTool simulates an unreliable external service.
// It fails randomly ~80% of the time, but the default instant retry
// ensures a snappy user experience.
type FlakyTool struct {
	callCount int
}

func (t *FlakyTool) Name() string {
	return "flaky_service"
}

func (t *FlakyTool) Description() string {
	return "Simulates an unreliable external service that may fail occasionally"
}

func (t *FlakyTool) InputSchema() tool.ToolSchema {
	return tool.ToolSchema{
		Type: "object",
		Properties: map[string]tool.PropertyDef{
			"query": {
				Type:        "string",
				Description: "The query to send to the service",
			},
		},
		Required: []string{"query"},
	}
}

func (t *FlakyTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	t.callCount++
	var params struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}

	// Simulate 80% failure rate
	if rand.Float64() < 0.8 {
		log.Printf("[FlakyTool] Call #%d FAILED (query: %q)", t.callCount, params.Query)
		return "", fmt.Errorf("temporary service error")
	}

	log.Printf("[FlakyTool] Call #%d SUCCESS (query: %q)", t.callCount, params.Query)
	return fmt.Sprintf("Result for query '%s': Service responded successfully!", params.Query), nil
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		log.Fatal("ANTHROPIC_API_KEY environment variable is required")
	}

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Fatal("DATABASE_URL environment variable is required")
	}

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer pool.Close()

	drv := pgxv5.New(pool)

	// Create client with DEFAULT retry config (instant retry)
	// This is the snappy default:
	// - MaxAttempts: 2 (1 retry on failure)
	// - Jitter: 0.0 (instant retry, no delay)
	client, err := agentpg.NewClient(drv, &agentpg.ClientConfig{
		APIKey: apiKey,
	})
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}

	// Register the flaky tool
	flakyTool := &FlakyTool{}
	if err := client.RegisterTool(flakyTool); err != nil {
		log.Fatalf("Failed to register tool: %v", err)
	}

	// Register agent with access to the flaky tool
	if err := client.RegisterAgent(&agentpg.AgentDefinition{
		Name:         "assistant",
		Model:        "claude-sonnet-4-5-20250929",
		SystemPrompt: "You are a helpful assistant. Use the flaky_service tool to answer user queries.",
		Tools:        []string{"flaky_service"},
	}); err != nil {
		log.Fatalf("Failed to register agent: %v", err)
	}

	if err := client.Start(ctx); err != nil {
		log.Fatalf("Failed to start client: %v", err)
	}
	defer client.Stop(context.Background())

	log.Println("Client started with DEFAULT instant retry (2 attempts, no delay)")
	log.Println("")

	// Create session and run
	sessionID, err := client.NewSession(ctx, nil, map[string]any{"demo": "retry"})
	if err != nil {
		log.Fatalf("Failed to create session: %v", err)
	}

	log.Println("--- Sending request that uses the flaky tool ---")
	log.Println("(The tool has 80% failure rate, but instant retry makes it snappy)")
	log.Println("")

	start := time.Now()
	response, err := client.RunFastSync(ctx, sessionID, "assistant", "Query the service for 'weather forecast' once, never retry, if it fails. if it succeeds, provide the result.")
	elapsed := time.Since(start)

	if err != nil {
		log.Printf("Request failed: %v", err)
	} else {
		fmt.Println("\nAgent response:")
		for _, block := range response.Message.Content {
			if block.Type == agentpg.ContentTypeText {
				fmt.Println(block.Text)
			}
		}
	}

	fmt.Printf("\nTotal tool calls: %d\n", flakyTool.callCount)
	fmt.Printf("Total time: %v\n", elapsed)
	fmt.Println("\nNote: With instant retry, failures are recovered immediately!")
}
