// Package main demonstrates the different tool error types.
//
// This example shows:
// - ToolCancel: Immediate cancellation, no retry (e.g., auth failure)
// - ToolDiscard: Permanent failure, invalid input (e.g., bad parameters)
// - ToolSnooze: Retry after duration without consuming attempt (e.g., rate limit)
// - Regular errors: Consumed retry attempts with instant retry
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/youssefsiam38/agentpg"
	"github.com/youssefsiam38/agentpg/driver/pgxv5"
	"github.com/youssefsiam38/agentpg/tool"
)

// APITool demonstrates different error types based on input.
type APITool struct {
	callCount int
}

func (t *APITool) Name() string {
	return "api_call"
}

func (t *APITool) Description() string {
	return "Makes an API call. Use action='auth_fail' to simulate auth failure, 'invalid' for bad input, 'rate_limit' for rate limiting, 'flaky' for random failures, or 'success' for normal operation."
}

func (t *APITool) InputSchema() tool.ToolSchema {
	return tool.ToolSchema{
		Type: "object",
		Properties: map[string]tool.PropertyDef{
			"action": {
				Type:        "string",
				Description: "The action to perform",
				Enum:        []string{"auth_fail", "invalid", "rate_limit", "flaky", "success"},
			},
		},
		Required: []string{"action"},
	}
}

func (t *APITool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	t.callCount++
	var params struct {
		Action string `json:"action"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}

	log.Printf("[APITool] Call #%d - action: %s", t.callCount, params.Action)

	switch params.Action {
	case "auth_fail":
		// ToolCancel: Authentication failed - never retry
		// Use when the error is unrecoverable (bad API key, permission denied)
		log.Printf("[APITool] Returning ToolCancel (no retry)")
		return "", tool.ToolCancel(errors.New("authentication failed: invalid API key"))

	case "invalid":
		// ToolDiscard: Invalid input - never retry
		// Use when the input is fundamentally wrong (malformed data, impossible request)
		log.Printf("[APITool] Returning ToolDiscard (no retry)")
		return "", tool.ToolDiscard(errors.New("invalid action requested"))

	case "rate_limit":
		// ToolSnooze: Rate limited - retry after duration WITHOUT consuming attempt
		// Use for temporary unavailability (rate limits, service maintenance)
		log.Printf("[APITool] Returning ToolSnooze (retry after 5s, does NOT consume attempt)")
		return "", tool.ToolSnooze(5*time.Second, errors.New("rate limit exceeded"))

	case "flaky":
		// Regular error: Will be retried (consumes an attempt)
		// Default behavior: instant retry up to MaxAttempts
		if t.callCount < 3 {
			log.Printf("[APITool] Returning regular error (will retry instantly)")
			return "", errors.New("temporary network error")
		}
		log.Printf("[APITool] Finally succeeded after retries!")
		return "Flaky service finally responded!", nil

	case "success":
		log.Printf("[APITool] Success!")
		return "API call successful!", nil

	default:
		return "", fmt.Errorf("unknown action: %s", params.Action)
	}
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
		log.Fatal("DATABASE_URL environment variable is required")
	}

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer pool.Close()

	drv := pgxv5.New(pool)

	client, err := agentpg.NewClient(drv, &agentpg.ClientConfig{
		APIKey: apiKey,
		// Use default retry config (2 attempts, instant retry)
	})
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}

	apiTool := &APITool{}
	if err := client.RegisterTool(apiTool); err != nil {
		log.Fatalf("Failed to register tool: %v", err)
	}

	if err := client.Start(ctx); err != nil {
		log.Fatalf("Failed to start client: %v", err)
	}
	defer client.Stop(context.Background())

	// Create agent in the database
	agent, err := client.CreateAgent(ctx, &agentpg.AgentDefinition{
		Name:         "assistant",
		Model:        "claude-sonnet-4-5-20250929",
		SystemPrompt: "You are a helpful assistant. Use the api_call tool to perform API operations.",
		Tools:        []string{"api_call"},
	})
	if err != nil {
		log.Fatalf("Failed to create agent: %v", err)
	}

	log.Println("=== Tool Error Types Demo ===")
	log.Println("")
	log.Println("Error Types:")
	log.Println("  ToolCancel  - Immediate failure, no retry (e.g., auth failure)")
	log.Println("  ToolDiscard - Permanent failure, invalid input")
	log.Println("  ToolSnooze  - Retry after delay, does NOT consume attempt")
	log.Println("  Regular err - Retried instantly up to MaxAttempts")
	log.Println("")

	// Demo each error type
	demos := []struct {
		name   string
		prompt string
	}{
		{
			name:   "SUCCESS",
			prompt: "Call the API with action 'success'",
		},
		{
			name:   "AUTH FAILURE (ToolCancel)",
			prompt: "Call the API with action 'auth_fail'",
		},
		{
			name:   "INVALID INPUT (ToolDiscard)",
			prompt: "Call the API with action 'invalid'",
		},
		// Note: rate_limit and flaky demos would take longer due to snooze/retry
		// Uncomment to test:
		// {
		// 	name:   "RATE LIMIT (ToolSnooze)",
		// 	prompt: "Call the API with action 'rate_limit'",
		// },
	}

	for i, demo := range demos {
		sessionID, err := client.NewSession(ctx, nil, map[string]any{"demo": fmt.Sprintf("error-types-%d", i)})
		if err != nil {
			log.Fatalf("Failed to create session: %v", err)
		}

		log.Printf("\n--- Demo: %s ---\n", demo.name)
		apiTool.callCount = 0

		response, err := client.RunFastSync(ctx, sessionID, agent.ID, demo.prompt, nil)
		if err != nil {
			log.Printf("Error: %v", err)
		} else {
			fmt.Println("Agent response:")
			for _, block := range response.Message.Content {
				if block.Type == agentpg.ContentTypeText {
					fmt.Println(block.Text)
				}
			}
		}
		fmt.Printf("Tool calls made: %d\n", apiTool.callCount)
	}

	log.Println("\n=== Demo Complete ===")
	log.Println("")
	log.Println("Key takeaways:")
	log.Println("- ToolCancel/ToolDiscard: Fail immediately, no retry")
	log.Println("- ToolSnooze: Retry after delay without consuming attempts")
	log.Println("- Regular errors: Instant retry (default) up to MaxAttempts")
}
