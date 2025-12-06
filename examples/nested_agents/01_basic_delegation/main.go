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

	// Create driver
	drv := pgxv5.New(pool)

	// ==========================================================
	// Create the SPECIALIST agent (research focused)
	// ==========================================================
	researchAgent, err := agentpg.New(
		drv,
		agentpg.Config{
			Client: &client,
			Model:  "claude-sonnet-4-5-20250929",
			SystemPrompt: `You are a research specialist. Your role is to:
1. Analyze topics thoroughly
2. Provide detailed explanations with examples
3. Break down complex concepts into understandable parts
4. Cite relevant information when applicable

When given a task, respond with well-structured, informative content.`,
		},
		agentpg.WithMaxTokens(2048),
	)
	if err != nil {
		log.Fatalf("Failed to create research agent: %v", err)
	}

	// ==========================================================
	// Create the MAIN agent (orchestrator)
	// ==========================================================
	mainAgent, err := agentpg.New(
		drv,
		agentpg.Config{
			Client: &client,
			Model:  "claude-sonnet-4-5-20250929",
			SystemPrompt: `You are a helpful assistant that can delegate research tasks to a specialist.
When users ask for detailed information or research on a topic, use the research agent tool.
Summarize the research findings in a user-friendly way.

For simple questions, answer directly without delegation.`,
		},
		agentpg.WithMaxTokens(1024),
	)
	if err != nil {
		log.Fatalf("Failed to create main agent: %v", err)
	}

	// ==========================================================
	// Register research agent as a tool for main agent
	// ==========================================================
	if err := researchAgent.AsToolFor(mainAgent); err != nil {
		log.Fatalf("Failed to register agent as tool: %v", err)
	}

	// Show registered tools
	fmt.Println("=== Main Agent Tools ===")
	for _, name := range mainAgent.GetTools() {
		fmt.Printf("- %s\n", name)
	}
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
	response1, err := mainAgent.Run(ctx, "What is 2 + 2?")
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
	response2, err := mainAgent.Run(ctx, "Can you research and explain how neural networks learn? I'd like a detailed explanation.")
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
	response3, err := mainAgent.Run(ctx, "Research the differences between SQL and NoSQL databases. Focus on when to use each one.")
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
