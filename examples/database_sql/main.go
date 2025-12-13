// Package main demonstrates using the Client API with database/sql driver.
//
// This example shows:
// - Using the standard library database/sql package instead of pgx
// - Per-client agent registration
// - Transaction support with RunTx and NewSessionTx
// - Compatible with any database/sql driver (lib/pq, pgx stdlib, etc.)
package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	_ "github.com/lib/pq" // PostgreSQL driver
	"github.com/youssefsiam38/agentpg"
	"github.com/youssefsiam38/agentpg/driver/databasesql"
)

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

	// ==========================================================
	// Create database/sql connection
	// ==========================================================

	fmt.Println("=== database/sql Driver Example (Client API) ===")
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

	// ==========================================================
	// Create driver and client
	// ==========================================================

	// Create the database/sql driver (requires connection string for LISTEN/NOTIFY)
	drv := databasesql.New(db, dbURL)

	// Create the AgentPG client
	client, err := agentpg.NewClient(drv, &agentpg.ClientConfig{
		APIKey: apiKey,
	})
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}

	// Register agent on the client
	maxTokens := 1024
	if err := client.RegisterAgent(&agentpg.AgentDefinition{
		Name:         "database-sql-demo",
		Description:  "Demonstrates database/sql driver usage",
		Model:        "claude-sonnet-4-5-20250929",
		SystemPrompt: "You are a helpful assistant. Keep responses concise.",
		MaxTokens:    &maxTokens,
	}); err != nil {
		log.Fatalf("Failed to register agent: %v", err)
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

	fmt.Printf("Client started (instance ID: %s)\n", client.InstanceID())
	fmt.Println()

	// ==========================================================
	// Create session
	// ==========================================================

	sessionID, err := client.NewSession(ctx, "tenant-1", "database-sql-demo", nil, map[string]any{
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

		response, err := client.RunSync(ctx, sessionID, "database-sql-demo", prompt)
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

	// Create a new session within the transaction
	sessionID2, err := client.NewSessionTx(ctx, tx, "tenant-1", "tx-demo", nil, map[string]any{
		"description": "Transaction demo session",
	})
	if err != nil {
		tx.Rollback()
		log.Fatalf("Failed to create session in transaction: %v", err)
	}

	// Create a run within the transaction
	runID, err := client.RunTx(ctx, tx, sessionID2, "database-sql-demo", "What's a fun fact about Tokyo?")
	if err != nil {
		tx.Rollback()
		log.Fatalf("Failed to create run in transaction: %v", err)
	}

	// Commit the transaction
	if err := tx.Commit(); err != nil {
		log.Fatalf("Failed to commit transaction: %v", err)
	}

	fmt.Printf("Created session %s and run %s in transaction\n", sessionID2.String()[:8]+"...", runID.String()[:8]+"...")

	// Wait for the run to complete (after transaction is committed)
	response, err := client.WaitForRun(ctx, runID)
	if err != nil {
		log.Fatalf("Failed to wait for run: %v", err)
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
	fmt.Println("3. Full transaction support with NewSessionTx and RunTx")
	fmt.Println("4. Standard library compatibility for existing codebases")
	fmt.Println()
	fmt.Println("Client API Usage:")
	fmt.Println("  db, _ := sql.Open(\"postgres\", dbURL)")
	fmt.Println("  drv := databasesql.New(db)")
	fmt.Println("  client, _ := agentpg.NewClient(drv, &agentpg.ClientConfig{APIKey: apiKey})")
	fmt.Println("  client.RegisterAgent(&agentpg.AgentDefinition{...})")
	fmt.Println("  client.Start(ctx)")
	fmt.Println("  response, _ := client.RunSync(ctx, sessionID, \"agent-name\", prompt)")
	fmt.Println()
	fmt.Println("=== Demo Complete ===")
}
