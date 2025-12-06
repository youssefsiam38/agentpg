package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	_ "github.com/lib/pq" // PostgreSQL driver
	"github.com/youssefsiam38/agentpg"
	"github.com/youssefsiam38/agentpg/driver/databasesql"
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

	// ==========================================================
	// Create database/sql connection
	// ==========================================================

	fmt.Println("=== database/sql Driver Example ===")
	fmt.Println()
	fmt.Println("This example demonstrates using AgentPG with the")
	fmt.Println("standard library database/sql package instead of pgx.")
	fmt.Println()

	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	// Verify connection
	if err := db.Ping(); err != nil {
		log.Fatalf("Failed to ping database: %v", err)
	}

	fmt.Println("Connected to PostgreSQL using database/sql")
	fmt.Println()

	// Create Anthropic client
	client := anthropic.NewClient(option.WithAPIKey(apiKey))

	// ==========================================================
	// Create driver and agent
	// ==========================================================

	// Create the database/sql driver
	drv := databasesql.New(db)

	// Create agent - note: no explicit generic type parameter needed!
	// Type inference handles it automatically based on the driver
	agent, err := agentpg.New(
		drv,
		agentpg.Config{
			Client:       &client,
			Model:        "claude-sonnet-4-5-20250929",
			SystemPrompt: "You are a helpful assistant. Keep responses concise.",
		},
		agentpg.WithMaxTokens(1024),
	)
	if err != nil {
		log.Fatalf("Failed to create agent: %v", err)
	}

	fmt.Println("Agent created with database/sql driver")
	fmt.Println()

	// ==========================================================
	// Create session
	// ==========================================================

	sessionID, err := agent.NewSession(ctx, "tenant-1", "database-sql-demo", nil, map[string]any{
		"driver": "database/sql",
	})
	if err != nil {
		log.Fatalf("Failed to create session: %v", err)
	}

	fmt.Printf("Session created: %s...\n\n", sessionID[:8])

	// ==========================================================
	// Run some prompts
	// ==========================================================

	prompts := []string{
		"Hello! What's the capital of Japan?",
		"What about France?",
		"Thanks! What were my previous questions about?",
	}

	for i, prompt := range prompts {
		fmt.Printf("=== Message %d ===\n", i+1)
		fmt.Printf("User: %s\n", prompt)

		response, err := agent.Run(ctx, prompt)
		if err != nil {
			log.Printf("Error: %v\n\n", err)
			continue
		}

		for _, block := range response.Message.Content {
			if block.Type == agentpg.ContentTypeText {
				fmt.Printf("Agent: %s\n", block.Text)
			}
		}

		fmt.Printf("Tokens: %d input, %d output\n\n",
			response.Usage.InputTokens,
			response.Usage.OutputTokens)
	}

	// ==========================================================
	// Demonstrate manual transactions
	// ==========================================================

	fmt.Println("=== Manual Transaction Example ===")
	fmt.Println()

	// Begin a transaction using database/sql
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		log.Fatalf("Failed to begin transaction: %v", err)
	}

	// Run agent within the transaction
	response, err := agent.RunTx(ctx, tx, "What's a fun fact about Tokyo?")
	if err != nil {
		tx.Rollback()
		log.Fatalf("Failed to run agent in transaction: %v", err)
	}

	// Commit the transaction
	if err := tx.Commit(); err != nil {
		log.Fatalf("Failed to commit transaction: %v", err)
	}

	fmt.Println("User: What's a fun fact about Tokyo?")
	for _, block := range response.Message.Content {
		if block.Type == agentpg.ContentTypeText {
			fmt.Printf("Agent: %s\n", block.Text)
		}
	}
	fmt.Println()

	// ==========================================================
	// Summary
	// ==========================================================

	fmt.Println("=== database/sql Driver Summary ===")
	fmt.Println()
	fmt.Println("The database/sql driver provides:")
	fmt.Println("1. Compatibility with any database/sql driver (lib/pq, pgx stdlib, etc.)")
	fmt.Println("2. Same API as pgxv5 driver - just swap the driver creation")
	fmt.Println("3. Full transaction support with savepoint-based nesting")
	fmt.Println("4. Standard library compatibility for existing codebases")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  db, _ := sql.Open(\"postgres\", dbURL)")
	fmt.Println("  drv := databasesql.New(db)")
	fmt.Println("  agent, _ := agentpg.New(drv, config)")
	fmt.Println()
	fmt.Println("=== Demo Complete ===")
}
