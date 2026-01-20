// Package main demonstrates the basic usage of AgentPG.
//
// This example shows the recommended Client API with database-driven agents.
// The Client API provides:
// - Database-driven agent management (agents stored in PostgreSQL)
// - Per-client tool registration
// - Automatic instance management
// - Multi-instance coordination
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/youssefsiam38/agentpg"
	"github.com/youssefsiam38/agentpg/driver/pgxv5"
)

func main() {
	ctx := context.Background()

	// Get configuration from environment
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

	// Start the client (registers instance, starts background services)
	if err := client.Start(ctx); err != nil {
		log.Fatalf("Failed to start client: %v", err)
	}
	defer client.Stop(context.Background())

	fmt.Printf("AgentPG client started (instance: %s)\n\n", client.InstanceID())

	// Get or create agent in the database (idempotent - safe to run multiple times)
	// Agents are database entities identified by UUID
	agent, err := client.GetOrCreateAgent(ctx, &agentpg.AgentDefinition{
		Name:         "assistant",
		Description:  "A helpful coding assistant",
		Model:        "claude-sonnet-4-5-20250929",
		SystemPrompt: "You are a helpful coding assistant. Be concise and accurate.",
	})
	if err != nil {
		log.Fatalf("Failed to get or create agent: %v", err)
	}

	fmt.Printf("Agent ready: %s (ID: %s)\n\n", agent.Name, agent.ID)

	// Create a new session
	// Parameters: parentSessionID, metadata
	// App-specific fields (tenant_id, user_id, etc.) go in metadata
	sessionID, err := client.NewSession(ctx, nil, map[string]any{
		"tenant_id":   "tenant-1",
		"user_id":     "example-user",
		"description": "Basic example session",
	})
	if err != nil {
		log.Fatalf("Failed to create session: %v", err)
	}

	fmt.Printf("Created session: %s\n\n", sessionID)

	// Run the agent with a prompt
	// Parameters: ctx, sessionID, agentID, prompt, variables
	response, err := client.RunSync(ctx, sessionID, agent.ID, "Explain what the AgentPG package does in 3 sentences.", nil)
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
