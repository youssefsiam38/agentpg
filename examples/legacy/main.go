// Package main demonstrates the legacy Agent API pattern.
//
// This example uses the direct Agent creation pattern.
// This API is still fully supported but the Client API is recommended
// for new projects, especially for multi-instance deployments.
//
// Use this pattern when:
// - You need a single-instance, simple setup
// - You need fine-grained control over agent configuration at runtime
//
// For the recommended Client API, see examples/basic/main.go
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/youssefsiam38/agentpg"
	"github.com/youssefsiam38/agentpg/driver/pgxv5"
)

func main() {
	ctx := context.Background()

	// Get API key from environment
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		log.Fatal("ANTHROPIC_API_KEY environment variable is required")
	}

	// Get database URL from environment
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
	client := anthropic.NewClient(
		option.WithAPIKey(apiKey),
	)

	// Create driver
	drv := pgxv5.New(pool)

	// Create agent directly
	// This pattern gives you direct control over the agent configuration
	agent, err := agentpg.New(
		drv,
		agentpg.Config{
			Client:       &client,
			Model:        "claude-sonnet-4-5-20250929",
			SystemPrompt: "You are a helpful coding assistant",
		},
		agentpg.WithMaxTokens(4096),
		agentpg.WithTemperature(0.7),
	)
	if err != nil {
		log.Fatalf("Failed to create agent: %v", err)
	}

	// Create a new session
	// For single-tenant apps, use a constant like "1" for tenant_id
	// identifier can be user ID, conversation ID, or any custom identifier
	sessionID, err := agent.NewSession(ctx, "1", "example-user", nil, map[string]any{
		"description": "Legacy example session",
	})
	if err != nil {
		log.Fatalf("Failed to create session: %v", err)
	}

	fmt.Printf("Created session: %s\n\n", sessionID)

	// Run the agent
	response, err := agent.Run(ctx, "Explain what the AgentPG package does in 3 sentences.")
	if err != nil {
		log.Fatalf("Failed to run agent: %v", err)
	}

	// Print the response
	fmt.Println("Agent response:")
	for _, block := range response.Message.Content {
		if block.Type == agentpg.ContentTypeText {
			fmt.Println(block.Text)
		}
	}

	// Print usage stats
	fmt.Printf("\nTokens used: %d input, %d output\n",
		response.Usage.InputTokens,
		response.Usage.OutputTokens)

	fmt.Printf("Stop reason: %s\n", response.StopReason)
}
