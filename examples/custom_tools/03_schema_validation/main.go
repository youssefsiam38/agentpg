// Package main demonstrates the Client API with advanced schema validation.
//
// This example shows:
// - All PropertyDef constraint types (Enum, Min/Max, MinLength/MaxLength)
// - Array types with Items schema
// - Nested objects with Properties
// - Per-client tool and agent registration for stateful tools
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
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

// TaskTool demonstrates advanced JSON Schema validation features
type TaskTool struct {
	tasks []Task
}

type Task struct {
	ID          int
	Title       string
	Description string
	Priority    string
	Score       float64
	Tags        []string
	Assignee    Assignee
	CreatedAt   time.Time
}

type Assignee struct {
	Name  string
	Email string
}

func NewTaskTool() *TaskTool {
	return &TaskTool{
		tasks: make([]Task, 0),
	}
}

func (t *TaskTool) Name() string {
	return "create_task"
}

func (t *TaskTool) Description() string {
	return "Create a new task with priority, score, tags, and assignee. Demonstrates advanced schema validation."
}

// InputSchema demonstrates all PropertyDef constraint types
func (t *TaskTool) InputSchema() tool.ToolSchema {
	// Helper to create pointer to float64
	minScore := 0.0
	maxScore := 100.0
	minTitleLen := 3
	maxTitleLen := 100

	return tool.ToolSchema{
		Type: "object",
		Properties: map[string]tool.PropertyDef{
			// Basic string with length constraints
			"title": {
				Type:        "string",
				Description: "Task title (3-100 characters)",
				MinLength:   &minTitleLen,
				MaxLength:   &maxTitleLen,
			},
			// Optional description
			"description": {
				Type:        "string",
				Description: "Detailed task description (optional)",
			},
			// Enum constraint - only specific values allowed
			"priority": {
				Type:        "string",
				Description: "Task priority level",
				Enum:        []string{"low", "medium", "high", "critical"},
			},
			// Number with min/max constraints
			"score": {
				Type:        "number",
				Description: "Task importance score from 0 to 100",
				Minimum:     &minScore,
				Maximum:     &maxScore,
			},
			// Array type with items schema
			"tags": {
				Type:        "array",
				Description: "List of tags for categorization",
				Items: &tool.PropertyDef{
					Type:        "string",
					Description: "A single tag",
				},
			},
			// Nested object with its own properties
			"assignee": {
				Type:        "object",
				Description: "Person assigned to this task",
				Properties: map[string]tool.PropertyDef{
					"name": {
						Type:        "string",
						Description: "Assignee's full name",
					},
					"email": {
						Type:        "string",
						Description: "Assignee's email address",
					},
				},
			},
		},
		// Required fields
		Required: []string{"title", "priority"},
	}
}

func (t *TaskTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var params struct {
		Title       string   `json:"title"`
		Description string   `json:"description"`
		Priority    string   `json:"priority"`
		Score       float64  `json:"score"`
		Tags        []string `json:"tags"`
		Assignee    struct {
			Name  string `json:"name"`
			Email string `json:"email"`
		} `json:"assignee"`
	}

	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}

	// Additional validation (schema handles most of this)
	if params.Title == "" {
		return "", fmt.Errorf("title is required")
	}
	if len(params.Title) < 3 {
		return "", fmt.Errorf("title must be at least 3 characters")
	}

	// Validate priority (schema should enforce this via enum)
	validPriorities := map[string]bool{
		"low": true, "medium": true, "high": true, "critical": true,
	}
	if !validPriorities[params.Priority] {
		return "", fmt.Errorf("invalid priority: must be low, medium, high, or critical")
	}

	// Validate score range (schema should enforce via min/max)
	if params.Score < 0 || params.Score > 100 {
		return "", fmt.Errorf("score must be between 0 and 100")
	}

	// Create the task
	task := Task{
		ID:          len(t.tasks) + 1,
		Title:       params.Title,
		Description: params.Description,
		Priority:    params.Priority,
		Score:       params.Score,
		Tags:        params.Tags,
		CreatedAt:   time.Now(),
	}

	if params.Assignee.Name != "" {
		task.Assignee = Assignee{
			Name:  params.Assignee.Name,
			Email: params.Assignee.Email,
		}
	}

	t.tasks = append(t.tasks, task)

	// Format response
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Task #%d created successfully!\n", task.ID))
	sb.WriteString(fmt.Sprintf("- Title: %s\n", task.Title))
	sb.WriteString(fmt.Sprintf("- Priority: %s\n", task.Priority))
	sb.WriteString(fmt.Sprintf("- Score: %.1f\n", task.Score))

	if len(task.Tags) > 0 {
		sb.WriteString(fmt.Sprintf("- Tags: %s\n", strings.Join(task.Tags, ", ")))
	}

	if task.Assignee.Name != "" {
		sb.WriteString(fmt.Sprintf("- Assignee: %s", task.Assignee.Name))
		if task.Assignee.Email != "" {
			sb.WriteString(fmt.Sprintf(" <%s>", task.Assignee.Email))
		}
		sb.WriteString("\n")
	}

	if task.Description != "" {
		sb.WriteString(fmt.Sprintf("- Description: %s\n", task.Description))
	}

	return sb.String(), nil
}

// ListTasksTool demonstrates a simpler schema for querying
type ListTasksTool struct {
	taskTool *TaskTool
}

func NewListTasksTool(taskTool *TaskTool) *ListTasksTool {
	return &ListTasksTool{taskTool: taskTool}
}

func (l *ListTasksTool) Name() string {
	return "list_tasks"
}

func (l *ListTasksTool) Description() string {
	return "List all tasks, optionally filtered by priority"
}

func (l *ListTasksTool) InputSchema() tool.ToolSchema {
	return tool.ToolSchema{
		Type: "object",
		Properties: map[string]tool.PropertyDef{
			"priority_filter": {
				Type:        "string",
				Description: "Filter by priority (optional)",
				Enum:        []string{"low", "medium", "high", "critical"},
			},
		},
		// No required fields - all optional
		Required: []string{},
	}
}

func (l *ListTasksTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var params struct {
		PriorityFilter string `json:"priority_filter"`
	}

	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}

	if len(l.taskTool.tasks) == 0 {
		return "No tasks found.", nil
	}

	var sb strings.Builder
	sb.WriteString("Tasks:\n")

	count := 0
	for _, task := range l.taskTool.tasks {
		if params.PriorityFilter != "" && task.Priority != params.PriorityFilter {
			continue
		}

		count++
		sb.WriteString(fmt.Sprintf("\n#%d: %s\n", task.ID, task.Title))
		sb.WriteString(fmt.Sprintf("    Priority: %s | Score: %.1f\n", task.Priority, task.Score))
		if len(task.Tags) > 0 {
			sb.WriteString(fmt.Sprintf("    Tags: %s\n", strings.Join(task.Tags, ", ")))
		}
	}

	if count == 0 {
		return fmt.Sprintf("No tasks found with priority '%s'.", params.PriorityFilter), nil
	}

	sb.WriteString(fmt.Sprintf("\nTotal: %d task(s)", count))
	return sb.String(), nil
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

	// Create and register tools (note: TaskTool is shared for state)
	// This demonstrates per-client registration for stateful tools
	taskTool := NewTaskTool()
	listTool := NewListTasksTool(taskTool)

	client.RegisterTool(taskTool)
	client.RegisterTool(listTool)

	// Register the agent with its tools
	maxTokens := 1024
	client.RegisterAgent(&agentpg.AgentDefinition{
		Name:         "task-manager",
		Description:  "A task management assistant",
		Model:        "claude-sonnet-4-5-20250929",
		SystemPrompt: "You are a task management assistant. Help users create and manage tasks using the available tools.",
		Tools:        []string{"create_task", "list_tasks"},
		MaxTokens:    &maxTokens,
	})

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

	// Create session
	sessionID, err := client.NewSession(ctx, "1", "schema-validation-demo", nil, map[string]any{
		"description": "Schema validation demonstration",
	})
	if err != nil {
		log.Fatalf("Failed to create session: %v", err)
	}

	fmt.Printf("Created session: %s\n\n", sessionID)

	// Example 1: Create task with all fields
	fmt.Println("=== Example 1: Full Task Creation ===")
	response1, err := client.RunSync(ctx, sessionID, "task-manager", `Create a high priority task called "Implement user authentication" with score 85, tags ["security", "backend", "urgent"], and assign it to John Smith (john@example.com). Add a description about implementing OAuth2.`)
	if err != nil {
		log.Fatalf("Failed to run agent: %v", err)
	}

	for _, block := range response1.Message.Content {
		if block.Type == agentpg.ContentTypeText {
			fmt.Println(block.Text)
		}
	}

	// Example 2: Create minimal task (only required fields)
	fmt.Println("\n=== Example 2: Minimal Task ===")
	response2, err := client.RunSync(ctx, sessionID, "task-manager", `Create a low priority task called "Update documentation"`)
	if err != nil {
		log.Fatalf("Failed to run agent: %v", err)
	}

	for _, block := range response2.Message.Content {
		if block.Type == agentpg.ContentTypeText {
			fmt.Println(block.Text)
		}
	}

	// Example 3: Create critical task with array of tags
	fmt.Println("\n=== Example 3: Critical Task with Tags ===")
	response3, err := client.RunSync(ctx, sessionID, "task-manager", `Create a critical priority task "Fix production database issue" with score 100 and tags ["production", "database", "emergency"]`)
	if err != nil {
		log.Fatalf("Failed to run agent: %v", err)
	}

	for _, block := range response3.Message.Content {
		if block.Type == agentpg.ContentTypeText {
			fmt.Println(block.Text)
		}
	}

	// Example 4: List all tasks
	fmt.Println("\n=== Example 4: List All Tasks ===")
	response4, err := client.RunSync(ctx, sessionID, "task-manager", "Show me all the tasks we've created")
	if err != nil {
		log.Fatalf("Failed to run agent: %v", err)
	}

	for _, block := range response4.Message.Content {
		if block.Type == agentpg.ContentTypeText {
			fmt.Println(block.Text)
		}
	}

	// Example 5: Filter by priority
	fmt.Println("\n=== Example 5: Filter by Priority ===")
	response5, err := client.RunSync(ctx, sessionID, "task-manager", "List only the critical priority tasks")
	if err != nil {
		log.Fatalf("Failed to run agent: %v", err)
	}

	for _, block := range response5.Message.Content {
		if block.Type == agentpg.ContentTypeText {
			fmt.Println(block.Text)
		}
	}

	fmt.Println("\n=== Demo Complete ===")
}
