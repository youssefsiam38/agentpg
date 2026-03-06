// Package main demonstrates combining multiple MCP servers with local tools.
//
// This example shows:
// - Multiple MCP servers registered on the same client
// - Local tools alongside MCP tools
// - Tool namespacing preventing collisions (everything__echo vs fs__read_file)
// - Agent using tools from different sources transparently
//
// Prerequisites: Node.js/npm installed (for npx)
// No authentication required.
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
	agentmcp "github.com/youssefsiam38/agentpg/mcp"
	"github.com/youssefsiam38/agentpg/tool"
)

// TimeTool is a simple local tool that returns the current time.
type TimeTool struct{}

func (t *TimeTool) Name() string        { return "current_time" }
func (t *TimeTool) Description() string { return "Get the current date and time" }
func (t *TimeTool) InputSchema() tool.ToolSchema {
	return tool.ToolSchema{
		Type:       "object",
		Properties: map[string]tool.PropertyDef{},
	}
}

func (t *TimeTool) Execute(_ context.Context, _ json.RawMessage) (string, error) {
	return time.Now().Format("Monday, January 2, 2006 at 3:04 PM MST"), nil
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		log.Fatal("ANTHROPIC_API_KEY environment variable is required")
	}

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Fatal("DATABASE_URL environment variable is required")
	}

	workDir := "/tmp/agentpg-mcp-multi-demo"
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		log.Fatalf("Failed to create work directory: %v", err)
	}

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer pool.Close()

	drv := pgxv5.New(pool)

	client, err := agentpg.NewClient(drv, &agentpg.ClientConfig{
		APIKey: apiKey,
	})
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}

	// ==========================================================
	// Step 1: Register a local tool (before Start, as usual)
	// ==========================================================
	if err := client.RegisterTool(&TimeTool{}); err != nil {
		log.Fatalf("Failed to register local tool: %v", err)
	}

	// ==========================================================
	// Step 2: Register MCP "Everything" test server
	// Tools will be prefixed: everything__echo, everything__get-sum, etc.
	// ==========================================================
	everythingServer, err := agentmcp.RegisterServer(ctx, client, &agentmcp.MCPServerConfig{
		Name: "everything",
		Stdio: &agentmcp.StdioTransportConfig{
			Command: "npx",
			Args:    []string{"-y", "@modelcontextprotocol/server-everything"},
		},
		ToolFilter: func(name string) bool {
			return name == "echo" || name == "get-sum"
		},
	})
	if err != nil {
		log.Fatalf("Failed to register Everything server: %v", err)
	}
	defer everythingServer.Close()

	// ==========================================================
	// Step 3: Register MCP "Filesystem" server
	// Tools will be prefixed: fs__read_file, fs__write_file, etc.
	// ==========================================================
	fsServer, err := agentmcp.RegisterServer(ctx, client, &agentmcp.MCPServerConfig{
		Name: "fs",
		Stdio: &agentmcp.StdioTransportConfig{
			Command: "npx",
			Args:    []string{"-y", "@modelcontextprotocol/server-filesystem", workDir},
		},
	})
	if err != nil {
		log.Fatalf("Failed to register Filesystem server: %v", err)
	}
	defer fsServer.Close()

	// Print all registered tools (local + MCP)
	fmt.Println("=== All Registered Tools ===")
	fmt.Println("  [local] current_time")
	for _, t := range everythingServer.Tools() {
		fmt.Printf("  [everything] %s\n", t.Name())
	}
	for _, t := range fsServer.Tools() {
		fmt.Printf("  [filesystem] %s\n", t.Name())
	}
	fmt.Println()

	if err := client.Start(ctx); err != nil {
		log.Fatalf("Failed to start client: %v", err)
	}
	defer func() {
		if err := client.Stop(context.Background()); err != nil {
			log.Printf("Error stopping client: %v", err)
		}
	}()

	// Collect all tool names from all sources
	allTools := make([]string, 0, 1+len(everythingServer.Tools())+len(fsServer.Tools()))
	allTools = append(allTools, "current_time") // local tool
	for _, t := range everythingServer.Tools() {
		allTools = append(allTools, t.Name())
	}
	for _, t := range fsServer.Tools() {
		allTools = append(allTools, t.Name())
	}

	maxTokens := 2048
	agent, err := client.GetOrCreateAgent(ctx, &agentpg.AgentDefinition{
		Name:  "mcp-multi-server-demo",
		Model: "claude-sonnet-4-5-20250929",
		SystemPrompt: fmt.Sprintf(`You are a versatile assistant with access to multiple tool sources:
- Time tool: get the current date/time
- Echo/Sum tools: echo messages and sum numbers
- Filesystem tools: read and write files in %s
Use the appropriate tool for each task.`, workDir),
		Tools:     allTools,
		MaxTokens: &maxTokens,
	})
	if err != nil {
		log.Fatalf("Failed to create agent: %v", err)
	}

	sessionID, err := client.NewSession(ctx, nil, map[string]any{
		"description": "Multi-server MCP demo",
	})
	if err != nil {
		log.Fatalf("Failed to create session: %v", err)
	}

	// Example: Use tools from all three sources in one conversation
	fmt.Println("=== Multi-Source Tool Usage ===")
	response, err := client.RunFastSync(ctx, sessionID, agent.ID,
		fmt.Sprintf("Do these three things: 1) Tell me the current time using the current_time tool, 2) Echo 'Multi-server works!' using the echo tool, 3) Write a file called 'status.txt' in %s with the text 'All systems operational'. Report each result.", workDir), nil)
	if err != nil {
		log.Fatalf("Failed to run agent: %v", err)
	}

	for _, block := range response.Message.Content {
		if block.Type == agentpg.ContentTypeText {
			fmt.Println(block.Text)
		}
	}

	fmt.Printf("\nTool iterations used: %d\n", response.ToolIterations)
	fmt.Println("\n=== Demo Complete ===")
}
