// Package main demonstrates the tool call visibility features of AgentPG.
//
// This example shows:
// - Real-time tool call callbacks (OnToolStart / OnToolComplete)
// - Inspecting ToolCalls on the Response after a run completes
// - Querying tool calls via GetRunToolCalls
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/youssefsiam38/agentpg"
	"github.com/youssefsiam38/agentpg/driver/pgxv5"
	"github.com/youssefsiam38/agentpg/tool"
)

// WeatherTool returns mock weather data.
type WeatherTool struct{}

func (t *WeatherTool) Name() string        { return "get_weather" }
func (t *WeatherTool) Description() string { return "Get the current weather for a city" }

func (t *WeatherTool) InputSchema() tool.ToolSchema {
	return tool.ToolSchema{
		Type: "object",
		Properties: map[string]tool.PropertyDef{
			"city": {Type: "string", Description: "City name"},
		},
		Required: []string{"city"},
	}
}

func (t *WeatherTool) Execute(_ context.Context, input json.RawMessage) (string, error) {
	var params struct {
		City string `json:"city"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}
	time.Sleep(500 * time.Millisecond) // simulate latency
	return fmt.Sprintf("Weather in %s: 22°C, sunny with light clouds", params.City), nil
}

// TimeTool returns the current time.
type TimeTool struct{}

func (t *TimeTool) Name() string        { return "get_time" }
func (t *TimeTool) Description() string { return "Get the current date and time" }

func (t *TimeTool) InputSchema() tool.ToolSchema {
	return tool.ToolSchema{
		Type:       "object",
		Properties: map[string]tool.PropertyDef{},
	}
}

func (t *TimeTool) Execute(_ context.Context, _ json.RawMessage) (string, error) {
	return time.Now().Format(time.RFC1123), nil
}

func main() {
	ctx := context.Background()

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
		APIKey: os.Getenv("ANTHROPIC_API_KEY"),
		Name:   "tool-calls-demo",
	})
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}

	// Register tools
	client.RegisterTool(&WeatherTool{})
	client.RegisterTool(&TimeTool{})

	if err := client.Start(ctx); err != nil {
		log.Fatalf("Failed to start client: %v", err)
	}
	defer client.Stop(context.Background())

	agent, err := client.GetOrCreateAgent(ctx, &agentpg.AgentDefinition{
		Name:         "tool-calls-demo-agent",
		Model:        "claude-sonnet-4-5-20250929",
		SystemPrompt: "You are a helpful assistant. When asked about weather or time, always use the appropriate tool. Be concise.",
		Tools:        []string{"get_weather", "get_time"},
	})
	if err != nil {
		log.Fatalf("Failed to create agent: %v", err)
	}

	sessionID, err := client.NewSession(ctx, nil, nil)
	if err != nil {
		log.Fatalf("Failed to create session: %v", err)
	}

	// -------------------------------------------------------
	// 1. Real-time callbacks
	// -------------------------------------------------------
	fmt.Println("=== Real-Time Tool Call Callbacks ===")
	fmt.Println()

	var mu sync.Mutex
	var callbackEvents []string

	response, err := client.RunFastSync(ctx, sessionID, agent.ID,
		"What's the weather in Cairo and what time is it?",
		&agentpg.RunOptions{
			OnToolStart: func(event agentpg.ToolCallEvent) {
				msg := fmt.Sprintf("[callback] Tool STARTED: %s", event.ToolName)
				fmt.Println(msg)
				mu.Lock()
				callbackEvents = append(callbackEvents, msg)
				mu.Unlock()
			},
			OnToolComplete: func(event agentpg.ToolCallEvent) {
				msg := fmt.Sprintf("[callback] Tool COMPLETED: %s (took %v, error=%v)",
					event.ToolName, event.Duration.Round(time.Millisecond), event.IsError)
				fmt.Println(msg)
				mu.Lock()
				callbackEvents = append(callbackEvents, msg)
				mu.Unlock()
			},
		},
	)
	if err != nil {
		log.Fatalf("Run failed: %v", err)
	}

	fmt.Println()
	fmt.Printf("Agent response: %s\n", response.Text)
	fmt.Println()

	// -------------------------------------------------------
	// 2. Inspect ToolCalls on Response
	// -------------------------------------------------------
	fmt.Println("=== Tool Calls on Response ===")
	fmt.Println()

	for i, tc := range response.ToolCalls {
		fmt.Printf("  [%d] %s (iteration %d, state=%s, duration=%v)\n",
			i+1, tc.Name, tc.IterationNumber, tc.State, tc.Duration.Round(time.Millisecond))
		fmt.Printf("      Input:  %s\n", string(tc.Input))
		output := tc.Output
		if len(output) > 80 {
			output = output[:80] + "..."
		}
		fmt.Printf("      Output: %s\n", output)
		if tc.IsError {
			fmt.Printf("      Error:  %s\n", tc.ErrorMessage)
		}
	}

	fmt.Println()

	// -------------------------------------------------------
	// 3. Query tool calls via GetRunToolCalls
	// -------------------------------------------------------
	fmt.Println("=== GetRunToolCalls Query ===")
	fmt.Println()

	// We need the run ID — get it from a separate run
	runID, err := client.RunFast(ctx, sessionID, agent.ID,
		"What is the current time?", nil)
	if err != nil {
		log.Fatalf("Failed to start run: %v", err)
	}

	queryResp, err := client.WaitForRun(ctx, runID)
	if err != nil {
		log.Fatalf("Run failed: %v", err)
	}

	toolCalls, err := client.GetRunToolCalls(ctx, runID)
	if err != nil {
		log.Fatalf("Failed to get run tool calls: %v", err)
	}

	fmt.Printf("  Run %s completed with %d tool call(s)\n", runID, len(toolCalls))
	for _, tc := range toolCalls {
		fmt.Printf("    - %s → %s\n", tc.Name, tc.Output)
	}
	fmt.Printf("  Response: %s\n", queryResp.Text)
	fmt.Println()

	// -------------------------------------------------------
	// Summary
	// -------------------------------------------------------
	fmt.Println("=== Summary ===")
	mu.Lock()
	fmt.Printf("  Callback events received: %d\n", len(callbackEvents))
	mu.Unlock()
	fmt.Printf("  Response.ToolCalls count: %d\n", len(response.ToolCalls))
	fmt.Printf("  GetRunToolCalls count:    %d\n", len(toolCalls))
	fmt.Println()
	fmt.Println("Done!")
}
