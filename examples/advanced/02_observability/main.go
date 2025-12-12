// Package main demonstrates the Client API with observability and per-client registration.
//
// This example shows:
// - Per-client agent registration (new API)
// - Structured logging with slog
// - Metrics collection
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/youssefsiam38/agentpg"
	"github.com/youssefsiam38/agentpg/driver/pgxv5"
	"github.com/youssefsiam38/agentpg/tool"
)

// Metrics tracks agent usage metrics
type Metrics struct {
	TotalRequests     atomic.Int64
	TotalInputTokens  atomic.Int64
	TotalOutputTokens atomic.Int64
	TotalToolCalls    atomic.Int64
	TotalCompactions  atomic.Int64
}

func (m *Metrics) Report() {
	fmt.Println()
	fmt.Println("=== Metrics Summary ===")
	fmt.Printf("Total Requests:     %d\n", m.TotalRequests.Load())
	fmt.Printf("Total Input Tokens: %d\n", m.TotalInputTokens.Load())
	fmt.Printf("Total Output Tokens: %d\n", m.TotalOutputTokens.Load())
	fmt.Printf("Total Tool Calls:   %d\n", m.TotalToolCalls.Load())
	fmt.Printf("Total Compactions:  %d\n", m.TotalCompactions.Load())
}

// SimpleTool for demonstration
type SimpleTool struct{}

func (s *SimpleTool) Name() string        { return "get_time" }
func (s *SimpleTool) Description() string { return "Get the current time" }
func (s *SimpleTool) InputSchema() tool.ToolSchema {
	return tool.ToolSchema{
		Type: "object",
		Properties: map[string]tool.PropertyDef{
			"timezone": {Type: "string", Description: "Timezone (e.g., UTC, America/New_York)"},
		},
		Required: []string{},
	}
}
func (s *SimpleTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	return time.Now().Format(time.RFC3339), nil
}


func main() {
	// Create a context that cancels on SIGINT/SIGTERM
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Configure structured logger
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

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

	// Create metrics collector
	metrics := &Metrics{}

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
	maxTokens := 1024
	if err := client.RegisterAgent(&agentpg.AgentDefinition{
		Name:         "observability-demo",
		Description:  "Assistant with observability",
		Model:        "claude-sonnet-4-5-20250929",
		SystemPrompt: "You are a helpful assistant. Use the get_time tool when asked about time.",
		MaxTokens:    &maxTokens,
		Tools:        []string{"get_time"},
		Config: map[string]any{
			"auto_compaction": true,
		},
	}); err != nil {
		log.Fatalf("Failed to register agent: %v", err)
	}

	// Register tool on the client
	if err := client.RegisterTool(&SimpleTool{}); err != nil {
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

	// ==========================================================
	// NOTE: Observability hooks (OnBeforeMessage, OnAfterMessage, etc.)
	// are part of the old agent-specific API and are not available in the
	// new per-client API. For observability in the new API, consider:
	// - Using the client's built-in Logger interface
	// - Querying agentpg_runs, agentpg_iterations, agentpg_tool_executions tables
	// - Implementing custom monitoring via database triggers
	// - Listening to LISTEN/NOTIFY events (agentpg_run_state, agentpg_tool_pending, etc.)
	// ==========================================================

	fmt.Println("=== Observability Example ===")
	fmt.Println()

	// Create session using the client (new API)
	sessionID, err := client.NewSession(ctx, "1", "observability-demo", nil, map[string]any{
		"description": "Observability demonstration",
	})
	if err != nil {
		log.Fatalf("Failed to create session: %v", err)
	}

	logger.Info("session.created",
		slog.String("session_id", sessionID),
	)

	// ==========================================================
	// Run some requests to generate logs
	// ==========================================================

	fmt.Println()
	fmt.Println("Running requests (check JSON logs above)...")
	fmt.Println()

	prompts := []string{
		"Hello! What time is it?",
		"Thanks! What's 2 + 2?",
		"One more question - what's the capital of France?",
	}

	for i, prompt := range prompts {
		fmt.Printf("--- Request %d ---\n", i+1)
		fmt.Printf("User: %s\n", prompt)

		// Use client.RunSync instead of agent.RunSync (new API)
		response, err := client.RunSync(ctx, sessionID, "observability-demo", prompt)
		if err != nil {
			log.Printf("Error: %v", err)
			continue
		}

		// Track metrics manually (since hooks are not available)
		metrics.TotalRequests.Add(1)
		metrics.TotalInputTokens.Add(int64(response.Usage.InputTokens))
		metrics.TotalOutputTokens.Add(int64(response.Usage.OutputTokens))

		// Count tool iterations as approximate tool calls
		metrics.TotalToolCalls.Add(int64(response.ToolIterations))

		logger.Info("request.completed",
			slog.Int("input_tokens", response.Usage.InputTokens),
			slog.Int("output_tokens", response.Usage.OutputTokens),
			slog.String("stop_reason", response.StopReason),
			slog.Int("iterations", response.IterationCount),
			slog.Int("tool_iterations", response.ToolIterations),
		)

		// Display response text
		text := response.Text
		if len(text) > 150 {
			text = text[:150] + "..."
		}
		fmt.Printf("Agent: %s\n", text)
		fmt.Println()
	}

	// ==========================================================
	// Report metrics
	// ==========================================================

	metrics.Report()

	fmt.Println()
	fmt.Println("=== Observability Notes ===")
	fmt.Println("The new per-client API uses structured logging via the Logger interface.")
	fmt.Println("For advanced observability, consider:")
	fmt.Println("  - Implementing custom Logger with detailed tracking")
	fmt.Println("  - Querying database tables (agentpg_runs, agentpg_iterations, etc.)")
	fmt.Println("  - Listening to LISTEN/NOTIFY events for real-time monitoring")
	fmt.Println("  - Setting up database triggers for custom metrics")
	fmt.Println()
	fmt.Println("=== Demo Complete ===")
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
