// Package main demonstrates MCP tool integration with the GitHub server.
//
// This example shows:
// - GitHub MCP server for repository, issue, and PR operations
// - Authentication via environment variables (StdioTransportConfig.Env)
// - ToolFilter to expose only specific tools
// - Agent interacting with GitHub via MCP
//
// Prerequisites:
// - Node.js/npm installed (for npx)
// - GITHUB_TOKEN environment variable with a personal access token
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

	githubToken := os.Getenv("GITHUB_TOKEN")
	if githubToken == "" {
		log.Fatal("GITHUB_TOKEN environment variable is required (GitHub personal access token)")
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
	// Register the GitHub MCP server with auth via Env
	// The GITHUB_PERSONAL_ACCESS_TOKEN is passed as an env var
	// to the subprocess. ToolFilter limits which tools are exposed.
	// ==========================================================
	mcpServer, err := agentmcp.RegisterServer(ctx, client, &agentmcp.MCPServerConfig{
		Name: "github",
		Stdio: &agentmcp.StdioTransportConfig{
			Command: "npx",
			Args:    []string{"-y", "@modelcontextprotocol/server-github"},
			Env:     []string{"GITHUB_PERSONAL_ACCESS_TOKEN=" + githubToken},
		},
		// Only expose read-only tools for this demo
		ToolFilter: func(name string) bool {
			switch name {
			case "search_repositories", "get_file_contents", "list_issues",
				"search_code", "search_issues", "search_users":
				return true
			default:
				return false
			}
		},
	})
	if err != nil {
		log.Fatalf("Failed to register MCP server: %v", err)
	}
	defer mcpServer.Close()

	// Print discovered tools (filtered)
	fmt.Println("=== Discovered GitHub Tools (filtered) ===")
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

	// Collect all discovered tool names
	mcpTools := mcpServer.Tools()
	toolNames := make([]string, 0, len(mcpTools))
	for _, t := range mcpTools {
		toolNames = append(toolNames, t.Name())
	}

	maxTokens := 2048
	agent, err := client.GetOrCreateAgent(ctx, &agentpg.AgentDefinition{
		Name:         "mcp-github-demo",
		Model:        "claude-sonnet-4-5-20250929",
		SystemPrompt: "You are a GitHub assistant. Use the available GitHub tools to search repositories, browse code, and find issues. Always be concise in your responses.",
		Tools:        toolNames,
		MaxTokens:    &maxTokens,
	})
	if err != nil {
		log.Fatalf("Failed to create agent: %v", err)
	}

	sessionID, err := client.NewSession(ctx, nil, map[string]any{
		"description": "MCP GitHub server demo",
	})
	if err != nil {
		log.Fatalf("Failed to create session: %v", err)
	}

	// Example 1: Search for repositories
	fmt.Println("=== Example 1: Search Repositories ===")
	response1, err := client.RunFastSync(ctx, sessionID, agent.ID,
		"Search for the top 3 most starred Go MCP client libraries on GitHub.", nil)
	if err != nil {
		log.Fatalf("Failed to run agent: %v", err)
	}

	for _, block := range response1.Message.Content {
		if block.Type == agentpg.ContentTypeText {
			fmt.Println(block.Text)
		}
	}

	// Example 2: Browse repository contents
	fmt.Println("\n=== Example 2: Browse Repository ===")
	response2, err := client.RunFastSync(ctx, sessionID, agent.ID,
		"Look at the README.md file in the modelcontextprotocol/go-sdk repository. Summarize it in 2 sentences.", nil)
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
