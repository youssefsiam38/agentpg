package agentpg

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/google/uuid"
	"github.com/youssefsiam38/agentpg/compaction"
	"github.com/youssefsiam38/agentpg/driver"
	"github.com/youssefsiam38/agentpg/internal/convert"
	"github.com/youssefsiam38/agentpg/leadership"
	"github.com/youssefsiam38/agentpg/maintenance"
	"github.com/youssefsiam38/agentpg/notifier"
	"github.com/youssefsiam38/agentpg/runstate"
	"github.com/youssefsiam38/agentpg/storage"
	"github.com/youssefsiam38/agentpg/tool"
	"github.com/youssefsiam38/agentpg/types"
	"github.com/youssefsiam38/agentpg/worker"
)

// Version is the current AgentPG version
const Version = "2.0.0"

// ClientConfig holds configuration for the Client.
type ClientConfig struct {
	// APIKey is the Anthropic API key (required if Client is not provided)
	APIKey string

	// Client is an existing Anthropic client (optional, takes precedence over APIKey)
	Client *anthropic.Client

	// InstanceID is a unique identifier for this client instance (optional)
	// If not provided, a UUID will be generated
	InstanceID string

	// Hostname is the hostname for this instance (optional)
	// If not provided, os.Hostname() is used
	Hostname string

	// Metadata is additional metadata to store with this instance
	Metadata map[string]any

	// HeartbeatInterval is how often to send heartbeats (optional)
	// Default: 30 seconds
	HeartbeatInterval time.Duration

	// CleanupInterval is how often to run cleanup operations when leader (optional)
	// Default: 1 minute
	CleanupInterval time.Duration

	// StuckRunTimeout is how long a run can be in "running" state before
	// it's considered stuck and will be marked as failed (optional)
	// Default: 1 hour
	StuckRunTimeout time.Duration

	// LeaderTTL is how long a leader's lease is valid (optional)
	// Default: 30 seconds
	LeaderTTL time.Duration

	// OnError is called when background operations fail
	OnError func(err error)

	// OnBecameLeader is called when this instance becomes the leader
	OnBecameLeader func()

	// OnLostLeadership is called when this instance loses leadership
	OnLostLeadership func()
}

// DefaultClientConfig returns the default client configuration.
func DefaultClientConfig() *ClientConfig {
	return &ClientConfig{
		HeartbeatInterval: maintenance.DefaultHeartbeatInterval,
		CleanupInterval:   maintenance.DefaultCleanupInterval,
		StuckRunTimeout:   maintenance.DefaultStuckRunTimeout,
		LeaderTTL:         leadership.DefaultLeaderTTL,
	}
}

// Client manages the lifecycle of AgentPG instances.
// It handles instance registration, heartbeats, leader election, and cleanup.
//
// TTx is the native transaction type from the driver (e.g., pgx.Tx, *sql.Tx).
type Client[TTx any] struct {
	driver          driver.Driver[TTx]
	store           storage.Store
	anthropicClient *anthropic.Client
	config          *ClientConfig
	instanceID      string

	// Tool registry for all registered tools
	toolRegistry *tool.Registry

	// Background services
	heartbeat *maintenance.Heartbeat
	cleanup   *maintenance.Cleanup
	elector   *leadership.Elector
	notif     *notifier.Notifier
	worker    *worker.Worker

	// State
	started  atomic.Bool
	isLeader atomic.Bool

	// Cancellation
	cancel context.CancelFunc
}

// NewClient creates a new AgentPG client with the given driver and configuration.
// The transaction type TTx is inferred from the driver argument.
//
// Example:
//
//	drv := pgxv5.New(pool)
//	client, err := agentpg.NewClient(drv, &agentpg.ClientConfig{
//	    APIKey: os.Getenv("ANTHROPIC_API_KEY"),
//	})
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer client.Stop(ctx)
//
//	if err := client.Start(ctx); err != nil {
//	    log.Fatal(err)
//	}
//
//	agent := client.Agent("chat")
//	response, err := agent.Run(ctx, sessionID, "Hello!")
func NewClient[TTx any](drv driver.Driver[TTx], config *ClientConfig) (*Client[TTx], error) {
	// Validate driver
	if drv == nil {
		return nil, fmt.Errorf("%w: driver is required", ErrInvalidConfig)
	}
	if !drv.PoolIsSet() {
		return nil, fmt.Errorf("%w: driver pool is not set", ErrInvalidConfig)
	}

	// Apply defaults
	if config == nil {
		config = DefaultClientConfig()
	} else {
		// Apply defaults for zero values
		if config.HeartbeatInterval == 0 {
			config.HeartbeatInterval = maintenance.DefaultHeartbeatInterval
		}
		if config.CleanupInterval == 0 {
			config.CleanupInterval = maintenance.DefaultCleanupInterval
		}
		if config.StuckRunTimeout == 0 {
			config.StuckRunTimeout = maintenance.DefaultStuckRunTimeout
		}
		if config.LeaderTTL == 0 {
			config.LeaderTTL = leadership.DefaultLeaderTTL
		}
	}

	// Create Anthropic client
	var anthropicClient *anthropic.Client
	if config.Client != nil {
		anthropicClient = config.Client
	} else if config.APIKey != "" {
		client := anthropic.NewClient()
		anthropicClient = &client
	} else {
		return nil, fmt.Errorf("%w: either APIKey or Client is required", ErrInvalidConfig)
	}

	// Generate instance ID if not provided
	instanceID := config.InstanceID
	if instanceID == "" {
		instanceID = uuid.New().String()
	}

	// Get hostname if not provided
	hostname := config.Hostname
	if hostname == "" {
		h, err := os.Hostname()
		if err != nil {
			hostname = "unknown"
		} else {
			hostname = h
		}
	}

	// Store the hostname in config for later use
	if config.Hostname == "" {
		config.Hostname = hostname
	}

	// Create tool registry
	toolRegistry := tool.NewRegistry()
	for _, name := range ListRegisteredTools() {
		t, _ := GetRegisteredTool(name)
		_ = toolRegistry.Register(t) // Ignore errors for duplicates
	}

	c := &Client[TTx]{
		driver:          drv,
		store:           drv.GetStore(),
		anthropicClient: anthropicClient,
		config:          config,
		instanceID:      instanceID,
		toolRegistry:    toolRegistry,
	}

	return c, nil
}

// Start begins background operations (heartbeat, leader election, cleanup).
// This registers the instance with the database and starts the heartbeat.
func (c *Client[TTx]) Start(ctx context.Context) error {
	if !c.started.CompareAndSwap(false, true) {
		return ErrClientAlreadyStarted
	}

	// Create cancellable context
	ctx, c.cancel = context.WithCancel(ctx)

	// Register instance
	if err := c.registerInstance(ctx); err != nil {
		c.started.Store(false)
		return fmt.Errorf("failed to register instance: %w", err)
	}

	// Register agents and tools with the database
	if err := c.registerAgentsAndTools(ctx); err != nil {
		c.started.Store(false)
		return fmt.Errorf("failed to register agents and tools: %w", err)
	}

	// Start heartbeat service
	c.heartbeat = maintenance.NewHeartbeat(c.store, c.instanceID, &maintenance.HeartbeatConfig{
		Interval: c.config.HeartbeatInterval,
		OnError:  c.config.OnError,
	})
	if err := c.heartbeat.Start(ctx); err != nil {
		c.started.Store(false)
		return fmt.Errorf("failed to start heartbeat: %w", err)
	}

	// Start leader election
	c.elector = leadership.NewElector(c.store, c.instanceID, &leadership.Config{
		LeaderTTL: c.config.LeaderTTL,
	}, leadership.Callbacks{
		OnBecameLeader:   c.onBecameLeader,
		OnLostLeadership: c.onLostLeadership,
	})
	if err := c.elector.Start(ctx); err != nil {
		_ = c.heartbeat.Stop(ctx) // best-effort cleanup
		c.started.Store(false)
		return fmt.Errorf("failed to start leader election: %w", err)
	}

	// Start notifier (if driver supports it)
	if c.driver.SupportsListener() || c.driver.SupportsNotify() {
		var getListener func(context.Context) (driver.Listener, error)
		if c.driver.SupportsListener() {
			getListener = c.driver.GetListener
		}

		c.notif = notifier.NewNotifier(getListener, c.driver.GetNotifier(), nil)
		if err := c.notif.Start(ctx); err != nil {
			_ = c.elector.Stop(ctx)   // best-effort cleanup
			_ = c.heartbeat.Stop(ctx) // best-effort cleanup
			c.started.Store(false)
			return fmt.Errorf("failed to start notifier: %w", err)
		}
	}

	// Start worker for async run processing
	c.worker = worker.New(c.store, c.anthropicClient, c.toolRegistry, c.notif, &worker.Config{
		InstanceID: c.instanceID,
		OnError:    c.config.OnError,
		OnRunComplete: func(runID string, state runstate.RunState) {
			// Notify via notifier if available
			if c.notif != nil && c.notif.IsRunning() {
				payload := `{"run_id":"` + runID + `","state":"` + string(state) + `"}`
				_ = c.notif.Notify(ctx, notifier.EventRunStateChanged, payload)
			}
		},
	})

	// Set up API call builder with global agent registry
	agentProvider := &globalAgentProvider{}
	apiBuilder := worker.NewAPICallBuilder(agentProvider, c.toolRegistry)
	c.worker.SetAPICallBuilder(apiBuilder)

	if err := c.worker.Start(ctx); err != nil {
		if c.notif != nil {
			_ = c.notif.Stop(ctx) // best-effort cleanup
		}
		_ = c.elector.Stop(ctx)   // best-effort cleanup
		_ = c.heartbeat.Stop(ctx) // best-effort cleanup
		c.started.Store(false)
		return fmt.Errorf("failed to start worker: %w", err)
	}

	return nil
}

// Stop gracefully shuts down the client.
// It stops all background services and deregisters the instance.
func (c *Client[TTx]) Stop(ctx context.Context) error {
	if !c.started.Load() {
		return ErrClientNotStarted
	}

	// Cancel background context
	if c.cancel != nil {
		c.cancel()
	}

	// Stop services in reverse order (best-effort, continue on errors)
	if c.worker != nil && c.worker.IsRunning() {
		_ = c.worker.Stop(ctx)
	}

	if c.cleanup != nil && c.cleanup.IsRunning() {
		_ = c.cleanup.Stop(ctx)
	}

	if c.notif != nil && c.notif.IsRunning() {
		_ = c.notif.Stop(ctx)
	}

	if c.elector != nil {
		_ = c.elector.Stop(ctx)
	}

	if c.heartbeat != nil {
		_ = c.heartbeat.Stop(ctx)
	}

	// Deregister instance (best effort)
	_ = c.store.DeregisterInstance(ctx, c.instanceID)

	c.started.Store(false)
	return nil
}

// InstanceID returns the unique identifier for this client instance.
func (c *Client[TTx]) InstanceID() string {
	return c.instanceID
}

// IsLeader returns true if this instance is currently the leader.
func (c *Client[TTx]) IsLeader() bool {
	return c.isLeader.Load()
}

// IsRunning returns true if the client is running.
func (c *Client[TTx]) IsRunning() bool {
	return c.started.Load()
}

// Store returns the storage interface for direct access.
func (c *Client[TTx]) Store() storage.Store {
	return c.store
}

// Driver returns the database driver.
func (c *Client[TTx]) Driver() driver.Driver[TTx] {
	return c.driver
}

// AnthropicClient returns the Anthropic API client.
func (c *Client[TTx]) AnthropicClient() *anthropic.Client {
	return c.anthropicClient
}

// Agent returns an agent handle for the given name.
// The agent must have been registered globally via Register() before calling this.
// Returns nil if no agent is registered with the given name.
func (c *Client[TTx]) Agent(name string) *AgentHandle[TTx] {
	def, ok := GetRegisteredAgent(name)
	if !ok {
		return nil
	}

	return &AgentHandle[TTx]{
		client:     c,
		definition: def,
	}
}

// registerInstance registers this instance with the database.
func (c *Client[TTx]) registerInstance(ctx context.Context) error {
	params := &storage.RegisterInstanceParams{
		ID:       c.instanceID,
		Hostname: c.config.Hostname,
		PID:      os.Getpid(),
		Version:  Version,
		Metadata: c.config.Metadata,
	}

	return c.store.RegisterInstance(ctx, params)
}

// registerAgentsAndTools registers all global agents and tools with the database.
func (c *Client[TTx]) registerAgentsAndTools(ctx context.Context) error {
	// Register agents
	for _, name := range ListRegisteredAgents() {
		def, _ := GetRegisteredAgent(name)

		params := &storage.RegisterAgentParams{
			Name:         def.Name,
			Description:  def.Description,
			Model:        def.Model,
			SystemPrompt: def.SystemPrompt,
			MaxTokens:    def.MaxTokens,
			Config:       def.Config,
		}

		// Convert *float32 to *float32 (they're the same type)
		if def.Temperature != nil {
			params.Temperature = def.Temperature
		}

		if err := c.store.RegisterAgent(ctx, params); err != nil {
			return fmt.Errorf("failed to register agent %q: %w", name, err)
		}

		// Link instance to agent
		if err := c.store.RegisterInstanceAgent(ctx, c.instanceID, name); err != nil {
			return fmt.Errorf("failed to link instance to agent %q: %w", name, err)
		}
	}

	// Register tools
	for _, name := range ListRegisteredTools() {
		t, _ := GetRegisteredTool(name)

		params := &storage.RegisterToolParams{
			Name:        t.Name(),
			Description: t.Description(),
			InputSchema: t.InputSchema().ToMap(),
		}

		if err := c.store.RegisterTool(ctx, params); err != nil {
			return fmt.Errorf("failed to register tool %q: %w", name, err)
		}

		// Link instance to tool
		if err := c.store.RegisterInstanceTool(ctx, c.instanceID, name); err != nil {
			return fmt.Errorf("failed to link instance to tool %q: %w", name, err)
		}
	}

	return nil
}

// onBecameLeader is called when this instance becomes the leader.
func (c *Client[TTx]) onBecameLeader(ctx context.Context) {
	c.isLeader.Store(true)

	// Start cleanup service
	c.cleanup = maintenance.NewCleanup(c.store, &maintenance.CleanupConfig{
		Interval:        c.config.CleanupInterval,
		StuckRunTimeout: c.config.StuckRunTimeout,
		OnError:         c.config.OnError,
	})
	if err := c.cleanup.Start(ctx); err != nil {
		if c.config.OnError != nil {
			c.config.OnError(fmt.Errorf("failed to start cleanup service: %w", err))
		}
	}

	// Call user callback
	if c.config.OnBecameLeader != nil {
		c.config.OnBecameLeader()
	}
}

// onLostLeadership is called when this instance loses leadership.
func (c *Client[TTx]) onLostLeadership(ctx context.Context) {
	c.isLeader.Store(false)

	// Stop cleanup service
	if c.cleanup != nil && c.cleanup.IsRunning() {
		if err := c.cleanup.Stop(ctx); err != nil {
			if c.config.OnError != nil {
				c.config.OnError(fmt.Errorf("failed to stop cleanup service: %w", err))
			}
		}
	}

	// Call user callback
	if c.config.OnLostLeadership != nil {
		c.config.OnLostLeadership()
	}
}

// AgentHandle is a lightweight handle to a registered agent.
// It provides methods to run the agent within a session.
type AgentHandle[TTx any] struct {
	client     *Client[TTx]
	definition *AgentDefinition

	// Hooks (thread-safe)
	hooksMu            sync.RWMutex
	beforeMessageHooks []func(ctx context.Context, messages []*types.Message) error
	afterMessageHooks  []func(ctx context.Context, response *types.Response) error
	toolCallHooks      []func(ctx context.Context, toolName string, input json.RawMessage, output string, err error) error
	beforeCompactHooks []func(ctx context.Context, sessionID string) error
	afterCompactHooks  []func(ctx context.Context, result *compaction.CompactionResult) error

	// Additional tools registered at runtime (thread-safe)
	toolsMu         sync.RWMutex
	additionalTools []tool.Tool
}

// Name returns the agent's name.
func (h *AgentHandle[TTx]) Name() string {
	return h.definition.Name
}

// Definition returns the agent's definition.
func (h *AgentHandle[TTx]) Definition() *AgentDefinition {
	return h.definition
}

// Run creates a new run for the agent with the given prompt and returns immediately.
// The run is processed asynchronously by the worker. Use WaitForRun() to block until completion.
//
// The run is tracked in the database with state transitions:
// - pending -> pending_api (worker claims run)
// - pending_api -> completed (on success)
// - pending_api -> pending_tools (when tools need execution)
// - pending_tools -> pending_api (when all tools complete)
// - * -> failed (on error)
// - * -> cancelled (on context cancellation)
//
// Returns the run ID immediately. The run will be processed by the worker.
func (h *AgentHandle[TTx]) Run(ctx context.Context, sessionID, prompt string) (string, error) {
	if !h.client.IsRunning() {
		return "", ErrClientNotStarted
	}

	// Check if there's already a transaction in context
	existingTx := driver.ExecutorFromContext(ctx)
	if existingTx != nil {
		// User provided a transaction - use it, they are responsible for commit
		return h.runInContext(ctx, sessionID, prompt)
	}

	// No transaction in context - create one for atomicity
	// The notification will only fire after commit, ensuring message is saved
	// before any worker can process the run.
	tx, err := h.client.driver.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to begin transaction: %w", err)
	}
	txCtx := driver.WithExecutor(ctx, tx)

	runID, err := h.runInContext(txCtx, sessionID, prompt)
	if err != nil {
		tx.Rollback(ctx)
		return "", err
	}

	// Commit - notification fires after this, ensuring message is saved
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("failed to commit transaction: %w", err)
	}

	return runID, nil
}

// runInContext performs the actual run creation within the given context.
// This allows the run to be part of an existing transaction if one is in context.
func (h *AgentHandle[TTx]) runInContext(ctx context.Context, sessionID, prompt string) (string, error) {
	// Create run record first to get the runID
	runID, err := h.client.store.CreateRun(ctx, &storage.CreateRunParams{
		SessionID:  sessionID,
		AgentName:  h.definition.Name,
		Prompt:     prompt,
		InstanceID: h.client.instanceID,
	})
	if err != nil {
		return "", fmt.Errorf("failed to create run: %w", err)
	}

	// Save the user message with the run_id
	userMsg := NewUserMessage(sessionID, prompt)
	storageMsg := convert.ToStorageMessage(userMsg)
	storageMsg.RunID = &runID // Link message to run
	if err := h.client.store.SaveMessage(ctx, storageMsg); err != nil {
		return "", fmt.Errorf("failed to save user message: %w", err)
	}

	// Save content blocks for the message
	if len(storageMsg.ContentBlocks) > 0 {
		if err := h.client.store.SaveContentBlocks(ctx, storageMsg.ContentBlocks); err != nil {
			return "", fmt.Errorf("failed to save content blocks: %w", err)
		}
	}

	return runID, nil
}

// RunSync executes the agent synchronously (blocks until completion).
// This is a convenience wrapper around Run() + WaitForRun().
func (h *AgentHandle[TTx]) RunSync(ctx context.Context, sessionID, prompt string) (*Response, error) {
	runID, err := h.Run(ctx, sessionID, prompt)
	if err != nil {
		return nil, err
	}
	return h.WaitForRun(ctx, runID)
}

// WaitForRun blocks until the run reaches a terminal state and returns the response.
// Returns the final response or an error if the run failed.
func (h *AgentHandle[TTx]) WaitForRun(ctx context.Context, runID string) (*Response, error) {
	if !h.client.IsRunning() {
		return nil, ErrClientNotStarted
	}

	// Poll until terminal state
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			run, err := h.client.store.GetRun(ctx, runID)
			if err != nil {
				return nil, fmt.Errorf("failed to get run: %w", err)
			}

			if run.State.IsTerminal() {
				return h.buildResponseFromRun(ctx, run)
			}
		}
	}
}

// GetRunStatus returns the current status of a run.
func (h *AgentHandle[TTx]) GetRunStatus(ctx context.Context, runID string) (*RunStatus, error) {
	if !h.client.IsRunning() {
		return nil, ErrClientNotStarted
	}

	run, err := h.client.store.GetRun(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("failed to get run: %w", err)
	}

	return &RunStatus{
		ID:             run.ID,
		State:          run.State,
		StopReason:     run.StopReason,
		ResponseText:   run.ResponseText,
		ErrorMessage:   run.ErrorMessage,
		InputTokens:    run.InputTokens,
		OutputTokens:   run.OutputTokens,
		IterationCount: run.IterationCount,
		CreatedAt:      run.StartedAt,
		FinalizedAt:    run.FinalizedAt,
	}, nil
}

// buildResponseFromRun builds a Response from a completed run.
func (h *AgentHandle[TTx]) buildResponseFromRun(ctx context.Context, run *storage.Run) (*Response, error) {
	// Check if run failed
	if run.State == runstate.RunStateFailed {
		errMsg := "run failed"
		if run.ErrorMessage != nil {
			errMsg = *run.ErrorMessage
		}
		return nil, fmt.Errorf("%s", errMsg)
	}

	if run.State == runstate.RunStateCancelled {
		return nil, fmt.Errorf("run was cancelled")
	}

	// Get the last assistant message for this run
	messages, err := h.client.store.GetRunMessages(ctx, run.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get run messages: %w", err)
	}

	// Find the last assistant message
	var lastAssistantMsg *storage.Message
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "assistant" {
			lastAssistantMsg = messages[i]
			break
		}
	}

	if lastAssistantMsg == nil {
		return nil, fmt.Errorf("no assistant message found for run")
	}

	// Load content blocks for the message
	contentBlocks, err := h.client.store.GetMessageContentBlocks(ctx, lastAssistantMsg.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get content blocks: %w", err)
	}
	lastAssistantMsg.ContentBlocks = contentBlocks

	// Convert to types.Message
	msg := convert.FromStorageMessage(lastAssistantMsg)

	// Build response
	response := &Response{
		RunID:   run.ID,
		Message: msg,
		Usage: &Usage{
			InputTokens:  run.InputTokens,
			OutputTokens: run.OutputTokens,
		},
	}

	if run.StopReason != nil {
		response.StopReason = *run.StopReason
	}

	return response, nil
}

// RunTx runs the agent with the given prompt within a user-managed transaction.
// The caller is responsible for committing or rolling back the transaction.
//
// This allows combining your own database operations with agent operations
// in a single atomic transaction:
//
//	tx, _ := pool.Begin(ctx)
//	defer tx.Rollback(ctx)
//
//	// Your business logic in the same transaction
//	tx.Exec(ctx, "INSERT INTO orders ...")
//
//	// Agent operations in the same transaction
//	response, err := agent.RunTx(ctx, tx, sessionID, "Process this order")
//	if err != nil {
//	    return err // Everything rolled back
//	}
//
//	tx.Commit(ctx)
func (h *AgentHandle[TTx]) RunTx(ctx context.Context, tx TTx, sessionID string, prompt string) (*types.Response, error) {
	if !h.client.IsRunning() {
		return nil, ErrClientNotStarted
	}

	// Create temporary agent with all hooks configured
	agent, err := h.createConfiguredAgent()
	if err != nil {
		return nil, err
	}

	// Load session
	if loadErr := agent.LoadSession(ctx, sessionID); loadErr != nil {
		return nil, loadErr
	}

	// Execute with the provided transaction
	return agent.RunTx(ctx, tx, prompt)
}

// NewSession creates a new session for this agent.
func (h *AgentHandle[TTx]) NewSession(ctx context.Context, tenantID, identifier string, parentSessionID *string, metadata map[string]any) (string, error) {
	if !h.client.IsRunning() {
		return "", ErrClientNotStarted
	}

	if metadata == nil {
		metadata = make(map[string]any)
	}

	// Add agent name to metadata
	metadata["agent_name"] = h.definition.Name

	return h.client.store.CreateSession(ctx, tenantID, identifier, parentSessionID, metadata)
}

// =============================================================================
// Hook Registration Methods
// =============================================================================

// OnBeforeMessage registers a hook called before sending messages to Claude.
func (h *AgentHandle[TTx]) OnBeforeMessage(hook func(ctx context.Context, messages []*types.Message) error) {
	h.hooksMu.Lock()
	defer h.hooksMu.Unlock()
	h.beforeMessageHooks = append(h.beforeMessageHooks, hook)
}

// OnAfterMessage registers a hook called after receiving a response from Claude.
func (h *AgentHandle[TTx]) OnAfterMessage(hook func(ctx context.Context, response *types.Response) error) {
	h.hooksMu.Lock()
	defer h.hooksMu.Unlock()
	h.afterMessageHooks = append(h.afterMessageHooks, hook)
}

// OnToolCall registers a hook called when a tool is executed.
func (h *AgentHandle[TTx]) OnToolCall(hook func(ctx context.Context, toolName string, input json.RawMessage, output string, err error) error) {
	h.hooksMu.Lock()
	defer h.hooksMu.Unlock()
	h.toolCallHooks = append(h.toolCallHooks, hook)
}

// OnBeforeCompaction registers a hook called before context compaction.
func (h *AgentHandle[TTx]) OnBeforeCompaction(hook func(ctx context.Context, sessionID string) error) {
	h.hooksMu.Lock()
	defer h.hooksMu.Unlock()
	h.beforeCompactHooks = append(h.beforeCompactHooks, hook)
}

// OnAfterCompaction registers a hook called after context compaction.
func (h *AgentHandle[TTx]) OnAfterCompaction(hook func(ctx context.Context, result *compaction.CompactionResult) error) {
	h.hooksMu.Lock()
	defer h.hooksMu.Unlock()
	h.afterCompactHooks = append(h.afterCompactHooks, hook)
}

// =============================================================================
// Tool Registration
// =============================================================================

// RegisterTool adds a tool to this agent at runtime.
// This is in addition to tools specified in AgentDefinition.Tools.
func (h *AgentHandle[TTx]) RegisterTool(t tool.Tool) error {
	if t == nil {
		return fmt.Errorf("%w: tool is nil", ErrInvalidConfig)
	}

	// Register in client's tool registry so the worker can execute it
	if err := h.client.toolRegistry.Register(t); err != nil {
		// Ignore duplicate registration errors
		if err.Error() != fmt.Sprintf("tool already registered: %s", t.Name()) {
			return fmt.Errorf("failed to register tool in client registry: %w", err)
		}
	}

	// Add tool name to the agent's definition so it's included in API calls
	if err := AddToolToAgent(h.definition.Name, t.Name()); err != nil {
		return fmt.Errorf("failed to add tool to agent: %w", err)
	}

	h.toolsMu.Lock()
	defer h.toolsMu.Unlock()
	h.additionalTools = append(h.additionalTools, t)
	return nil
}

// =============================================================================
// Session and Message Access
// =============================================================================

// GetSession returns session information.
func (h *AgentHandle[TTx]) GetSession(ctx context.Context, sessionID string) (*SessionInfo, error) {
	if !h.client.IsRunning() {
		return nil, ErrClientNotStarted
	}

	session, err := h.client.store.GetSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	// Get messages to calculate total tokens and message count
	messages, err := h.client.store.GetMessages(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	// Calculate total tokens
	totalTokens := 0
	for _, msg := range messages {
		if msg.Usage != nil {
			totalTokens += msg.Usage.TotalTokens()
		}
	}

	return &SessionInfo{
		ID:              session.ID,
		TenantID:        session.TenantID,
		Identifier:      session.Identifier,
		ParentSessionID: session.ParentSessionID,
		Metadata:        session.Metadata,
		TotalTokens:     totalTokens,
		CompactionCount: session.CompactionCount,
		MessageCount:    len(messages),
		CreatedAt:       session.CreatedAt,
		UpdatedAt:       session.UpdatedAt,
	}, nil
}

// GetMessages returns all messages for a session.
func (h *AgentHandle[TTx]) GetMessages(ctx context.Context, sessionID string) ([]*Message, error) {
	if !h.client.IsRunning() {
		return nil, ErrClientNotStarted
	}
	msgs, err := h.client.store.GetMessages(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	// Convert storage messages to agentpg messages
	return convert.FromStorageMessages(msgs), nil
}

// =============================================================================
// Compaction Methods
// =============================================================================

// Compact manually triggers context compaction for a session.
// Returns the compaction result, or nil if no compaction was needed.
func (h *AgentHandle[TTx]) Compact(ctx context.Context, sessionID string) (*compaction.CompactionResult, error) {
	if !h.client.IsRunning() {
		return nil, ErrClientNotStarted
	}

	// Create a temporary agent with hooks configured
	agent, err := h.createConfiguredAgent()
	if err != nil {
		return nil, err
	}

	// Load the session
	if err := agent.LoadSession(ctx, sessionID); err != nil {
		return nil, err
	}

	return agent.Compact(ctx)
}

// GetCompactionStats returns compaction statistics for a session.
func (h *AgentHandle[TTx]) GetCompactionStats(ctx context.Context, sessionID string) (*compaction.CompactionStats, error) {
	if !h.client.IsRunning() {
		return nil, ErrClientNotStarted
	}

	// Create a temporary agent
	agent, err := h.createConfiguredAgent()
	if err != nil {
		return nil, err
	}

	// Load the session
	if err := agent.LoadSession(ctx, sessionID); err != nil {
		return nil, err
	}

	return agent.GetCompactionStats(ctx)
}

// =============================================================================
// Nested Agent Support
// =============================================================================

// AsToolFor registers this agent as a tool for another AgentHandle.
// This allows nested agent orchestration patterns.
func (h *AgentHandle[TTx]) AsToolFor(parent *AgentHandle[TTx]) error {
	// Create a wrapper tool that delegates to this agent
	agentTool := &agentHandleToolWrapper[TTx]{
		handle:      h,
		name:        fmt.Sprintf("agent_%s", h.definition.Name),
		description: h.definition.Description,
	}

	return parent.RegisterTool(agentTool)
}

// agentHandleToolWrapper implements tool.Tool for nested AgentHandle.
type agentHandleToolWrapper[TTx any] struct {
	handle      *AgentHandle[TTx]
	name        string
	description string
	sessionID   string // Created on first use
}

func (a *agentHandleToolWrapper[TTx]) Name() string {
	return a.name
}

func (a *agentHandleToolWrapper[TTx]) Description() string {
	if a.description == "" {
		return fmt.Sprintf("Delegate task to %s agent", a.handle.definition.Name)
	}
	return a.description
}

func (a *agentHandleToolWrapper[TTx]) InputSchema() tool.ToolSchema {
	return tool.ToolSchema{
		Type: "object",
		Properties: map[string]tool.PropertyDef{
			"task": {
				Type:        "string",
				Description: "The task or question to delegate to the nested agent",
			},
		},
		Required: []string{"task"},
	}
}

func (a *agentHandleToolWrapper[TTx]) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var params struct {
		Task string `json:"task"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}

	// Create a session for this nested agent if not exists
	if a.sessionID == "" {
		sessionID, err := a.handle.NewSession(ctx, "nested", a.name, nil, map[string]any{
			"parent_tool": a.name,
		})
		if err != nil {
			return "", fmt.Errorf("failed to create nested session: %w", err)
		}
		a.sessionID = sessionID
	}

	// Run the nested agent synchronously
	response, err := a.handle.RunSync(ctx, a.sessionID, params.Task)
	if err != nil {
		return "", err
	}

	// Extract text from response
	var result string
	for _, block := range response.Message.Content {
		if block.Type == ContentTypeText {
			result += block.Text
		}
	}

	return result, nil
}

// =============================================================================
// Helper Methods
// =============================================================================

// createConfiguredAgent creates a temporary Agent instance with all hooks configured.
func (h *AgentHandle[TTx]) createConfiguredAgent() (*Agent[TTx], error) {
	// Build options from definition
	var opts []Option

	// Default auto compaction to true unless explicitly disabled via Config
	autoCompaction := true
	if h.definition.Config != nil {
		if v, ok := h.definition.Config["auto_compaction"].(bool); ok {
			autoCompaction = v
		}
	}
	opts = append(opts, WithAutoCompaction(autoCompaction))

	if h.definition.MaxTokens != nil {
		opts = append(opts, WithMaxTokens(int64(*h.definition.MaxTokens)))
	}
	if h.definition.Temperature != nil {
		opts = append(opts, WithTemperature(float64(*h.definition.Temperature)))
	}

	// Handle extended context from Config
	if h.definition.Config != nil {
		if v, ok := h.definition.Config["extended_context"].(bool); ok && v {
			opts = append(opts, WithExtendedContext(true))
		}
		// Handle compaction options
		if v, ok := h.definition.Config["compaction_trigger"].(float64); ok {
			opts = append(opts, WithCompactionTrigger(v))
		}
		if v, ok := h.definition.Config["compaction_target"].(int); ok {
			opts = append(opts, WithCompactionTarget(v))
		}
		if v, ok := h.definition.Config["compaction_preserve_n"].(int); ok {
			opts = append(opts, WithCompactionPreserveN(v))
		}
	}

	agent, err := New(h.client.driver, Config{
		Client:       h.client.anthropicClient,
		Model:        h.definition.Model,
		SystemPrompt: h.definition.SystemPrompt,
	}, opts...)
	if err != nil {
		return nil, err
	}

	// Register tools from definition
	for _, toolName := range h.definition.Tools {
		t, ok := GetRegisteredTool(toolName)
		if ok {
			if regErr := agent.RegisterTool(t); regErr != nil {
				return nil, regErr
			}
		}
	}

	// Register additional runtime tools
	h.toolsMu.RLock()
	for _, t := range h.additionalTools {
		if regErr := agent.RegisterTool(t); regErr != nil {
			h.toolsMu.RUnlock()
			return nil, regErr
		}
	}
	h.toolsMu.RUnlock()

	// Register hooks
	h.hooksMu.RLock()
	for _, hook := range h.beforeMessageHooks {
		agent.OnBeforeMessage(hook)
	}
	for _, hook := range h.afterMessageHooks {
		agent.OnAfterMessage(hook)
	}
	for _, hook := range h.toolCallHooks {
		agent.OnToolCall(hook)
	}
	for _, hook := range h.beforeCompactHooks {
		agent.OnBeforeCompaction(hook)
	}
	for _, hook := range h.afterCompactHooks {
		agent.OnAfterCompaction(hook)
	}
	h.hooksMu.RUnlock()

	return agent, nil
}

// =============================================================================
// Global Agent Provider (implements worker.AgentDefinitionProvider)
// =============================================================================

// globalAgentProvider implements worker.AgentDefinitionProvider using the global registry.
type globalAgentProvider struct{}

// GetAgent returns an agent definition from the global registry.
func (p *globalAgentProvider) GetAgent(name string) (worker.AgentDef, bool) {
	def, ok := GetRegisteredAgent(name)
	if !ok {
		return worker.AgentDef{}, false
	}
	return worker.AgentDef{
		Name:         def.Name,
		Model:        def.Model,
		SystemPrompt: def.SystemPrompt,
		MaxTokens:    def.MaxTokens,
		Temperature:  def.Temperature,
		Tools:        def.Tools,
	}, true
}
