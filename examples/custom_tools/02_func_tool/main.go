// Package main demonstrates the Client API with function-based tools.
//
// This example shows:
// - Quick tool creation with tool.NewFuncTool
// - No need for a struct - just a function
// - Per-client tool registration
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

// Timezone offset data (simplified)
var timezoneOffsets = map[string]int{
	"UTC":       0,
	"EST":       -5,
	"PST":       -8,
	"CET":       1,
	"JST":       9,
	"AEST":      10,
	"GMT":       0,
	"IST":       5, // India
	"CST":       -6,
	"MST":       -7,
	"HST":       -10,
	"AKST":      -9,
	"AST":       -4,
	"NST":       -3,
	"BRT":       -3,
	"ART":       -3,
	"SAST":      2,
	"EAT":       3,
	"GST":       4,
	"PKT":       5,
	"BST":       6,
	"ICT":       7,
	"CST_CHINA": 8,
	"KST":       9,
	"NZST":      12,
}

// createTimeTool creates a tool for getting time in different timezones.
func createTimeTool() tool.Tool {
	return tool.NewFuncTool(
		"get_time",
		"Get the current time in a specified timezone. Returns formatted time and date.",
		tool.ToolSchema{
			Type: "object",
			Properties: map[string]tool.PropertyDef{
				"timezone": {
					Type:        "string",
					Description: "Timezone abbreviation (e.g., 'UTC', 'EST', 'PST', 'JST', 'CET')",
				},
				"format": {
					Type:        "string",
					Description: "Output format: '12h' for 12-hour clock, '24h' for 24-hour clock",
					Enum:        []string{"12h", "24h"},
				},
			},
			Required: []string{"timezone"},
		},
		func(ctx context.Context, input json.RawMessage) (string, error) {
			var params struct {
				Timezone string `json:"timezone"`
				Format   string `json:"format"`
			}

			if err := json.Unmarshal(input, &params); err != nil {
				return "", fmt.Errorf("invalid input: %w", err)
			}

			// Get timezone offset
			offset, found := timezoneOffsets[params.Timezone]
			if !found {
				return "", fmt.Errorf("unknown timezone: %s. Valid options: UTC, EST, PST, CET, JST, AEST, GMT, IST, CST, MST", params.Timezone)
			}

			// Calculate time in timezone
			now := time.Now().UTC().Add(time.Duration(offset) * time.Hour)

			// Format based on preference
			var timeStr string
			if params.Format == "12h" {
				timeStr = now.Format("3:04:05 PM")
			} else {
				timeStr = now.Format("15:04:05")
			}

			dateStr := now.Format("Monday, January 2, 2006")

			return fmt.Sprintf("Current time in %s:\nTime: %s\nDate: %s\nUTC Offset: %+d hours",
				params.Timezone, timeStr, dateStr, offset), nil
		},
	)
}

// createDateDiffTool creates a tool for calculating date differences.
func createDateDiffTool() tool.Tool {
	return tool.NewFuncTool(
		"calculate_date_diff",
		"Calculate the difference between two dates in days, weeks, or months.",
		tool.ToolSchema{
			Type: "object",
			Properties: map[string]tool.PropertyDef{
				"start_date": {
					Type:        "string",
					Description: "Start date in YYYY-MM-DD format",
				},
				"end_date": {
					Type:        "string",
					Description: "End date in YYYY-MM-DD format",
				},
				"unit": {
					Type:        "string",
					Description: "Unit for the result: 'days', 'weeks', or 'months'",
					Enum:        []string{"days", "weeks", "months"},
				},
			},
			Required: []string{"start_date", "end_date"},
		},
		func(ctx context.Context, input json.RawMessage) (string, error) {
			var params struct {
				StartDate string `json:"start_date"`
				EndDate   string `json:"end_date"`
				Unit      string `json:"unit"`
			}

			if err := json.Unmarshal(input, &params); err != nil {
				return "", fmt.Errorf("invalid input: %w", err)
			}

			// Parse dates
			start, err := time.Parse("2006-01-02", params.StartDate)
			if err != nil {
				return "", fmt.Errorf("invalid start_date format (use YYYY-MM-DD): %w", err)
			}

			end, err := time.Parse("2006-01-02", params.EndDate)
			if err != nil {
				return "", fmt.Errorf("invalid end_date format (use YYYY-MM-DD): %w", err)
			}

			// Calculate difference
			diff := end.Sub(start)
			days := int(diff.Hours() / 24)

			// Default to days if unit not specified
			unit := params.Unit
			if unit == "" {
				unit = "days"
			}

			var result string
			switch unit {
			case "days":
				result = fmt.Sprintf("%d days", days)
			case "weeks":
				weeks := float64(days) / 7.0
				result = fmt.Sprintf("%.1f weeks (%d days)", weeks, days)
			case "months":
				months := float64(days) / 30.44 // Average days per month
				result = fmt.Sprintf("%.1f months (%d days)", months, days)
			default:
				result = fmt.Sprintf("%d days", days)
			}

			return fmt.Sprintf("Date difference from %s to %s:\n%s",
				params.StartDate, params.EndDate, result), nil
		},
	)
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

	// Register tools on the client
	if err := client.RegisterTool(createTimeTool()); err != nil {
		log.Fatalf("Failed to register time tool: %v", err)
	}
	if err := client.RegisterTool(createDateDiffTool()); err != nil {
		log.Fatalf("Failed to register date diff tool: %v", err)
	}

	// Start the client
	if err := client.Start(ctx); err != nil {
		log.Fatalf("Failed to start client: %v", err)
	}

	// Create the time assistant agent in the database (after client.Start)
	maxTokens := 1024
	agent, err := client.CreateAgent(ctx, &agentpg.AgentDefinition{
		Name:         "agent.ID",
		Description:  "A helpful time assistant",
		Model:        "claude-sonnet-4-5-20250929",
		SystemPrompt: "You are a helpful time assistant. Use the available tools to provide time information.",
		Tools:        []string{"get_time", "calculate_date_diff"},
		MaxTokens:    &maxTokens,
	})
	if err != nil {
		log.Fatalf("Failed to create agent: %v", err)
	}
	defer func() {
		if err := client.Stop(context.Background()); err != nil {
			log.Printf("Error stopping client: %v", err)
		}
	}()

	log.Printf("Client started (instance ID: %s)", client.InstanceID())

	// Create session
	sessionID, err := client.NewSession(ctx, nil, map[string]any{
		"description": "Function-based tool demonstration",
	})
	if err != nil {
		log.Fatalf("Failed to create session: %v", err)
	}

	fmt.Printf("Created session: %s\n\n", sessionID)

	// Example 1: Get time in different timezone
	fmt.Println("=== Example 1: Get Time in Tokyo ===")
	response1, err := client.RunSync(ctx, sessionID, agent.ID, "What time is it in Tokyo right now?")
	if err != nil {
		log.Fatalf("Failed to run agent: %v", err)
	}

	for _, block := range response1.Message.Content {
		if block.Type == agentpg.ContentTypeText {
			fmt.Println(block.Text)
		}
	}

	// Example 2: Get time with specific format
	fmt.Println("\n=== Example 2: Time in 12-hour Format ===")
	response2, err := client.RunSync(ctx, sessionID, agent.ID, "What's the current time in New York (EST) in 12-hour format?")
	if err != nil {
		log.Fatalf("Failed to run agent: %v", err)
	}

	for _, block := range response2.Message.Content {
		if block.Type == agentpg.ContentTypeText {
			fmt.Println(block.Text)
		}
	}

	// Example 3: Calculate date difference
	fmt.Println("\n=== Example 3: Date Difference ===")
	response3, err := client.RunSync(ctx, sessionID, agent.ID, "How many weeks are between 2024-01-01 and 2024-12-31?")
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
