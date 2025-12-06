package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/youssefsiam38/agentpg"
	"github.com/youssefsiam38/agentpg/driver/pgxv5"
	"github.com/youssefsiam38/agentpg/types"
)

// CostTracker tracks token usage and calculates costs
type CostTracker struct {
	mu sync.Mutex

	// Pricing per 1M tokens (as of 2024, update as needed)
	inputPricePer1M  float64
	outputPricePer1M float64

	// Per-session tracking
	sessionCosts map[string]*SessionCost

	// Budget settings
	sessionBudget float64
	totalBudget   float64
	totalSpent    float64
}

type SessionCost struct {
	InputTokens  int
	OutputTokens int
	TotalCost    float64
	RequestCount int
}

func NewCostTracker(inputPrice, outputPrice float64) *CostTracker {
	return &CostTracker{
		inputPricePer1M:  inputPrice,
		outputPricePer1M: outputPrice,
		sessionCosts:     make(map[string]*SessionCost),
		sessionBudget:    1.00,  // $1 per session by default
		totalBudget:      10.00, // $10 total by default
	}
}

func (ct *CostTracker) SetBudgets(sessionBudget, totalBudget float64) {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	ct.sessionBudget = sessionBudget
	ct.totalBudget = totalBudget
}

func (ct *CostTracker) CalculateCost(inputTokens, outputTokens int) float64 {
	inputCost := float64(inputTokens) / 1_000_000 * ct.inputPricePer1M
	outputCost := float64(outputTokens) / 1_000_000 * ct.outputPricePer1M
	return inputCost + outputCost
}

func (ct *CostTracker) RecordUsage(sessionID string, inputTokens, outputTokens int) (cost float64, warnings []string) {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	cost = ct.CalculateCost(inputTokens, outputTokens)

	// Initialize session if needed
	if _, exists := ct.sessionCosts[sessionID]; !exists {
		ct.sessionCosts[sessionID] = &SessionCost{}
	}

	session := ct.sessionCosts[sessionID]
	session.InputTokens += inputTokens
	session.OutputTokens += outputTokens
	session.TotalCost += cost
	session.RequestCount++

	ct.totalSpent += cost

	// Check budget warnings
	if session.TotalCost > ct.sessionBudget*0.8 {
		if session.TotalCost > ct.sessionBudget {
			warnings = append(warnings, fmt.Sprintf("SESSION BUDGET EXCEEDED: $%.4f > $%.2f", session.TotalCost, ct.sessionBudget))
		} else {
			warnings = append(warnings, fmt.Sprintf("Session at %.0f%% of budget", session.TotalCost/ct.sessionBudget*100))
		}
	}

	if ct.totalSpent > ct.totalBudget*0.8 {
		if ct.totalSpent > ct.totalBudget {
			warnings = append(warnings, fmt.Sprintf("TOTAL BUDGET EXCEEDED: $%.4f > $%.2f", ct.totalSpent, ct.totalBudget))
		} else {
			warnings = append(warnings, fmt.Sprintf("Total spending at %.0f%% of budget", ct.totalSpent/ct.totalBudget*100))
		}
	}

	return cost, warnings
}

func (ct *CostTracker) GetSessionCost(sessionID string) *SessionCost {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	if cost, exists := ct.sessionCosts[sessionID]; exists {
		return cost
	}
	return &SessionCost{}
}

func (ct *CostTracker) GetTotalSpent() float64 {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	return ct.totalSpent
}

func (ct *CostTracker) Report() {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	fmt.Println()
	fmt.Println("=== Cost Report ===")
	fmt.Printf("Pricing: $%.2f/1M input, $%.2f/1M output\n", ct.inputPricePer1M, ct.outputPricePer1M)
	fmt.Println()

	for sessionID, cost := range ct.sessionCosts {
		displayID := sessionID
		if len(displayID) > 8 {
			displayID = displayID[:8] + "..."
		}
		fmt.Printf("Session %s:\n", displayID)
		fmt.Printf("  Requests:      %d\n", cost.RequestCount)
		fmt.Printf("  Input tokens:  %d\n", cost.InputTokens)
		fmt.Printf("  Output tokens: %d\n", cost.OutputTokens)
		fmt.Printf("  Cost:          $%.6f\n", cost.TotalCost)
		fmt.Println()
	}

	fmt.Printf("Total Spent: $%.6f / $%.2f budget (%.1f%%)\n",
		ct.totalSpent, ct.totalBudget, ct.totalSpent/ct.totalBudget*100)
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

	// ==========================================================
	// Create cost tracker
	// Claude 3.5 Sonnet pricing (update as needed)
	// ==========================================================

	fmt.Println("=== Cost Tracking Example ===")
	fmt.Println()

	// Prices as of early 2024 - update as needed
	costTracker := NewCostTracker(
		3.00,  // $3 per 1M input tokens
		15.00, // $15 per 1M output tokens
	)

	// Set budgets
	costTracker.SetBudgets(
		0.50, // $0.50 per session
		5.00, // $5.00 total
	)

	fmt.Println("Budget Configuration:")
	fmt.Printf("  Per-session budget: $0.50\n")
	fmt.Printf("  Total budget:       $5.00\n")
	fmt.Println()

	// Create driver
	drv := pgxv5.New(pool)

	// Create agent
	agent, err := agentpg.New(
		drv,
		agentpg.Config{
			Client:       &client,
			Model:        "claude-sonnet-4-5-20250929",
			SystemPrompt: "You are a helpful assistant. Be concise.",
		},
		agentpg.WithMaxTokens(512),
	)
	if err != nil {
		log.Fatalf("Failed to create agent: %v", err)
	}

	// ==========================================================
	// Register cost tracking hook
	// ==========================================================

	var currentSessionID string

	agent.OnAfterMessage(func(ctx context.Context, response *types.Response) error {
		cost, warnings := costTracker.RecordUsage(
			currentSessionID,
			response.Usage.InputTokens,
			response.Usage.OutputTokens,
		)

		fmt.Printf("  [Cost] $%.6f (in: %d, out: %d tokens)\n",
			cost, response.Usage.InputTokens, response.Usage.OutputTokens)

		for _, warning := range warnings {
			fmt.Printf("  [WARNING] %s\n", warning)
		}

		return nil
	})

	// ==========================================================
	// Demo: Multiple sessions
	// ==========================================================

	sessions := []struct {
		name    string
		prompts []string
	}{
		{
			name: "Session 1",
			prompts: []string{
				"What is Go programming language?",
				"Give me 3 reasons to use it.",
			},
		},
		{
			name: "Session 2",
			prompts: []string{
				"What is PostgreSQL?",
				"How does it compare to MySQL?",
				"Which should I choose for a new project?",
			},
		},
	}

	for _, sess := range sessions {
		fmt.Printf("=== %s ===\n", sess.name)

		sessionID, err := agent.NewSession(ctx, "1", sess.name, nil, nil)
		if err != nil {
			log.Fatalf("Failed to create session: %v", err)
		}
		currentSessionID = sessionID

		for _, prompt := range sess.prompts {
			fmt.Printf("\nUser: %s\n", prompt)

			response, err := agent.Run(ctx, prompt)
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
		}

		// Show session cost
		sessionCost := costTracker.GetSessionCost(sessionID)
		fmt.Printf("\n[Session Total] Requests: %d, Cost: $%.6f\n\n",
			sessionCost.RequestCount, sessionCost.TotalCost)
	}

	// ==========================================================
	// Final report
	// ==========================================================

	costTracker.Report()

	fmt.Println()
	fmt.Println("=== Cost Tracking Best Practices ===")
	fmt.Println("1. Track costs per session and per tenant")
	fmt.Println("2. Set budget alerts at 80% threshold")
	fmt.Println("3. Use shorter max_tokens for cost control")
	fmt.Println("4. Monitor input/output ratio for optimization")
	fmt.Println("5. Consider caching for repeated queries")
	fmt.Println()
	fmt.Println("=== Demo Complete ===")
}
