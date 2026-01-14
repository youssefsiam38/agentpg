// Package main demonstrates the Client API with struct-based tools.
//
// This example shows:
// - Implementing the tool.Tool interface with a struct
// - Tools with internal state (API keys, configuration, mock data)
// - Per-client tool registration with client.RegisterTool
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/youssefsiam38/agentpg"
	"github.com/youssefsiam38/agentpg/driver/pgxv5"
	"github.com/youssefsiam38/agentpg/tool"
)

// WeatherTool demonstrates implementing the tool.Tool interface with a struct.
// This pattern is useful when your tool needs:
// - Internal state (like API keys or configuration)
// - Complex initialization logic
// - Methods that share common data
type WeatherTool struct {
	// Internal state - could be API keys, database connections, etc.
	defaultUnit string
	locations   map[string]weatherData
}

// weatherData represents simulated weather information
type weatherData struct {
	Temperature float64
	Humidity    int
	Condition   string
}

// NewWeatherTool creates a new WeatherTool with default configuration
func NewWeatherTool() *WeatherTool {
	return &WeatherTool{
		defaultUnit: "celsius",
		locations: map[string]weatherData{
			"new york":      {Temperature: 22.5, Humidity: 65, Condition: "partly cloudy"},
			"london":        {Temperature: 15.2, Humidity: 78, Condition: "rainy"},
			"tokyo":         {Temperature: 28.0, Humidity: 70, Condition: "sunny"},
			"sydney":        {Temperature: 19.8, Humidity: 55, Condition: "clear"},
			"paris":         {Temperature: 18.5, Humidity: 60, Condition: "cloudy"},
			"san francisco": {Temperature: 16.0, Humidity: 72, Condition: "foggy"},
		},
	}
}

// Name returns the tool name used in API calls
func (w *WeatherTool) Name() string {
	return "get_weather"
}

// Description returns a human-readable description for the AI
func (w *WeatherTool) Description() string {
	return "Get current weather information for a specified city. Returns temperature, humidity, and conditions."
}

// InputSchema returns the JSON Schema for the tool's parameters
func (w *WeatherTool) InputSchema() tool.ToolSchema {
	return tool.ToolSchema{
		Type: "object",
		Properties: map[string]tool.PropertyDef{
			"city": {
				Type:        "string",
				Description: "The city name to get weather for (e.g., 'New York', 'London', 'Tokyo')",
			},
			"unit": {
				Type:        "string",
				Description: "Temperature unit: 'celsius' or 'fahrenheit'",
				Enum:        []string{"celsius", "fahrenheit"},
			},
		},
		Required: []string{"city"},
	}
}

// Execute runs the tool with the provided input
func (w *WeatherTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	// Parse input parameters
	var params struct {
		City string `json:"city"`
		Unit string `json:"unit"`
	}

	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}

	// Validate required fields
	if params.City == "" {
		return "", fmt.Errorf("city is required")
	}

	// Use default unit if not specified
	unit := params.Unit
	if unit == "" {
		unit = w.defaultUnit
	}

	// Look up weather data (case-insensitive)
	cityLower := toLower(params.City)
	data, found := w.locations[cityLower]
	if !found {
		// Generate random weather for unknown cities
		data = weatherData{
			Temperature: 15.0 + rand.Float64()*20.0,
			Humidity:    40 + rand.Intn(40),
			Condition:   []string{"sunny", "cloudy", "rainy", "partly cloudy"}[rand.Intn(4)],
		}
	}

	// Convert temperature if needed
	temp := data.Temperature
	if unit == "fahrenheit" {
		temp = temp*9/5 + 32
	}

	// Format response
	result := fmt.Sprintf("Weather in %s:\n- Temperature: %.1f°%s\n- Humidity: %d%%\n- Conditions: %s",
		params.City,
		temp,
		string(unit[0]-32), // Uppercase first letter (C or F)
		data.Humidity,
		data.Condition,
	)

	return result, nil
}

// toLower is a simple lowercase helper
func toLower(s string) string {
	result := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 32
		}
		result[i] = c
	}
	return string(result)
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

	// Create the pgx/v5 driver
	drv := pgxv5.New(pool)

	// Create the AgentPG client
	client, err := agentpg.NewClient(drv, &agentpg.ClientConfig{
		APIKey: apiKey,
	})
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}

	// Register the weather tool on this client
	if err := client.RegisterTool(NewWeatherTool()); err != nil {
		log.Fatalf("Failed to register tool: %v", err)
	}

	// Register the weather agent on this client
	maxTokens := 1024
	if err := client.RegisterAgent(&agentpg.AgentDefinition{
		Name:         "weather-assistant",
		Description:  "A helpful weather assistant",
		Model:        "claude-sonnet-4-5-20250929",
		SystemPrompt: "You are a helpful weather assistant. Use the get_weather tool to provide weather information when asked.",
		Tools:        []string{"get_weather"},
		MaxTokens:    &maxTokens,
	}); err != nil {
		log.Fatalf("Failed to register agent: %v", err)
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

	// Create a new session
	sessionID, err := client.NewSession(ctx, "1", "struct-tool-demo", nil, map[string]any{
		"description": "Struct-based tool demonstration",
	})
	if err != nil {
		log.Fatalf("Failed to create session: %v", err)
	}

	fmt.Printf("Created session: %s\n\n", sessionID)

	// Example 1: Basic weather query
	fmt.Println("=== Example 1: Basic Weather Query ===")
	response1, err := client.RunFastSync(ctx, sessionID, "weather-assistant", "What's the weather like in Tokyo?")
	if err != nil {
		log.Fatalf("Failed to run agent: %v", err)
	}

	for _, block := range response1.Message.Content {
		if block.Type == agentpg.ContentTypeText {
			fmt.Println(block.Text)
		}
	}

	// Example 2: Weather with unit preference
	fmt.Println("\n=== Example 2: Weather with Fahrenheit ===")
	response2, err := client.RunSync(ctx, sessionID, "weather-assistant", "What's the temperature in New York in Fahrenheit?")
	if err != nil {
		log.Fatalf("Failed to run agent: %v", err)
	}

	for _, block := range response2.Message.Content {
		if block.Type == agentpg.ContentTypeText {
			fmt.Println(block.Text)
		}
	}

	// Example 3: Unknown city (generates random weather)
	fmt.Println("\n=== Example 3: Unknown City ===")
	response3, err := client.RunFastSync(ctx, sessionID, "weather-assistant", "How's the weather in Reykjavik?")
	if err != nil {
		log.Fatalf("Failed to run agent: %v", err)
	}

	for _, block := range response3.Message.Content {
		if block.Type == agentpg.ContentTypeText {
			fmt.Println(block.Text)
		}
	}

	fmt.Println("\n=== Demo Complete ===")
}
