// Package main demonstrates the Client API for multi-instance deployment.
//
// This example shows how to use the new Client API that provides:
// - Global agent and tool registration
// - Instance management with heartbeats
// - Leader election for cleanup coordination
// - Real-time events via PostgreSQL LISTEN/NOTIFY
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

// Register agents and tools at package initialization.
// This happens before main() runs and sets up the global registry.
func init() {
	// Register the calculator tool globally
	agentpg.MustRegisterTool(&CalculatorTool{})

	// Register the chat agent globally
	agentpg.MustRegister(&agentpg.AgentDefinition{
		Name:         "chat",
		Description:  "General purpose chat agent with math capabilities",
		Model:        "claude-sonnet-4-5-20250929",
		SystemPrompt: "You are a helpful assistant. Use the calculator tool when asked to perform math operations.",
		Tools:        []string{"calculator"},
	})

	// Register a simple agent without tools
	agentpg.MustRegister(&agentpg.AgentDefinition{
		Name:         "simple",
		Description:  "Simple chat agent without tools",
		Model:        "claude-sonnet-4-5-20250929",
		SystemPrompt: "You are a concise assistant. Answer in 2-3 sentences maximum.",
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

	// Create the pgx/v5 driver
	drv := pgxv5.New(pool)

	// Create the AgentPG client with configuration
	client, err := agentpg.NewClient(drv, &agentpg.ClientConfig{
		APIKey: apiKey,

		// Instance identification (optional - auto-generated if not provided)
		// InstanceID: "instance-1",
		// Hostname:   "my-server-1",

		// Metadata for this instance (useful for debugging/monitoring)
		Metadata: map[string]any{
			"environment": "development",
			"region":      "us-east-1",
		},

		// Background service intervals (these are defaults, shown for clarity)
		HeartbeatInterval: 30 * time.Second, // How often to send heartbeats
		CleanupInterval:   1 * time.Minute,  // How often leader runs cleanup
		StuckRunTimeout:   1 * time.Hour,    // When to consider a run stuck
		LeaderTTL:         30 * time.Second, // Leader lease duration

		// Callbacks for observability
		OnError: func(err error) {
			log.Printf("[ERROR] Background service error: %v", err)
		},
		OnBecameLeader: func() {
			log.Println("[LEADER] This instance became the leader")
		},
		OnLostLeadership: func() {
			log.Println("[LEADER] This instance lost leadership")
		},
	})
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
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
	log.Printf("Is leader: %v", client.IsLeader())

	// Get the registered agent handle
	chatAgent := client.Agent("chat")
	if chatAgent == nil {
		log.Fatal("Agent 'chat' not found in registry")
	}

	// Create a new session
	sessionID, err := chatAgent.NewSession(ctx, "tenant1", "user-123", nil, map[string]any{
		"source": "distributed-example",
	})
	if err != nil {
		log.Fatalf("Failed to create session: %v", err)
	}
	log.Printf("Created session: %s", sessionID)

	// Run the agent with a math question
	log.Println("\n--- Running agent with math question ---")
	response, err := chatAgent.Run(ctx, sessionID, "What is 42 * 17?")
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
	response, err = chatAgent.Run(ctx, sessionID, "Now divide that result by 7")
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
	fmt.Printf("Is Running: %v\n", client.IsRunning())
	fmt.Printf("Is Leader: %v\n", client.IsLeader())

	// The client will be stopped by the defer above
	// In a real application, you would wait for shutdown signals:
	// <-ctx.Done()
	// log.Println("Received shutdown signal")
}
