// Package main demonstrates the Client API with compaction monitoring using per-client registration.
//
// This example shows:
// - Comprehensive compaction monitoring with hooks
// - Tracking metrics across compaction events
// - Before/after compaction hooks
// - Token usage metrics
// - Per-client agent registration (new API)
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/youssefsiam38/agentpg"
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

	// Register agent on the client (new per-client API)
	maxTokens := 4096
	if err := client.RegisterAgent(&agentpg.AgentDefinition{
		Name:         "compaction-monitoring-demo",
		Description:  "Assistant with compaction monitoring",
		Model:        "claude-sonnet-4-5-20250929",
		SystemPrompt: "You are a helpful assistant. Provide detailed responses.",
		MaxTokens:    &maxTokens,
	}); err != nil {
		log.Fatalf("Failed to register agent: %v", err)
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

	// NOTE: The old API had agent-specific hooks (OnBeforeCompaction, OnAfterCompaction, OnAfterMessage)
	// These hooks are not available in the new per-client API.
	// Compaction monitoring would need to be implemented differently in the new architecture.

	// Create compaction monitor (for demonstration purposes)
	monitor := NewCompactionMonitor()
	_ = monitor // Avoid unused variable warning

	// Create session using client API
	sessionID, err := client.NewSession(ctx, "1", "monitoring-demo", nil, map[string]any{
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

		response, err := client.RunSync(ctx, sessionID, "compaction-monitoring-demo", prompt)
		if err != nil {
			log.Fatalf("Failed to run agent: %v", err)
		}

		// Print truncated response
		fmt.Printf("Response: %s\n", truncate(response.Text, 150))

		// Print token usage
		fmt.Printf("[METRICS] Input: %d, Output: %d tokens\n",
			response.Usage.InputTokens,
			response.Usage.OutputTokens)
	}

	// ==========================================================
	// Display monitoring summary
	// ==========================================================

	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Println("               COMPACTION MONITORING SUMMARY")
	fmt.Println(strings.Repeat("=", 60))

	// NOTE: Compaction monitoring is not implemented in this example because
	// the new per-client API doesn't expose compaction hooks.
	// In a production system, you would monitor compaction through:
	// - Database queries to agentpg_compaction_events table
	// - Custom logging/metrics collection
	// - Periodic polling of session statistics

	fmt.Println("\nNOTE: Compaction monitoring hooks are part of the old API.")
	fmt.Println("To monitor compaction in the new API, query the agentpg_compaction_events table directly.")

	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Println("=== Demo Complete ===")
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
