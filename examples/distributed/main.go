// Package main demonstrates the Client API with distributed workers.
//
// This example shows:
// - Per-client registration of agents and tools
// - Client lifecycle management (Start/Stop)
// - Leader election callbacks
// - Instance metadata
// - Multi-instance deployment pattern
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/youssefsiam38/agentpg"
	"github.com/youssefsiam38/agentpg/driver/pgxv5"
	"github.com/youssefsiam38/agentpg/tool"
)

// CalculatorTool is an example tool that performs basic math operations.
type CalculatorTool struct{}

func (t *CalculatorTool) Name() string {
	return "calculator"
}

func (t *CalculatorTool) Description() string {
	return "Perform basic arithmetic operations (add, subtract, multiply, divide)"
}

func (t *CalculatorTool) InputSchema() tool.ToolSchema {
	return tool.ToolSchema{
		Type: "object",
		Properties: map[string]tool.PropertyDef{
			"operation": {
				Type:        "string",
				Description: "The operation to perform",
				Enum:        []string{"add", "subtract", "multiply", "divide"},
			},
			"a": {
				Type:        "number",
				Description: "First operand",
			},
			"b": {
				Type:        "number",
				Description: "Second operand",
			},
		},
		Required: []string{"operation", "a", "b"},
	}
}

func (t *CalculatorTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var params struct {
		Operation string  `json:"operation"`
		A         float64 `json:"a"`
		B         float64 `json:"b"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}

	var result float64
	switch params.Operation {
	case "add":
		result = params.A + params.B
	case "subtract":
		result = params.A - params.B
	case "multiply":
		result = params.A * params.B
	case "divide":
		if params.B == 0 {
			return "", fmt.Errorf("division by zero")
		}
		result = params.A / params.B
	default:
		return "", fmt.Errorf("unknown operation: %s", params.Operation)
	}

	return fmt.Sprintf("%.2f", result), nil
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

	// Create the AgentPG client with configuration
	client, err := agentpg.NewClient(drv, &agentpg.ClientConfig{
		APIKey: apiKey,

		// Instance identification (optional - auto-generated if not provided)
		// ID: "instance-1",
		// Name: "my-server-1",

		// Concurrency settings
		MaxConcurrentRuns:  10,
		MaxConcurrentTools: 50,

		// Polling intervals
		BatchPollInterval: 30 * time.Second,
		RunPollInterval:   1 * time.Second,
		ToolPollInterval:  500 * time.Millisecond,

		// Instance health
		HeartbeatInterval: 15 * time.Second,
		LeaderTTL:         30 * time.Second,
		StuckRunTimeout:   5 * time.Minute,
	})
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}

	// Register the calculator tool on the client
	if err := client.RegisterTool(&CalculatorTool{}); err != nil {
		log.Fatalf("Failed to register tool: %v", err)
	}

	// Start the client (registers instance, starts heartbeat, leader election)
	if err := client.Start(ctx); err != nil {
		log.Fatalf("Failed to start client: %v", err)
	}
	defer func() {
		log.Println("Shutting down client...")
		if err := client.Stop(context.Background()); err != nil {
			log.Printf("Error stopping client: %v", err)
		}
	}()

	log.Printf("Client started (instance ID: %s)", client.InstanceID())

	// Create agents in the database (after client.Start)
	chatAgent, err := client.CreateAgent(ctx, &agentpg.AgentDefinition{
		Name:         "chat",
		Description:  "General purpose chat agent with math capabilities",
		Model:        "claude-sonnet-4-5-20250929",
		SystemPrompt: "You are a helpful assistant. Use the calculator tool when asked to perform math operations.",
		Tools:        []string{"calculator"}, // Agent has access to calculator tool
	})
	if err != nil {
		log.Fatalf("Failed to create chat agent: %v", err)
	}

	_, err = client.CreateAgent(ctx, &agentpg.AgentDefinition{
		Name:         "simple",
		Description:  "Simple chat agent without tools",
		Model:        "claude-sonnet-4-5-20250929",
		SystemPrompt: "You are a concise assistant. Answer in 2-3 sentences maximum.",
		// No Tools field = no tool access
	})
	if err != nil {
		log.Fatalf("Failed to create simple agent: %v", err)
	}

	// Create a new session
	// App-specific fields (tenant_id, user_id, etc.) go in metadata
	sessionID, err := client.NewSession(ctx, nil, map[string]any{
		"tenant_id": "tenant1",
		"user_id":   "user-123",
		"source":    "distributed-example",
	})
	if err != nil {
		log.Fatalf("Failed to create session: %v", err)
	}
	log.Printf("Created session: %s", sessionID)

	// Run the agent with a math question
	log.Println("\n--- Running agent with math question ---")
	response, err := client.RunSync(ctx, sessionID, chatAgent.ID, "What is 42 * 17?")
	if err != nil {
		log.Fatalf("Failed to run agent: %v", err)
	}

	// Print response
	fmt.Println("\nAgent response:")
	for _, block := range response.Message.Content {
		if block.Type == agentpg.ContentTypeText {
			fmt.Println(block.Text)
		}
	}

	fmt.Printf("\nTokens used: %d input, %d output\n",
		response.Usage.InputTokens,
		response.Usage.OutputTokens)

	// Continue the conversation
	log.Println("\n--- Continuing conversation ---")
	response, err = client.RunSync(ctx, sessionID, chatAgent.ID, "Now divide that result by 7")
	if err != nil {
		log.Fatalf("Failed to run agent: %v", err)
	}

	fmt.Println("\nAgent response:")
	for _, block := range response.Message.Content {
		if block.Type == agentpg.ContentTypeText {
			fmt.Println(block.Text)
		}
	}

	// Show client status
	fmt.Printf("\n--- Client Status ---\n")
	fmt.Printf("Instance ID: %s\n", client.InstanceID())

	// The client will be stopped by the defer above
	// In a real application, you would wait for shutdown signals:
	// <-ctx.Done()
	// log.Println("Received shutdown signal")
}
