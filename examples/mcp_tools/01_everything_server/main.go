// Package main demonstrates MCP tool integration using the Everything test server.
//
// This example shows:
// - Connecting to an MCP server via stdio (npx subprocess)
// - Automatic tool discovery and registration
// - Agent using MCP tools (echo, get-sum) transparently
//
// Prerequisites: Node.js/npm installed (for npx)
// No authentication required.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/youssefsiam38/agentpg"
	"github.com/youssefsiam38/agentpg/driver/pgxv5"
	agentmcp "github.com/youssefsiam38/agentpg/mcp"
)

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
	// Register the MCP "Everything" test server via stdio
	// This server exposes tools like echo, get-sum, etc.
	// ToolFilter limits registration to only the tools we need.
	// ==========================================================
	mcpServer, err := agentmcp.RegisterServer(ctx, client, &agentmcp.MCPServerConfig{
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
		log.Fatalf("Failed to register MCP server: %v", err)
	}
	defer mcpServer.Close()

	// Print discovered tools
	fmt.Println("=== Discovered MCP Tools ===")
	for _, t := range mcpServer.Tools() {
		fmt.Printf("  %s - %s\n", t.Name(), t.Description())
	}
	fmt.Println()

	// Start client (syncs all tools — local + MCP — to database)
	if err := client.Start(ctx); err != nil {
		log.Fatalf("Failed to start client: %v", err)
	}
	defer func() {
		if err := client.Stop(context.Background()); err != nil {
			log.Printf("Error stopping client: %v", err)
		}
	}()

	// Create agent that uses the discovered MCP tools
	mcpTools := mcpServer.Tools()
	toolNames := make([]string, 0, len(mcpTools))
	for _, t := range mcpTools {
		toolNames = append(toolNames, t.Name())
	}

	maxTokens := 1024
	agent, err := client.GetOrCreateAgent(ctx, &agentpg.AgentDefinition{
		Name:         "mcp-everything-demo",
		Model:        "claude-sonnet-4-5-20250929",
		SystemPrompt: "You are a helpful assistant. Use the available tools to answer questions. When asked to echo something, use the echo tool. When asked to add numbers, use the get-sum tool.",
		Tools:        toolNames,
		MaxTokens:    &maxTokens,
	})
	if err != nil {
		log.Fatalf("Failed to create agent: %v", err)
	}

	sessionID, err := client.NewSession(ctx, nil, map[string]any{
		"description": "MCP Everything server demo",
	})
	if err != nil {
		log.Fatalf("Failed to create session: %v", err)
	}

	// Example 1: Use the echo tool
	fmt.Println("=== Example 1: Echo Tool ===")
	response1, err := client.RunFastSync(ctx, sessionID, agent.ID, "Please echo the message 'Hello from MCP!'", nil)
	if err != nil {
		log.Fatalf("Failed to run agent: %v", err)
	}

	for _, block := range response1.Message.Content {
		if block.Type == agentpg.ContentTypeText {
			fmt.Println(block.Text)
		}
	}

	// Example 2: Use the get-sum tool
	fmt.Println("\n=== Example 2: Get-Sum Tool ===")
	response2, err := client.RunFastSync(ctx, sessionID, agent.ID, "What is 42 + 58? Use the get-sum tool.", nil)
	if err != nil {
		log.Fatalf("Failed to run agent: %v", err)
	}

	for _, block := range response2.Message.Content {
		if block.Type == agentpg.ContentTypeText {
			fmt.Println(block.Text)
		}
	}

	fmt.Println("\n=== Demo Complete ===")
}
