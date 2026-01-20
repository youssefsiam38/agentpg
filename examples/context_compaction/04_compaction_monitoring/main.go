// Package main demonstrates compaction monitoring using the Client API.
//
// This example shows:
// - Tracking compaction events and metrics
// - Querying compaction history from database
// - Monitoring token usage over time
// - Manual vs automatic compaction comparison
package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
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
	SessionID       uuid.UUID
	Strategy        compaction.Strategy
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

func (m *CompactionMonitor) RecordEvent(sessionID uuid.UUID, result *compaction.Result) {
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
		Duration:        result.Duration,
	}

	m.events = append(m.events, event)

	// Log in structured format
	fmt.Printf("\n[COMPACTION EVENT] %s\n", event.Timestamp.Format(time.RFC3339))
	fmt.Printf("  Session: %s\n", event.SessionID.String()[:8]+"...")
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

// ==========================================================
// TokenTracker monitors token usage over time
// ==========================================================

type TokenTracker struct {
	mu      sync.Mutex
	samples []TokenSample
}

type TokenSample struct {
	Timestamp   time.Time
	SessionID   uuid.UUID
	TotalTokens int
	Messages    int
}

func NewTokenTracker() *TokenTracker {
	return &TokenTracker{
		samples: make([]TokenSample, 0),
	}
}

func (t *TokenTracker) RecordSample(sessionID uuid.UUID, stats *compaction.Stats) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.samples = append(t.samples, TokenSample{
		Timestamp:   time.Now(),
		SessionID:   sessionID,
		TotalTokens: stats.TotalTokens,
		Messages:    stats.TotalMessages,
	})
}

func (t *TokenTracker) GetSamples() []TokenSample {
	t.mu.Lock()
	defer t.mu.Unlock()
	result := make([]TokenSample, len(t.samples))
	copy(result, t.samples)
	return result
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

	// Create client with low thresholds to trigger compaction
	client, err := agentpg.NewClient(drv, &agentpg.ClientConfig{
		APIKey:                apiKey,
		AutoCompactionEnabled: false, // Manual control for monitoring demo
		CompactionConfig: &compaction.Config{
			Strategy:     compaction.StrategyHybrid,
			Trigger:      0.30, // 30% threshold for demo
			TargetTokens: 3000,
		},
		Logger: slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		})),
	})
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}

	if err := client.Start(ctx); err != nil {
		log.Fatalf("Failed to start client: %v", err)
	}

	// Create agent in the database (after client.Start)
	maxTokens := 4096
	agent, err := client.CreateAgent(ctx, &agentpg.AgentDefinition{
		Name:         "agent.ID",
		Description:  "Assistant for compaction monitoring demo",
		Model:        "claude-sonnet-4-5-20250929",
		SystemPrompt: "You are a helpful assistant. Provide detailed responses.",
		MaxTokens:    &maxTokens,
	})
	if err != nil {
		log.Fatalf("Failed to create agent: %v", err)
	}
	defer client.Stop(context.Background())

	log.Printf("Client started (instance ID: %s)", client.InstanceID())

	// Initialize monitors
	compactionMonitor := NewCompactionMonitor()
	tokenTracker := NewTokenTracker()

	// Create session
	sessionID, err := client.NewSession(ctx, nil, map[string]any{"demo": "monitoring"})
	if err != nil {
		log.Fatalf("Failed to create session: %v", err)
	}

	fmt.Println(strings.Repeat("=", 70))
	fmt.Println("COMPACTION MONITORING DEMO")
	fmt.Println(strings.Repeat("=", 70))
	fmt.Printf("\nSession: %s\n", sessionID)

	// ==========================================================
	// Run conversation with monitoring
	// ==========================================================

	prompts := []string{
		"Explain the complete history of artificial intelligence, from its origins to modern deep learning. Include all major milestones.",
		"Describe all the different types of neural network architectures in detail, including CNNs, RNNs, Transformers, and their applications.",
		"Explain the mathematics behind backpropagation, gradient descent, and optimization algorithms used in machine learning.",
		"Discuss the ethical considerations and potential risks of AI systems, including bias, privacy, and safety concerns.",
		"Provide a comprehensive overview of natural language processing, including tokenization, embeddings, attention mechanisms, and large language models.",
	}

	for i, prompt := range prompts {
		fmt.Printf("\n--- Query %d/%d ---\n", i+1, len(prompts))
		fmt.Printf("Prompt: %s\n", truncate(prompt, 60))

		response, err := client.RunFastSync(ctx, sessionID, agent.ID, prompt, nil)
		if err != nil {
			log.Fatalf("Failed to run agent: %v", err)
		}

		fmt.Printf("Response: %s\n", truncate(response.Text, 100))
		fmt.Printf("Tokens - Input: %d, Output: %d\n",
			response.Usage.InputTokens, response.Usage.OutputTokens)

		// Track token usage
		stats, err := client.GetCompactionStats(ctx, sessionID)
		if err != nil {
			log.Printf("Failed to get stats: %v", err)
			continue
		}

		tokenTracker.RecordSample(sessionID, stats)
		fmt.Printf("Context: %d tokens (%.1f%%), %d messages\n",
			stats.TotalTokens, stats.UsagePercent*100, stats.TotalMessages)

		// Check if compaction is needed and perform it
		if stats.NeedsCompaction {
			fmt.Println("\n>>> Compaction threshold reached! Triggering compaction...")

			result, err := client.Compact(ctx, sessionID)
			if err != nil {
				log.Printf("Compaction failed: %v", err)
			} else {
				compactionMonitor.RecordEvent(sessionID, result)
			}
		}
	}

	// ==========================================================
	// Display Monitoring Summary
	// ==========================================================

	fmt.Println("\n" + strings.Repeat("=", 70))
	fmt.Println("MONITORING SUMMARY")
	fmt.Println(strings.Repeat("=", 70))

	// Token usage history
	fmt.Println("\n--- Token Usage History ---")
	samples := tokenTracker.GetSamples()
	for i, sample := range samples {
		fmt.Printf("  [%d] %s: %d tokens, %d messages\n",
			i+1, sample.Timestamp.Format("15:04:05"), sample.TotalTokens, sample.Messages)
	}

	// Compaction events
	fmt.Println("\n--- Compaction Events ---")
	events := compactionMonitor.GetEvents()
	if len(events) == 0 {
		fmt.Println("  No compaction events recorded")
	} else {
		for i, event := range events {
			fmt.Printf("  [%d] %s: %d -> %d tokens (%.1f%% reduction)\n",
				i+1, event.Timestamp.Format("15:04:05"),
				event.OriginalTokens, event.CompactedTokens, event.Reduction)
		}
	}

	// Overall stats
	fmt.Println("\n--- Overall Statistics ---")
	total, avgReduction, tokensSaved := compactionMonitor.GetStats()
	fmt.Printf("  Total compactions: %d\n", total)
	fmt.Printf("  Average reduction: %.1f%%\n", avgReduction)
	fmt.Printf("  Total tokens saved: %d\n", tokensSaved)

	// Final session stats
	finalStats, err := client.GetCompactionStats(ctx, sessionID)
	if err != nil {
		log.Printf("Failed to get final stats: %v", err)
	} else {
		fmt.Println("\n--- Final Session State ---")
		fmt.Printf("  Total messages: %d\n", finalStats.TotalMessages)
		fmt.Printf("  Total tokens: %d\n", finalStats.TotalTokens)
		fmt.Printf("  Context usage: %.1f%%\n", finalStats.UsagePercent*100)
		fmt.Printf("  Compaction count: %d\n", finalStats.CompactionCount)
		fmt.Printf("  Compactable messages: %d\n", finalStats.CompactableMessages)
		fmt.Printf("  Summary messages: %d\n", finalStats.SummaryMessages)
	}

	// Database query hint
	fmt.Println("\n--- Database Monitoring ---")
	fmt.Println("Query compaction history:")
	fmt.Printf("  SELECT * FROM agentpg_compaction_events WHERE session_id = '%s';\n", sessionID)
	fmt.Println("\nQuery archived messages:")
	fmt.Printf("  SELECT * FROM agentpg_message_archive WHERE session_id = '%s';\n", sessionID)

	fmt.Println("\n" + strings.Repeat("=", 70))
	fmt.Println("DEMO COMPLETE")
	fmt.Println(strings.Repeat("=", 70))
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
