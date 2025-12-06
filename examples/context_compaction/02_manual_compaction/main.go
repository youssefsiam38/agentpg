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

	// ==========================================================
	// Create agent with MANUAL compaction control
	// ==========================================================

	agent, err := agentpg.New(
		agentpg.Config{
			DB:           pool,
			Client:       &client,
			Model:        "claude-sonnet-4-5-20250929",
			SystemPrompt: "You are a research assistant. Use the search tool to find information.",
		},
		// DISABLE auto-compaction for manual control
		agentpg.WithAutoCompaction(false),

		// These settings are still configured but only used when manually triggered
		agentpg.WithCompactionTrigger(0.80),          // 80% threshold
		agentpg.WithCompactionTarget(40000),          // Target 40K tokens
		agentpg.WithCompactionPreserveN(5),           // Keep last 5 messages
		agentpg.WithCompactionProtectedTokens(20000), // Protect last 20K tokens

		// Use Haiku for cost-effective summarization
		agentpg.WithSummarizerModel("claude-3-5-haiku-20241022"),

		agentpg.WithMaxTokens(2048),
	)
	if err != nil {
		log.Fatalf("Failed to create agent: %v", err)
	}

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

	fmt.Printf("Created session: %s\n\n", sessionID)
	fmt.Println("=== Configuration ===")
	fmt.Println("- Auto-compaction: DISABLED")
	fmt.Println("- Compaction trigger: 80%")
	fmt.Println("- Target after compaction: 40,000 tokens")
	fmt.Println("- Preserve last N messages: 5")
	fmt.Println("- Protected tokens: 20,000")
	fmt.Println("- Summarizer model: claude-3-5-haiku-20241022")
	fmt.Println()

	// ==========================================================
	// Run several queries to build up context
	// ==========================================================

	queries := []string{
		"Search for information about microservices architecture",
		"Search for best practices in API design",
		"Search for database optimization techniques",
		"Search for containerization with Docker and Kubernetes",
	}

	for i, query := range queries {
		fmt.Printf("=== Query %d: %s ===\n", i+1, query)

		response, err := agent.Run(ctx, query)
		if err != nil {
			log.Fatalf("Failed to run agent: %v", err)
		}

		// Print truncated response
		for _, block := range response.Message.Content {
			if block.Type == agentpg.ContentTypeText {
				text := block.Text
				if len(text) > 150 {
					text = text[:150] + "..."
				}
				fmt.Println(text)
			}
		}

		fmt.Printf("Tokens used - Input: %d, Output: %d\n\n",
			response.Usage.InputTokens,
			response.Usage.OutputTokens)
	}

	// ==========================================================
	// Get session info before compaction
	// ==========================================================

	fmt.Println("=== Session Status Before Compaction ===")
	session, err := agent.GetSession(ctx, sessionID)
	if err != nil {
		log.Fatalf("Failed to get session: %v", err)
	}

	messages, err := agent.GetMessages(ctx)
	if err != nil {
		log.Fatalf("Failed to get messages: %v", err)
	}

	// Calculate total tokens
	totalTokens := 0
	for _, msg := range messages {
		totalTokens += msg.TokenCount()
	}

	fmt.Printf("Session ID: %s\n", session.ID)
	fmt.Printf("Total messages: %d\n", len(messages))
	fmt.Printf("Estimated total tokens: %d\n", totalTokens)
	fmt.Printf("Compaction count: %d\n", session.CompactionCount)

	// ==========================================================
	// Note: Manual compaction would require direct access to
	// the compaction manager, which is internal. In practice,
	// you would trigger compaction by:
	// 1. Re-enabling auto-compaction temporarily
	// 2. Using hooks to detect when compaction is needed
	// 3. Implementing custom compaction logic
	// ==========================================================

	fmt.Println("\n=== Manual Compaction Control ===")
	fmt.Println("With auto-compaction disabled, you have full control over when")
	fmt.Println("context is compacted. This is useful for:")
	fmt.Println("1. Batch processing - compact at logical breakpoints")
	fmt.Println("2. Critical conversations - preserve everything during important exchanges")
	fmt.Println("3. Testing - observe context growth without automatic intervention")
	fmt.Println("4. Cost optimization - compact only when truly necessary")

	// ==========================================================
	// Continue conversation to show context accumulation
	// ==========================================================

	fmt.Println("\n=== Additional Conversation ===")
	response, err := agent.Run(ctx, "Summarize what we've learned about microservices and API design.")
	if err != nil {
		log.Fatalf("Failed to run agent: %v", err)
	}

	for _, block := range response.Message.Content {
		if block.Type == agentpg.ContentTypeText {
			text := block.Text
			if len(text) > 300 {
				text = text[:300] + "..."
			}
			fmt.Println(text)
		}
	}

	// Final status
	messages, _ = agent.GetMessages(ctx)
	totalTokens = 0
	for _, msg := range messages {
		totalTokens += msg.TokenCount()
	}

	fmt.Println("\n=== Final Session Status ===")
	fmt.Printf("Total messages: %d\n", len(messages))
	fmt.Printf("Estimated total tokens: %d\n", totalTokens)
	fmt.Println("(No automatic compaction occurred because it was disabled)")

	fmt.Println("\n=== Demo Complete ===")
}
