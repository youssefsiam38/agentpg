// Package main demonstrates custom compaction strategies.
//
// This example shows:
// - Switching between Hybrid and Summarization strategies
// - Comparing compaction results with different strategies
// - Using CompactWithConfig for one-off custom configuration
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

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/youssefsiam38/agentpg"
	"github.com/youssefsiam38/agentpg/compaction"
	"github.com/youssefsiam38/agentpg/driver/pgxv5"
	"github.com/youssefsiam38/agentpg/tool"
)

// VerboseDocTool generates large documentation outputs
type VerboseDocTool struct{}

func (v *VerboseDocTool) Name() string { return "get_docs" }
func (v *VerboseDocTool) Description() string {
	return "Get documentation for a topic (returns detailed docs)"
}

func (v *VerboseDocTool) InputSchema() tool.ToolSchema {
	return tool.ToolSchema{
		Type: "object",
		Properties: map[string]tool.PropertyDef{
			"topic": {Type: "string", Description: "Documentation topic"},
		},
		Required: []string{"topic"},
	}
}

func (v *VerboseDocTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var params struct {
		Topic string `json:"topic"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", err
	}

	// Generate verbose documentation to fill context
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# Documentation: %s\n\n", params.Topic))

	sections := []string{"Overview", "Getting Started", "API Reference", "Examples", "Best Practices"}
	for _, section := range sections {
		sb.WriteString(fmt.Sprintf("## %s\n\n", section))
		sb.WriteString(fmt.Sprintf("This section covers %s for %s. ", section, params.Topic))
		sb.WriteString("It provides comprehensive information including code samples, ")
		sb.WriteString("configuration options, and common use cases. ")
		sb.WriteString("The documentation is designed to help both beginners and ")
		sb.WriteString("experienced developers understand the concepts.\n\n")
	}

	return sb.String(), nil
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		log.Fatal("ANTHROPIC_API_KEY environment variable is required")
	}

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://agentpg:agentpg@localhost:5432/agentpg?sslmode=disable"
	}

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer pool.Close()

	drv := pgxv5.New(pool)

	// Create client with Hybrid strategy (default)
	client, err := agentpg.NewClient(drv, &agentpg.ClientConfig{
		APIKey:                apiKey,
		AutoCompactionEnabled: false,
		CompactionConfig: &compaction.Config{
			Strategy:     compaction.StrategyHybrid, // Default: prune tool outputs first
			Trigger:      0.30,                      // Lower threshold for demo
			TargetTokens: 3000,
		},
	})
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}

	maxTokens := 1024
	if err := client.RegisterAgent(&agentpg.AgentDefinition{
		Name:         "strategy-demo",
		Description:  "Documentation assistant",
		Model:        "claude-sonnet-4-5-20250929",
		SystemPrompt: "You are a documentation assistant. Use the get_docs tool to retrieve documentation. Keep responses concise.",
		MaxTokens:    &maxTokens,
		Tools:        []string{"get_docs"},
	}); err != nil {
		log.Fatalf("Failed to register agent: %v", err)
	}

	if err := client.RegisterTool(&VerboseDocTool{}); err != nil {
		log.Fatalf("Failed to register tool: %v", err)
	}

	if err := client.Start(ctx); err != nil {
		log.Fatalf("Failed to start client: %v", err)
	}
	defer client.Stop(context.Background())

	log.Printf("Client started (instance ID: %s)", client.InstanceID())

	fmt.Println(strings.Repeat("=", 70))
	fmt.Println("CUSTOM COMPACTION STRATEGY DEMO")
	fmt.Println(strings.Repeat("=", 70))

	// ==========================================================
	// Demo 1: Hybrid Strategy (prune tool outputs first)
	// ==========================================================

	fmt.Println("\n" + strings.Repeat("-", 70))
	fmt.Println("DEMO 1: HYBRID STRATEGY")
	fmt.Println("(Prunes tool outputs first, then summarizes if needed)")
	fmt.Println(strings.Repeat("-", 70))

	sessionID1, err := client.NewSession(ctx, nil, map[string]any{"strategy": "hybrid"})
	if err != nil {
		log.Fatalf("Failed to create session: %v", err)
	}
	fmt.Printf("\nSession: %s\n", sessionID1)

	runConversation(ctx, client, sessionID1)
	compactAndReport(ctx, client, sessionID1, "Hybrid")

	// ==========================================================
	// Demo 2: Summarization Strategy
	// ==========================================================

	fmt.Println("\n" + strings.Repeat("-", 70))
	fmt.Println("DEMO 2: SUMMARIZATION STRATEGY")
	fmt.Println("(Summarizes all compactable messages directly)")
	fmt.Println(strings.Repeat("-", 70))

	sessionID2, err := client.NewSession(ctx, nil, map[string]any{"strategy": "summarization"})
	if err != nil {
		log.Fatalf("Failed to create session: %v", err)
	}
	fmt.Printf("\nSession: %s\n", sessionID2)

	runConversation(ctx, client, sessionID2)

	// Use CompactWithConfig to override strategy for this session
	fmt.Println("\nUsing CompactWithConfig with Summarization strategy...")
	result, err := client.CompactWithConfig(ctx, sessionID2, &compaction.Config{
		Strategy:     compaction.StrategySummarization, // Direct summarization
		Trigger:      0.30,
		TargetTokens: 3000,
	})
	if err != nil {
		log.Printf("Compaction error: %v", err)
	} else if result != nil {
		printCompactionResult(result, "Summarization")
	}

	// ==========================================================
	// Strategy Comparison
	// ==========================================================

	fmt.Println("\n" + strings.Repeat("=", 70))
	fmt.Println("STRATEGY COMPARISON")
	fmt.Println(strings.Repeat("=", 70))

	fmt.Println(`
Hybrid Strategy:
  - First phase: Prunes tool outputs (replaces with "[TOOL OUTPUT PRUNED]")
  - Second phase: Summarizes if still over target
  - Best for: Conversations with many tool calls and large tool outputs
  - Pros: More cost-effective, preserves tool structure
  - Cons: May lose some tool output details

Summarization Strategy:
  - Directly summarizes all compactable messages
  - Uses Claude to create a structured 9-section summary
  - Best for: Conversations without many tool calls
  - Pros: Comprehensive context preservation
  - Cons: Higher API cost, may lose fine-grained details`)

	fmt.Println("\n" + strings.Repeat("=", 70))
	fmt.Println("DEMO COMPLETE")
	fmt.Println(strings.Repeat("=", 70))
}

func runConversation(ctx context.Context, client *agentpg.Client[pgx.Tx], sessionID uuid.UUID) {
	topics := []string{
		"Get documentation for Kubernetes deployment",
		"Get documentation for Docker networking",
		"Get documentation for PostgreSQL indexing",
	}

	for i, topic := range topics {
		fmt.Printf("\nQuery %d: %s\n", i+1, topic)

		response, err := client.RunFastSync(ctx, sessionID, "strategy-demo", topic)
		if err != nil {
			log.Printf("Failed to run agent: %v", err)
			continue
		}

		text := response.Text
		if len(text) > 60 {
			text = text[:60] + "..."
		}
		fmt.Printf("  Response: %s\n", text)
	}
}

func compactAndReport(ctx context.Context, client *agentpg.Client[pgx.Tx], sessionID uuid.UUID, strategyName string) {
	// Get stats before
	statsBefore, _ := client.GetCompactionStats(ctx, sessionID)
	fmt.Printf("\nBefore: %d tokens, %d messages\n", statsBefore.TotalTokens, statsBefore.TotalMessages)

	// Compact using client's default config
	result, err := client.Compact(ctx, sessionID)
	if err != nil {
		log.Printf("Compaction error: %v", err)
		return
	}

	printCompactionResult(result, strategyName)
}

func printCompactionResult(result *compaction.Result, strategyName string) {
	reduction := 100.0 * (1.0 - float64(result.CompactedTokens)/float64(result.OriginalTokens))

	fmt.Printf("\n%s Strategy Result:\n", strategyName)
	fmt.Printf("  Original Tokens:  %d\n", result.OriginalTokens)
	fmt.Printf("  Compacted Tokens: %d\n", result.CompactedTokens)
	fmt.Printf("  Reduction:        %.1f%%\n", reduction)
	fmt.Printf("  Messages Removed: %d\n", result.MessagesRemoved)
	fmt.Printf("  Summary Created:  %v\n", result.SummaryCreated)
	fmt.Printf("  Duration:         %v\n", result.Duration)
}
