// Package main demonstrates the Client API with multi-level agent hierarchy.
//
// This example shows:
// - 3-level agent hierarchy (Project Manager → Team Leads → Workers)
// - Worker agents with specialized tools
// - Per-client registration (no global state)
// - Using Agents field in AgentDefinition to build the hierarchy
// - Top-down delegation and bottom-up status aggregation
//
// Architecture:
//
//	Level 1 (Project Manager)
//	    └── Engineering Lead (Level 2)
//	    │       ├── Frontend Developer (Level 3) [lint_frontend]
//	    │       ├── Backend Developer (Level 3)  [run_tests]
//	    │       └── Database Specialist (Level 3) [run_migration]
//	    └── Design Lead (Level 2)
//	            └── UX Designer (Level 3)        [review_design]
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/youssefsiam38/agentpg"
	"github.com/youssefsiam38/agentpg/driver/pgxv5"
	"github.com/youssefsiam38/agentpg/tool"
)

// ==========================================================
// LEVEL 3 TOOLS (Worker-level tools)
// ==========================================================

// FrontendLintTool simulates frontend code linting
type FrontendLintTool struct{}

func (f *FrontendLintTool) Name() string        { return "lint_frontend" }
func (f *FrontendLintTool) Description() string { return "Lint frontend code for issues" }

func (f *FrontendLintTool) InputSchema() tool.ToolSchema {
	return tool.ToolSchema{
		Type: "object",
		Properties: map[string]tool.PropertyDef{
			"code": {Type: "string", Description: "Frontend code to lint"},
		},
		Required: []string{"code"},
	}
}

func (f *FrontendLintTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	return "Frontend lint passed: No ESLint warnings. React best practices followed.", nil
}

// BackendTestTool simulates backend testing
type BackendTestTool struct{}

func (b *BackendTestTool) Name() string        { return "run_tests" }
func (b *BackendTestTool) Description() string { return "Run backend tests" }

func (b *BackendTestTool) InputSchema() tool.ToolSchema {
	return tool.ToolSchema{
		Type: "object",
		Properties: map[string]tool.PropertyDef{
			"package": {Type: "string", Description: "Package to test"},
		},
		Required: []string{"package"},
	}
}

func (b *BackendTestTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	return "Tests passed: 45/45 tests passed. Coverage: 87%.", nil
}

// DatabaseMigrationTool simulates database migrations
type DatabaseMigrationTool struct{}

func (d *DatabaseMigrationTool) Name() string        { return "run_migration" }
func (d *DatabaseMigrationTool) Description() string { return "Run database migration" }

func (d *DatabaseMigrationTool) InputSchema() tool.ToolSchema {
	return tool.ToolSchema{
		Type: "object",
		Properties: map[string]tool.PropertyDef{
			"migration": {Type: "string", Description: "Migration name or command"},
		},
		Required: []string{"migration"},
	}
}

func (d *DatabaseMigrationTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	return "Migration completed: Applied 3 pending migrations. Schema up to date.", nil
}

// DesignReviewTool simulates design review
type DesignReviewTool struct{}

func (d *DesignReviewTool) Name() string        { return "review_design" }
func (d *DesignReviewTool) Description() string { return "Review UI/UX design" }

func (d *DesignReviewTool) InputSchema() tool.ToolSchema {
	return tool.ToolSchema{
		Type: "object",
		Properties: map[string]tool.PropertyDef{
			"component": {Type: "string", Description: "Component to review"},
		},
		Required: []string{"component"},
	}
}

func (d *DesignReviewTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	return "Design review: Accessibility score 94/100. Color contrast meets WCAG 2.1 AA.", nil
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
		Name:   "hierarchy-demo",
	})
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}

	// ==========================================================
	// REGISTER TOOLS FIRST (agents will reference them)
	// ==========================================================

	fmt.Println("Registering tools...")

	if err := client.RegisterTool(&FrontendLintTool{}); err != nil {
		log.Fatalf("Failed to register lint_frontend tool: %v", err)
	}
	if err := client.RegisterTool(&BackendTestTool{}); err != nil {
		log.Fatalf("Failed to register run_tests tool: %v", err)
	}
	if err := client.RegisterTool(&DatabaseMigrationTool{}); err != nil {
		log.Fatalf("Failed to register run_migration tool: %v", err)
	}
	if err := client.RegisterTool(&DesignReviewTool{}); err != nil {
		log.Fatalf("Failed to register review_design tool: %v", err)
	}

	// ==========================================================
	// START THE CLIENT (before creating agents)
	// ==========================================================

	if err := client.Start(ctx); err != nil {
		log.Fatalf("Failed to start client: %v", err)
	}
	defer func() {
		if err := client.Stop(context.Background()); err != nil {
			log.Printf("Error stopping client: %v", err)
		}
	}()

	maxTokensWorker := 1024
	maxTokensLead := 1536
	maxTokensManager := 2048

	// ==========================================================
	// LEVEL 3: Worker Agents (with their specialized tools)
	// ==========================================================

	fmt.Println("Creating agents...")

	// Frontend Developer
	frontendDev, err := client.CreateAgent(ctx, &agentpg.AgentDefinition{
		Name:        "frontend-developer",
		Description: "Frontend developer specialist for React/TypeScript",
		Model:       "claude-sonnet-4-5-20250929",
		SystemPrompt: `You are a frontend developer specialist. You:
1. Work with React, TypeScript, and CSS
2. Use lint_frontend tool to check code quality
3. Focus on UI components and user experience
4. Report status clearly to your team lead`,
		Tools:     []string{"lint_frontend"},
		MaxTokens: &maxTokensWorker,
	})
	if err != nil {
		log.Fatalf("Failed to create frontend-developer: %v", err)
	}

	// Backend Developer
	backendDev, err := client.CreateAgent(ctx, &agentpg.AgentDefinition{
		Name:        "backend-developer",
		Description: "Backend developer specialist for Go/APIs",
		Model:       "claude-sonnet-4-5-20250929",
		SystemPrompt: `You are a backend developer specialist. You:
1. Work with Go, APIs, and databases
2. Use run_tests tool to verify code
3. Focus on performance and reliability
4. Report status clearly to your team lead`,
		Tools:     []string{"run_tests"},
		MaxTokens: &maxTokensWorker,
	})
	if err != nil {
		log.Fatalf("Failed to create backend-developer: %v", err)
	}

	// Database Specialist
	dbSpecialist, err := client.CreateAgent(ctx, &agentpg.AgentDefinition{
		Name:        "database-specialist",
		Description: "Database specialist for schemas and migrations",
		Model:       "claude-sonnet-4-5-20250929",
		SystemPrompt: `You are a database specialist. You:
1. Manage database schemas and migrations
2. Use run_migration tool for schema changes
3. Optimize queries and indexes
4. Report status clearly to your team lead`,
		Tools:     []string{"run_migration"},
		MaxTokens: &maxTokensWorker,
	})
	if err != nil {
		log.Fatalf("Failed to create database-specialist: %v", err)
	}

	// UX Designer
	uxDesigner, err := client.CreateAgent(ctx, &agentpg.AgentDefinition{
		Name:        "ux-designer",
		Description: "UX designer for accessibility and design",
		Model:       "claude-sonnet-4-5-20250929",
		SystemPrompt: `You are a UX designer. You:
1. Create and review UI designs
2. Use review_design tool for accessibility checks
3. Focus on user experience and accessibility
4. Report status clearly to your team lead`,
		Tools:     []string{"review_design"},
		MaxTokens: &maxTokensWorker,
	})
	if err != nil {
		log.Fatalf("Failed to create ux-designer: %v", err)
	}

	// ==========================================================
	// LEVEL 2: Team Lead Agents (delegate to Level 3 workers)
	// ==========================================================

	// Engineering Lead - delegates to frontend, backend, and database specialists
	engineeringLead, err := client.CreateAgent(ctx, &agentpg.AgentDefinition{
		Name:        "engineering-lead",
		Description: "Engineering team lead coordinating technical work",
		Model:       "claude-sonnet-4-5-20250929",
		SystemPrompt: `You are the Engineering Team Lead. You:
1. Coordinate frontend, backend, and database work
2. Delegate technical tasks to appropriate specialists
3. Synthesize status updates from your team
4. Report technical progress to the project manager

Your team members:
- Frontend Developer: React/TypeScript specialist
- Backend Developer: Go/API specialist
- Database Specialist: Schema/migration specialist`,
		AgentIDs:  []uuid.UUID{frontendDev.ID, backendDev.ID, dbSpecialist.ID},
		MaxTokens: &maxTokensLead,
	})
	if err != nil {
		log.Fatalf("Failed to create engineering-lead: %v", err)
	}

	// Design Lead - delegates to UX designer
	designLead, err := client.CreateAgent(ctx, &agentpg.AgentDefinition{
		Name:        "design-lead",
		Description: "Design team lead coordinating UX work",
		Model:       "claude-sonnet-4-5-20250929",
		SystemPrompt: `You are the Design Team Lead. You:
1. Coordinate design and UX work
2. Delegate design tasks to your team
3. Ensure accessibility and user experience standards
4. Report design progress to the project manager

Your team:
- UX Designer: Accessibility and design specialist`,
		AgentIDs:  []uuid.UUID{uxDesigner.ID},
		MaxTokens: &maxTokensLead,
	})
	if err != nil {
		log.Fatalf("Failed to create design-lead: %v", err)
	}

	// ==========================================================
	// LEVEL 1: Project Manager (delegates to Level 2 leads)
	// ==========================================================

	projectManager, err := client.CreateAgent(ctx, &agentpg.AgentDefinition{
		Name:        "project-manager",
		Description: "Project manager coordinating all teams",
		Model:       "claude-sonnet-4-5-20250929",
		SystemPrompt: `You are the Project Manager. You:
1. Coordinate the overall project progress
2. Delegate to team leads based on the request type
3. Synthesize updates from all teams
4. Provide clear, executive summaries

Your direct reports:
- Engineering Lead: Manages frontend, backend, and database teams
- Design Lead: Manages UX and design team

When given a task:
1. Break it down into engineering and design components
2. Delegate appropriately to team leads
3. Synthesize their reports into a cohesive update`,
		AgentIDs:  []uuid.UUID{engineeringLead.ID, designLead.ID},
		MaxTokens: &maxTokensManager,
	})
	if err != nil {
		log.Fatalf("Failed to create project-manager: %v", err)
	}

	log.Printf("Client started (instance ID: %s)", client.InstanceID())

	// ==========================================================
	// Display hierarchy
	// ==========================================================

	fmt.Println("\n=== Agent Hierarchy ===")
	fmt.Println(`Level 1 (Project Manager)
    └── Engineering Lead (Level 2)
    │       ├── Frontend Developer (Level 3) [lint_frontend]
    │       ├── Backend Developer (Level 3)  [run_tests]
    │       └── Database Specialist (Level 3) [run_migration]
    └── Design Lead (Level 2)
            └── UX Designer (Level 3)        [review_design]`)
	fmt.Println()

	// Create session
	sessionID, err := client.NewSession(ctx, nil, map[string]any{
		"description": "Multi-level hierarchy demonstration",
	})
	if err != nil {
		log.Fatalf("Failed to create session: %v", err)
	}

	fmt.Printf("Created session: %s\n\n", sessionID)

	// ==========================================================
	// Example 1: Full project status
	// ==========================================================
	fmt.Println("=== Example 1: Full Project Status ===")
	response1, err := client.RunFastSync(ctx, sessionID, projectManager.ID,
		"I need a complete status update on the user authentication feature. Check with engineering on code quality and tests, and with design on the login page accessibility.")
	if err != nil {
		log.Fatalf("Failed to run agent: %v", err)
	}

	if response1.Message != nil {
		for _, block := range response1.Message.Content {
			if block.Type == agentpg.ContentTypeText {
				fmt.Println(block.Text)
			}
		}
	} else {
		fmt.Println(response1.Text)
	}

	// ==========================================================
	// Example 2: Engineering-focused request
	// ==========================================================
	fmt.Println("\n=== Example 2: Engineering Focus ===")
	response2, err := client.RunFastSync(ctx, sessionID, projectManager.ID,
		"We need to deploy a database migration and ensure all backend tests pass. Please coordinate with the engineering team.")
	if err != nil {
		log.Fatalf("Failed to run agent: %v", err)
	}

	if response2.Message != nil {
		for _, block := range response2.Message.Content {
			if block.Type == agentpg.ContentTypeText {
				fmt.Println(block.Text)
			}
		}
	} else {
		fmt.Println(response2.Text)
	}

	// ==========================================================
	// Example 3: Design-focused request
	// ==========================================================
	fmt.Println("\n=== Example 3: Design Focus ===")
	response3, err := client.RunFastSync(ctx, sessionID, projectManager.ID,
		"Please have the design team review the new dashboard component for accessibility compliance.")
	if err != nil {
		log.Fatalf("Failed to run agent: %v", err)
	}

	if response3.Message != nil {
		for _, block := range response3.Message.Content {
			if block.Type == agentpg.ContentTypeText {
				fmt.Println(block.Text)
			}
		}
	} else {
		fmt.Println(response3.Text)
	}

	// Print token usage
	fmt.Println("\n=== Token Usage (Last Response) ===")
	fmt.Printf("Input tokens: %d\n", response3.Usage.InputTokens)
	fmt.Printf("Output tokens: %d\n", response3.Usage.OutputTokens)

	fmt.Println("\n=== Demo Complete ===")
}
