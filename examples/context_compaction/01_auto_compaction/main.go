// Package main demonstrates the Client API with auto compaction using per-client registration.
//
// This example shows:
// - Per-client agent registration
// - Enabling auto compaction via ClientConfig
// - Long conversation that may trigger compaction
// - Checking compaction stats after conversation
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
	"github.com/youssefsiam38/agentpg/compaction"
	"github.com/youssefsiam38/agentpg/driver/pgxv5"
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
		dbURL = "postgres://agentpg:agentpg@localhost:5432/agentpg?sslmode=disable"
	}

	// Create PostgreSQL connection pool
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer pool.Close()

	// Create the pgx/v5 driver
	drv := pgxv5.New(pool)

	// Create the AgentPG client with auto-compaction enabled
	client, err := agentpg.NewClient(drv, &agentpg.ClientConfig{
		APIKey: apiKey,
		// Enable auto-compaction - runs automatically after each completed run
		AutoCompactionEnabled: true,
		// Configure compaction settings
		CompactionConfig: &compaction.Config{
			Strategy:        compaction.StrategyHybrid, // Prune tool outputs first, then summarize
			Trigger:         0.85,                      // Trigger at 85% context usage
			TargetTokens:    80000,                     // Target after compaction
			PreserveLastN:   10,                        // Always keep last 10 messages
			ProtectedTokens: 40000,                     // Never touch last 40K tokens
		},
	})
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}

	// Start the client
	if err := client.Start(ctx); err != nil {
		log.Fatalf("Failed to start client: %v", err)
	}

	// Create agent in the database (after client.Start)
	maxTokens := 4096
	agent, err := client.CreateAgent(ctx, &agentpg.AgentDefinition{
		Name:         "agent.ID",
		Description:  "Assistant with auto compaction enabled",
		Model:        "claude-sonnet-4-5-20250929",
		SystemPrompt: "You are a helpful assistant. Provide detailed, thorough responses to questions.",
		MaxTokens:    &maxTokens,
	})
	if err != nil {
		log.Fatalf("Failed to create agent: %v", err)
	}
	defer func() {
		if err := client.Stop(context.Background()); err != nil {
			log.Printf("Error stopping client: %v", err)
		}
	}()

	log.Printf("Client started (instance ID: %s)", client.InstanceID())
	log.Println("Auto-compaction is ENABLED")

	// Create session
	sessionID, err := client.NewSession(ctx, nil, map[string]any{
		"description": "Auto compaction demonstration",
	})
	if err != nil {
		log.Fatalf("Failed to create session: %v", err)
	}

	fmt.Printf("Created session: %s\n\n", sessionID)

	// ==========================================================
	// Simulate a long conversation that might trigger compaction
	// ==========================================================

	questions := []string{
		"Explain the history of computer programming from the 1950s to today, including major milestones, influential languages, and key figures.",
		"Compare and contrast object-oriented programming with functional programming. Give examples in multiple languages.",
		"Describe how databases have evolved from hierarchical models to modern distributed systems. Include discussion of SQL, NoSQL, and NewSQL.",
		"Explain the principles of clean code and software architecture. Cover SOLID principles, design patterns, and testing strategies.",
		"Discuss the evolution of web development from static HTML to modern frameworks. Include frontend, backend, and full-stack perspectives.",
	}

	for i, question := range questions {
		fmt.Printf("=== Question %d/%d ===\n", i+1, len(questions))
		fmt.Printf("Q: %s\n\n", truncateString(question, 80))

		response, err := client.RunFastSync(ctx, sessionID, agent.ID, question)
		if err != nil {
			log.Fatalf("Failed to run agent: %v", err)
		}

		// Print truncated response
		for _, block := range response.Message.Content {
			if block.Type == agentpg.ContentTypeText {
				fmt.Printf("A: %s\n", truncateString(block.Text, 200))
			}
		}

		// Print usage info
		fmt.Printf("\nTokens - Input: %d, Output: %d\n",
			response.Usage.InputTokens,
			response.Usage.OutputTokens)
		fmt.Println()

		// Check compaction stats after each run
		stats, err := client.GetCompactionStats(ctx, sessionID)
		if err != nil {
			log.Printf("Failed to get compaction stats: %v", err)
		} else {
			fmt.Printf("Context Usage: %.1f%% (%d tokens)\n", stats.UsagePercent*100, stats.TotalTokens)
			fmt.Printf("Messages: %d total, %d compactable, %d preserved\n",
				stats.TotalMessages, stats.CompactableMessages, stats.PreservedMessages)
			if stats.CompactionCount > 0 {
				fmt.Printf("Compaction Events: %d\n", stats.CompactionCount)
			}
		}
		fmt.Println()
	}

	// ==========================================================
	// Final Summary
	// ==========================================================

	fmt.Println("=== Final Summary ===")

	stats, err := client.GetCompactionStats(ctx, sessionID)
	if err != nil {
		log.Printf("Failed to get final stats: %v", err)
	} else {
		fmt.Printf("Final Context Usage: %.1f%% (%d tokens)\n", stats.UsagePercent*100, stats.TotalTokens)
		fmt.Printf("Total Messages: %d\n", stats.TotalMessages)
		fmt.Printf("Compaction Events: %d\n", stats.CompactionCount)
		fmt.Printf("Needs Compaction: %v\n", stats.NeedsCompaction)
	}

	fmt.Println("\nConversation completed successfully.")
	fmt.Println("Note: Compaction events can be monitored via the agentpg_compaction_events table.")
	fmt.Printf("Query example: SELECT * FROM agentpg_compaction_events WHERE session_id = '%s';\n", sessionID)
	fmt.Println("\n=== Demo Complete ===")
}

// truncateString truncates a string to maxLen with ellipsis
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
