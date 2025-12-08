// Package main demonstrates sharing tools across multiple agents.
//
// This example shows:
// - Global tool registration with agentpg.MustRegisterTool()
// - Referencing tools by name in AgentDefinition.Tools
// - Different agents with different tool subsets
package main

import (
	"context"
	"encoding/json"
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

// =============================================================================
// Shared Tools - Registered globally, used by multiple agents
// =============================================================================

// GetTimeTool - shared across all agents
type GetTimeTool struct{}

func (t *GetTimeTool) Name() string        { return "get_time" }
func (t *GetTimeTool) Description() string { return "Get the current time" }
func (t *GetTimeTool) InputSchema() tool.ToolSchema {
	return tool.ToolSchema{Type: "object", Properties: map[string]tool.PropertyDef{}}
}
func (t *GetTimeTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	return time.Now().Format(time.RFC3339), nil
}

// CalculatorTool - shared across all agents
type CalculatorTool struct{}

func (t *CalculatorTool) Name() string        { return "calculator" }
func (t *CalculatorTool) Description() string { return "Perform basic math operations" }
func (t *CalculatorTool) InputSchema() tool.ToolSchema {
	return tool.ToolSchema{
		Type: "object",
		Properties: map[string]tool.PropertyDef{
			"operation": {Type: "string", Enum: []string{"add", "subtract", "multiply", "divide"}},
			"a":         {Type: "number", Description: "First number"},
			"b":         {Type: "number", Description: "Second number"},
		},
		Required: []string{"operation", "a", "b"},
	}
}
func (t *CalculatorTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var params struct {
		Operation string  `json:"operation"`
		A         float64 `json:"a"`
		B         float64 `json:"b"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", err
	}

	var result float64
	switch params.Operation {
	case "add":
		result = params.A + params.B
	case "subtract":
		result = params.A - params.B
	case "multiply":
		result = params.A * params.B
	case "divide":
		if params.B == 0 {
			return "", fmt.Errorf("division by zero")
		}
		result = params.A / params.B
	}
	return fmt.Sprintf("%.2f", result), nil
}

// WeatherTool - only used by some agents
type WeatherTool struct{}

func (t *WeatherTool) Name() string        { return "get_weather" }
func (t *WeatherTool) Description() string { return "Get weather for a city (simulated)" }
func (t *WeatherTool) InputSchema() tool.ToolSchema {
	return tool.ToolSchema{
		Type: "object",
		Properties: map[string]tool.PropertyDef{
			"city": {Type: "string", Description: "City name"},
		},
		Required: []string{"city"},
	}
}
func (t *WeatherTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var params struct {
		City string `json:"city"`
	}
	json.Unmarshal(input, &params)
	return fmt.Sprintf("Weather in %s: 22°C, Partly Cloudy", params.City), nil
}

// =============================================================================
// Global Registration - Tools registered once, referenced by name
// =============================================================================

func init() {
	// Register tools globally (persisted to DB during client.Start())
	agentpg.MustRegisterTool(&GetTimeTool{})
	agentpg.MustRegisterTool(&CalculatorTool{})
	agentpg.MustRegisterTool(&WeatherTool{})

	maxTokens := 1024

	// Agent 1: General Assistant - has ALL tools
	agentpg.MustRegister(&agentpg.AgentDefinition{
		Name:         "general-assistant",
		Description:  "General purpose assistant with all tools",
		Model:        "claude-sonnet-4-5-20250929",
		SystemPrompt: "You are a helpful general assistant. Be concise.",
		MaxTokens:    &maxTokens,
		Tools:        []string{"get_time", "calculator", "get_weather"}, // All 3 tools
	})

	// Agent 2: Math Tutor - only calculator and time
	agentpg.MustRegister(&agentpg.AgentDefinition{
		Name:         "math-tutor",
		Description:  "Math tutor with calculation abilities",
		Model:        "claude-sonnet-4-5-20250929",
		SystemPrompt: "You are a math tutor. Help with calculations. Be concise.",
		MaxTokens:    &maxTokens,
		Tools:        []string{"calculator", "get_time"}, // Subset of tools
	})

	// Agent 3: Weather Bot - only weather and time
	agentpg.MustRegister(&agentpg.AgentDefinition{
		Name:         "weather-bot",
		Description:  "Weather information assistant",
		Model:        "claude-sonnet-4-5-20250929",
		SystemPrompt: "You are a weather assistant. Provide weather info. Be concise.",
		MaxTokens:    &maxTokens,
		Tools:        []string{"get_weather", "get_time"}, // Different subset
	})
}

func main() {
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

	// Create the pgx/v5 driver
	drv := pgxv5.New(pool)

	// Create and start the client
	client, err := agentpg.NewClient(drv, &agentpg.ClientConfig{
		APIKey: apiKey,
	})
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}

	if err := client.Start(ctx); err != nil {
		log.Fatalf("Failed to start client: %v", err)
	}
	defer func() {
		if err := client.Stop(context.Background()); err != nil {
			log.Printf("Error stopping client: %v", err)
		}
	}()

	fmt.Printf("Client started (instance: %s)\n", client.InstanceID())
	fmt.Println()

	// =============================================================================
	// Demo: Each agent has different tool access
	// =============================================================================

	fmt.Println("=== Shared Tools Demo ===")
	fmt.Println()
	fmt.Println("Tool assignments:")
	fmt.Println("  general-assistant: get_time, calculator, get_weather")
	fmt.Println("  math-tutor:        get_time, calculator")
	fmt.Println("  weather-bot:       get_time, get_weather")
	fmt.Println()

	// General Assistant - can do everything
	fmt.Println("--- General Assistant (all tools) ---")
	general := client.Agent("general-assistant")
	if general == nil {
		log.Fatal("Agent 'general-assistant' not found")
	}
	sid1, err := general.NewSession(ctx, "1", "demo", nil, nil)
	if err != nil {
		log.Fatalf("Failed to create session: %v", err)
	}
	resp1, err := general.Run(ctx, sid1, "What time is it, what's 15*7, and what's the weather in Paris?")
	if err != nil {
		log.Fatalf("Failed to run agent: %v", err)
	}
	printResponse(resp1)

	// Math Tutor - can only do math and time
	fmt.Println("--- Math Tutor (calculator + time) ---")
	math := client.Agent("math-tutor")
	if math == nil {
		log.Fatal("Agent 'math-tutor' not found")
	}
	sid2, err := math.NewSession(ctx, "1", "demo", nil, nil)
	if err != nil {
		log.Fatalf("Failed to create session: %v", err)
	}
	resp2, err := math.Run(ctx, sid2, "What's 144 divided by 12?")
	if err != nil {
		log.Fatalf("Failed to run agent: %v", err)
	}
	printResponse(resp2)

	// Weather Bot - can only do weather and time
	fmt.Println("--- Weather Bot (weather + time) ---")
	weather := client.Agent("weather-bot")
	if weather == nil {
		log.Fatal("Agent 'weather-bot' not found")
	}
	sid3, err := weather.NewSession(ctx, "1", "demo", nil, nil)
	if err != nil {
		log.Fatalf("Failed to create session: %v", err)
	}
	resp3, err := weather.Run(ctx, sid3, "What's the weather in Tokyo?")
	if err != nil {
		log.Fatalf("Failed to run agent: %v", err)
	}
	printResponse(resp3)

	fmt.Println("=== Demo Complete ===")
}

func printResponse(resp *agentpg.Response) {
	for _, block := range resp.Message.Content {
		if block.Type == agentpg.ContentTypeText {
			text := block.Text
			if len(text) > 300 {
				text = text[:300] + "..."
			}
			fmt.Printf("Response: %s\n", text)
		}
	}
	fmt.Printf("Tokens: %d in, %d out\n\n", resp.Usage.InputTokens, resp.Usage.OutputTokens)
}
