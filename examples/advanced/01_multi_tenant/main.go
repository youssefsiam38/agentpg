package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/youssefsiam38/agentpg"
	"github.com/youssefsiam38/agentpg/driver/pgxv5"
)

// TenantManager manages agents and sessions per tenant
type TenantManager struct {
	mu       sync.RWMutex
	pool     *pgxpool.Pool
	client   *anthropic.Client
	drv      *pgxv5.Driver
	sessions map[string]string // tenantID:userID -> sessionID
}

func NewTenantManager(pool *pgxpool.Pool, client *anthropic.Client) *TenantManager {
	return &TenantManager{
		pool:     pool,
		client:   client,
		drv:      pgxv5.New(pool),
		sessions: make(map[string]string),
	}
}

func (tm *TenantManager) GetOrCreateSession(ctx context.Context, tenantID, userID string) (string, *agentpg.Agent[pgx.Tx], error) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	key := fmt.Sprintf("%s:%s", tenantID, userID)

	// Create a new agent for this request
	agent, err := agentpg.New(
		tm.drv,
		agentpg.Config{
			Client:       tm.client,
			Model:        "claude-sonnet-4-5-20250929",
			SystemPrompt: fmt.Sprintf("You are a helpful assistant for tenant %s. Be concise and helpful.", tenantID),
		},
		agentpg.WithMaxTokens(1024),
		agentpg.WithAutoCompaction(true),
	)
	if err != nil {
		return "", nil, fmt.Errorf("failed to create agent: %w", err)
	}

	// Check if session exists
	if sessionID, exists := tm.sessions[key]; exists {
		// Resume existing session
		if err := agent.LoadSession(ctx, sessionID); err != nil {
			// Session may have expired, create new one
			delete(tm.sessions, key)
		} else {
			return sessionID, agent, nil
		}
	}

	// Create new session
	sessionID, err := agent.NewSession(ctx, tenantID, fmt.Sprintf("user-%s", userID), nil, map[string]any{
		"tenant_id": tenantID,
		"user_id":   userID,
	})
	if err != nil {
		return "", nil, fmt.Errorf("failed to create session: %w", err)
	}

	tm.sessions[key] = sessionID
	return sessionID, agent, nil
}

// ChatRequest represents the API request body
type ChatRequest struct {
	Message string `json:"message"`
}

// ChatResponse represents the API response body
type ChatResponse struct {
	Response  string `json:"response"`
	SessionID string `json:"session_id"`
	Tokens    struct {
		Input  int `json:"input"`
		Output int `json:"output"`
	} `json:"tokens"`
}

// ErrorResponse represents an error response
type ErrorResponse struct {
	Error string `json:"error"`
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

	// Create tenant manager
	tm := NewTenantManager(pool, &client)

	// ==========================================================
	// HTTP Handler
	// ==========================================================

	http.HandleFunc("/chat", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Extract tenant and user from headers
		tenantID := r.Header.Get("X-Tenant-ID")
		userID := r.Header.Get("X-User-ID")

		if tenantID == "" || userID == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(ErrorResponse{
				Error: "X-Tenant-ID and X-User-ID headers are required",
			})
			return
		}

		// Parse request body
		var req ChatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(ErrorResponse{Error: "Invalid request body"})
			return
		}

		// Get or create session
		sessionID, agent, err := tm.GetOrCreateSession(r.Context(), tenantID, userID)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(ErrorResponse{Error: err.Error()})
			return
		}

		// Run agent
		response, err := agent.Run(r.Context(), req.Message)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(ErrorResponse{Error: err.Error()})
			return
		}

		// Build response
		var responseText string
		for _, block := range response.Message.Content {
			if block.Type == agentpg.ContentTypeText {
				responseText = block.Text
				break
			}
		}

		resp := ChatResponse{
			Response:  responseText,
			SessionID: sessionID,
		}
		resp.Tokens.Input = response.Usage.InputTokens
		resp.Tokens.Output = response.Usage.OutputTokens

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	// ==========================================================
	// Demo mode (simulated requests)
	// ==========================================================

	fmt.Println("=== Multi-Tenant API Demo ===")
	fmt.Println()
	fmt.Println("In production, this would start an HTTP server on :8080")
	fmt.Println("For this demo, we'll simulate some tenant requests.")
	fmt.Println()

	// Simulate requests from different tenants
	tenants := []struct {
		tenantID string
		userID   string
		messages []string
	}{
		{
			tenantID: "acme-corp",
			userID:   "user-1",
			messages: []string{
				"Hello, what can you help me with?",
				"What was my first message?",
			},
		},
		{
			tenantID: "globex",
			userID:   "user-42",
			messages: []string{
				"Hi there! Tell me a quick fact.",
			},
		},
		{
			tenantID: "acme-corp",
			userID:   "user-2",
			messages: []string{
				"I'm a different user from acme-corp.",
			},
		},
	}

	for _, tenant := range tenants {
		fmt.Printf("=== Tenant: %s, User: %s ===\n", tenant.tenantID, tenant.userID)

		for _, msg := range tenant.messages {
			fmt.Printf("User: %s\n", msg)

			sessionID, agent, err := tm.GetOrCreateSession(ctx, tenant.tenantID, tenant.userID)
			if err != nil {
				log.Printf("Error: %v", err)
				continue
			}

			response, err := agent.Run(ctx, msg)

			if err != nil {
				log.Printf("Error: %v", err)
				continue
			}

			for _, block := range response.Message.Content {
				if block.Type == agentpg.ContentTypeText {
					text := block.Text
					if len(text) > 200 {
						text = text[:200] + "..."
					}
					fmt.Printf("Agent: %s\n", text)
				}
			}

			fmt.Printf("Session: %s... | Tokens: %d in, %d out\n\n",
				sessionID[:8], response.Usage.InputTokens, response.Usage.OutputTokens)
		}
	}

	fmt.Println("=== Demo Complete ===")
	fmt.Println()
	fmt.Println("To start the HTTP server, uncomment the following:")
	fmt.Println("  log.Println(\"Starting server on :8080\")")
	fmt.Println("  log.Fatal(http.ListenAndServe(\":8080\", nil))")
}
