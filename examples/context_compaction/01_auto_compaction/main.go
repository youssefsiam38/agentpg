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
	"github.com/youssefsiam38/agentpg/compaction"
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

	// Track compaction events
	compactionCount := 0
	var lastCompaction *compaction.CompactionResult

	// Create agent with auto-compaction enabled
	agent, err := agentpg.New(
		drv,
		agentpg.Config{
			Client:       &client,
			Model:        "claude-sonnet-4-5-20250929",
			SystemPrompt: "You are a helpful assistant. Provide detailed, thorough responses to questions.",
		},
		// Enable auto-compaction with default settings
		agentpg.WithAutoCompaction(true),
		// Lower the trigger threshold for demo purposes
		// In production, 0.85 (85%) is recommended
		agentpg.WithCompactionTrigger(0.85),
		// Target a lower token count after compaction
		agentpg.WithCompactionTarget(80000),
		agentpg.WithMaxTokens(4096),
	)
	if err != nil {
		log.Fatalf("Failed to create agent: %v", err)
	}

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

		response, err := agent.Run(ctx, question)
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
