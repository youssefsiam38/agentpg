// Package agentpg provides an event-driven framework for building async AI agents
// using PostgreSQL for state management and distribution.
//
// AgentPG uses PostgreSQL LISTEN/NOTIFY for real-time events with polling fallback,
// supports multi-level nested agents (agents as tools for other agents), and provides
// a transaction-first API for atomic operations.
//
// Key features:
//   - Per-client registration (no global state)
//   - Claude Batch API integration with automatic polling
//   - Multi-level agent hierarchies (PM → Lead → Worker pattern)
//   - Race-safe distributed workers using SELECT FOR UPDATE SKIP LOCKED
//   - Transaction-first architecture (RunTx accepts user transactions)
//
// Example usage:
//
//	pool, _ := pgxpool.New(ctx, databaseURL)
//	drv := pgxv5.New(pool)
//
//	client, _ := agentpg.NewClient(drv, &agentpg.ClientConfig{
//	    APIKey: os.Getenv("ANTHROPIC_API_KEY"),
//	    Name:   "my-worker",
//	})
//
//	// Register agents on this client instance (no global state)
//	client.RegisterAgent(&agentpg.AgentDefinition{
//	    Name:         "assistant",
//	    Model:        "claude-sonnet-4-5-20250929",
//	    SystemPrompt: "You are a helpful assistant.",
//	})
//
//	client.RegisterTool(&MyTool{})
//
//	client.Start(ctx)
//	defer client.Stop(context.Background())
//
//	sessionID, _ := client.NewSession(ctx, "tenant-1", "user-1", nil, nil)
//	runID, _ := client.Run(ctx, sessionID, "assistant", "Hello!")
//	response, _ := client.WaitForRun(ctx, runID)
package agentpg

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/youssefsiam38/agentpg/driver"
	"github.com/youssefsiam38/agentpg/tool"
)

// =============================================================================
// CLIENT CONFIGURATION
// =============================================================================

// ClientConfig holds configuration for the AgentPG client.
type ClientConfig struct {
	// APIKey is the Anthropic API key (required).
	// If not set, falls back to ANTHROPIC_API_KEY environment variable.
	APIKey string

	// Name is the name of this service instance (optional).
	// Used for instance identification in the database.
	// Defaults to hostname-based name.
	Name string

	// ID is the unique identifier for this client instance (optional).
	// If not set, a UUID will be generated.
	// Must be unique across all running instances.
	ID string

	// MaxConcurrentRuns is the maximum number of runs this instance will process
	// concurrently. Defaults to 10.
	MaxConcurrentRuns int

	// MaxConcurrentTools is the maximum number of tool executions this instance
	// will process concurrently. Defaults to 50.
	MaxConcurrentTools int

	// BatchPollInterval is how often to poll Claude Batch API for status updates.
	// Defaults to 30 seconds.
	BatchPollInterval time.Duration

	// RunPollInterval is how often to poll for new runs when LISTEN/NOTIFY
	// is unavailable. Defaults to 1 second.
	RunPollInterval time.Duration

	// ToolPollInterval is how often to poll for pending tool executions.
	// Defaults to 500 milliseconds.
	ToolPollInterval time.Duration

	// HeartbeatInterval is how often this instance sends heartbeats.
	// Defaults to 15 seconds.
	HeartbeatInterval time.Duration

	// LeaderTTL is how long a leader election lease lasts.
	// Defaults to 30 seconds.
	LeaderTTL time.Duration

	// StuckRunTimeout is how long a run can be claimed before it's considered stuck.
	// Defaults to 5 minutes.
	StuckRunTimeout time.Duration

	// Logger is an optional logger. If nil, logs are discarded.
	Logger Logger
}

// Logger interface for structured logging.
type Logger interface {
	Debug(msg string, args ...any)
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
}

// setDefaults applies default values to the config.
func (c *ClientConfig) setDefaults() {
	if c.APIKey == "" {
		c.APIKey = os.Getenv("ANTHROPIC_API_KEY")
	}
	if c.Name == "" {
		hostname, _ := os.Hostname()
		if hostname == "" {
			hostname = "agentpg"
		}
		c.Name = hostname
	}
	if c.ID == "" {
		c.ID = uuid.New().String()
	}
	if c.MaxConcurrentRuns <= 0 {
		c.MaxConcurrentRuns = 10
	}
	if c.MaxConcurrentTools <= 0 {
		c.MaxConcurrentTools = 50
	}
	if c.BatchPollInterval <= 0 {
		c.BatchPollInterval = 30 * time.Second
	}
	if c.RunPollInterval <= 0 {
		c.RunPollInterval = 1 * time.Second
	}
	if c.ToolPollInterval <= 0 {
		c.ToolPollInterval = 500 * time.Millisecond
	}
	if c.HeartbeatInterval <= 0 {
		c.HeartbeatInterval = 15 * time.Second
	}
	if c.LeaderTTL <= 0 {
		c.LeaderTTL = 30 * time.Second
	}
	if c.StuckRunTimeout <= 0 {
		c.StuckRunTimeout = 5 * time.Minute
	}
}

// validate validates the configuration.
func (c *ClientConfig) validate() error {
	if c.APIKey == "" {
		return fmt.Errorf("%w: API key is required (set APIKey or ANTHROPIC_API_KEY)", ErrInvalidConfig)
	}
	return nil
}

// =============================================================================
// AGENT DEFINITION
// =============================================================================

// AgentDefinition defines an agent's configuration.
// Register agents with Client.RegisterAgent().
type AgentDefinition struct {
	// Name is the unique identifier for this agent (required).
	Name string

	// Description is a human-readable description of this agent.
	// Used when this agent is registered as a tool for another agent.
	Description string

	// Model is the Claude model to use (required).
	// Examples: "claude-sonnet-4-5-20250929", "claude-opus-4-5-20251101"
	Model string

	// SystemPrompt is the system prompt for this agent.
	SystemPrompt string

	// Tools is the list of tool names this agent can use.
	// Only tools listed here will be available to the agent.
	// Must reference tools registered via client.RegisterTool().
	Tools []string

	// Agents is the list of agent names this agent can delegate to.
	// Listed agents become available as tools to this agent.
	// Enables multi-level agent hierarchies (PM → Lead → Worker pattern).
	//
	// Example:
	//   // Engineering Lead can delegate to specialists
	//   Agents: []string{"frontend-developer", "backend-developer"}
	Agents []string

	// MaxTokens is the maximum tokens to generate per response.
	// If nil, uses model default.
	MaxTokens *int

	// Temperature controls randomness (0.0 to 1.0).
	// If nil, uses model default.
	Temperature *float64

	// TopK limits token selection to top K options.
	// If nil, uses model default.
	TopK *int

	// TopP (nucleus sampling) limits cumulative probability.
	// If nil, uses model default.
	TopP *float64

	// Config holds additional configuration as JSON.
	// Examples: auto_compaction, compaction_trigger, extended_context
	Config map[string]any
}

// validate validates the agent definition.
func (d *AgentDefinition) validate() error {
	if d.Name == "" {
		return fmt.Errorf("%w: agent name is required", ErrInvalidConfig)
	}
	if d.Model == "" {
		return fmt.Errorf("%w: agent model is required", ErrInvalidConfig)
	}
	return nil
}

// =============================================================================
// CLIENT
// =============================================================================

// Client is the main entry point for AgentPG.
// It manages agents, tools, sessions, and runs with per-client registration.
//
// Client is safe for concurrent use.
type Client[TTx any] struct {
	mu sync.RWMutex

	// Configuration
	config *ClientConfig
	driver driver.Driver[TTx]

	// Registry (per-client, no global state)
	agents map[string]*AgentDefinition
	tools  map[string]tool.Tool

	// Runtime state
	started    bool
	instanceID string
	stopCh     chan struct{}
	wg         sync.WaitGroup
}

// NewClient creates a new AgentPG client.
//
// The driver parameter determines the database backend (pgxv5 or database/sql).
// Configuration is optional; defaults are applied for omitted values.
//
// Example:
//
//	pool, _ := pgxpool.New(ctx, databaseURL)
//	drv := pgxv5.New(pool)
//	client, err := agentpg.NewClient(drv, nil) // uses defaults
func NewClient[TTx any](drv driver.Driver[TTx], cfg *ClientConfig) (*Client[TTx], error) {
	if drv == nil {
		return nil, fmt.Errorf("%w: driver is required", ErrInvalidConfig)
	}

	if cfg == nil {
		cfg = &ClientConfig{}
	}
	cfg.setDefaults()

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return &Client[TTx]{
		config:     cfg,
		driver:     drv,
		agents:     make(map[string]*AgentDefinition),
		tools:      make(map[string]tool.Tool),
		instanceID: cfg.ID,
	}, nil
}

// =============================================================================
// REGISTRATION METHODS
// =============================================================================

// RegisterAgent registers an agent definition with this client.
// Must be called before Start().
//
// Returns an error if the agent name is already registered or if the
// definition is invalid.
func (c *Client[TTx]) RegisterAgent(def *AgentDefinition) error {
	if def == nil {
		return fmt.Errorf("%w: agent definition is required", ErrInvalidConfig)
	}
	if err := def.validate(); err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.started {
		return fmt.Errorf("%w: cannot register agents after Start()", ErrClientAlreadyStarted)
	}

	if _, exists := c.agents[def.Name]; exists {
		return fmt.Errorf("%w: agent %q already registered", ErrInvalidConfig, def.Name)
	}

	c.agents[def.Name] = def
	return nil
}

// RegisterTool registers a tool with this client.
// Must be called before Start().
//
// Tools are available to all agents that include the tool name in their
// tool_names configuration.
func (c *Client[TTx]) RegisterTool(t tool.Tool) error {
	if t == nil {
		return fmt.Errorf("%w: tool is required", ErrInvalidConfig)
	}

	// Validate tool schema
	schema := t.InputSchema()
	if schema.Type != "object" {
		return fmt.Errorf("%w: tool %q schema type must be 'object'", ErrInvalidToolSchema, t.Name())
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.started {
		return fmt.Errorf("%w: cannot register tools after Start()", ErrClientAlreadyStarted)
	}

	if _, exists := c.tools[t.Name()]; exists {
		return fmt.Errorf("%w: tool %q already registered", ErrInvalidConfig, t.Name())
	}

	c.tools[t.Name()] = t
	return nil
}

// validateAgentReferences validates that all tools and agents referenced
// by agent definitions are registered. Called during Start().
func (c *Client[TTx]) validateAgentReferences() error {
	for agentName, def := range c.agents {
		// Validate tool references
		for _, toolName := range def.Tools {
			if _, exists := c.tools[toolName]; !exists {
				return fmt.Errorf("%w: agent %q references unregistered tool %q",
					ErrToolNotFound, agentName, toolName)
			}
		}

		// Validate agent references (delegate agents)
		for _, delegateName := range def.Agents {
			if _, exists := c.agents[delegateName]; !exists {
				return fmt.Errorf("%w: agent %q references unregistered agent %q",
					ErrAgentNotFound, agentName, delegateName)
			}
			// Prevent self-reference
			if delegateName == agentName {
				return fmt.Errorf("%w: agent %q cannot delegate to itself",
					ErrInvalidConfig, agentName)
			}
		}
	}
	return nil
}

// =============================================================================
// LIFECYCLE METHODS
// =============================================================================

// Start starts the client's background workers.
// This registers the instance in the database and begins processing runs.
//
// Start must be called before Run(), RunTx(), WaitForRun(), or other
// operational methods.
func (c *Client[TTx]) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.started {
		return ErrClientAlreadyStarted
	}

	// Validate all agent references (tools and delegate agents)
	if err := c.validateAgentReferences(); err != nil {
		return err
	}

	// TODO: Implement worker startup
	// 1. Register instance in database
	// 2. Register agents and tools in database (upsert)
	// 3. Start heartbeat worker
	// 4. Start leader election
	// 5. Start run worker (claims and processes runs)
	// 6. Start tool worker (claims and executes tools)
	// 7. Start batch poller (polls Claude Batch API)
	// 8. Start LISTEN/NOTIFY handler

	c.started = true
	c.stopCh = make(chan struct{})

	if c.config.Logger != nil {
		c.config.Logger.Info("client started",
			"instance_id", c.instanceID,
			"name", c.config.Name,
			"agents", len(c.agents),
			"tools", len(c.tools),
		)
	}

	return nil
}

// Stop gracefully shuts down the client.
// It waits for in-progress work to complete before returning.
//
// The context can be used to set a deadline for shutdown.
func (c *Client[TTx]) Stop(ctx context.Context) error {
	c.mu.Lock()
	if !c.started {
		c.mu.Unlock()
		return ErrClientNotStarted
	}
	c.started = false
	close(c.stopCh)
	c.mu.Unlock()

	// Wait for workers to finish
	done := make(chan struct{})
	go func() {
		c.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Clean shutdown
	case <-ctx.Done():
		return ctx.Err()
	}

	// TODO: Implement cleanup
	// 1. Deregister instance from database
	// 2. Release any held work

	if c.config.Logger != nil {
		c.config.Logger.Info("client stopped", "instance_id", c.instanceID)
	}

	return nil
}

// InstanceID returns the unique identifier for this client instance.
func (c *Client[TTx]) InstanceID() string {
	return c.instanceID
}

// =============================================================================
// SESSION METHODS
// =============================================================================

// NewSession creates a new conversation session.
//
// Parameters:
//   - tenantID: Multi-tenant isolation key (required for queries)
//   - identifier: User-provided identifier (unique within tenant)
//   - parentSessionID: Optional parent session for nested agents
//   - metadata: Optional arbitrary metadata
//
// Returns the session UUID.
func (c *Client[TTx]) NewSession(
	ctx context.Context,
	tenantID string,
	identifier string,
	parentSessionID *uuid.UUID,
	metadata map[string]any,
) (uuid.UUID, error) {
	c.mu.RLock()
	if !c.started {
		c.mu.RUnlock()
		return uuid.Nil, ErrClientNotStarted
	}
	c.mu.RUnlock()

	// TODO: Implement session creation via driver
	// 1. Calculate depth from parent
	// 2. Insert session row
	// 3. Return session ID

	return uuid.New(), nil
}

// NewSessionTx creates a new session within an existing transaction.
// This allows atomic session creation as part of a larger operation.
func (c *Client[TTx]) NewSessionTx(
	ctx context.Context,
	tx TTx,
	tenantID string,
	identifier string,
	parentSessionID *uuid.UUID,
	metadata map[string]any,
) (uuid.UUID, error) {
	c.mu.RLock()
	if !c.started {
		c.mu.RUnlock()
		return uuid.Nil, ErrClientNotStarted
	}
	c.mu.RUnlock()

	// TODO: Implement session creation within transaction

	return uuid.New(), nil
}

// =============================================================================
// RUN METHODS
// =============================================================================

// Run submits a new agent run for async processing.
//
// The run is created in 'pending' state and will be picked up by a worker
// (potentially on a different instance). Use WaitForRun() to wait for completion,
// or RunSync() for synchronous execution.
//
// Parameters:
//   - sessionID: The session to run within
//   - agentName: The agent to execute
//   - prompt: The user prompt
//
// Returns the run UUID.
func (c *Client[TTx]) Run(
	ctx context.Context,
	sessionID uuid.UUID,
	agentName string,
	prompt string,
) (uuid.UUID, error) {
	c.mu.RLock()
	if !c.started {
		c.mu.RUnlock()
		return uuid.Nil, ErrClientNotStarted
	}
	c.mu.RUnlock()

	// TODO: Implement run creation
	// 1. Validate agent exists
	// 2. Create user message in session
	// 3. Insert run row with state='pending'
	// 4. NOTIFY workers

	return uuid.New(), nil
}

// RunTx submits a new agent run within an existing transaction.
//
// This is the transaction-first API: the run is created atomically with
// whatever other operations are in the transaction. The run won't be
// visible to workers until the transaction commits.
//
// IMPORTANT: Do not use WaitForRun() inside the same transaction as it
// will deadlock (the run won't be visible until commit).
func (c *Client[TTx]) RunTx(
	ctx context.Context,
	tx TTx,
	sessionID uuid.UUID,
	agentName string,
	prompt string,
) (uuid.UUID, error) {
	c.mu.RLock()
	if !c.started {
		c.mu.RUnlock()
		return uuid.Nil, ErrClientNotStarted
	}
	c.mu.RUnlock()

	// TODO: Implement run creation within transaction

	return uuid.New(), nil
}

// WaitForRun waits for a run to reach a terminal state (completed, failed, cancelled).
//
// Returns the final response when the run completes successfully.
// Returns an error if the run fails or is cancelled.
//
// The context can be used to set a timeout for waiting.
func (c *Client[TTx]) WaitForRun(ctx context.Context, runID uuid.UUID) (*Response, error) {
	c.mu.RLock()
	if !c.started {
		c.mu.RUnlock()
		return nil, ErrClientNotStarted
	}
	c.mu.RUnlock()

	// TODO: Implement run waiting
	// 1. Subscribe to run state changes (LISTEN or poll)
	// 2. Wait for terminal state
	// 3. Return response or error

	return &Response{}, nil
}

// RunSync is a convenience wrapper that calls Run() followed by WaitForRun().
//
// This provides a synchronous interface for simple use cases.
// For more control, use Run() and WaitForRun() separately.
//
// NOTE: There is intentionally no RunSyncTx - calling RunTx followed by
// WaitForRun in the same transaction would deadlock.
func (c *Client[TTx]) RunSync(
	ctx context.Context,
	sessionID uuid.UUID,
	agentName string,
	prompt string,
) (*Response, error) {
	runID, err := c.Run(ctx, sessionID, agentName, prompt)
	if err != nil {
		return nil, err
	}
	return c.WaitForRun(ctx, runID)
}

// =============================================================================
// QUERY METHODS
// =============================================================================

// GetRun retrieves the current state of a run.
func (c *Client[TTx]) GetRun(ctx context.Context, runID uuid.UUID) (*Run, error) {
	c.mu.RLock()
	if !c.started {
		c.mu.RUnlock()
		return nil, ErrClientNotStarted
	}
	c.mu.RUnlock()

	// TODO: Implement

	return nil, ErrRunNotFound
}

// GetSession retrieves a session by ID.
func (c *Client[TTx]) GetSession(ctx context.Context, sessionID uuid.UUID) (*Session, error) {
	c.mu.RLock()
	if !c.started {
		c.mu.RUnlock()
		return nil, ErrClientNotStarted
	}
	c.mu.RUnlock()

	// TODO: Implement

	return nil, ErrSessionNotFound
}

// =============================================================================
// RESPONSE TYPES
// =============================================================================

// Response represents the result of a completed run.
type Response struct {
	// Text is the final text response from the agent.
	Text string

	// StopReason indicates why the run stopped.
	// Values: "end_turn", "max_tokens", "tool_use"
	StopReason string

	// Usage contains token usage statistics.
	Usage Usage

	// Message is the full message with all content blocks.
	Message *Message

	// IterationCount is how many batch API calls were made.
	IterationCount int

	// ToolIterations is how many iterations involved tool use.
	ToolIterations int
}

// Usage contains token usage statistics.
type Usage struct {
	InputTokens              int
	OutputTokens             int
	CacheCreationInputTokens int
	CacheReadInputTokens     int
}

// Run represents an agent run.
type Run struct {
	ID                     uuid.UUID
	SessionID              uuid.UUID
	AgentName              string
	State                  RunState
	ParentRunID            *uuid.UUID
	ParentToolExecutionID  *uuid.UUID
	Depth                  int
	Prompt                 string
	ResponseText           string
	StopReason             string
	CurrentIteration       int
	IterationCount         int
	ToolIterations         int
	InputTokens            int
	OutputTokens           int
	CacheCreationTokens    int
	CacheReadTokens        int
	ErrorMessage           string
	ErrorType              string
	CreatedByInstanceID    string
	ClaimedByInstanceID    string
	ClaimedAt              *time.Time
	Metadata               map[string]any
	CreatedAt              time.Time
	StartedAt              *time.Time
	FinalizedAt            *time.Time
}

// RunState represents the state of a run.
type RunState string

const (
	RunStatePending         RunState = "pending"
	RunStateBatchSubmitting RunState = "batch_submitting"
	RunStateBatchPending    RunState = "batch_pending"
	RunStateBatchProcessing RunState = "batch_processing"
	RunStatePendingTools    RunState = "pending_tools"
	RunStateAwaitingInput   RunState = "awaiting_input"
	RunStateCompleted       RunState = "completed"
	RunStateCancelled       RunState = "cancelled"
	RunStateFailed          RunState = "failed"
)

// Session represents a conversation session.
type Session struct {
	ID              uuid.UUID
	TenantID        string
	Identifier      string
	ParentSessionID *uuid.UUID
	Depth           int
	Metadata        map[string]any
	CompactionCount int
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// Message represents a conversation message.
type Message struct {
	ID          uuid.UUID
	SessionID   uuid.UUID
	RunID       *uuid.UUID
	Role        MessageRole
	Content     []ContentBlock
	Usage       map[string]any
	IsPreserved bool
	IsSummary   bool
	Metadata    map[string]any
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// MessageRole represents the role of a message.
type MessageRole string

const (
	MessageRoleUser      MessageRole = "user"
	MessageRoleAssistant MessageRole = "assistant"
	MessageRoleSystem    MessageRole = "system"
)

// ContentBlock represents a block of content within a message.
type ContentBlock struct {
	Type               ContentType
	Text               string
	ToolUseID          string
	ToolName           string
	ToolInput          map[string]any
	ToolResultForUseID string
	ToolContent        string
	IsError            bool
	Source             map[string]any
	SearchResults      []map[string]any
	Metadata           map[string]any
}

// ContentType represents the type of a content block.
type ContentType string

const (
	ContentTypeText            ContentType = "text"
	ContentTypeToolUse         ContentType = "tool_use"
	ContentTypeToolResult      ContentType = "tool_result"
	ContentTypeImage           ContentType = "image"
	ContentTypeDocument        ContentType = "document"
	ContentTypeThinking        ContentType = "thinking"
	ContentTypeServerToolUse   ContentType = "server_tool_use"
	ContentTypeWebSearchResult ContentType = "web_search_result"
)
