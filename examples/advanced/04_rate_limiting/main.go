package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/youssefsiam38/agentpg"
	"github.com/youssefsiam38/agentpg/driver/pgxv5"
	"github.com/youssefsiam38/agentpg/types"
)

// RateLimiter implements a token bucket rate limiter
type RateLimiter struct {
	mu sync.Mutex

	// Per-tenant rate limits
	tenantBuckets map[string]*TokenBucket

	// Default settings
	defaultRate     float64 // tokens per second
	defaultCapacity int     // max burst size
}

// TokenBucket implements the token bucket algorithm
type TokenBucket struct {
	tokens     float64
	capacity   float64
	rate       float64 // tokens per second
	lastRefill time.Time
}

func NewRateLimiter(defaultRate float64, defaultCapacity int) *RateLimiter {
	return &RateLimiter{
		tenantBuckets:   make(map[string]*TokenBucket),
		defaultRate:     defaultRate,
		defaultCapacity: defaultCapacity,
	}
}

func (rl *RateLimiter) getBucket(tenantID string) *TokenBucket {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	if bucket, exists := rl.tenantBuckets[tenantID]; exists {
		return bucket
	}

	// Create new bucket for tenant
	bucket := &TokenBucket{
		tokens:     float64(rl.defaultCapacity),
		capacity:   float64(rl.defaultCapacity),
		rate:       rl.defaultRate,
		lastRefill: time.Now(),
	}
	rl.tenantBuckets[tenantID] = bucket
	return bucket
}

func (b *TokenBucket) refill() {
	now := time.Now()
	elapsed := now.Sub(b.lastRefill).Seconds()
	b.tokens += elapsed * b.rate
	if b.tokens > b.capacity {
		b.tokens = b.capacity
	}
	b.lastRefill = now
}

func (rl *RateLimiter) Allow(tenantID string) (bool, time.Duration) {
	bucket := rl.getBucket(tenantID)

	rl.mu.Lock()
	defer rl.mu.Unlock()

	bucket.refill()

	if bucket.tokens >= 1 {
		bucket.tokens--
		return true, 0
	}

	// Calculate wait time
	waitTime := time.Duration((1 - bucket.tokens) / bucket.rate * float64(time.Second))
	return false, waitTime
}

func (rl *RateLimiter) GetStats(tenantID string) (available float64, capacity float64) {
	bucket := rl.getBucket(tenantID)

	rl.mu.Lock()
	defer rl.mu.Unlock()

	bucket.refill()
	return bucket.tokens, bucket.capacity
}

// ErrRateLimited is returned when rate limit is exceeded
var ErrRateLimited = errors.New("rate limit exceeded")

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
	// Create rate limiter
	// ==========================================================

	fmt.Println("=== Rate Limiting Example ===")
	fmt.Println()

	// Allow 2 requests per second with burst of 5
	rateLimiter := NewRateLimiter(2.0, 5)

	fmt.Println("Rate Limit Configuration:")
	fmt.Println("  Rate:     2 requests/second")
	fmt.Println("  Capacity: 5 (burst size)")
	fmt.Println()

	// Create driver
	drv := pgxv5.New(pool)

	// Create agent
	agent, err := agentpg.New(
		drv,
		agentpg.Config{
			Client:       &client,
			Model:        "claude-sonnet-4-5-20250929",
			SystemPrompt: "You are a helpful assistant. Be very brief.",
		},
		agentpg.WithMaxTokens(256),
	)
	if err != nil {
		log.Fatalf("Failed to create agent: %v", err)
	}

	// ==========================================================
	// Register rate limiting hook
	// ==========================================================

	currentTenantID := "tenant-1"

	agent.OnBeforeMessage(func(ctx context.Context, messages []*types.Message) error {
		allowed, waitTime := rateLimiter.Allow(currentTenantID)

		available, capacity := rateLimiter.GetStats(currentTenantID)
		fmt.Printf("  [Rate] Tokens: %.1f/%.0f | ", available, capacity)

		if !allowed {
			fmt.Printf("BLOCKED - wait %v\n", waitTime)
			return fmt.Errorf("%w: retry after %v", ErrRateLimited, waitTime)
		}

		fmt.Printf("ALLOWED\n")
		return nil
	})

	// Create session
	sessionID, err := agent.NewSession(ctx, "1", "rate-limit-demo", nil, nil)
	if err != nil {
		log.Fatalf("Failed to create session: %v", err)
	}
	fmt.Printf("Created session: %s\n\n", sessionID[:8]+"...")

	// ==========================================================
	// Demo: Rapid requests to trigger rate limiting
	// ==========================================================

	fmt.Println("=== Sending Rapid Requests ===")
	fmt.Println()

	prompts := []string{
		"Say hi",
		"Say hello",
		"Say hey",
		"Say greetings",
		"Say good day",
		"Say howdy", // Should start hitting limits
		"Say salutations",
		"Say welcome",
	}

	for i, prompt := range prompts {
		fmt.Printf("Request %d: %s\n", i+1, prompt)

		response, err := agent.Run(ctx, prompt)
		if err != nil {
			if errors.Is(err, ErrRateLimited) {
				fmt.Printf("  Result: Rate limited!\n\n")
				// In real app, you might wait and retry
				time.Sleep(500 * time.Millisecond)
				continue
			}
			log.Printf("Error: %v", err)
			continue
		}

		for _, block := range response.Message.Content {
			if block.Type == agentpg.ContentTypeText {
				fmt.Printf("  Response: %s\n", block.Text)
			}
		}
		fmt.Println()
	}

	// ==========================================================
	// Demo: Waiting for rate limit to reset
	// ==========================================================

	fmt.Println("=== Waiting for Token Refill ===")
	fmt.Println()

	available, capacity := rateLimiter.GetStats(currentTenantID)
	fmt.Printf("Before wait: %.1f/%.0f tokens\n", available, capacity)

	fmt.Println("Waiting 2 seconds...")
	time.Sleep(2 * time.Second)

	available, capacity = rateLimiter.GetStats(currentTenantID)
	fmt.Printf("After wait:  %.1f/%.0f tokens\n", available, capacity)
	fmt.Println()

	fmt.Println("=== Final Request After Wait ===")
	response, err := agent.Run(ctx, "Say goodbye")
	if err != nil {
		log.Printf("Error: %v", err)
	} else {
		for _, block := range response.Message.Content {
			if block.Type == agentpg.ContentTypeText {
				fmt.Printf("Response: %s\n", block.Text)
			}
		}
	}

	// ==========================================================
	// Multi-tenant demo
	// ==========================================================

	fmt.Println()
	fmt.Println("=== Multi-Tenant Rate Limits ===")
	fmt.Println()

	tenants := []string{"tenant-a", "tenant-b", "tenant-c"}

	for _, tenant := range tenants {
		currentTenantID = tenant

		// Each tenant has their own bucket
		for i := 0; i < 3; i++ {
			allowed, _ := rateLimiter.Allow(tenant)
			available, _ := rateLimiter.GetStats(tenant)
			status := "allowed"
			if !allowed {
				status = "blocked"
			}
			fmt.Printf("%s request %d: %s (%.1f tokens left)\n", tenant, i+1, status, available)
		}
		fmt.Println()
	}

	fmt.Println("=== Rate Limiting Patterns ===")
	fmt.Println("1. Per-tenant isolation: Each tenant has own limits")
	fmt.Println("2. Token bucket: Allows bursts while enforcing rate")
	fmt.Println("3. Hook-based: Clean separation from business logic")
	fmt.Println("4. Graceful rejection: Return retry-after duration")
	fmt.Println()
	fmt.Println("=== Demo Complete ===")
}
