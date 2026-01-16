// Package main demonstrates the Client API with extended context support.
//
// This example shows:
// - Using the 1M token extended context window
// - Configuring extended context via AgentDefinition.Config
// - Processing very long documents
// - Per-client agent registration
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/youssefsiam38/agentpg"
	"github.com/youssefsiam38/agentpg/driver/pgxv5"
)

// generateLongDocument creates a simulated long document for testing
func generateLongDocument(sections int) string {
	var sb strings.Builder

	sb.WriteString("# Comprehensive Technical Documentation\n\n")
	sb.WriteString("## Introduction\n\n")
	sb.WriteString("This document provides comprehensive coverage of our system architecture, ")
	sb.WriteString("implementation details, and best practices. It is designed to serve as ")
	sb.WriteString("the definitive reference for developers and architects working with our platform.\n\n")

	topics := []string{
		"System Architecture",
		"Database Design",
		"API Specifications",
		"Security Guidelines",
		"Performance Optimization",
		"Deployment Procedures",
		"Monitoring and Alerting",
		"Disaster Recovery",
		"Scaling Strategies",
		"Testing Frameworks",
	}

	for i := 0; i < sections; i++ {
		topicIndex := i % len(topics)
		topic := topics[topicIndex]

		sb.WriteString(fmt.Sprintf("## Section %d: %s\n\n", i+1, topic))

		// Add substantial content for each section
		for j := 0; j < 5; j++ {
			sb.WriteString(fmt.Sprintf("### %s - Part %d\n\n", topic, j+1))
			sb.WriteString(fmt.Sprintf("This section covers important aspects of %s. ", topic))
			sb.WriteString("We will discuss the theoretical foundations, practical implementations, ")
			sb.WriteString("common pitfalls, and best practices. Understanding these concepts is ")
			sb.WriteString("essential for building robust and maintainable systems.\n\n")

			// Add bullet points
			sb.WriteString("Key considerations include:\n\n")
			for k := 0; k < 5; k++ {
				sb.WriteString(fmt.Sprintf("- Point %d: Detailed explanation of concept %d in the context of %s. ",
					k+1, k+1, topic))
				sb.WriteString("This encompasses various sub-topics and related considerations ")
				sb.WriteString("that should be taken into account during implementation.\n")
			}
			sb.WriteString("\n")

			// Add code examples
			sb.WriteString("Example implementation:\n\n")
			sb.WriteString("```go\n")
			sb.WriteString(fmt.Sprintf("// %s implementation\n", topic))
			sb.WriteString(fmt.Sprintf("func Handle%s(ctx context.Context) error {\n", strings.ReplaceAll(topic, " ", "")))
			sb.WriteString("    // Initialize components\n")
			sb.WriteString("    // Process data\n")
			sb.WriteString("    // Return results\n")
			sb.WriteString("    return nil\n")
			sb.WriteString("}\n")
			sb.WriteString("```\n\n")
		}
	}

	sb.WriteString("## Conclusion\n\n")
	sb.WriteString("This documentation provides a comprehensive overview of our system. ")
	sb.WriteString("For additional information, please refer to the appendices and ")
	sb.WriteString("supplementary materials.\n")

	return sb.String()
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

	// ==========================================================
	// Create client with extended context support
	// ==========================================================

	fmt.Println("=== Extended Context (1M Token) Example ===")
	fmt.Println()

	// Create the AgentPG client
	client, err := agentpg.NewClient(drv, &agentpg.ClientConfig{
		APIKey: apiKey,
	})
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}

	// Register agent with extended context enabled
	maxTokens := 4096
	if err := client.RegisterAgent(&agentpg.AgentDefinition{
		Name:         "extended-context-demo",
		Description:  "Document analysis with extended context support",
		Model:        "claude-sonnet-4-5-20250929",
		SystemPrompt: "You are a document analysis assistant. You can process very long documents and answer questions about them accurately.",
		MaxTokens:    &maxTokens,
		Config: map[string]any{
			"extended_context": true,  // Enable 1M token support
			"auto_compaction":  false, // Disable compaction - rely on extended context
		},
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

	fmt.Printf("Client started (instance ID: %s)\n", client.InstanceID())
	fmt.Println()

	fmt.Println("Configuration:")
	fmt.Println("- Extended context: ENABLED")
	fmt.Println("- Auto-compaction: DISABLED (relying on 1M context)")
	fmt.Println("- Max output tokens: 4096")
	fmt.Println()

	// Create session
	sessionID, err := client.NewSession(ctx, nil, map[string]any{
		"description": "Extended context demonstration",
	})
	if err != nil {
		log.Fatalf("Failed to create session: %v", err)
	}

	fmt.Printf("Created session: %s\n\n", sessionID)

	// ==========================================================
	// Generate and process a long document
	// ==========================================================

	fmt.Println("=== Processing Long Document ===")
	fmt.Println()

	// Generate a document (adjust sections for desired length)
	// Each section is roughly 2-3KB, so 20 sections ~ 50KB of text
	document := generateLongDocument(20)

	fmt.Printf("Generated document: %d characters (~%d tokens estimated)\n",
		len(document), len(document)/4)

	// Submit the document for analysis
	prompt := fmt.Sprintf(`I'm providing you with a comprehensive technical document. Please analyze it and be ready to answer questions.

Here is the document:

%s

Please confirm you've received the document and provide a brief summary of its structure and main topics.`, document)

	fmt.Println("\nSubmitting document for analysis...")

	response1, err := client.RunFastSync(ctx, sessionID, "extended-context-demo", prompt)
	if err != nil {
		log.Fatalf("Failed to run agent: %v", err)
	}

	fmt.Println("\nAgent response:")
	for _, block := range response1.Message.Content {
		if block.Type == agentpg.ContentTypeText {
			text := block.Text
			if len(text) > 500 {
				text = text[:500] + "..."
			}
			fmt.Println(text)
		}
	}

	fmt.Printf("\nTokens - Input: %d, Output: %d\n",
		response1.Usage.InputTokens,
		response1.Usage.OutputTokens)

	// ==========================================================
	// Ask follow-up questions about the document
	// ==========================================================

	fmt.Println()
	fmt.Println("=== Follow-up Questions ===")
	fmt.Println()

	questions := []string{
		"What are the main sections covered in the document?",
		"Can you explain the key points about System Architecture?",
		"What security guidelines are mentioned in the document?",
	}

	for i, question := range questions {
		fmt.Printf("Question %d: %s\n", i+1, question)

		response, err := client.RunFastSync(ctx, sessionID, "extended-context-demo", question)
		if err != nil {
			log.Fatalf("Failed to run agent: %v", err)
		}

		for _, block := range response.Message.Content {
			if block.Type == agentpg.ContentTypeText {
				text := block.Text
				if len(text) > 300 {
					text = text[:300] + "..."
				}
				fmt.Printf("Answer: %s\n", text)
			}
		}

		fmt.Printf("Tokens - Input: %d, Output: %d\n\n",
			response.Usage.InputTokens,
			response.Usage.OutputTokens)
	}

	// ==========================================================
	// Show how extended context handles retries
	// ==========================================================

	fmt.Println("=== Extended Context Features ===")
	fmt.Println()
	fmt.Println("When extended_context is enabled via Config:")
	fmt.Println()
	fmt.Println("1. AUTOMATIC FALLBACK:")
	fmt.Println("   If the API returns a max_tokens error, AgentPG")
	fmt.Println("   automatically retries with the extended context header.")
	fmt.Println()
	fmt.Println("2. BETA HEADER INJECTION:")
	fmt.Println("   Adds 'anthropic-beta: context-1m-2025-08-07' header")
	fmt.Println("   to enable 1M token context window.")
	fmt.Println()
	fmt.Println("3. SIMPLE CONFIGURATION:")
	fmt.Println("   Just add extended_context: true to your agent's Config")
	fmt.Println("   in the AgentDefinition - everything else is handled automatically.")

	// ==========================================================
	// When to use extended context vs compaction
	// ==========================================================

	fmt.Println()
	fmt.Println("=== Extended Context vs Compaction ===")
	fmt.Println()

	fmt.Println("Use EXTENDED CONTEXT when:")
	fmt.Println("  - Processing very long documents")
	fmt.Println("  - Need to reference all previous context")
	fmt.Println("  - Cost is less of a concern")
	fmt.Println("  - Context window is the limiting factor")
	fmt.Println()
	fmt.Println("Use COMPACTION when:")
	fmt.Println("  - Long-running conversations")
	fmt.Println("  - Cost optimization is important")
	fmt.Println("  - Older context can be summarized")
	fmt.Println("  - Want to stay within standard context limits")
	fmt.Println()
	fmt.Println("You can also use BOTH:")
	fmt.Println("  - Enable extended context as a fallback")
	fmt.Println("  - Use compaction to manage long conversations")
	fmt.Println("  - Extended context kicks in if compaction isn't enough")

	fmt.Println("\n=== Demo Complete ===")
}
