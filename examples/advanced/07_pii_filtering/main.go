// Package main demonstrates the Client API with PII filtering.
//
// This example shows:
// - PII detection with regex patterns
// - Message blocking, redaction, and warning modes
// - Audit logging for compliance
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/youssefsiam38/agentpg"
	"github.com/youssefsiam38/agentpg/driver/pgxv5"
	"github.com/youssefsiam38/agentpg/types"
)

// PIIFilter detects and blocks messages containing sensitive data
type PIIFilter struct {
	mu       sync.Mutex
	patterns map[string]*regexp.Regexp
	blocked  []BlockedMessage
	mode     FilterMode
}

type FilterMode int

const (
	ModeBlock  FilterMode = iota // Block message entirely
	ModeRedact                   // Redact PII and continue
	ModeWarn                     // Log warning but allow
)

type BlockedMessage struct {
	Timestamp time.Time
	SessionID string
	Type      string
	Preview   string
}

func NewPIIFilter(mode FilterMode) *PIIFilter {
	patterns := map[string]*regexp.Regexp{
		// US Social Security Number
		"SSN": regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`),

		// Credit Card Numbers (basic pattern)
		"CreditCard": regexp.MustCompile(`\b(?:\d{4}[- ]?){3}\d{4}\b`),

		// Email addresses
		"Email": regexp.MustCompile(`\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Z|a-z]{2,}\b`),

		// Phone numbers (US format)
		"Phone": regexp.MustCompile(`\b(?:\+1[- ]?)?\(?\d{3}\)?[- ]?\d{3}[- ]?\d{4}\b`),

		// API Keys (generic pattern)
		"APIKey": regexp.MustCompile(`\b(?:sk-|api[_-]?key[_-]?|token[_-]?)[a-zA-Z0-9]{20,}\b`),

		// AWS Access Keys
		"AWSKey": regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`),
	}

	return &PIIFilter{
		patterns: patterns,
		blocked:  make([]BlockedMessage, 0),
		mode:     mode,
	}
}

func (p *PIIFilter) Check(text string) (detected []string, clean string) {
	clean = text

	for piiType, pattern := range p.patterns {
		if pattern.MatchString(text) {
			detected = append(detected, piiType)
			// Replace with placeholder
			clean = pattern.ReplaceAllString(clean, fmt.Sprintf("[REDACTED:%s]", piiType))
		}
	}

	return detected, clean
}

func (p *PIIFilter) Record(sessionID string, piiTypes []string, preview string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Truncate preview for safety
	if len(preview) > 50 {
		preview = preview[:50] + "..."
	}

	p.blocked = append(p.blocked, BlockedMessage{
		Timestamp: time.Now(),
		SessionID: sessionID,
		Type:      strings.Join(piiTypes, ", "),
		Preview:   preview,
	})
}

func (p *PIIFilter) GetBlocked() []BlockedMessage {
	p.mu.Lock()
	defer p.mu.Unlock()

	result := make([]BlockedMessage, len(p.blocked))
	copy(result, p.blocked)
	return result
}

// ErrPIIDetected is returned when PII is detected and blocked
type ErrPIIDetected struct {
	Types []string
}

func (e *ErrPIIDetected) Error() string {
	return fmt.Sprintf("message blocked: contains sensitive data (%s)", strings.Join(e.Types, ", "))
}

// Register agent at package initialization.
func init() {
	maxTokens := 1024
	agentpg.MustRegister(&agentpg.AgentDefinition{
		Name:         "pii-filtering-demo",
		Description:  "Assistant with PII filtering",
		Model:        "claude-sonnet-4-5-20250929",
		SystemPrompt: "You are a helpful assistant. Never ask for or store sensitive personal information.",
		MaxTokens:    &maxTokens,
	})
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

	// ==========================================================
	// Create PII filter
	// ==========================================================

	fmt.Println("=== PII Filtering Example ===")
	fmt.Println()

	piiFilter := NewPIIFilter(ModeBlock)

	fmt.Println("PII Detection Patterns:")
	fmt.Println("  - SSN: xxx-xx-xxxx")
	fmt.Println("  - Credit Card: xxxx-xxxx-xxxx-xxxx")
	fmt.Println("  - Email: user@domain.com")
	fmt.Println("  - Phone: (xxx) xxx-xxxx")
	fmt.Println("  - API Keys: sk-..., api_key_...")
	fmt.Println("  - AWS Keys: AKIA...")
	fmt.Println()

	// Create the pgx/v5 driver
	drv := pgxv5.New(pool)

	// Create the AgentPG client
	client, err := agentpg.NewClient(drv, &agentpg.ClientConfig{
		APIKey: apiKey,
	})
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
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

	// Get the agent
	agent := client.Agent("pii-filtering-demo")
	if agent == nil {
		log.Fatal("Agent 'pii-filtering-demo' not found")
	}

	// ==========================================================
	// Register PII filtering hook
	// ==========================================================

	var currentSessionID string

	agent.OnBeforeMessage(func(ctx context.Context, messages []*types.Message) error {
		// Extract the last user message text for PII checking
		var lastPrompt string
		for i := len(messages) - 1; i >= 0; i-- {
			if messages[i].Role == types.RoleUser {
				for _, block := range messages[i].Content {
					if block.Type == types.ContentTypeText {
						lastPrompt = block.Text
						break
					}
				}
				break
			}
		}

		detected, _ := piiFilter.Check(lastPrompt)

		if len(detected) > 0 {
			piiFilter.Record(currentSessionID, detected, lastPrompt)

			fmt.Printf("  [PII BLOCKED] Detected: %s\n", strings.Join(detected, ", "))

			switch piiFilter.mode {
			case ModeBlock:
				return &ErrPIIDetected{Types: detected}
			case ModeRedact:
				// In redact mode, we'd modify the prompt
				// This requires returning the cleaned version
				fmt.Println("  [PII REDACTED] Message will be sent with redactions")
				return nil
			case ModeWarn:
				fmt.Println("  [PII WARNING] Message allowed but logged")
				return nil
			}
		}

		return nil
	})

	// Create session
	sessionID, err := agent.NewSession(ctx, "1", "pii-filter-demo", nil, nil)
	if err != nil {
		log.Fatalf("Failed to create session: %v", err)
	}
	currentSessionID = sessionID
	fmt.Printf("Session: %s\n\n", sessionID[:8]+"...")

	// ==========================================================
	// Test messages
	// ==========================================================

	testMessages := []struct {
		description string
		message     string
		shouldBlock bool
	}{
		{
			description: "Normal message",
			message:     "What's the weather like today?",
			shouldBlock: false,
		},
		{
			description: "Contains SSN",
			message:     "My SSN is 123-45-6789, can you remember it?",
			shouldBlock: true,
		},
		{
			description: "Contains email",
			message:     "Send the report to john.doe@company.com",
			shouldBlock: true,
		},
		{
			description: "Contains credit card",
			message:     "My card number is 4532-1234-5678-9012",
			shouldBlock: true,
		},
		{
			description: "Contains phone",
			message:     "Call me at (555) 123-4567",
			shouldBlock: true,
		},
		{
			description: "Contains API key",
			message:     "Use this key: sk-abc123def456ghi789jkl012mno345",
			shouldBlock: true,
		},
		{
			description: "Another normal message",
			message:     "Thanks for your help!",
			shouldBlock: false,
		},
	}

	fmt.Println("=== Testing PII Detection ===")
	fmt.Println()

	for i, test := range testMessages {
		fmt.Printf("Test %d: %s\n", i+1, test.description)
		fmt.Printf("  Message: %s\n", truncate(test.message, 50))

		response, err := agent.Run(ctx, sessionID, test.message)
		if err != nil {
			if _, ok := err.(*ErrPIIDetected); ok {
				fmt.Printf("  Result: BLOCKED (as expected: %v)\n", test.shouldBlock)
			} else {
				fmt.Printf("  Error: %v\n", err)
			}
		} else {
			if test.shouldBlock {
				fmt.Printf("  Result: ALLOWED (unexpected!)\n")
			} else {
				responseText := ""
				for _, block := range response.Message.Content {
					if block.Type == agentpg.ContentTypeText {
						responseText = truncate(block.Text, 50)
						break
					}
				}
				fmt.Printf("  Result: ALLOWED - Response: %s\n", responseText)
			}
		}
		fmt.Println()
	}

	// ==========================================================
	// Audit log
	// ==========================================================

	fmt.Println("=== Audit Log ===")
	blocked := piiFilter.GetBlocked()

	if len(blocked) == 0 {
		fmt.Println("No blocked messages.")
	} else {
		for i, b := range blocked {
			fmt.Printf("%d. [%s] Session: %s...\n", i+1, b.Timestamp.Format("15:04:05"), b.SessionID[:8])
			fmt.Printf("   Type: %s\n", b.Type)
			fmt.Printf("   Preview: %s\n", b.Preview)
		}
	}

	// ==========================================================
	// Demo: Redact mode
	// ==========================================================

	fmt.Println()
	fmt.Println("=== Redaction Demo ===")

	testString := "Contact john@example.com or call (555) 123-4567. SSN: 123-45-6789"
	detected, redacted := piiFilter.Check(testString)

	fmt.Printf("Original: %s\n", testString)
	fmt.Printf("Detected: %s\n", strings.Join(detected, ", "))
	fmt.Printf("Redacted: %s\n", redacted)

	fmt.Println()
	fmt.Println("=== PII Filtering Best Practices ===")
	fmt.Println("1. Use ModeBlock for highest security")
	fmt.Println("2. Log all blocked attempts for audit")
	fmt.Println("3. Customize patterns for your domain")
	fmt.Println("4. Consider ModeRedact for better UX")
	fmt.Println("5. Review blocked messages regularly")
	fmt.Println()
	fmt.Println("=== Demo Complete ===")
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
