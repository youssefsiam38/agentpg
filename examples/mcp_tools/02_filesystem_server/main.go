// Package main demonstrates MCP tool integration with the Filesystem server.
//
// This example shows:
// - Filesystem MCP server for reading/writing files
// - Passing allowed directories as command arguments
// - Agent performing file operations via MCP tools
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

	// Use /tmp/agentpg-mcp-demo as the sandbox directory
	workDir := "/tmp/agentpg-mcp-demo"
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
	// Register the Filesystem MCP server
	// The allowed directory is passed as a command argument
	// ==========================================================
	mcpServer, err := agentmcp.RegisterServer(ctx, client, &agentmcp.MCPServerConfig{
		Name: "fs",
		Stdio: &agentmcp.StdioTransportConfig{
			Command: "npx",
			Args:    []string{"-y", "@modelcontextprotocol/server-filesystem", workDir},
		},
	})
	if err != nil {
		log.Fatalf("Failed to register MCP server: %v", err)
	}
	defer mcpServer.Close()

	// Print discovered tools
	fmt.Println("=== Discovered Filesystem Tools ===")
	for _, t := range mcpServer.Tools() {
		fmt.Printf("  %s - %s\n", t.Name(), t.Description())
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

	// Collect all discovered tool names for the agent
	var toolNames []string
	for _, t := range mcpServer.Tools() {
		toolNames = append(toolNames, t.Name())
	}

	maxTokens := 1024
	agent, err := client.GetOrCreateAgent(ctx, &agentpg.AgentDefinition{
		Name:         "mcp-filesystem-demo",
		Model:        "claude-sonnet-4-5-20250929",
		SystemPrompt: fmt.Sprintf("You are a file management assistant. Use the available filesystem tools to read, write, and manage files. The allowed directory is %s.", workDir),
		Tools:        toolNames,
		MaxTokens:    &maxTokens,
	})
	if err != nil {
		log.Fatalf("Failed to create agent: %v", err)
	}

	sessionID, err := client.NewSession(ctx, nil, map[string]any{
		"description": "MCP Filesystem server demo",
	})
	if err != nil {
		log.Fatalf("Failed to create session: %v", err)
	}

	// Example 1: Write a file
	fmt.Println("=== Example 1: Write a File ===")
	response1, err := client.RunFastSync(ctx, sessionID, agent.ID,
		fmt.Sprintf("Write a file called 'hello.txt' in %s with the content 'Hello from AgentPG + MCP!'", workDir), nil)
	if err != nil {
		log.Fatalf("Failed to run agent: %v", err)
	}

	for _, block := range response1.Message.Content {
		if block.Type == agentpg.ContentTypeText {
			fmt.Println(block.Text)
		}
	}

	// Example 2: Read the file back
	fmt.Println("\n=== Example 2: Read the File ===")
	response2, err := client.RunFastSync(ctx, sessionID, agent.ID,
		fmt.Sprintf("Read the file 'hello.txt' from %s and tell me what it says.", workDir), nil)
	if err != nil {
		log.Fatalf("Failed to run agent: %v", err)
	}

	for _, block := range response2.Message.Content {
		if block.Type == agentpg.ContentTypeText {
			fmt.Println(block.Text)
		}
	}

	// Example 3: List directory contents
	fmt.Println("\n=== Example 3: List Directory ===")
	response3, err := client.RunFastSync(ctx, sessionID, agent.ID,
		fmt.Sprintf("List all files in %s.", workDir), nil)
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
