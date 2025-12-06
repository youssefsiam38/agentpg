package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
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
	var lastCompaction *compaction.CompactionResult

	// ==========================================================
	// Create agent with LOW thresholds to demonstrate compaction
	// ==========================================================

	agent, err := agentpg.New(
		drv,
		agentpg.Config{
			Client:       &client,
			Model:        "claude-sonnet-4-5-20250929",
			SystemPrompt: "You are a research assistant. Use the search tool to find information. Keep responses brief.",
		},
		// DISABLE auto-compaction for manual control
		agentpg.WithAutoCompaction(false),

		// LOW thresholds to trigger SUMMARIZATION (not just pruning)
		agentpg.WithCompactionTarget(500),           // Target only 500 tokens - forces summarization
		agentpg.WithCompactionPreserveN(2),          // Keep only last 2 messages
		agentpg.WithCompactionProtectedTokens(1000), // Protect only last 1K tokens

		// Use Haiku for cost-effective summarization
		agentpg.WithSummarizerModel("claude-3-5-haiku-20241022"),

		agentpg.WithMaxTokens(1024),
	)
	if err != nil {
		log.Fatalf("Failed to create agent: %v", err)
	}

	// Register compaction hooks for monitoring
	agent.OnBeforeCompaction(func(ctx context.Context, sessionID string) error {
		fmt.Println("\n" + strings.Repeat("=", 60))
		fmt.Println("COMPACTION STARTING...")
		fmt.Println(strings.Repeat("=", 60))
		return nil
	})

	agent.OnAfterCompaction(func(ctx context.Context, result *compaction.CompactionResult) error {
		lastCompaction = result
		return nil
	})

	// Register verbose search tool
	if err := agent.RegisterTool(&VerboseSearchTool{}); err != nil {
		log.Fatalf("Failed to register tool: %v", err)
	}

	// Create session
	sessionID, err := agent.NewSession(ctx, "1", "manual-compaction-demo", nil, map[string]any{
		"description": "Manual compaction demonstration",
	})
	if err != nil {
		log.Fatalf("Failed to create session: %v", err)
	}

	fmt.Println(strings.Repeat("=", 60))
	fmt.Println("MANUAL COMPACTION DEMO")
	fmt.Println(strings.Repeat("=", 60))
	fmt.Printf("\nSession ID: %s\n", sessionID)
	fmt.Println("\nConfiguration (LOW thresholds to trigger SUMMARIZATION):")
	fmt.Println("  - Auto-compaction: DISABLED")
	fmt.Println("  - Target tokens: 500 (forces summarization after pruning)")
	fmt.Println("  - Preserve last N: 2 messages")
	fmt.Println("  - Protected tokens: 1,000")
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

		response, err := agent.Run(ctx, query)
		if err != nil {
			log.Fatalf("Failed to run agent: %v", err)
		}

		// Print brief response
		for _, block := range response.Message.Content {
			if block.Type == agentpg.ContentTypeText {
				text := block.Text
				if len(text) > 80 {
					text = text[:80] + "..."
				}
				fmt.Printf("  -> %s\n", text)
			}
		}
	}

	// ==========================================================
	// Show messages BEFORE compaction
	// ==========================================================

	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Println("BEFORE COMPACTION")
	fmt.Println(strings.Repeat("=", 60))

	messagesBefore, err := agent.GetMessages(ctx)
	if err != nil {
		log.Fatalf("Failed to get messages: %v", err)
	}

	statsBefore, err := agent.GetCompactionStats(ctx)
	if err != nil {
		log.Fatalf("Failed to get stats: %v", err)
	}

	fmt.Printf("\nTotal Messages: %d\n", len(messagesBefore))
	fmt.Printf("Total Tokens: %d\n", statsBefore.CurrentTokens)
	fmt.Printf("Context Utilization: %.1f%%\n", statsBefore.UtilizationPct)

	fmt.Println("\nMessage List:")
	for i, msg := range messagesBefore {
		content := getMessagePreview(msg)
		tokens := msg.TokenCount()
		fmt.Printf("  [%d] %s (%d tokens): %s\n", i+1, msg.Role, tokens, content)
	}

	// ==========================================================
	// Trigger manual compaction
	// ==========================================================

	result, err := agent.Compact(ctx)
	if err != nil {
		log.Fatalf("Failed to compact: %v", err)
	}

	// ==========================================================
	// Show messages AFTER compaction
	// ==========================================================

	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Println("AFTER COMPACTION")
	fmt.Println(strings.Repeat("=", 60))

	messagesAfter, err := agent.GetMessages(ctx)
	if err != nil {
		log.Fatalf("Failed to get messages: %v", err)
	}

	statsAfter, err := agent.GetCompactionStats(ctx)
	if err != nil {
		log.Fatalf("Failed to get stats: %v", err)
	}

	fmt.Printf("\nTotal Messages: %d\n", len(messagesAfter))
	fmt.Printf("Total Tokens: %d\n", statsAfter.CurrentTokens)
	fmt.Printf("Context Utilization: %.1f%%\n", statsAfter.UtilizationPct)

	fmt.Println("\nMessage List:")
	for i, msg := range messagesAfter {
		content := getMessagePreview(msg)
		tokens := msg.TokenCount()
		label := ""
		if msg.IsSummary {
			label = " [SUMMARY]"
		}
		fmt.Printf("  [%d] %s%s (%d tokens): %s\n", i+1, msg.Role, label, tokens, content)
	}

	// ==========================================================
	// Show the DIFF
	// ==========================================================

	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Println("COMPACTION DIFF")
	fmt.Println(strings.Repeat("=", 60))

	if result != nil && lastCompaction != nil {
		fmt.Printf("\nStrategy Used: %s\n", result.Strategy)
		fmt.Println()

		// Token diff
		tokensSaved := result.OriginalTokens - result.CompactedTokens
		reductionPct := 100.0 * (1.0 - float64(result.CompactedTokens)/float64(result.OriginalTokens))
		fmt.Println("Tokens:")
		fmt.Printf("  Before: %d\n", result.OriginalTokens)
		fmt.Printf("  After:  %d\n", result.CompactedTokens)
		fmt.Printf("  Saved:  %d (%.1f%% reduction)\n", tokensSaved, reductionPct)
		fmt.Println()

		// Message diff
		fmt.Println("Messages:")
		fmt.Printf("  Before: %d\n", len(messagesBefore))
		fmt.Printf("  After:  %d\n", len(messagesAfter))
		fmt.Printf("  Removed: %d\n", result.MessagesRemoved)
		fmt.Printf("  Preserved: %d\n", len(result.PreservedMessages))
		fmt.Println()

		// Summary info
		if result.Summary != "" {
			fmt.Println("Generated Summary:")
			fmt.Println(strings.Repeat("-", 40))
			summary := result.Summary
			if len(summary) > 500 {
				summary = summary[:500] + "..."
			}
			fmt.Println(summary)
			fmt.Println(strings.Repeat("-", 40))
		}
	} else {
		fmt.Println("\nNo compaction was performed (context within limits)")
	}

	// ==========================================================
	// Verify conversation still works
	// ==========================================================

	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Println("VERIFICATION: Conversation continues with context")
	fmt.Println(strings.Repeat("=", 60))

	response, err := agent.Run(ctx, "Based on our previous discussion, what were the main topics we covered?")
	if err != nil {
		log.Fatalf("Failed to run agent: %v", err)
	}

	fmt.Println("\nAgent response:")
	for _, block := range response.Message.Content {
		if block.Type == agentpg.ContentTypeText {
			text := block.Text
			if len(text) > 400 {
				text = text[:400] + "..."
			}
			fmt.Println(text)
		}
	}

	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Println("DEMO COMPLETE")
	fmt.Println(strings.Repeat("=", 60))
}

// getMessagePreview returns a short preview of message content
func getMessagePreview(msg *agentpg.Message) string {
	for _, block := range msg.Content {
		switch block.Type {
		case agentpg.ContentTypeText:
			text := block.Text
			if len(text) > 50 {
				text = text[:50] + "..."
			}
			return text
		case agentpg.ContentTypeToolUse:
			return fmt.Sprintf("[tool_use: %s]", block.ToolName)
		case agentpg.ContentTypeToolResult:
			content := block.ToolContent
			if len(content) > 30 {
				content = content[:30] + "..."
			}
			return fmt.Sprintf("[tool_result: %s]", content)
		}
	}
	return "[empty]"
}
