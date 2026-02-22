// Package main demonstrates the CancelRun and RegenerateRun APIs.
//
// This example shows:
// - Starting a run with a slow tool
// - Cancelling the run mid-execution
// - Regenerating the cancelled run
// - Sending follow-up messages after cancellation
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/youssefsiam38/agentpg"
	"github.com/youssefsiam38/agentpg/driver/pgxv5"
	"github.com/youssefsiam38/agentpg/tool"
)

// SlowTool simulates a long-running operation that respects context cancellation.
type SlowTool struct{}

func (t *SlowTool) Name() string {
	return "slow_operation"
}

func (t *SlowTool) Description() string {
	return "A slow operation that takes 30 seconds to complete. Use this when asked to do something slow."
}

func (t *SlowTool) InputSchema() tool.ToolSchema {
	return tool.ToolSchema{
		Type: "object",
		Properties: map[string]tool.PropertyDef{
			"task": {
				Type:        "string",
				Description: "Description of the task to perform",
			},
		},
		Required: []string{"task"},
	}
}

func (t *SlowTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var params struct {
		Task string `json:"task"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}

	// Simulate a long-running operation that respects context cancellation
	select {
	case <-time.After(30 * time.Second):
		return fmt.Sprintf("Completed slow operation: %s", params.Task), nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func main() {
	ctx := context.Background()

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
	client, err := agentpg.NewClient(drv, &agentpg.ClientConfig{
		APIKey: os.Getenv("ANTHROPIC_API_KEY"),
		Name:   "cancel-demo",
	})
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}

	// Register the slow tool
	client.RegisterTool(&SlowTool{})

	if err := client.Start(ctx); err != nil {
		log.Fatalf("Failed to start client: %v", err)
	}
	defer client.Stop(context.Background())

	// Create agent with slow tool
	agent, err := client.GetOrCreateAgent(ctx, &agentpg.AgentDefinition{
		Name:         "slow-assistant",
		Model:        "claude-sonnet-4-5-20250929",
		SystemPrompt: "You are a helpful assistant. When asked to do something slow, use the slow_operation tool.",
		Tools:        []string{"slow_operation"},
	})
	if err != nil {
		log.Fatalf("Failed to create agent: %v", err)
	}

	// Create a session
	sessionID, err := client.NewSession(ctx, nil, nil)
	if err != nil {
		log.Fatalf("Failed to create session: %v", err)
	}

	fmt.Println("=== Step 1: Start a run with slow tool ===")
	runID, err := client.RunFast(ctx, sessionID, agent.ID, "Please do a slow operation called 'data processing'", nil)
	if err != nil {
		log.Fatalf("Failed to start run: %v", err)
	}
	fmt.Printf("Run started: %s\n", runID)

	// Wait a few seconds for the tool to start executing
	time.Sleep(3 * time.Second)

	fmt.Println("\n=== Step 2: Cancel the run ===")
	if err := client.CancelRun(ctx, runID); err != nil {
		log.Fatalf("Failed to cancel run: %v", err)
	}
	fmt.Println("Run cancelled successfully")

	// Verify run state
	run, err := client.GetRun(ctx, runID)
	if err != nil {
		log.Fatalf("Failed to get run: %v", err)
	}
	fmt.Printf("Run state: %s\n", run.State)

	fmt.Println("\n=== Step 3: Regenerate the cancelled run ===")
	newRunID, err := client.RegenerateRun(ctx, runID)
	if err != nil {
		log.Fatalf("Failed to regenerate run: %v", err)
	}
	fmt.Printf("New run created: %s\n", newRunID)

	// Wait for the regenerated run to complete
	response, err := client.WaitForRun(ctx, newRunID)
	if err != nil {
		log.Fatalf("Failed to wait for regenerated run: %v", err)
	}
	fmt.Printf("Response: %s\n", response.Text)

	fmt.Println("\n=== Step 4: Send follow-up message ===")
	followUp, err := client.RunFastSync(ctx, sessionID, agent.ID, "What did you just do?", nil)
	if err != nil {
		log.Fatalf("Failed to send follow-up: %v", err)
	}
	fmt.Printf("Follow-up response: %s\n", followUp.Text)

	fmt.Println("\nDone!")
}
