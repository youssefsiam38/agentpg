// Package main demonstrates the Client API with error recovery.
//
// This example shows:
// - Error classification (transient, rate limit, permanent)
// - Retry with exponential backoff and jitter
// - Graceful degradation patterns
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/youssefsiam38/agentpg"
	"github.com/youssefsiam38/agentpg/driver/pgxv5"
	"github.com/youssefsiam38/agentpg/tool"
)

// ErrorType classifies errors for handling decisions
type ErrorType int

const (
	ErrorTypeUnknown   ErrorType = iota
	ErrorTypeTransient           // Retry after delay
	ErrorTypeRateLimit           // Retry after longer delay
	ErrorTypePermanent           // Don't retry
)

// RetryConfig configures retry behavior
type RetryConfig struct {
	MaxRetries    int
	InitialDelay  time.Duration
	MaxDelay      time.Duration
	BackoffFactor float64
}

func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries:    3,
		InitialDelay:  100 * time.Millisecond,
		MaxDelay:      10 * time.Second,
		BackoffFactor: 2.0,
	}
}

// ClassifyError determines the type of error
func ClassifyError(err error) ErrorType {
	if err == nil {
		return ErrorTypeUnknown
	}

	errStr := err.Error()

	// Check for rate limiting
	if contains(errStr, "rate limit", "429", "too many requests") {
		return ErrorTypeRateLimit
	}

	// Check for transient errors
	if contains(errStr, "timeout", "connection refused", "temporary", "503", "502") {
		return ErrorTypeTransient
	}

	// Check for permanent errors
	if contains(errStr, "invalid", "not found", "401", "403", "400") {
		return ErrorTypePermanent
	}

	return ErrorTypeTransient // Default to transient for unknown errors
}

func contains(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if len(sub) > 0 && len(s) >= len(sub) {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
		}
	}
	return false
}

// WithRetry wraps an operation with retry logic
func WithRetry[T any](ctx context.Context, config RetryConfig, operation func() (T, error)) (T, error) {
	var result T
	var lastErr error

	delay := config.InitialDelay

	for attempt := 0; attempt <= config.MaxRetries; attempt++ {
		if attempt > 0 {
			fmt.Printf("  [Retry] Attempt %d/%d after %v delay\n", attempt, config.MaxRetries, delay)

			select {
			case <-ctx.Done():
				return result, ctx.Err()
			case <-time.After(delay):
			}

			// Exponential backoff with jitter
			delay = time.Duration(float64(delay) * config.BackoffFactor)
			if delay > config.MaxDelay {
				delay = config.MaxDelay
			}
			// Add jitter (±10%)
			jitter := time.Duration(rand.Float64()*0.2*float64(delay) - 0.1*float64(delay))
			delay += jitter
		}

		result, lastErr = operation()
		if lastErr == nil {
			return result, nil
		}

		errorType := ClassifyError(lastErr)
		fmt.Printf("  [Error] %v (type: %v)\n", lastErr, errorTypeName(errorType))

		if errorType == ErrorTypePermanent {
			return result, lastErr // Don't retry permanent errors
		}

		if errorType == ErrorTypeRateLimit {
			delay = config.MaxDelay // Use max delay for rate limits
		}
	}

	return result, fmt.Errorf("max retries exceeded: %w", lastErr)
}

func errorTypeName(t ErrorType) string {
	switch t {
	case ErrorTypeTransient:
		return "transient"
	case ErrorTypeRateLimit:
		return "rate_limit"
	case ErrorTypePermanent:
		return "permanent"
	default:
		return "unknown"
	}
}

// UnreliableTool simulates a tool that sometimes fails
type UnreliableTool struct {
	failureRate float64
	failCount   int
}

func NewUnreliableTool(failureRate float64) *UnreliableTool {
	return &UnreliableTool{failureRate: failureRate}
}

func (u *UnreliableTool) Name() string        { return "unreliable_api" }
func (u *UnreliableTool) Description() string { return "Call an unreliable external API" }

func (u *UnreliableTool) InputSchema() tool.ToolSchema {
	return tool.ToolSchema{
		Type: "object",
		Properties: map[string]tool.PropertyDef{
			"query": {Type: "string", Description: "Query to send to API"},
		},
		Required: []string{"query"},
	}
}

func (u *UnreliableTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var params struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", err
	}

	// Simulate random failures
	if rand.Float64() < u.failureRate {
		u.failCount++
		errors := []error{
			fmt.Errorf("connection timeout"),
			fmt.Errorf("503 service temporarily unavailable"),
			fmt.Errorf("rate limit exceeded (429)"),
		}
		return "", errors[rand.Intn(len(errors))]
	}

	return fmt.Sprintf("API response for '%s': Success! Data retrieved.", params.Query), nil
}

// Register agent at package initialization.
func init() {
	maxTokens := 1024
	agentpg.MustRegister(&agentpg.AgentDefinition{
		Name:         "error-recovery-demo",
		Description:  "Assistant with error recovery",
		Model:        "claude-sonnet-4-5-20250929",
		SystemPrompt: "You are a helpful assistant. Use the unreliable_api tool when asked to fetch data. If the tool fails, try again or explain the issue.",
		MaxTokens:    &maxTokens,
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
	// Demo error classification
	// ==========================================================

	fmt.Println("=== Error Recovery Example ===")
	fmt.Println()

	fmt.Println("Error Classification:")
	testErrors := []error{
		errors.New("connection timeout"),
		errors.New("rate limit exceeded"),
		errors.New("invalid request"),
		errors.New("503 service unavailable"),
		errors.New("some unknown error"),
	}

	for _, err := range testErrors {
		errorType := ClassifyError(err)
		fmt.Printf("  %q → %s\n", err.Error(), errorTypeName(errorType))
	}
	fmt.Println()

	// ==========================================================
	// Demo retry logic
	// ==========================================================

	fmt.Println("=== Retry Logic Demo ===")
	fmt.Println()

	config := DefaultRetryConfig()
	fmt.Printf("Config: MaxRetries=%d, InitialDelay=%v, BackoffFactor=%.1f\n\n",
		config.MaxRetries, config.InitialDelay, config.BackoffFactor)

	// Simulate an operation that fails a few times then succeeds
	attemptCount := 0
	result, err := WithRetry(ctx, config, func() (string, error) {
		attemptCount++
		if attemptCount < 3 {
			return "", errors.New("connection timeout")
		}
		return "Success after retries!", nil
	})

	if err != nil {
		fmt.Printf("Final result: Error - %v\n", err)
	} else {
		fmt.Printf("Final result: %s (after %d attempts)\n", result, attemptCount)
	}
	fmt.Println()

	// ==========================================================
	// Create agent with error-prone tool
	// ==========================================================

	fmt.Println("=== Agent with Unreliable Tool ===")
	fmt.Println()

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

	// Get the agent
	agent := client.Agent("error-recovery-demo")
	if agent == nil {
		log.Fatal("Agent 'error-recovery-demo' not found")
	}

	// Register unreliable tool (50% failure rate for demo)
	unreliableTool := NewUnreliableTool(0.5)
	if err := agent.RegisterTool(unreliableTool); err != nil {
		log.Fatalf("Failed to register tool: %v", err)
	}

	// Add error tracking hook
	var errorCount int
	agent.OnToolCall(func(ctx context.Context, toolName string, input json.RawMessage, output string, toolErr error) error {
		status := "success"
		if toolErr != nil {
			status = "error: " + toolErr.Error()
		}
		fmt.Printf("  [Hook] Tool called: %s (%s)\n", toolName, status)
		return nil
	})

	// Create session
	sessionID, err := agent.NewSession(ctx, "1", "error-recovery-demo", nil, nil)
	if err != nil {
		log.Fatalf("Failed to create session: %v", err)
	}
	fmt.Printf("Session: %s\n\n", sessionID[:8]+"...")

	// Run agent multiple times to see error handling
	prompts := []string{
		"Please fetch data about 'user statistics' from the API",
		"Now fetch data about 'system metrics'",
	}

	for i, prompt := range prompts {
		fmt.Printf("=== Request %d ===\n", i+1)
		fmt.Printf("User: %s\n\n", prompt)

		// Run with manual retry wrapper
		var response *agentpg.Response
		retryConfig := RetryConfig{
			MaxRetries:    2,
			InitialDelay:  50 * time.Millisecond,
			MaxDelay:      1 * time.Second,
			BackoffFactor: 2.0,
		}

		response, err = WithRetry(ctx, retryConfig, func() (*agentpg.Response, error) {
			return agent.RunSync(ctx, sessionID, prompt)
		})

		if err != nil {
			errorCount++
			fmt.Printf("Agent Error (after retries): %v\n", err)
			fmt.Println("Fallback: I apologize, but I'm having trouble accessing the API. Please try again later.")
		} else {
			for _, block := range response.Message.Content {
				if block.Type == agentpg.ContentTypeText {
					text := block.Text
					if len(text) > 300 {
						text = text[:300] + "..."
					}
					fmt.Printf("Agent: %s\n", text)
				}
			}
		}
		fmt.Println()
	}

	// ==========================================================
	// Summary
	// ==========================================================

	fmt.Println("=== Error Recovery Patterns ===")
	fmt.Println("1. Error Classification:")
	fmt.Println("   - Transient: Retry with backoff")
	fmt.Println("   - Rate Limit: Retry with longer delay")
	fmt.Println("   - Permanent: Fail fast, don't retry")
	fmt.Println()
	fmt.Println("2. Retry Strategy:")
	fmt.Println("   - Exponential backoff")
	fmt.Println("   - Jitter to prevent thundering herd")
	fmt.Println("   - Max delay cap")
	fmt.Println()
	fmt.Println("3. Graceful Degradation:")
	fmt.Println("   - Fallback responses")
	fmt.Println("   - Cached results")
	fmt.Println("   - Alternative data sources")
	fmt.Println()
	fmt.Printf("Tool failures during demo: %d\n", unreliableTool.failCount)
	fmt.Printf("Total errors handled: %d\n", errorCount)
	fmt.Println()
	fmt.Println("=== Demo Complete ===")
}
