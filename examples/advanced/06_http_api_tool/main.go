// Package main demonstrates the Client API with HTTP API tool.
//
// This example shows:
// - HTTP API tool with host whitelist
// - Response size limits and timeouts
// - Mock API server for demonstration
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/youssefsiam38/agentpg"
	"github.com/youssefsiam38/agentpg/driver/pgxv5"
	"github.com/youssefsiam38/agentpg/tool"
)

// HTTPAPITool allows the agent to make HTTP requests to allowed APIs
type HTTPAPITool struct {
	client         *http.Client
	allowedHosts   []string
	defaultHeaders map[string]string
}

func NewHTTPAPITool(allowedHosts []string, timeout time.Duration) *HTTPAPITool {
	return &HTTPAPITool{
		client: &http.Client{
			Timeout: timeout,
		},
		allowedHosts:   allowedHosts,
		defaultHeaders: make(map[string]string),
	}
}

func (h *HTTPAPITool) SetDefaultHeader(key, value string) {
	h.defaultHeaders[key] = value
}

func (h *HTTPAPITool) Name() string {
	return "http_request"
}

func (h *HTTPAPITool) Description() string {
	return fmt.Sprintf("Make HTTP GET requests to allowed APIs. Allowed hosts: %s",
		strings.Join(h.allowedHosts, ", "))
}

func (h *HTTPAPITool) InputSchema() tool.ToolSchema {
	return tool.ToolSchema{
		Type: "object",
		Properties: map[string]tool.PropertyDef{
			"url": {
				Type:        "string",
				Description: "Full URL to request (must be from allowed hosts)",
			},
			"headers": {
				Type:        "object",
				Description: "Optional headers to include in the request",
			},
		},
		Required: []string{"url"},
	}
}

func (h *HTTPAPITool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var params struct {
		URL     string            `json:"url"`
		Headers map[string]string `json:"headers"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}

	// Validate URL against allowed hosts
	allowed := false
	for _, host := range h.allowedHosts {
		if strings.Contains(params.URL, host) {
			allowed = true
			break
		}
	}
	if !allowed {
		return "", fmt.Errorf("URL not allowed. Must use one of: %s", strings.Join(h.allowedHosts, ", "))
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, "GET", params.URL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	// Add default headers
	for k, v := range h.defaultHeaders {
		req.Header.Set(k, v)
	}

	// Add request-specific headers
	for k, v := range params.Headers {
		req.Header.Set(k, v)
	}

	// Make request
	resp, err := h.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read body (with size limit)
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024*100)) // 100KB limit
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	// Format response
	result := fmt.Sprintf("Status: %s\nContent-Type: %s\n\n",
		resp.Status,
		resp.Header.Get("Content-Type"))

	// Try to pretty-print JSON
	if strings.Contains(resp.Header.Get("Content-Type"), "application/json") {
		var jsonData interface{}
		if err := json.Unmarshal(body, &jsonData); err == nil {
			prettyJSON, err := json.MarshalIndent(jsonData, "", "  ")
			if err == nil {
				body = prettyJSON
			}
		}
	}

	result += string(body)

	// Truncate if too long
	if len(result) > 5000 {
		result = result[:5000] + "\n...(truncated)"
	}

	return result, nil
}

// MockWeatherAPI simulates a weather API for demo purposes
type MockWeatherAPI struct {
	server *http.Server
}

func NewMockWeatherAPI(port string) *MockWeatherAPI {
	mux := http.NewServeMux()

	mux.HandleFunc("/weather", func(w http.ResponseWriter, r *http.Request) {
		city := r.URL.Query().Get("city")
		if city == "" {
			city = "Unknown"
		}

		response := map[string]interface{}{
			"city":        city,
			"temperature": 22,
			"unit":        "celsius",
			"condition":   "Partly Cloudy",
			"humidity":    65,
			"wind_speed":  12,
			"wind_unit":   "km/h",
			"forecast": []map[string]interface{}{
				{"day": "Tomorrow", "high": 24, "low": 18, "condition": "Sunny"},
				{"day": "Day After", "high": 21, "low": 16, "condition": "Rainy"},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	})

	mux.HandleFunc("/cities", func(w http.ResponseWriter, r *http.Request) {
		response := map[string]interface{}{
			"cities": []string{"London", "Paris", "Tokyo", "New York", "Sydney"},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	})

	return &MockWeatherAPI{
		server: &http.Server{
			Addr:    ":" + port,
			Handler: mux,
		},
	}
}

func (m *MockWeatherAPI) Start() {
	go m.server.ListenAndServe()
}

func (m *MockWeatherAPI) Stop() {
	m.server.Close()
}

// Register agent at package initialization.
func init() {
	maxTokens := 1024
	agentpg.MustRegister(&agentpg.AgentDefinition{
		Name:        "http-tool-demo",
		Description: "Weather assistant with HTTP API access",
		Model:       "claude-sonnet-4-5-20250929",
		SystemPrompt: `You are a helpful weather assistant. You have access to a weather API.

Use the http_request tool to:
- Get weather for a city: http://localhost:8888/weather?city=CityName
- List available cities: http://localhost:8888/cities

Always use the API to get real data before answering weather questions.`,
		MaxTokens: &maxTokens,
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
	// Start mock weather API
	// ==========================================================

	fmt.Println("=== HTTP API Tool Example ===")
	fmt.Println()

	mockAPI := NewMockWeatherAPI("8888")
	mockAPI.Start()
	defer mockAPI.Stop()

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	fmt.Println("Mock weather API started on http://localhost:8888")
	fmt.Println()

	// ==========================================================
	// Create agent with HTTP tool
	// ==========================================================

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
	agent := client.Agent("http-tool-demo")
	if agent == nil {
		log.Fatal("Agent 'http-tool-demo' not found")
	}

	// Register HTTP tool with allowed hosts (runtime registration for stateful tool)
	httpTool := NewHTTPAPITool(
		[]string{"localhost:8888"},
		10*time.Second,
	)
	httpTool.SetDefaultHeader("User-Agent", "AgentPG-Demo/1.0")
	if err := agent.RegisterTool(httpTool); err != nil {
		log.Fatalf("Failed to register tool: %v", err)
	}

	// Create session
	sessionID, err := agent.NewSession(ctx, "1", "http-tool-demo", nil, nil)
	if err != nil {
		log.Fatalf("Failed to create session: %v", err)
	}
	fmt.Printf("Session: %s\n\n", sessionID[:8]+"...")

	// ==========================================================
	// Demo queries
	// ==========================================================

	queries := []string{
		"What cities can you get weather for?",
		"What's the weather like in Tokyo?",
		"Compare the forecast - will it be warmer tomorrow or the day after in London?",
	}

	for i, query := range queries {
		fmt.Printf("=== Query %d ===\n", i+1)
		fmt.Printf("User: %s\n\n", query)

		response, err := agent.Run(ctx, sessionID, query)
		if err != nil {
			log.Printf("Error: %v\n\n", err)
			continue
		}

		for _, block := range response.Message.Content {
			if block.Type == agentpg.ContentTypeText {
				text := block.Text
				if len(text) > 500 {
					text = text[:500] + "..."
				}
				fmt.Printf("Agent: %s\n", text)
			}
		}
		fmt.Println()
	}

	// ==========================================================
	// Demo: Security - blocked requests
	// ==========================================================

	fmt.Println("=== Security Demo ===")
	fmt.Println()

	blockedURLs := []string{
		"https://api.openai.com/v1/chat",
		"http://internal-service.local/admin",
		"https://example.com/api",
	}

	fmt.Println("Testing URL restrictions:")
	for _, url := range blockedURLs {
		input, _ := json.Marshal(map[string]string{"url": url})
		_, err := httpTool.Execute(ctx, input)
		if err != nil {
			fmt.Printf("  BLOCKED: %s\n", url)
		} else {
			fmt.Printf("  ALLOWED: %s\n", url)
		}
	}

	fmt.Println()
	fmt.Println("=== HTTP Tool Safety Features ===")
	fmt.Println("1. Host whitelist: Only allowed domains can be accessed")
	fmt.Println("2. Timeout: Prevents hanging on slow endpoints")
	fmt.Println("3. Size limits: Response body capped at 100KB")
	fmt.Println("4. GET only: No POST/PUT/DELETE (modifiable)")
	fmt.Println()
	fmt.Println("=== Demo Complete ===")
}
