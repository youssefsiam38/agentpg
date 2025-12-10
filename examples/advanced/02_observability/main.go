// Package main demonstrates the Client API with observability hooks.
//
// This example shows:
// - All 5 observability hooks
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
	"github.com/youssefsiam38/agentpg/compaction"
	"github.com/youssefsiam38/agentpg/driver/pgxv5"
	"github.com/youssefsiam38/agentpg/tool"
	"github.com/youssefsiam38/agentpg/types"
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

// Register agent at package initialization.
func init() {
	maxTokens := 1024
	agentpg.MustRegister(&agentpg.AgentDefinition{
		Name:         "observability-demo",
		Description:  "Assistant with observability",
		Model:        "claude-sonnet-4-5-20250929",
		SystemPrompt: "You are a helpful assistant. Use the get_time tool when asked about time.",
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

	// ==========================================================
	// Get agent and register hooks
	// ==========================================================

	fmt.Println("=== Observability Example ===")
	fmt.Println()

	agent := client.Agent("observability-demo")
	if agent == nil {
		log.Fatal("Agent 'observability-demo' not found")
	}

	// Register tool
	if err := agent.RegisterTool(&SimpleTool{}); err != nil {
		log.Fatalf("Failed to register tool: %v", err)
	}

	// ==========================================================
	// Hook 1: OnBeforeMessage
	// ==========================================================

	agent.OnBeforeMessage(func(ctx context.Context, messages []*types.Message) error {
		metrics.TotalRequests.Add(1)
		requestID := fmt.Sprintf("req-%d", metrics.TotalRequests.Load())

		// Get last user message for logging
		var lastPrompt string
		for i := len(messages) - 1; i >= 0; i-- {
			if messages[i].Role == types.RoleUser {
				for _, block := range messages[i].Content {
					if block.Type == types.ContentTypeText {
						lastPrompt = block.Text
						break
					}
				}
				break
			}
		}

		logger.Info("message.started",
			slog.String("request_id", requestID),
			slog.Int("message_count", len(messages)),
			slog.Int("prompt_length", len(lastPrompt)),
			slog.Time("timestamp", time.Now()),
		)

		return nil
	})

	// ==========================================================
	// Hook 2: OnAfterMessage
	// ==========================================================

	agent.OnAfterMessage(func(ctx context.Context, response *agentpg.Response) error {
		metrics.TotalInputTokens.Add(int64(response.Usage.InputTokens))
		metrics.TotalOutputTokens.Add(int64(response.Usage.OutputTokens))

		logger.Info("message.completed",
			slog.Int("input_tokens", response.Usage.InputTokens),
			slog.Int("output_tokens", response.Usage.OutputTokens),
			slog.String("stop_reason", response.StopReason),
			slog.Time("timestamp", time.Now()),
		)

		return nil
	})

	// ==========================================================
	// Hook 3: OnToolCall
	// ==========================================================

	agent.OnToolCall(func(ctx context.Context, toolName string, input json.RawMessage, output string, toolErr error) error {
		metrics.TotalToolCalls.Add(1)

		errStr := ""
		if toolErr != nil {
			errStr = toolErr.Error()
		}

		logger.Info("tool.called",
			slog.String("tool_name", toolName),
			slog.String("input", string(input)),
			slog.String("output", truncate(output, 100)),
			slog.String("error", errStr),
			slog.Time("timestamp", time.Now()),
		)

		return nil
	})

	// ==========================================================
	// Hook 4: OnBeforeCompaction
	// ==========================================================

	agent.OnBeforeCompaction(func(ctx context.Context, sessionID string) error {
		logger.Warn("compaction.starting",
			slog.String("session_id", sessionID),
			slog.Time("timestamp", time.Now()),
		)

		return nil
	})

	// ==========================================================
	// Hook 5: OnAfterCompaction
	// ==========================================================

	agent.OnAfterCompaction(func(ctx context.Context, result *compaction.CompactionResult) error {
		metrics.TotalCompactions.Add(1)

		logger.Info("compaction.completed",
			slog.String("strategy", result.Strategy),
			slog.Int("original_tokens", result.OriginalTokens),
			slog.Int("compacted_tokens", result.CompactedTokens),
			slog.Int("messages_removed", result.MessagesRemoved),
			slog.Time("timestamp", time.Now()),
		)

		return nil
	})

	// Create session
	sessionID, err := agent.NewSession(ctx, "1", "observability-demo", nil, map[string]any{
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

		response, err := agent.RunSync(ctx, sessionID, prompt)
		if err != nil {
			log.Printf("Error: %v", err)
			continue
		}

		for _, block := range response.Message.Content {
			if block.Type == agentpg.ContentTypeText {
				text := block.Text
				if len(text) > 150 {
					text = text[:150] + "..."
				}
				fmt.Printf("Agent: %s\n", text)
			}
		}
		fmt.Println()
	}

	// ==========================================================
	// Report metrics
	// ==========================================================

	metrics.Report()

	fmt.Println()
	fmt.Println("=== Hook Summary ===")
	fmt.Println("OnBeforeMessage:    Log request start, validate input")
	fmt.Println("OnAfterMessage:     Log completion, track tokens")
	fmt.Println("OnToolCall:         Log tool usage, audit actions")
	fmt.Println("OnBeforeCompaction: Log compaction trigger")
	fmt.Println("OnAfterCompaction:  Log compaction results")
	fmt.Println()
	fmt.Println("=== Demo Complete ===")
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
