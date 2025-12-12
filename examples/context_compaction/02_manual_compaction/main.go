// Package main demonstrates the per-client API with manual compaction.
//
// This example shows:
// - Per-client agent registration
// - Disabling auto compaction
// - Manual compaction control
// - Before/after comparison of messages
// - Verbose search tool to fill context
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

	// Register agent on the client (per-client registration)
	maxTokens := 1024
	if err := client.RegisterAgent(&agentpg.AgentDefinition{
		Name:         "manual-compaction-demo",
		Description:  "Research assistant with manual compaction",
		Model:        "claude-sonnet-4-5-20250929",
		SystemPrompt: "You are a research assistant. Use the search tool to find information. Keep responses brief.",
		MaxTokens:    &maxTokens,
		Tools:        []string{"search"}, // List tools this agent can use
	}); err != nil {
		log.Fatalf("Failed to register agent: %v", err)
	}

	// Register verbose search tool on the client
	if err := client.RegisterTool(&VerboseSearchTool{}); err != nil {
		log.Fatalf("Failed to register tool: %v", err)
	}

	// Start the client (must be after all registrations)
	if err := client.Start(ctx); err != nil {
		log.Fatalf("Failed to start client: %v", err)
	}
	defer func() {
		if err := client.Stop(context.Background()); err != nil {
			log.Printf("Error stopping client: %v", err)
		}
	}()

	log.Printf("Client started (instance ID: %s)", client.InstanceID())

	// Note: Manual compaction and compaction hooks are part of the old Agent API.
	// In the new per-client API, compaction is handled automatically by the framework
	// based on session-level configuration. Manual compaction control via Compact()
	// and compaction hooks (OnBeforeCompaction/OnAfterCompaction) are not yet
	// available in the new API.
	//
	// For now, this example demonstrates the per-client registration pattern.
	// Compaction features will be re-introduced in a future API update.

	// Create session using client API
	sessionID, err := client.NewSession(ctx, "1", "manual-compaction-demo", nil, map[string]any{
		"description": "Manual compaction demonstration",
	})
	if err != nil {
		log.Fatalf("Failed to create session: %v", err)
	}

	fmt.Println(strings.Repeat("=", 60))
	fmt.Println("MANUAL COMPACTION DEMO (Per-Client API)")
	fmt.Println(strings.Repeat("=", 60))
	fmt.Printf("\nSession ID: %s\n", sessionID)
	fmt.Println("\nNote: Manual compaction features are being re-introduced in the new API.")
	fmt.Println("This example demonstrates per-client registration and tool usage.")
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

		response, err := client.RunSync(ctx, sessionID, "manual-compaction-demo", query)
		if err != nil {
			log.Fatalf("Failed to run agent: %v", err)
		}

		// Print brief response
		fmt.Printf("  -> Response: %s\n", response.Text[:min(80, len(response.Text))])
		if len(response.Text) > 80 {
			fmt.Print("...")
		}
		fmt.Println()
	}

	// ==========================================================
	// Manual compaction features (OLD API - currently unavailable)
	// ==========================================================
	//
	// The following features are part of the old Agent API and are not yet
	// available in the new per-client API:
	//
	// - agent.GetMessages(ctx, sessionID) - retrieve session messages
	// - agent.GetCompactionStats(ctx, sessionID) - get compaction statistics
	// - agent.Compact(ctx, sessionID) - manually trigger compaction
	//
	// These will be re-introduced in a future update to the new API.
	// For now, compaction happens automatically based on framework settings.

	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Println("COMPACTION FEATURES")
	fmt.Println(strings.Repeat("=", 60))
	fmt.Println("\nManual compaction control (Compact(), GetMessages(), GetCompactionStats())")
	fmt.Println("is being re-introduced in the new per-client API.")
	fmt.Println("\nFor now, compaction is handled automatically by the framework.")

	// ==========================================================
	// Verify conversation still works
	// ==========================================================

	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Println("VERIFICATION: Conversation continues with context")
	fmt.Println(strings.Repeat("=", 60))

	response, err := client.RunSync(ctx, sessionID, "manual-compaction-demo", "Based on our previous discussion, what were the main topics we covered?")
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

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
