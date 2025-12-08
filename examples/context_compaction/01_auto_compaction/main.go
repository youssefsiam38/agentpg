// Package main demonstrates the Client API with auto compaction.
//
// This example shows:
// - Enabling auto compaction via Config
// - Compaction hooks for monitoring
// - Long conversation that may trigger compaction
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

// Register agent at package initialization.
func init() {
	maxTokens := 4096
	agentpg.MustRegister(&agentpg.AgentDefinition{
		Name:         "auto-compaction-demo",
		Description:  "Assistant with auto compaction enabled",
		Model:        "claude-sonnet-4-5-20250929",
		SystemPrompt: "You are a helpful assistant. Provide detailed, thorough responses to questions.",
		MaxTokens:    &maxTokens,
		Config: map[string]any{
			// Enable auto-compaction with settings
			"auto_compaction":    true,
			"compaction_trigger": 0.85,  // 85% threshold
			"compaction_target":  80000, // Target token count after compaction
		},
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
	agent := client.Agent("auto-compaction-demo")
	if agent == nil {
		log.Fatal("Agent 'auto-compaction-demo' not found in registry")
	}

	// Track compaction events
	compactionCount := 0
	var lastCompaction *compaction.CompactionResult

	// Register compaction hooks for monitoring
	agent.OnBeforeCompaction(func(ctx context.Context, sessionID string) error {
		fmt.Printf("\n[COMPACTION] Starting compaction for session %s\n", sessionID)
		return nil
	})

	agent.OnAfterCompaction(func(ctx context.Context, result *compaction.CompactionResult) error {
		compactionCount++
		lastCompaction = result
		fmt.Printf("[COMPACTION] Completed: %d -> %d tokens (%.1f%% reduction)\n",
			result.OriginalTokens,
			result.CompactedTokens,
			100.0*(1.0-float64(result.CompactedTokens)/float64(result.OriginalTokens)))
		return nil
	})

	// Create session
	sessionID, err := agent.NewSession(ctx, "1", "auto-compaction-demo", nil, map[string]any{
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

		response, err := agent.Run(ctx, sessionID, question)
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
	}

	// ==========================================================
	// Summary
	// ==========================================================

	fmt.Println("=== Compaction Summary ===")
	fmt.Printf("Total compactions triggered: %d\n", compactionCount)

	if lastCompaction != nil {
		fmt.Printf("\nLast compaction details:\n")
		fmt.Printf("  Strategy: %s\n", lastCompaction.Strategy)
		fmt.Printf("  Original tokens: %d\n", lastCompaction.OriginalTokens)
		fmt.Printf("  Compacted tokens: %d\n", lastCompaction.CompactedTokens)
		fmt.Printf("  Messages preserved: %d\n", len(lastCompaction.PreservedMessages))
	} else {
		fmt.Println("No compaction was triggered during this session.")
		fmt.Println("This is normal for short conversations within context limits.")
	}

	fmt.Println("\n=== Demo Complete ===")
}

// truncateString truncates a string to maxLen with ellipsis
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
