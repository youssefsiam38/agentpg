// Package main demonstrates the Client API with custom compaction strategy.
//
// This example shows:
// - Implementing the compaction.Strategy interface
// - Custom logic for preserving tool results
// - How to integrate custom strategies with AgentPG
package main

import (
	"context"
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
	"github.com/youssefsiam38/agentpg/types"
)

// ==========================================================
// Custom Strategy: KeepToolResultsStrategy
//
// This strategy demonstrates implementing compaction.Strategy
// It keeps tool results intact while summarizing regular messages.
// ==========================================================

// KeepToolResultsStrategy is a custom compaction strategy
// that prioritizes keeping tool call results intact.
type KeepToolResultsStrategy struct{}

// NewKeepToolResultsStrategy creates a new custom strategy
func NewKeepToolResultsStrategy() *KeepToolResultsStrategy {
	return &KeepToolResultsStrategy{}
}

// Name returns the strategy name
func (s *KeepToolResultsStrategy) Name() string {
	return "keep_tools"
}

// ShouldCompact checks if compaction is needed
func (s *KeepToolResultsStrategy) ShouldCompact(
	messages []*types.Message,
	config compaction.CompactionConfig,
) bool {
	// Calculate total tokens
	totalTokens := 0
	for _, msg := range messages {
		totalTokens += msg.TokenCount()
	}

	// Trigger at threshold percentage
	threshold := int(float64(config.MaxContextTokens) * config.TriggerThreshold)
	return totalTokens >= threshold
}

// Compact performs the custom compaction logic
func (s *KeepToolResultsStrategy) Compact(
	ctx context.Context,
	messages []*types.Message,
	config compaction.CompactionConfig,
) (*compaction.CompactionResult, error) {
	if len(messages) == 0 {
		return nil, nil
	}

	// Calculate original tokens
	originalTokens := 0
	for _, msg := range messages {
		originalTokens += msg.TokenCount()
	}

	// Separate messages into categories
	var toolMessages []*types.Message
	var textMessages []*types.Message
	var preservedMessages []*types.Message

	// Protect the last N messages
	protectedStart := len(messages) - config.PreserveLastN
	if protectedStart < 0 {
		protectedStart = 0
	}

	for i, msg := range messages {
		// Always preserve recent messages
		if i >= protectedStart {
			preservedMessages = append(preservedMessages, msg)
			continue
		}

		// Check if message contains tool content
		hasToolContent := false
		for _, block := range msg.Content {
			if block.Type == types.ContentTypeToolUse || block.Type == types.ContentTypeToolResult {
				hasToolContent = true
				break
			}
		}

		if hasToolContent {
			toolMessages = append(toolMessages, msg)
		} else {
			textMessages = append(textMessages, msg)
		}
	}

	// Create summary of text-only messages
	var summary string
	if len(textMessages) > 0 {
		// In production, you would call Claude to summarize
		// Here we create a simple structural summary
		summary = s.createSummary(textMessages)
	}

	// Calculate compacted tokens (estimate)
	compactedTokens := 0

	// Tool messages kept intact
	for _, msg := range toolMessages {
		compactedTokens += msg.TokenCount()
	}

	// Preserved messages kept intact
	for _, msg := range preservedMessages {
		compactedTokens += msg.TokenCount()
	}

	// Summary tokens (rough estimate)
	compactedTokens += len(summary) / 4

	// Collect all preserved messages (both recent and tool messages)
	allPreserved := make([]*types.Message, 0, len(preservedMessages)+len(toolMessages))
	allPreserved = append(allPreserved, preservedMessages...)
	allPreserved = append(allPreserved, toolMessages...)

	return &compaction.CompactionResult{
		Strategy:          s.Name(),
		OriginalTokens:    originalTokens,
		CompactedTokens:   compactedTokens,
		Summary:           summary,
		PreservedMessages: allPreserved,
		MessagesRemoved:   len(textMessages),
	}, nil
}

// createSummary creates a structural summary of messages
func (s *KeepToolResultsStrategy) createSummary(messages []*types.Message) string {
	var sb strings.Builder
	sb.WriteString("## Conversation Summary\n\n")

	userTopics := []string{}
	assistantPoints := []string{}

	for _, msg := range messages {
		for _, block := range msg.Content {
			if block.Type == types.ContentTypeText {
				text := block.Text
				if len(text) > 100 {
					text = text[:100] + "..."
				}

				if msg.Role == types.RoleUser {
					userTopics = append(userTopics, text)
				} else if msg.Role == types.RoleAssistant {
					assistantPoints = append(assistantPoints, text)
				}
			}
		}
	}

	if len(userTopics) > 0 {
		sb.WriteString("**User discussed:**\n")
		for i, topic := range userTopics {
			if i >= 5 {
				sb.WriteString(fmt.Sprintf("- ... and %d more topics\n", len(userTopics)-5))
				break
			}
			sb.WriteString(fmt.Sprintf("- %s\n", topic))
		}
	}

	if len(assistantPoints) > 0 {
		sb.WriteString("\n**Key points covered:**\n")
		for i, point := range assistantPoints {
			if i >= 5 {
				sb.WriteString(fmt.Sprintf("- ... and %d more responses\n", len(assistantPoints)-5))
				break
			}
			sb.WriteString(fmt.Sprintf("- %s\n", point))
		}
	}

	return sb.String()
}

// Register agent at package initialization.
func init() {
	maxTokens := 1024
	agentpg.MustRegister(&agentpg.AgentDefinition{
		Name:         "custom-strategy-demo",
		Description:  "Assistant for custom strategy demonstration",
		Model:        "claude-sonnet-4-5-20250929",
		SystemPrompt: "You are a helpful assistant.",
		MaxTokens:    &maxTokens,
		Config: map[string]any{
			"auto_compaction": true,
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

	// ==========================================================
	// Demonstrate the custom strategy
	// ==========================================================

	fmt.Println("=== Custom Compaction Strategy Demo ===")
	fmt.Println()

	// Create the custom strategy
	customStrategy := NewKeepToolResultsStrategy()

	fmt.Printf("Strategy Name: %s\n", customStrategy.Name())
	fmt.Println("\nThis strategy:")
	fmt.Println("1. Keeps all tool call results intact")
	fmt.Println("2. Summarizes regular text messages")
	fmt.Println("3. Always preserves recent messages")
	fmt.Println()

	// ==========================================================
	// Create sample messages to demonstrate compaction
	// ==========================================================

	fmt.Println("=== Sample Message Compaction ===")
	fmt.Println()

	// Create sample messages
	sampleMessages := []*types.Message{
		{
			ID:      "msg-1",
			Role:    types.RoleUser,
			Content: []types.ContentBlock{{Type: types.ContentTypeText, Text: "What is the weather in Tokyo?"}},
			Usage:   &types.Usage{InputTokens: 10},
		},
		{
			ID:   "msg-2",
			Role: types.RoleAssistant,
			Content: []types.ContentBlock{
				{Type: types.ContentTypeToolUse, ToolName: "get_weather"},
			},
			Usage: &types.Usage{OutputTokens: 25},
		},
		{
			ID:   "msg-3",
			Role: types.RoleUser,
			Content: []types.ContentBlock{
				{Type: types.ContentTypeToolResult, ToolContent: "Tokyo: 22°C, Sunny"},
			},
			Usage: &types.Usage{InputTokens: 20},
		},
		{
			ID:      "msg-4",
			Role:    types.RoleAssistant,
			Content: []types.ContentBlock{{Type: types.ContentTypeText, Text: "The weather in Tokyo is currently 22°C and sunny."}},
			Usage:   &types.Usage{OutputTokens: 15},
		},
		{
			ID:      "msg-5",
			Role:    types.RoleUser,
			Content: []types.ContentBlock{{Type: types.ContentTypeText, Text: "Tell me about Japanese culture and traditions."}},
			Usage:   &types.Usage{InputTokens: 12},
		},
		{
			ID:      "msg-6",
			Role:    types.RoleAssistant,
			Content: []types.ContentBlock{{Type: types.ContentTypeText, Text: "Japanese culture is rich with traditions including tea ceremonies, calligraphy, and seasonal festivals."}},
			Usage:   &types.Usage{OutputTokens: 45},
		},
		{
			ID:      "msg-7",
			Role:    types.RoleUser,
			Content: []types.ContentBlock{{Type: types.ContentTypeText, Text: "What about food recommendations?"}},
			Usage:   &types.Usage{InputTokens: 8},
		},
		{
			ID:      "msg-8",
			Role:    types.RoleAssistant,
			Content: []types.ContentBlock{{Type: types.ContentTypeText, Text: "I recommend trying sushi, ramen, and tempura for authentic Japanese cuisine."}},
			Usage:   &types.Usage{OutputTokens: 20},
		},
	}

	// Show original messages
	fmt.Println("Original messages:")
	for _, msg := range sampleMessages {
		msgType := "text"
		for _, block := range msg.Content {
			if block.Type == types.ContentTypeToolUse || block.Type == types.ContentTypeToolResult {
				msgType = "tool"
				break
			}
		}
		fmt.Printf("  [%s] %s (%s, %d tokens)\n", msg.Role, msg.ID, msgType, msg.TokenCount())
	}

	// Configure compaction
	config := compaction.CompactionConfig{
		MaxContextTokens: 200000,
		TriggerThreshold: 0.85,
		TargetTokens:     80000,
		PreserveLastN:    2, // Keep last 2 messages
	}

	// Check if should compact
	fmt.Printf("\nTotal tokens: %d\n", sumTokens(sampleMessages))
	fmt.Printf("Should compact: %v\n", customStrategy.ShouldCompact(sampleMessages, config))

	// Perform compaction
	result, err := customStrategy.Compact(ctx, sampleMessages, config)
	if err != nil {
		log.Fatalf("Compaction failed: %v", err)
	}

	fmt.Println("\n=== Compaction Result ===")
	fmt.Printf("Strategy: %s\n", result.Strategy)
	fmt.Printf("Original tokens: %d\n", result.OriginalTokens)
	fmt.Printf("Compacted tokens: %d\n", result.CompactedTokens)
	fmt.Printf("Reduction: %.1f%%\n", 100.0*(1.0-float64(result.CompactedTokens)/float64(result.OriginalTokens)))
	fmt.Printf("Messages removed: %d\n", result.MessagesRemoved)

	// Extract IDs from preserved messages for display
	preservedIDs := make([]string, len(result.PreservedMessages))
	for i, msg := range result.PreservedMessages {
		preservedIDs[i] = msg.ID
	}
	fmt.Printf("Preserved message IDs: %v\n", preservedIDs)

	fmt.Println("\n=== Generated Summary ===")
	fmt.Println(result.Summary)

	// ==========================================================
	// Show how to use with AgentPG
	// ==========================================================

	fmt.Println("=== Using with AgentPG ===")
	fmt.Println()
	fmt.Println("To use a custom strategy with AgentPG:")
	fmt.Println()
	fmt.Println("1. Implement the compaction.Strategy interface")
	fmt.Println("2. The strategy will be called during compaction")
	fmt.Println("3. Your Compact() method determines what gets preserved")
	fmt.Println()
	fmt.Println("Note: Currently, strategies are registered internally.")
	fmt.Println("Custom strategies can be added by extending the compaction package.")

	// Create driver
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

	// Get the registered agent
	agent := client.Agent("custom-strategy-demo")
	if agent == nil {
		log.Fatal("Agent 'custom-strategy-demo' not found in registry")
	}

	fmt.Println("\nAgent created with default compaction strategy.")
	fmt.Println("Custom strategies extend the same Strategy interface.")

	fmt.Println("\n=== Demo Complete ===")
}

func sumTokens(messages []*types.Message) int {
	total := 0
	for _, msg := range messages {
		total += msg.TokenCount()
	}
	return total
}
