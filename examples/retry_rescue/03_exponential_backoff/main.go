// Package main demonstrates opt-in exponential backoff for retries.
//
// This example shows:
// - Configuring exponential backoff with Jitter > 0
// - River's attempt^4 formula (1s, 16s, 81s, 256s, 625s...)
// - When to use backoff (external APIs with rate limits)
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
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

// RateLimitedAPITool simulates an external API with strict rate limits.
// With exponential backoff, failed requests wait progressively longer,
// giving the external service time to recover.
type RateLimitedAPITool struct {
	callCount     int64
	lastCallTime  time.Time
	failuresUntil int
}

func (t *RateLimitedAPITool) Name() string {
	return "external_api"
}

func (t *RateLimitedAPITool) Description() string {
	return "Calls an external API with rate limiting"
}

func (t *RateLimitedAPITool) InputSchema() tool.ToolSchema {
	return tool.ToolSchema{
		Type: "object",
		Properties: map[string]tool.PropertyDef{
			"endpoint": {
				Type:        "string",
				Description: "The API endpoint to call",
			},
		},
		Required: []string{"endpoint"},
	}
}

func (t *RateLimitedAPITool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	count := atomic.AddInt64(&t.callCount, 1)
	now := time.Now()

	var params struct {
		Endpoint string `json:"endpoint"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}

	// Calculate time since last call
	timeSinceLastCall := time.Duration(0)
	if !t.lastCallTime.IsZero() {
		timeSinceLastCall = now.Sub(t.lastCallTime)
	}
	t.lastCallTime = now

	log.Printf("[ExternalAPI] Call #%d to %s (time since last: %v)",
		count, params.Endpoint, timeSinceLastCall.Round(time.Millisecond))

	// Fail the first few calls to demonstrate backoff
	if int(count) <= t.failuresUntil {
		log.Printf("[ExternalAPI] Simulating rate limit error (will retry with backoff)")
		return "", fmt.Errorf("rate limit exceeded, please retry later")
	}

	log.Printf("[ExternalAPI] Success!")
	return fmt.Sprintf("API response from %s: OK", params.Endpoint), nil
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

	// Create client with EXPONENTIAL BACKOFF retry config
	// Set Jitter > 0 to enable River's attempt^4 backoff formula
	client, err := agentpg.NewClient(drv, &agentpg.ClientConfig{
		APIKey: apiKey,

		// Opt-in to exponential backoff by setting Jitter > 0
		ToolRetryConfig: &agentpg.ToolRetryConfig{
			MaxAttempts: 7,   // More attempts for unreliable services
			Jitter:      0.1, // 10% jitter enables exponential backoff
		},
	})
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}

	// This tool will fail the first 5 calls
	apiTool := &RateLimitedAPITool{failuresUntil: 5}
	if err := client.RegisterTool(apiTool); err != nil {
		log.Fatalf("Failed to register tool: %v", err)
	}

	if err := client.RegisterAgent(&agentpg.AgentDefinition{
		Name:         "assistant",
		Model:        "claude-sonnet-4-5-20250929",
		SystemPrompt: "You are a helpful assistant. Use the external_api tool to make API calls.",
		Tools:        []string{"external_api"},
	}); err != nil {
		log.Fatalf("Failed to register agent: %v", err)
	}

	if err := client.Start(ctx); err != nil {
		log.Fatalf("Failed to start client: %v", err)
	}
	defer client.Stop(context.Background())

	log.Println("=== Exponential Backoff Demo ===")
	log.Println("")
	log.Println("Configuration:")
	log.Println("  MaxAttempts: 5")
	log.Println("  Jitter: 0.1 (enables backoff)")
	log.Println("")
	log.Println("Backoff delays (attempt^4 formula):")
	log.Println("  Attempt 1: ~1 second")
	log.Println("  Attempt 2: ~16 seconds")
	log.Println("  Attempt 3: ~81 seconds")
	log.Println("  Attempt 4: ~256 seconds")
	log.Println("  Attempt 5: ~625 seconds")
	log.Println("")
	log.Println("The tool will fail first 2 calls to show backoff in action...")
	log.Println("")

	sessionID, err := client.NewSession(ctx, nil, map[string]any{"demo": "backoff"})
	if err != nil {
		log.Fatalf("Failed to create session: %v", err)
	}

	log.Println("--- Starting request ---")
	start := time.Now()

	response, err := client.RunFastSync(ctx, sessionID, "assistant", "Call the external API endpoint '/users'")
	elapsed := time.Since(start)

	if err != nil {
		log.Printf("Error: %v", err)
	} else {
		fmt.Println("\nAgent response:")
		for _, block := range response.Message.Content {
			if block.Type == agentpg.ContentTypeText {
				fmt.Println(block.Text)
			}
		}
	}

	fmt.Printf("\nTotal calls: %d\n", atomic.LoadInt64(&apiTool.callCount))
	fmt.Printf("Total time: %v\n", elapsed.Round(time.Millisecond))

	log.Println("\n=== Demo Complete ===")
	log.Println("")
	log.Println("When to use exponential backoff:")
	log.Println("- External APIs with rate limits")
	log.Println("- Services that need recovery time")
	log.Println("- High-volume batch processing")
	log.Println("")
	log.Println("When to use instant retry (default):")
	log.Println("- Interactive applications")
	log.Println("- Tools with transient failures")
	log.Println("- When fast feedback is important")
}
