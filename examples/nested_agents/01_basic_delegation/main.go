// Package main demonstrates the Client API with basic agent delegation.
//
// This example shows:
// - Creating multiple agents with different roles
// - Using AsToolFor to delegate work between agents
// - Research agent as a tool for the main agent
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/youssefsiam38/agentpg"
	"github.com/youssefsiam38/agentpg/driver/pgxv5"
)

// Register agents at package initialization.
func init() {
	maxTokensResearch := 2048
	maxTokensMain := 1024

	// Register the research specialist agent
	agentpg.MustRegister(&agentpg.AgentDefinition{
		Name:        "research-specialist",
		Description: "A research specialist for detailed analysis",
		Model:       "claude-sonnet-4-5-20250929",
		SystemPrompt: `You are a research specialist. Your role is to:
1. Analyze topics thoroughly
2. Provide detailed explanations with examples
3. Break down complex concepts into understandable parts
4. Cite relevant information when applicable

When given a task, respond with well-structured, informative content.`,
		MaxTokens: &maxTokensResearch,
	})

	// Register the main orchestrator agent
	agentpg.MustRegister(&agentpg.AgentDefinition{
		Name:        "main-orchestrator",
		Description: "Main assistant that can delegate to specialists",
		Model:       "claude-sonnet-4-5-20250929",
		SystemPrompt: `You are a helpful assistant that can delegate research tasks to a specialist.
When users ask for detailed information or research on a topic, use the research agent tool.
Summarize the research findings in a user-friendly way.

For simple questions, answer directly without delegation.`,
		MaxTokens: &maxTokensMain,
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

	// Get the agent handles
	researchAgent := client.Agent("research-specialist")
	if researchAgent == nil {
		log.Fatal("Agent 'research-specialist' not found in registry")
	}

	mainAgent := client.Agent("main-orchestrator")
	if mainAgent == nil {
		log.Fatal("Agent 'main-orchestrator' not found in registry")
	}

	// ==========================================================
	// Register research agent as a tool for main agent
	// ==========================================================
	if err := researchAgent.AsToolFor(mainAgent); err != nil {
		log.Fatalf("Failed to register agent as tool: %v", err)
	}

	fmt.Println("=== Agent Delegation Setup Complete ===")
	fmt.Println("Research agent is now available as a tool for the main agent")
	fmt.Println()

	// Create session for main agent
	sessionID, err := mainAgent.NewSession(ctx, "1", "delegation-demo", nil, map[string]any{
		"description": "Basic delegation demonstration",
	})
	if err != nil {
		log.Fatalf("Failed to create session: %v", err)
	}

	fmt.Printf("Created session: %s\n\n", sessionID)

	// ==========================================================
	// Example 1: Simple question (no delegation needed)
	// ==========================================================
	fmt.Println("=== Example 1: Simple Question (No Delegation) ===")
	response1, err := mainAgent.Run(ctx, sessionID, "What is 2 + 2?")
	if err != nil {
		log.Fatalf("Failed to run agent: %v", err)
	}

	for _, block := range response1.Message.Content {
		if block.Type == agentpg.ContentTypeText {
			fmt.Println(block.Text)
		}
	}

	// ==========================================================
	// Example 2: Research question (triggers delegation)
	// ==========================================================
	fmt.Println("\n=== Example 2: Research Question (With Delegation) ===")
	response2, err := mainAgent.Run(ctx, sessionID, "Can you research and explain how neural networks learn? I'd like a detailed explanation.")
	if err != nil {
		log.Fatalf("Failed to run agent: %v", err)
	}

	for _, block := range response2.Message.Content {
		if block.Type == agentpg.ContentTypeText {
			fmt.Println(block.Text)
		}
	}

	// ==========================================================
	// Example 3: Another delegation with context
	// ==========================================================
	fmt.Println("\n=== Example 3: Research with Specific Context ===")
	response3, err := mainAgent.Run(ctx, sessionID, "Research the differences between SQL and NoSQL databases. Focus on when to use each one.")
	if err != nil {
		log.Fatalf("Failed to run agent: %v", err)
	}

	for _, block := range response3.Message.Content {
		if block.Type == agentpg.ContentTypeText {
			fmt.Println(block.Text)
		}
	}

	// Print token usage
	fmt.Println("\n=== Token Usage (Last Response) ===")
	fmt.Printf("Input tokens: %d\n", response3.Usage.InputTokens)
	fmt.Printf("Output tokens: %d\n", response3.Usage.OutputTokens)

	fmt.Println("\n=== Demo Complete ===")
}
