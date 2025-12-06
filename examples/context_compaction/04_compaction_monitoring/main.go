package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/youssefsiam38/agentpg"
	"github.com/youssefsiam38/agentpg/compaction"
	"github.com/youssefsiam38/agentpg/driver/pgxv5"
)

// ==========================================================
// CompactionMonitor tracks and logs compaction events
// ==========================================================

type CompactionMonitor struct {
	mu     sync.Mutex
	events []CompactionEvent
}

type CompactionEvent struct {
	Timestamp       time.Time
	SessionID       string
	Strategy        string
	OriginalTokens  int
	CompactedTokens int
	Reduction       float64
	MessagesRemoved int
	Duration        time.Duration
}

func NewCompactionMonitor() *CompactionMonitor {
	return &CompactionMonitor{
		events: make([]CompactionEvent, 0),
	}
}

func (m *CompactionMonitor) RecordEvent(
	sessionID string,
	result *compaction.CompactionResult,
	duration time.Duration,
) {
	m.mu.Lock()
	defer m.mu.Unlock()

	reduction := 0.0
	if result.OriginalTokens > 0 {
		reduction = 100.0 * (1.0 - float64(result.CompactedTokens)/float64(result.OriginalTokens))
	}

	event := CompactionEvent{
		Timestamp:       time.Now(),
		SessionID:       sessionID,
		Strategy:        result.Strategy,
		OriginalTokens:  result.OriginalTokens,
		CompactedTokens: result.CompactedTokens,
		Reduction:       reduction,
		MessagesRemoved: result.MessagesRemoved,
		Duration:        duration,
	}

	m.events = append(m.events, event)

	// Log in structured format
	fmt.Printf("[COMPACTION EVENT] %s\n", event.Timestamp.Format(time.RFC3339))
	fmt.Printf("  Session: %s\n", event.SessionID[:8]+"...")
	fmt.Printf("  Strategy: %s\n", event.Strategy)
	fmt.Printf("  Tokens: %d -> %d (%.1f%% reduction)\n",
		event.OriginalTokens, event.CompactedTokens, event.Reduction)
	fmt.Printf("  Messages removed: %d\n", event.MessagesRemoved)
	fmt.Printf("  Duration: %v\n", event.Duration)
}

func (m *CompactionMonitor) GetEvents() []CompactionEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]CompactionEvent, len(m.events))
	copy(result, m.events)
	return result
}

func (m *CompactionMonitor) GetStats() (total int, avgReduction float64, totalTokensSaved int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.events) == 0 {
		return 0, 0, 0
	}

	var sumReduction float64
	for _, e := range m.events {
		sumReduction += e.Reduction
		totalTokensSaved += e.OriginalTokens - e.CompactedTokens
	}

	return len(m.events), sumReduction / float64(len(m.events)), totalTokensSaved
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

	// Create compaction monitor
	monitor := NewCompactionMonitor()

	// Track timing for compaction
	var compactionStart time.Time
	var currentSessionID string

	// Create agent with compaction enabled
	agent, err := agentpg.New(
		drv,
		agentpg.Config{
			Client:       &client,
			Model:        "claude-sonnet-4-5-20250929",
			SystemPrompt: "You are a helpful assistant. Provide detailed responses.",
		},
		agentpg.WithAutoCompaction(true),
		agentpg.WithCompactionTrigger(0.85),
		agentpg.WithCompactionTarget(80000),
		agentpg.WithMaxTokens(4096),
	)
	if err != nil {
		log.Fatalf("Failed to create agent: %v", err)
	}

	// ==========================================================
	// Register monitoring hooks
	// ==========================================================

	agent.OnBeforeCompaction(func(ctx context.Context, sessionID string) error {
		compactionStart = time.Now()
		currentSessionID = sessionID

		fmt.Println("\n" + strings.Repeat("=", 50))
		fmt.Println("[MONITOR] Compaction triggered")
		fmt.Printf("[MONITOR] Session: %s\n", sessionID)
		fmt.Printf("[MONITOR] Start time: %s\n", compactionStart.Format(time.RFC3339))
		fmt.Println(strings.Repeat("=", 50))

		return nil
	})

	agent.OnAfterCompaction(func(ctx context.Context, result *compaction.CompactionResult) error {
		duration := time.Since(compactionStart)

		// Record in monitor
		monitor.RecordEvent(currentSessionID, result, duration)

		// Log detailed results
		fmt.Println("\n[MONITOR] Compaction completed")
		fmt.Printf("[MONITOR] Summary content length: %d chars\n", len(result.Summary))
		fmt.Printf("[MONITOR] Preserved message count: %d\n", len(result.PreservedMessages))

		if len(result.Summary) > 0 {
			fmt.Println("\n[MONITOR] Summary preview:")
			summary := result.Summary
			if len(summary) > 200 {
				summary = summary[:200] + "..."
			}
			fmt.Printf("  %s\n", summary)
		}

		return nil
	})

	// Also monitor regular message activity
	agent.OnAfterMessage(func(ctx context.Context, response *agentpg.Response) error {
		fmt.Printf("[METRICS] Message received - Input: %d, Output: %d tokens\n",
			response.Usage.InputTokens,
			response.Usage.OutputTokens)
		return nil
	})

	// Create session
	sessionID, err := agent.NewSession(ctx, "1", "monitoring-demo", nil, map[string]any{
		"description": "Compaction monitoring demonstration",
	})
	if err != nil {
		log.Fatalf("Failed to create session: %v", err)
	}

	fmt.Printf("Created session: %s\n\n", sessionID)

	// ==========================================================
	// Run conversation to potentially trigger compaction
	// ==========================================================

	prompts := []string{
		"Explain the complete history of artificial intelligence, from its origins to modern deep learning. Include all major milestones.",
		"Describe all the different types of neural network architectures in detail, including CNNs, RNNs, Transformers, and their applications.",
		"Explain the mathematics behind backpropagation, gradient descent, and optimization algorithms used in machine learning.",
		"Discuss the ethical considerations and potential risks of AI systems, including bias, privacy, and safety concerns.",
		"Provide a comprehensive overview of natural language processing, including tokenization, embeddings, attention mechanisms, and large language models.",
	}

	for i, prompt := range prompts {
		fmt.Printf("\n=== Query %d/%d ===\n", i+1, len(prompts))
		fmt.Printf("Prompt: %s\n\n", truncate(prompt, 60))

		response, err := agent.Run(ctx, prompt)
		if err != nil {
			log.Fatalf("Failed to run agent: %v", err)
		}

		// Print truncated response
		for _, block := range response.Message.Content {
			if block.Type == agentpg.ContentTypeText {
				fmt.Printf("Response: %s\n", truncate(block.Text, 150))
			}
		}
	}

	// ==========================================================
	// Display monitoring summary
	// ==========================================================

	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Println("               COMPACTION MONITORING SUMMARY")
	fmt.Println(strings.Repeat("=", 60))

	total, avgReduction, tokensSaved := monitor.GetStats()

	fmt.Printf("\nTotal compaction events: %d\n", total)

	if total > 0 {
		fmt.Printf("Average reduction: %.1f%%\n", avgReduction)
		fmt.Printf("Total tokens saved: %d\n", tokensSaved)

		fmt.Println("\nEvent History:")
		fmt.Println(strings.Repeat("-", 60))
		events := monitor.GetEvents()
		for i, event := range events {
			fmt.Printf("%d. %s\n", i+1, event.Timestamp.Format("15:04:05"))
			fmt.Printf("   Strategy: %s | Reduction: %.1f%% | Duration: %v\n",
				event.Strategy, event.Reduction, event.Duration)
		}
	} else {
		fmt.Println("\nNo compaction events occurred during this session.")
		fmt.Println("This is normal for conversations within context limits.")
	}

	// Get session info
	session, err := agent.GetSession(ctx, sessionID)
	if err != nil {
		log.Printf("Failed to get session: %v", err)
	} else {
		fmt.Printf("\nSession compaction count: %d\n", session.CompactionCount)
	}

	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Println("=== Demo Complete ===")
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
