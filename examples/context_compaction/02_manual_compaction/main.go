// Package main demonstrates the per-client API with manual compaction.
//
// This example shows:
// - Per-client agent registration
// - Manual compaction control via client.Compact()
// - Before/after comparison of messages and token usage
// - Verbose search tool to fill context quickly
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/youssefsiam38/agentpg"
	"github.com/youssefsiam38/agentpg/compaction"
	"github.com/youssefsiam38/agentpg/driver/pgxv5"
	"github.com/youssefsiam38/agentpg/tool"
)

// VerboseSearchTool generates large outputs to simulate context growth
type VerboseSearchTool struct{}

func (v *VerboseSearchTool) Name() string { return "search" }
func (v *VerboseSearchTool) Description() string {
	return "Search for information (returns verbose results)"
}

func (v *VerboseSearchTool) InputSchema() tool.ToolSchema {
	return tool.ToolSchema{
		Type: "object",
		Properties: map[string]tool.PropertyDef{
			"query": {Type: "string", Description: "Search query"},
		},
		Required: []string{"query"},
	}
}

func (v *VerboseSearchTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var params struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", err
	}

	// Generate verbose output to fill context quickly
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Search results for '%s':\n\n", params.Query))

	for i := 1; i <= 5; i++ {
		sb.WriteString(fmt.Sprintf("## Result %d: %s - Comprehensive Guide\n", i, params.Query))
		sb.WriteString(fmt.Sprintf("This comprehensive article covers %s in great detail. ", params.Query))
		sb.WriteString("It includes background information, best practices, examples, and common pitfalls. ")
		sb.WriteString("The guide is designed for both beginners and advanced practitioners. ")
		sb.WriteString("Key topics include implementation strategies, performance optimization, ")
		sb.WriteString("testing approaches, and real-world case studies from leading organizations.\n\n")
	}

	return sb.String(), nil
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

	// Create the AgentPG client (auto-compaction disabled for manual control)
	client, err := agentpg.NewClient(drv, &agentpg.ClientConfig{
		APIKey:                apiKey,
		AutoCompactionEnabled: false, // We'll trigger compaction manually
		CompactionConfig: &compaction.Config{
			Strategy:     compaction.StrategyHybrid,
			Trigger:      0.50, // Lower threshold for demo purposes
			TargetTokens: 5000, // Low target to see compaction in action
		},
	})
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}

	// Register verbose search tool on the client
	if err := client.RegisterTool(&VerboseSearchTool{}); err != nil {
		log.Fatalf("Failed to register tool: %v", err)
	}

	// Start the client (must be after all tool registrations)
	if err := client.Start(ctx); err != nil {
		log.Fatalf("Failed to start client: %v", err)
	}

	// Create agent in the database (after client.Start)
	maxTokens := 1024
	agent, err := client.CreateAgent(ctx, &agentpg.AgentDefinition{
		Name:         "agent.ID",
		Description:  "Research assistant with manual compaction",
		Model:        "claude-sonnet-4-5-20250929",
		SystemPrompt: "You are a research assistant. Use the search tool to find information. Keep responses brief.",
		MaxTokens:    &maxTokens,
		Tools:        []string{"search"}, // List tools this agent can use
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

	// Create session using client API
	sessionID, err := client.NewSession(ctx, nil, map[string]any{
		"description": "Manual compaction demonstration",
	})
	if err != nil {
		log.Fatalf("Failed to create session: %v", err)
	}

	fmt.Println(strings.Repeat("=", 60))
	fmt.Println("MANUAL COMPACTION DEMO")
	fmt.Println(strings.Repeat("=", 60))
	fmt.Printf("\nSession ID: %s\n", sessionID)
	fmt.Println()

	// ==========================================================
	// Run several queries to build up context
	// ==========================================================

	queries := []string{
		"Search for microservices architecture",
		"Search for API design patterns",
		"Search for database optimization",
		"Search for Docker containerization",
	}

	for i, query := range queries {
		fmt.Printf("Query %d: %s\n", i+1, query)

		response, err := client.RunFastSync(ctx, sessionID, agent.ID, query, nil)
		if err != nil {
			log.Fatalf("Failed to run agent: %v", err)
		}

		// Print brief response
		text := response.Text
		if len(text) > 80 {
			text = text[:80] + "..."
		}
		fmt.Printf("  -> Response: %s\n", text)
	}

	// ==========================================================
	// Check compaction stats BEFORE compaction
	// ==========================================================

	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Println("BEFORE COMPACTION")
	fmt.Println(strings.Repeat("=", 60))

	statsBefore, err := client.GetCompactionStats(ctx, sessionID)
	if err != nil {
		log.Fatalf("Failed to get compaction stats: %v", err)
	}

	fmt.Printf("\nTotal Messages: %d\n", statsBefore.TotalMessages)
	fmt.Printf("Total Tokens: %d\n", statsBefore.TotalTokens)
	fmt.Printf("Context Usage: %.1f%%\n", statsBefore.UsagePercent*100)
	fmt.Printf("Compactable Messages: %d\n", statsBefore.CompactableMessages)
	fmt.Printf("Preserved Messages: %d\n", statsBefore.PreservedMessages)
	fmt.Printf("Needs Compaction: %v\n", statsBefore.NeedsCompaction)

	// ==========================================================
	// Manual compaction
	// ==========================================================

	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Println("TRIGGERING MANUAL COMPACTION")
	fmt.Println(strings.Repeat("=", 60))

	// Check if compaction is needed first
	needsCompaction, err := client.NeedsCompaction(ctx, sessionID)
	if err != nil {
		log.Fatalf("Failed to check compaction: %v", err)
	}

	if needsCompaction {
		fmt.Println("\nCompaction needed - triggering now...")

		result, err := client.Compact(ctx, sessionID)
		if err != nil {
			log.Fatalf("Failed to compact: %v", err)
		}

		fmt.Printf("\nCompaction Result:\n")
		fmt.Printf("  Strategy: %s\n", result.Strategy)
		fmt.Printf("  Original Tokens: %d\n", result.OriginalTokens)
		fmt.Printf("  Compacted Tokens: %d\n", result.CompactedTokens)
		fmt.Printf("  Token Reduction: %.1f%%\n", 100.0*(1.0-float64(result.CompactedTokens)/float64(result.OriginalTokens)))
		fmt.Printf("  Messages Removed: %d\n", result.MessagesRemoved)
		fmt.Printf("  Summary Created: %v\n", result.SummaryCreated)
		fmt.Printf("  Duration: %v\n", result.Duration)
	} else {
		fmt.Println("\nCompaction not needed yet (context usage below threshold)")
		fmt.Println("Using CompactIfNeeded() to compact only when necessary...")

		result, err := client.CompactIfNeeded(ctx, sessionID)
		if err != nil {
			log.Fatalf("Failed to compact: %v", err)
		}
		if result == nil {
			fmt.Println("  -> No compaction performed (threshold not reached)")
		} else {
			fmt.Printf("  -> Compaction performed: %d -> %d tokens\n", result.OriginalTokens, result.CompactedTokens)
		}
	}

	// ==========================================================
	// Check compaction stats AFTER compaction
	// ==========================================================

	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Println("AFTER COMPACTION")
	fmt.Println(strings.Repeat("=", 60))

	statsAfter, err := client.GetCompactionStats(ctx, sessionID)
	if err != nil {
		log.Fatalf("Failed to get compaction stats: %v", err)
	}

	fmt.Printf("\nTotal Messages: %d\n", statsAfter.TotalMessages)
	fmt.Printf("Total Tokens: %d\n", statsAfter.TotalTokens)
	fmt.Printf("Context Usage: %.1f%%\n", statsAfter.UsagePercent*100)
	fmt.Printf("Compactable Messages: %d\n", statsAfter.CompactableMessages)
	fmt.Printf("Compaction Count: %d\n", statsAfter.CompactionCount)

	// ==========================================================
	// Verify conversation still works
	// ==========================================================

	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Println("VERIFICATION: Conversation continues with context")
	fmt.Println(strings.Repeat("=", 60))

	response, err := client.RunFastSync(ctx, sessionID, agent.ID, "Based on our previous discussion, what were the main topics we covered?", nil)
	if err != nil {
		log.Fatalf("Failed to run agent: %v", err)
	}

	fmt.Println("\nAgent response:")
	text := response.Text
	if len(text) > 400 {
		text = text[:400] + "..."
	}
	fmt.Println(text)

	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Println("DEMO COMPLETE")
	fmt.Println(strings.Repeat("=", 60))
}
