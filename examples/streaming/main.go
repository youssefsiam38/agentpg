// Package main demonstrates the Client API with tools and hooks.
//
// This example shows:
// - Global tool and agent registration in init()
// - All 5 observability hooks
// - Automatic context compaction
// - Tool usage with calculator
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/youssefsiam38/agentpg"
	"github.com/youssefsiam38/agentpg/compaction"
	"github.com/youssefsiam38/agentpg/driver/pgxv5"
	"github.com/youssefsiam38/agentpg/tool"
	"github.com/youssefsiam38/agentpg/types"
)

// CalculatorTool performs basic arithmetic operations.
type CalculatorTool struct{}

func (c *CalculatorTool) Name() string {
	return "calculator"
}

func (c *CalculatorTool) Description() string {
	return "Performs basic arithmetic operations (add, subtract, multiply, divide)"
}

func (c *CalculatorTool) InputSchema() tool.ToolSchema {
	return tool.ToolSchema{
		Type: "object",
		Properties: map[string]tool.PropertyDef{
			"operation": {
				Type:        "string",
				Description: "The operation to perform: add, subtract, multiply, or divide",
				Enum:        []string{"add", "subtract", "multiply", "divide"},
			},
			"a": {
				Type:        "number",
				Description: "First number",
			},
			"b": {
				Type:        "number",
				Description: "Second number",
			},
		},
		Required: []string{"operation", "a", "b"},
	}
}

func (c *CalculatorTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
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

	return fmt.Sprintf("%g", result), nil
}

// Register tools and agents at package initialization.
func init() {
	// Register the calculator tool globally
	agentpg.MustRegisterTool(&CalculatorTool{})

	// Register the streaming demo agent
	maxTokens := 4096
	temp := float32(0.7)
	agentpg.MustRegister(&agentpg.AgentDefinition{
		Name:         "streaming-demo",
		Description:  "Demonstrates tools and hooks with streaming",
		Model:        "claude-sonnet-4-5-20250929",
		SystemPrompt: "You are a helpful assistant that can use tools to help answer questions.",
		Tools:        []string{"calculator"},
		MaxTokens:    &maxTokens,
		Temperature:  &temp,
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

	// Get the registered agent handle
	agent := client.Agent("streaming-demo")
	if agent == nil {
		log.Fatal("Agent 'streaming-demo' not found in registry")
	}

	// Register hooks for observability
	agent.OnBeforeMessage(func(ctx context.Context, messages []*types.Message) error {
		fmt.Printf("\n[HOOK] About to send %d messages to Claude\n", len(messages))
		return nil
	})

	agent.OnAfterMessage(func(ctx context.Context, response *types.Response) error {
		fmt.Printf("[HOOK] Received response with %d content blocks\n", len(response.Message.Content))
		fmt.Printf("[HOOK] Usage: %d input tokens, %d output tokens\n",
			response.Usage.InputTokens, response.Usage.OutputTokens)
		return nil
	})

	agent.OnToolCall(func(ctx context.Context, toolName string, input json.RawMessage, output string, err error) error {
		fmt.Printf("[HOOK] Tool '%s' called\n", toolName)
		fmt.Printf("[HOOK] Input: %s\n", string(input))
		if err != nil {
			fmt.Printf("[HOOK] Error: %v\n", err)
		} else {
			fmt.Printf("[HOOK] Output: %s\n", output)
		}
		return nil
	})

	agent.OnBeforeCompaction(func(ctx context.Context, sessionID string) error {
		fmt.Printf("[HOOK] Context compaction starting for session %s\n", sessionID)
		return nil
	})

	agent.OnAfterCompaction(func(ctx context.Context, result *compaction.CompactionResult) error {
		fmt.Printf("[HOOK] Context compaction completed: %d -> %d tokens\n",
			result.OriginalTokens, result.CompactedTokens)
		return nil
	})

	// Create a new session
	sessionID, err := agent.NewSession(ctx, "1", "tools-demo", nil, map[string]any{
		"description": "Demonstrating tools and hooks",
	})
	if err != nil {
		log.Fatalf("Failed to create session: %v", err)
	}

	fmt.Printf("Created session: %s\n", sessionID)

	// Example 1: Simple calculation using tool
	fmt.Println("\n=== Example 1: Tool Usage ===")
	response1, err := agent.RunSync(ctx, sessionID, "What is 42 multiplied by 1337?")
	if err != nil {
		log.Fatalf("Failed to run agent: %v", err)
	}

	fmt.Println("\nAgent response:")
	for _, block := range response1.Message.Content {
		if block.Type == agentpg.ContentTypeText {
			fmt.Println(block.Text)
		}
	}

	// Example 2: Multiple tool calls
	fmt.Println("\n=== Example 2: Multiple Calculations ===")
	response2, err := agent.RunSync(ctx, sessionID, "Calculate (100 + 50) and then multiply that result by 2. Show your work.")
	if err != nil {
		log.Fatalf("Failed to run agent: %v", err)
	}

	fmt.Println("\nAgent response:")
	for _, block := range response2.Message.Content {
		if block.Type == agentpg.ContentTypeText {
			fmt.Println(block.Text)
		}
	}

	fmt.Println("\n=== Demo Complete ===")
}
