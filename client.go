package agentpg

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/google/uuid"
	"github.com/youssefsiam38/agentpg/compaction"
	"github.com/youssefsiam38/agentpg/driver"
	"github.com/youssefsiam38/agentpg/tool"
)

// Client is the main AgentPG client that orchestrates agents, tools, and workers.
// The TTx type parameter represents the native transaction type for the driver
// (e.g., pgx.Tx for pgxv5, *sql.Tx for database/sql).
type Client[TTx any] struct {
	driver    driver.Driver[TTx]
	config    *ClientConfig
	anthropic anthropic.Client

	instanceID string
	started    bool
	mu         sync.RWMutex

	// Registered tools (in-memory, thread-safe).
	// Tools can be registered before or after Start().
	tools sync.Map // string -> tool.Tool

	// Background workers
	runWorker       *runWorker[TTx]
	streamingWorker *streamingWorker[TTx]
	toolWorker      *toolWorker[TTx]
	batchPoller     *batchPoller[TTx]
	rescuer         *rescuer[TTx]

	// Compaction
	compactor *compaction.Compactor[TTx]

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Waiters for run completion
	runWaiters   map[uuid.UUID][]chan *Run
	runWaitersMu sync.Mutex

	// Tool call callbacks (in-memory only, per-run)
	runCallbacks   map[uuid.UUID]*runCallbackEntry
	runCallbacksMu sync.Mutex

	// Leadership tracking
	isLeader bool
	leaderMu sync.RWMutex
}

// NewClient creates a new AgentPG client with the given driver and configuration.
// Agents and tools must be registered before calling Start().
func NewClient[TTx any](drv driver.Driver[TTx], config *ClientConfig) (*Client[TTx], error) {
	if drv == nil {
		return nil, fmt.Errorf("%w: driver is required", ErrInvalidConfig)
	}

	if config == nil {
		config = &ClientConfig{}
	}

	if err := config.validate(); err != nil {
		return nil, err
	}

	// Create Anthropic client
	var opts []option.RequestOption
	if config.APIKey != "" {
		opts = append(opts, option.WithAPIKey(config.APIKey))
	}
	anthropicClient := anthropic.NewClient(opts...)

	// Generate instance ID if not provided
	instanceID := config.ID
	if instanceID == "" {
		instanceID = uuid.New().String()
	}

	// Create compactor (lazy initialization, always available)
	compactorConfig := config.CompactionConfig
	if compactorConfig == nil {
		compactorConfig = compaction.DefaultConfig()
	}
	compactorLogger := compaction.Logger(nil)
	if config.Logger != nil {
		compactorLogger = config.Logger
	}
	comp := compaction.New(drv.Store(), &anthropicClient, compactorConfig, compactorLogger)

	return &Client[TTx]{
		driver:     drv,
		config:     config,
		anthropic:  anthropicClient,
		instanceID: instanceID,
		runWaiters:   make(map[uuid.UUID][]chan *Run),
		runCallbacks: make(map[uuid.UUID]*runCallbackEntry),
		compactor:    comp,
	}, nil
}

// InstanceID returns the unique identifier for this client instance.
func (c *Client[TTx]) InstanceID() string {
	return c.instanceID
}

// Config returns the client configuration.
func (c *Client[TTx]) Config() *ClientConfig {
	return c.config
}

// CreateAgent creates a new agent in the database.
// Returns the created agent with its ID populated.
func (c *Client[TTx]) CreateAgent(ctx context.Context, def *AgentDefinition) (*AgentDefinition, error) {
	if def == nil {
		return nil, fmt.Errorf("%w: agent definition is nil", ErrInvalidConfig)
	}

	if def.Name == "" {
		return nil, fmt.Errorf("%w: agent name is required", ErrInvalidConfig)
	}

	if def.Model == "" {
		return nil, fmt.Errorf("%w: agent model is required for agent %q", ErrInvalidConfig, def.Name)
	}

	driverDef := &driver.AgentDefinition{
		Name:         def.Name,
		Description:  def.Description,
		Model:        def.Model,
		SystemPrompt: def.SystemPrompt,
		ToolNames:    def.Tools,
		AgentIDs:     def.AgentIDs,
		MaxTokens:    def.MaxTokens,
		Temperature:  def.Temperature,
		TopK:         def.TopK,
		TopP:         def.TopP,
		Metadata:     def.Metadata,
		Config:       def.Config,
	}

	created, err := c.driver.Store().CreateAgent(ctx, driverDef)
	if err != nil {
		return nil, fmt.Errorf("failed to create agent: %w", err)
	}

	return convertDriverAgent(created), nil
}

// UpdateAgent updates an existing agent in the database.
func (c *Client[TTx]) UpdateAgent(ctx context.Context, def *AgentDefinition) error {
	if def.ID == uuid.Nil {
		return fmt.Errorf("%w: agent ID is required for update", ErrInvalidConfig)
	}

	driverDef := &driver.AgentDefinition{
		ID:           def.ID,
		Name:         def.Name,
		Description:  def.Description,
		Model:        def.Model,
		SystemPrompt: def.SystemPrompt,
		ToolNames:    def.Tools,
		AgentIDs:     def.AgentIDs,
		MaxTokens:    def.MaxTokens,
		Temperature:  def.Temperature,
		TopK:         def.TopK,
		TopP:         def.TopP,
		Metadata:     def.Metadata,
		Config:       def.Config,
	}

	if err := c.driver.Store().UpdateAgent(ctx, driverDef); err != nil {
		return fmt.Errorf("failed to update agent: %w", err)
	}

	return nil
}

// DeleteAgent removes an agent from the database.
func (c *Client[TTx]) DeleteAgent(ctx context.Context, id uuid.UUID) error {
	if err := c.driver.Store().DeleteAgent(ctx, id); err != nil {
		return fmt.Errorf("failed to delete agent: %w", err)
	}
	return nil
}

// ListAgents returns agents from the database with optional filtering.
func (c *Client[TTx]) ListAgents(ctx context.Context, metadata map[string]any, limit, offset int) ([]*AgentDefinition, int, error) {
	agents, total, err := c.driver.Store().ListAgents(ctx, driver.ListAgentsParams{
		MetadataFilter: metadata,
		Limit:          limit,
		Offset:         offset,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list agents: %w", err)
	}

	result := make([]*AgentDefinition, len(agents))
	for i, a := range agents {
		result[i] = convertDriverAgent(a)
	}
	return result, total, nil
}

// RegisterTool registers a tool with the client.
// Can be called before or after Start(). When called after Start(),
// the tool is also synced to the database immediately.
func (c *Client[TTx]) RegisterTool(t tool.Tool) error {
	if t == nil {
		return fmt.Errorf("%w: tool is nil", ErrInvalidConfig)
	}

	name := t.Name()
	if name == "" {
		return fmt.Errorf("%w: tool name is required", ErrInvalidConfig)
	}

	c.tools.Store(name, t)

	// If client already started, sync to DB immediately.
	// Reading c.started without lock is safe: it transitions false→true once, never back.
	if c.started {
		if err := c.syncToolToDatabase(context.Background(), t); err != nil {
			return fmt.Errorf("failed to sync tool %q to database: %w", name, err)
		}
	}

	c.log().Debug("registered tool", "name", name)
	return nil
}

// GetAgentByID retrieves an agent definition by ID from the database.
func (c *Client[TTx]) GetAgentByID(ctx context.Context, id uuid.UUID) (*AgentDefinition, error) {
	agent, err := c.driver.Store().GetAgent(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get agent: %w", err)
	}
	if agent == nil {
		return nil, ErrAgentNotFound
	}
	return convertDriverAgent(agent), nil
}

// GetAgentByName retrieves an agent definition by name and optional metadata from the database.
func (c *Client[TTx]) GetAgentByName(ctx context.Context, name string, metadata map[string]any) (*AgentDefinition, error) {
	agent, err := c.driver.Store().GetAgentByName(ctx, name, metadata)
	if err != nil {
		return nil, fmt.Errorf("failed to get agent: %w", err)
	}
	if agent == nil {
		return nil, ErrAgentNotFound
	}
	return convertDriverAgent(agent), nil
}

// GetOrCreateAgent returns an existing agent or creates a new one if it doesn't exist.
// Matching is done by name and metadata (agents with the same name but different metadata are distinct).
// If the agent exists, the existing agent is returned (the definition is NOT updated).
// If you need to update an existing agent, use UpdateAgent instead.
func (c *Client[TTx]) GetOrCreateAgent(ctx context.Context, def *AgentDefinition) (*AgentDefinition, error) {
	if def == nil {
		return nil, fmt.Errorf("%w: agent definition is nil", ErrInvalidConfig)
	}

	if def.Name == "" {
		return nil, fmt.Errorf("%w: agent name is required", ErrInvalidConfig)
	}

	// Try to find existing agent by name and metadata
	existing, err := c.driver.Store().GetAgentByName(ctx, def.Name, def.Metadata)
	if err != nil {
		return nil, fmt.Errorf("failed to check for existing agent: %w", err)
	}

	if existing != nil {
		// Agent exists — update it to match the provided definition
		def.ID = existing.ID
		if err := c.UpdateAgent(ctx, def); err != nil {
			return nil, fmt.Errorf("failed to update existing agent: %w", err)
		}
		def.ID = existing.ID
		return def, nil
	}

	// Agent doesn't exist, create it
	return c.CreateAgent(ctx, def)
}

// GetTool returns the registered tool by name.
func (c *Client[TTx]) GetTool(name string) tool.Tool {
	if v, ok := c.tools.Load(name); ok {
		return v.(tool.Tool)
	}
	return nil
}

// GetAllToolNames returns the names of all registered tools.
func (c *Client[TTx]) GetAllToolNames() []string {
	var names []string
	c.tools.Range(func(key, _ any) bool {
		names = append(names, key.(string))
		return true
	})
	return names
}

// Start initializes the client and begins background processing.
// This method:
// 1. Validates agent/tool references
// 2. Registers the instance in the database
// 3. Syncs agents and tools to the database
// 4. Starts background workers
func (c *Client[TTx]) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.started {
		return ErrClientAlreadyStarted
	}

	// Validate agent references
	if err := c.validateReferences(); err != nil {
		return err
	}

	// Create cancellable context
	c.ctx, c.cancel = context.WithCancel(ctx)

	// Register instance
	if err := c.registerInstance(c.ctx); err != nil {
		c.cancel()
		return fmt.Errorf("failed to register instance: %w", err)
	}

	// Sync agents and tools to database
	if err := c.syncRegistrations(c.ctx); err != nil {
		c.cancel()
		return fmt.Errorf("failed to sync registrations: %w", err)
	}

	// Start heartbeat loop
	c.wg.Add(1)
	go c.heartbeatLoop()

	// Start leader election loop
	c.wg.Add(1)
	go c.leaderLoop()

	// Start cleanup loop (only runs jobs if this instance is leader)
	c.wg.Add(1)
	go c.cleanupLoop()

	// Start notification listener
	c.wg.Add(1)
	go c.notificationLoop()

	// Initialize and start workers
	c.runWorker = newRunWorker(c)
	c.streamingWorker = newStreamingWorker(c)
	c.toolWorker = newToolWorker(c)
	c.batchPoller = newBatchPoller(c)
	c.rescuer = newRescuer(c)

	c.wg.Add(5)
	go func() {
		defer c.wg.Done()
		c.runWorker.run(c.ctx)
	}()
	go func() {
		defer c.wg.Done()
		c.streamingWorker.run(c.ctx)
	}()
	go func() {
		defer c.wg.Done()
		c.toolWorker.run(c.ctx)
	}()
	go func() {
		defer c.wg.Done()
		c.batchPoller.run(c.ctx)
	}()
	go func() {
		defer c.wg.Done()
		c.rescuer.run(c.ctx)
	}()

	c.started = true
	c.log().Info("client started", "instance_id", c.instanceID, "tools", len(c.GetAllToolNames()))
	return nil
}

// Stop gracefully shuts down the client.
func (c *Client[TTx]) Stop(ctx context.Context) error {
	c.mu.Lock()
	if !c.started {
		c.mu.Unlock()
		return ErrClientNotStarted
	}
	c.started = false
	c.mu.Unlock()

	c.log().Info("stopping client", "instance_id", c.instanceID)

	// Cancel background tasks
	c.cancel()

	// Wait for workers with timeout
	done := make(chan struct{})
	go func() {
		c.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Clean shutdown
	case <-ctx.Done():
		c.log().Warn("shutdown timeout, some workers may not have completed")
	}

	// Release leadership if we were the leader
	// Use background context since c.ctx is cancelled
	if c.isLeaderInstance() {
		if err := c.driver.Store().ReleaseLeader(context.Background(), c.instanceID); err != nil {
			c.log().Error("failed to release leadership", "error", err)
		} else {
			c.log().Info("released leadership", "instance_id", c.instanceID)
		}
	}

	// Unregister instance (this triggers agentpg_cleanup_orphaned_work)
	if err := c.driver.Store().UnregisterInstance(context.Background(), c.instanceID); err != nil {
		c.log().Error("failed to unregister instance", "error", err)
	}

	// Close driver listener
	if listener := c.driver.Listener(); listener != nil {
		_ = listener.Close()
	}

	c.log().Info("client stopped", "instance_id", c.instanceID)
	return nil
}

// NewSession creates a new conversation session.
// App-specific fields (tenant_id, user_id, etc.) should be stored in metadata.
func (c *Client[TTx]) NewSession(ctx context.Context, parentSessionID *uuid.UUID, metadata map[string]any) (uuid.UUID, error) {
	c.mu.RLock()
	started := c.started
	c.mu.RUnlock()

	if !started {
		return uuid.Nil, ErrClientNotStarted
	}

	session, err := c.driver.Store().CreateSession(ctx, driver.CreateSessionParams{
		ParentSessionID: parentSessionID,
		Metadata:        metadata,
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("failed to create session: %w", err)
	}

	return session.ID, nil
}

// NewSessionTx creates a new conversation session within a transaction.
// App-specific fields (tenant_id, user_id, etc.) should be stored in metadata.
func (c *Client[TTx]) NewSessionTx(ctx context.Context, tx TTx, parentSessionID *uuid.UUID, metadata map[string]any) (uuid.UUID, error) {
	c.mu.RLock()
	started := c.started
	c.mu.RUnlock()

	if !started {
		return uuid.Nil, ErrClientNotStarted
	}

	session, err := c.driver.Store().CreateSessionTx(ctx, tx, driver.CreateSessionParams{
		ParentSessionID: parentSessionID,
		Metadata:        metadata,
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("failed to create session: %w", err)
	}

	return session.ID, nil
}

// GetSession retrieves a session by ID.
func (c *Client[TTx]) GetSession(ctx context.Context, id uuid.UUID) (*Session, error) {
	session, err := c.driver.Store().GetSession(ctx, id)
	if err != nil {
		return nil, err
	}
	if session == nil {
		return nil, ErrSessionNotFound
	}

	return &Session{
		ID:              session.ID,
		ParentSessionID: session.ParentSessionID,
		Depth:           session.Depth,
		Metadata:        session.Metadata,
		CompactionCount: session.CompactionCount,
		CreatedAt:       session.CreatedAt,
		UpdatedAt:       session.UpdatedAt,
	}, nil
}

// Run creates a new asynchronous agent run and returns immediately.
// Use WaitForRun to wait for completion.
// The agentID must reference an agent that exists in the database.
// Options can provide variables for tools and override/append to the agent's system prompt.
func (c *Client[TTx]) Run(ctx context.Context, sessionID uuid.UUID, agentID uuid.UUID, prompt string, opts *RunOptions) (uuid.UUID, error) {
	c.mu.RLock()
	started := c.started
	c.mu.RUnlock()

	if !started {
		return uuid.Nil, ErrClientNotStarted
	}

	run, err := c.driver.Store().CreateRun(ctx, driver.CreateRunParams{
		SessionID:           sessionID,
		AgentID:             agentID,
		Prompt:              prompt,
		Depth:               0,
		CreatedByInstanceID: c.instanceID,
		Metadata:            runOptsVariables(opts),
		Options:             runOptsToMap(opts),
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("failed to create run: %w", err)
	}

	c.registerRunCallbacks(run.ID, opts)
	return run.ID, nil
}

// RunTx creates a new asynchronous agent run within a transaction.
// The run won't be visible to workers until the transaction commits.
// The agentID must reference an agent that exists in the database.
// Options can provide variables for tools and override/append to the agent's system prompt.
func (c *Client[TTx]) RunTx(ctx context.Context, tx TTx, sessionID uuid.UUID, agentID uuid.UUID, prompt string, opts *RunOptions) (uuid.UUID, error) {
	c.mu.RLock()
	started := c.started
	c.mu.RUnlock()

	if !started {
		return uuid.Nil, ErrClientNotStarted
	}

	run, err := c.driver.Store().CreateRunTx(ctx, tx, driver.CreateRunParams{
		SessionID:           sessionID,
		AgentID:             agentID,
		Prompt:              prompt,
		Depth:               0,
		CreatedByInstanceID: c.instanceID,
		Metadata:            runOptsVariables(opts),
		Options:             runOptsToMap(opts),
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("failed to create run: %w", err)
	}

	c.registerRunCallbacks(run.ID, opts)
	return run.ID, nil
}

// WaitForRun waits for a run to complete and returns the response.
func (c *Client[TTx]) WaitForRun(ctx context.Context, runID uuid.UUID) (*Response, error) {
	// Create a channel to receive notification
	ch := make(chan *Run, 1)

	c.runWaitersMu.Lock()
	c.runWaiters[runID] = append(c.runWaiters[runID], ch)
	c.runWaitersMu.Unlock()

	defer func() {
		c.runWaitersMu.Lock()
		waiters := c.runWaiters[runID]
		for i, w := range waiters {
			if w == ch {
				c.runWaiters[runID] = append(waiters[:i], waiters[i+1:]...)
				break
			}
		}
		if len(c.runWaiters[runID]) == 0 {
			delete(c.runWaiters, runID)
		}
		c.runWaitersMu.Unlock()
	}()

	// Check if already complete
	run, err := c.driver.Store().GetRun(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("failed to get run: %w", err)
	}
	if run == nil {
		return nil, ErrRunNotFound
	}

	if isTerminalState(RunState(run.State)) {
		return c.buildResponse(ctx, run)
	}

	// Poll interval for checking run state
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()

		case finalRun := <-ch:
			return c.buildResponse(ctx, &driver.Run{
				ID:                       finalRun.ID,
				SessionID:                finalRun.SessionID,
				AgentID:                  finalRun.AgentID,
				RunMode:                  string(finalRun.RunMode),
				ParentRunID:              finalRun.ParentRunID,
				ParentToolExecutionID:    finalRun.ParentToolExecutionID,
				Depth:                    finalRun.Depth,
				State:                    driver.RunState(finalRun.State),
				PreviousState:            (*driver.RunState)(finalRun.PreviousState),
				Prompt:                   finalRun.Prompt,
				CurrentIteration:         finalRun.CurrentIteration,
				CurrentIterationID:       finalRun.CurrentIterationID,
				ResponseText:             finalRun.ResponseText,
				StopReason:               finalRun.StopReason,
				InputTokens:              finalRun.InputTokens,
				OutputTokens:             finalRun.OutputTokens,
				CacheCreationInputTokens: finalRun.CacheCreationInputTokens,
				CacheReadInputTokens:     finalRun.CacheReadInputTokens,
				IterationCount:           finalRun.IterationCount,
				ToolIterations:           finalRun.ToolIterations,
				ErrorMessage:             finalRun.ErrorMessage,
				ErrorType:                finalRun.ErrorType,
				CreatedByInstanceID:      finalRun.CreatedByInstanceID,
				ClaimedByInstanceID:      finalRun.ClaimedByInstanceID,
				ClaimedAt:                finalRun.ClaimedAt,
				Metadata:                 finalRun.Metadata,
				Options:                  runOptsToMap(finalRun.Options),
				CreatedAt:                finalRun.CreatedAt,
				StartedAt:                finalRun.StartedAt,
				FinalizedAt:              finalRun.FinalizedAt,
			})

		case <-ticker.C:
			// Poll for state change
			run, err := c.driver.Store().GetRun(ctx, runID)
			if err != nil {
				return nil, fmt.Errorf("failed to get run: %w", err)
			}
			if run == nil {
				return nil, ErrRunNotFound
			}

			if isTerminalState(RunState(run.State)) {
				return c.buildResponse(ctx, run)
			}
		}
	}
}

// RunSync creates a run and waits for completion. This is a convenience wrapper
// around Run and WaitForRun.
// Note: Do not use RunSync inside a transaction as it will deadlock.
// Options can provide variables for tools and override/append to the agent's system prompt.
func (c *Client[TTx]) RunSync(ctx context.Context, sessionID uuid.UUID, agentID uuid.UUID, prompt string, opts *RunOptions) (*Response, error) {
	runID, err := c.Run(ctx, sessionID, agentID, prompt, opts)
	if err != nil {
		return nil, err
	}

	return c.WaitForRun(ctx, runID)
}

// RunFast creates a new asynchronous agent run using the streaming API.
// This provides faster response times compared to the batch API.
// Use WaitForRun to wait for completion.
// The agentID must reference an agent that exists in the database.
// Options can provide variables for tools and override/append to the agent's system prompt.
func (c *Client[TTx]) RunFast(ctx context.Context, sessionID uuid.UUID, agentID uuid.UUID, prompt string, opts *RunOptions) (uuid.UUID, error) {
	c.mu.RLock()
	started := c.started
	c.mu.RUnlock()

	if !started {
		return uuid.Nil, ErrClientNotStarted
	}

	run, err := c.driver.Store().CreateRun(ctx, driver.CreateRunParams{
		SessionID:           sessionID,
		AgentID:             agentID,
		Prompt:              prompt,
		RunMode:             string(RunModeStreaming),
		Depth:               0,
		CreatedByInstanceID: c.instanceID,
		Metadata:            runOptsVariables(opts),
		Options:             runOptsToMap(opts),
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("failed to create streaming run: %w", err)
	}

	c.registerRunCallbacks(run.ID, opts)
	return run.ID, nil
}

// RunFastTx creates a new asynchronous agent run using the streaming API within a transaction.
// The run won't be visible to workers until the transaction commits.
// The agentID must reference an agent that exists in the database.
// Options can provide variables for tools and override/append to the agent's system prompt.
func (c *Client[TTx]) RunFastTx(ctx context.Context, tx TTx, sessionID uuid.UUID, agentID uuid.UUID, prompt string, opts *RunOptions) (uuid.UUID, error) {
	c.mu.RLock()
	started := c.started
	c.mu.RUnlock()

	if !started {
		return uuid.Nil, ErrClientNotStarted
	}

	run, err := c.driver.Store().CreateRunTx(ctx, tx, driver.CreateRunParams{
		SessionID:           sessionID,
		AgentID:             agentID,
		Prompt:              prompt,
		RunMode:             string(RunModeStreaming),
		Depth:               0,
		CreatedByInstanceID: c.instanceID,
		Metadata:            runOptsVariables(opts),
		Options:             runOptsToMap(opts),
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("failed to create streaming run: %w", err)
	}

	c.registerRunCallbacks(run.ID, opts)
	return run.ID, nil
}

// RunFastSync creates a streaming run and waits for completion.
// This is a convenience wrapper around RunFast and WaitForRun.
// Note: Do not use RunFastSync inside a transaction as it will deadlock.
// Options can provide variables for tools and override/append to the agent's system prompt.
func (c *Client[TTx]) RunFastSync(ctx context.Context, sessionID uuid.UUID, agentID uuid.UUID, prompt string, opts *RunOptions) (*Response, error) {
	runID, err := c.RunFast(ctx, sessionID, agentID, prompt, opts)
	if err != nil {
		return nil, err
	}

	return c.WaitForRun(ctx, runID)
}

// Compact performs context compaction on the specified session.
// This replaces older messages with a structured summary to reduce context size
// while preserving essential information.
//
// Use NeedsCompaction to check if compaction is needed before calling this method.
func (c *Client[TTx]) Compact(ctx context.Context, sessionID uuid.UUID) (*compaction.Result, error) {
	c.mu.RLock()
	started := c.started
	c.mu.RUnlock()

	if !started {
		return nil, ErrClientNotStarted
	}

	return c.compactor.Compact(ctx, sessionID)
}

// CompactWithConfig performs context compaction with a custom configuration.
// This allows overriding the default compaction settings for a specific operation.
func (c *Client[TTx]) CompactWithConfig(ctx context.Context, sessionID uuid.UUID, cfg *compaction.Config) (*compaction.Result, error) {
	c.mu.RLock()
	started := c.started
	c.mu.RUnlock()

	if !started {
		return nil, ErrClientNotStarted
	}

	// Create a temporary compactor with the custom config
	compactorLogger := compaction.Logger(nil)
	if c.config.Logger != nil {
		compactorLogger = c.config.Logger
	}
	tempCompactor := compaction.New(c.driver.Store(), &c.anthropic, cfg, compactorLogger)
	return tempCompactor.Compact(ctx, sessionID)
}

// NeedsCompaction checks if a session needs compaction based on token usage.
// Returns true if the session's context exceeds the configured trigger threshold.
func (c *Client[TTx]) NeedsCompaction(ctx context.Context, sessionID uuid.UUID) (bool, error) {
	c.mu.RLock()
	started := c.started
	c.mu.RUnlock()

	if !started {
		return false, ErrClientNotStarted
	}

	return c.compactor.NeedsCompaction(ctx, sessionID)
}

// GetCompactionStats returns statistics about a session's compaction state.
// This includes token counts, usage percentage, and whether compaction is needed.
func (c *Client[TTx]) GetCompactionStats(ctx context.Context, sessionID uuid.UUID) (*compaction.Stats, error) {
	c.mu.RLock()
	started := c.started
	c.mu.RUnlock()

	if !started {
		return nil, ErrClientNotStarted
	}

	return c.compactor.GetStats(ctx, sessionID)
}

// CompactIfNeeded performs compaction only if the session exceeds the trigger threshold.
// Returns nil result if compaction was not needed.
// This is useful for automatic compaction after runs complete.
func (c *Client[TTx]) CompactIfNeeded(ctx context.Context, sessionID uuid.UUID) (*compaction.Result, error) {
	c.mu.RLock()
	started := c.started
	c.mu.RUnlock()

	if !started {
		return nil, ErrClientNotStarted
	}

	return c.compactor.CompactIfNeeded(ctx, sessionID)
}

// getCompactor returns the internal compactor for use by workers.
func (c *Client[TTx]) getCompactor() *compaction.Compactor[TTx] {
	return c.compactor
}

// GetRun retrieves a run by ID.
func (c *Client[TTx]) GetRun(ctx context.Context, id uuid.UUID) (*Run, error) {
	run, err := c.driver.Store().GetRun(ctx, id)
	if err != nil {
		return nil, err
	}
	if run == nil {
		return nil, ErrRunNotFound
	}

	return convertRun(run), nil
}

// CancelRun cancels a running or pending run and all its child runs.
// If the run is already in a terminal state, this is a no-op (idempotent).
// Any in-progress batch API requests will be cancelled asynchronously.
// WaitForRun callers will be unblocked with a cancelled error.
func (c *Client[TTx]) CancelRun(ctx context.Context, runID uuid.UUID) error {
	c.mu.RLock()
	started := c.started
	c.mu.RUnlock()

	if !started {
		return ErrClientNotStarted
	}

	store := c.driver.Store()

	// Atomically cancel the run in the database
	cancelled, batchID, _, err := store.CancelRun(ctx, runID)
	if err != nil {
		return fmt.Errorf("failed to cancel run: %w", err)
	}

	// Already terminal — idempotent no-op
	if !cancelled {
		return nil
	}

	// If there was an active batch, cancel it asynchronously via API
	if batchID != "" {
		go func() {
			_, cancelErr := c.anthropic.Messages.Batches.Cancel(context.Background(), batchID)
			if cancelErr != nil {
				c.log().Warn("failed to cancel batch API request",
					"batch_id", batchID,
					"run_id", runID,
					"error", cancelErr,
				)
			} else {
				c.log().Info("cancelled batch API request",
					"batch_id", batchID,
					"run_id", runID,
				)
			}
		}()
	}

	// Fetch updated run and notify waiters
	run, err := store.GetRun(ctx, runID)
	if err == nil && run != nil {
		c.notifyRunWaiters(runID, convertRun(run))
	}

	c.log().Info("run cancelled", "run_id", runID)
	return nil
}

// RegenerateRun re-creates a run that was previously cancelled or failed.
// It deletes all messages from the original run and creates a new run with
// the same session, agent, prompt, mode, and options.
// The original run must be in a terminal state (completed, cancelled, or failed).
// Returns the new run ID.
func (c *Client[TTx]) RegenerateRun(ctx context.Context, runID uuid.UUID) (uuid.UUID, error) {
	c.mu.RLock()
	started := c.started
	c.mu.RUnlock()

	if !started {
		return uuid.Nil, ErrClientNotStarted
	}

	store := c.driver.Store()

	// Get the original run
	run, err := store.GetRun(ctx, runID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("failed to get run: %w", err)
	}
	if run == nil {
		return uuid.Nil, ErrRunNotFound
	}

	// Must be in a terminal state
	if !isTerminalState(RunState(run.State)) {
		return uuid.Nil, ErrInvalidStateTransition
	}

	// Delete all messages from the cancelled/failed run
	_, err = store.DeleteRunMessages(ctx, runID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("failed to delete run messages: %w", err)
	}

	// Create a new run with the same parameters
	newRun, err := store.CreateRun(ctx, driver.CreateRunParams{
		SessionID:           run.SessionID,
		AgentID:             run.AgentID,
		Prompt:              run.Prompt,
		RunMode:             run.RunMode,
		Depth:               0,
		CreatedByInstanceID: c.instanceID,
		Metadata:            run.Metadata,
		Options:             run.Options,
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("failed to create regenerated run: %w", err)
	}

	c.log().Info("run regenerated",
		"original_run_id", runID,
		"new_run_id", newRun.ID,
	)

	return newRun.ID, nil
}

// Internal methods

func (c *Client[TTx]) validateReferences() error {
	// Tools are validated at registration time (RegisterTool checks for nil and empty name)
	// Agent validation happens at the database level with foreign key constraints
	return nil
}

func (c *Client[TTx]) registerInstance(ctx context.Context) error {
	hostname, _ := os.Hostname()
	pid := os.Getpid()

	return c.driver.Store().RegisterInstance(ctx, driver.RegisterInstanceParams{
		ID:                 c.instanceID,
		Name:               c.config.Name,
		Hostname:           hostname,
		PID:                pid,
		Version:            "1.0.0",
		MaxConcurrentRuns:  c.config.MaxConcurrentRuns,
		MaxConcurrentTools: c.config.MaxConcurrentTools,
	})
}

func (c *Client[TTx]) syncRegistrations(ctx context.Context) error {
	// Sync tools to database and register instance capabilities
	// Note: Agents are now managed via CRUD API (CreateAgent, etc.) not auto-synced
	var syncErr error
	c.tools.Range(func(_, value any) bool {
		t := value.(tool.Tool)
		if err := c.syncToolToDatabase(ctx, t); err != nil {
			syncErr = err
			return false
		}
		return true
	})
	return syncErr
}

// syncToolToDatabase upserts a tool definition and registers instance capability.
func (c *Client[TTx]) syncToolToDatabase(ctx context.Context, t tool.Tool) error {
	store := c.driver.Store()
	schema := t.InputSchema()
	schemaMap := map[string]any{
		"type":       schema.Type,
		"properties": schema.Properties,
		"required":   schema.Required,
	}

	if err := store.UpsertTool(ctx, &driver.ToolDefinition{
		Name:        t.Name(),
		Description: t.Description(),
		InputSchema: schemaMap,
		IsAgentTool: false,
	}); err != nil {
		return fmt.Errorf("failed to upsert tool %q: %w", t.Name(), err)
	}

	if err := store.RegisterInstanceTool(ctx, c.instanceID, t.Name()); err != nil {
		return fmt.Errorf("failed to register instance tool %q: %w", t.Name(), err)
	}

	return nil
}

func (c *Client[TTx]) heartbeatLoop() {
	defer c.wg.Done()

	ticker := time.NewTicker(c.config.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			if err := c.driver.Store().UpdateHeartbeat(c.ctx, c.instanceID); err != nil {
				c.log().Error("failed to update heartbeat", "error", err)
			}
		}
	}
}

// leaderLoop manages leader election and lease refresh.
// It runs on a regular interval (LeaderTTL / 2) to:
// 1. Attempt to acquire leadership if not already leader
// 2. Refresh the lease if currently leader
func (c *Client[TTx]) leaderLoop() {
	defer c.wg.Done()

	// Use half the TTL as the refresh interval to ensure we refresh before expiry
	refreshInterval := c.config.LeaderTTL / 2
	if refreshInterval < time.Second {
		refreshInterval = time.Second
	}

	ticker := time.NewTicker(refreshInterval)
	defer ticker.Stop()

	// Try to acquire leadership immediately on start
	c.tryAcquireOrRefreshLeadership()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			c.tryAcquireOrRefreshLeadership()
		}
	}
}

func (c *Client[TTx]) tryAcquireOrRefreshLeadership() {
	store := c.driver.Store()

	c.leaderMu.RLock()
	wasLeader := c.isLeader
	c.leaderMu.RUnlock()

	if wasLeader {
		// Already leader, try to refresh
		err := store.RefreshLeader(c.ctx, c.instanceID, c.config.LeaderTTL)
		if err != nil {
			// Lost leadership (someone else took it or it expired)
			c.leaderMu.Lock()
			c.isLeader = false
			c.leaderMu.Unlock()
			c.log().Info("lost leadership", "instance_id", c.instanceID)
		}
	} else {
		// Not leader, try to acquire
		acquired, err := store.TryAcquireLeader(c.ctx, c.instanceID, c.config.LeaderTTL)
		if err != nil {
			c.log().Error("failed to try acquire leadership", "error", err)
			return
		}

		if acquired {
			c.leaderMu.Lock()
			c.isLeader = true
			c.leaderMu.Unlock()
			c.log().Info("acquired leadership", "instance_id", c.instanceID)
		}
	}
}

// isLeaderInstance returns true if this instance is currently the elected leader.
func (c *Client[TTx]) isLeaderInstance() bool {
	c.leaderMu.RLock()
	defer c.leaderMu.RUnlock()
	return c.isLeader
}

// cleanupLoop runs periodic cleanup jobs.
// Only the elected leader runs cleanup to avoid duplicate work.
// Jobs include deleting stale instances (no heartbeat for InstanceTTL).
func (c *Client[TTx]) cleanupLoop() {
	defer c.wg.Done()

	ticker := time.NewTicker(c.config.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			if c.isLeaderInstance() {
				c.runCleanupJobs()
			}
		}
	}
}

func (c *Client[TTx]) runCleanupJobs() {
	ctx := c.ctx
	store := c.driver.Store()
	log := c.log()

	// Delete stale instances (no heartbeat for longer than InstanceTTL)
	// The agentpg_cleanup_orphaned_work trigger will handle marking
	// orphaned runs/tools as failed when the instance row is deleted
	deleted, err := store.DeleteStaleInstances(ctx, c.config.InstanceTTL)
	if err != nil {
		log.Error("failed to delete stale instances", "error", err)
	} else if deleted > 0 {
		log.Info("cleaned up stale instances", "count", deleted)
	}
}

func (c *Client[TTx]) notificationLoop() {
	defer c.wg.Done()

	listener := c.driver.Listener()
	if listener == nil {
		c.log().Debug("no listener available, notification loop disabled")
		return
	}

	// Start listening on relevant channels
	channels := []string{
		ChannelRunCreated,
		ChannelRunState,
		ChannelRunFinalized,
		ChannelToolPending,
		ChannelToolsComplete,
	}

	if err := listener.Listen(c.ctx, channels...); err != nil {
		c.log().Error("failed to start listener", "error", err)
		return
	}

	for {
		select {
		case <-c.ctx.Done():
			return
		case notif, ok := <-listener.Notifications():
			if !ok {
				return
			}
			c.handleNotification(notif)
		}
	}
}

func (c *Client[TTx]) handleNotification(notif driver.Notification) {
	switch notif.Channel {
	case ChannelRunCreated:
		// Parse payload to determine run mode
		var payload struct {
			RunID     uuid.UUID `json:"run_id"`
			SessionID uuid.UUID `json:"session_id"`
			AgentName string    `json:"agent_name"`
			RunMode   string    `json:"run_mode"`
		}
		if err := json.Unmarshal([]byte(notif.Payload), &payload); err != nil {
			c.log().Error("failed to parse run created payload", "error", err)
			// Fall back to triggering both workers
			if c.runWorker != nil {
				c.runWorker.trigger()
			}
			if c.streamingWorker != nil {
				c.streamingWorker.trigger()
			}
			return
		}

		// Trigger appropriate worker based on run mode
		if payload.RunMode == string(RunModeStreaming) {
			if c.streamingWorker != nil {
				c.streamingWorker.trigger()
			}
		} else {
			if c.runWorker != nil {
				c.runWorker.trigger()
			}
		}

	case ChannelRunFinalized:
		// Parse payload and notify waiters
		var payload struct {
			RunID                 uuid.UUID  `json:"run_id"`
			SessionID             uuid.UUID  `json:"session_id"`
			State                 string     `json:"state"`
			ParentRunID           *uuid.UUID `json:"parent_run_id"`
			ParentToolExecutionID *uuid.UUID `json:"parent_tool_execution_id"`
		}
		if err := json.Unmarshal([]byte(notif.Payload), &payload); err != nil {
			c.log().Error("failed to parse run finalized payload", "error", err)
			return
		}

		// Fetch full run and notify waiters
		run, err := c.driver.Store().GetRun(c.ctx, payload.RunID)
		if err != nil {
			c.log().Error("failed to get finalized run", "error", err, "run_id", payload.RunID)
			return
		}

		c.notifyRunWaiters(payload.RunID, convertRun(run))

	case ChannelToolPending:
		// Signal tool worker to check for new executions
		if c.toolWorker != nil {
			c.toolWorker.trigger()
		}

	case ChannelToolsComplete:
		// Signal tool worker that all tools for a run are complete
		var payload struct {
			RunID uuid.UUID `json:"run_id"`
		}
		if err := json.Unmarshal([]byte(notif.Payload), &payload); err != nil {
			c.log().Error("failed to parse tools complete payload", "error", err)
			return
		}
		if c.toolWorker != nil {
			c.toolWorker.handleToolsComplete(payload.RunID)
		}
	}
}

func (c *Client[TTx]) notifyRunWaiters(runID uuid.UUID, run *Run) {
	c.runWaitersMu.Lock()
	waiters := c.runWaiters[runID]
	delete(c.runWaiters, runID)
	c.runWaitersMu.Unlock()

	for _, ch := range waiters {
		select {
		case ch <- run:
		default:
		}
	}

	// Clean up callbacks for finalized runs
	c.cleanupRunCallbacks(runID)
}

// runCallbackEntry holds the callbacks for a run.
type runCallbackEntry struct {
	onToolStart    func(ToolCallEvent)
	onToolComplete func(ToolCallEvent)
}

// registerRunCallbacks stores callbacks for a run ID if opts has any callbacks set.
func (c *Client[TTx]) registerRunCallbacks(runID uuid.UUID, opts *RunOptions) {
	if opts == nil || (opts.OnToolStart == nil && opts.OnToolComplete == nil) {
		return
	}
	c.runCallbacksMu.Lock()
	c.runCallbacks[runID] = &runCallbackEntry{
		onToolStart:    opts.OnToolStart,
		onToolComplete: opts.OnToolComplete,
	}
	c.runCallbacksMu.Unlock()
}

// cleanupRunCallbacks removes callbacks for a run ID.
func (c *Client[TTx]) cleanupRunCallbacks(runID uuid.UUID) {
	c.runCallbacksMu.Lock()
	delete(c.runCallbacks, runID)
	c.runCallbacksMu.Unlock()
}

// propagateRunCallbacks copies parent run callbacks to a child run ID.
func (c *Client[TTx]) propagateRunCallbacks(parentRunID, childRunID uuid.UUID) {
	c.runCallbacksMu.Lock()
	defer c.runCallbacksMu.Unlock()
	if entry, ok := c.runCallbacks[parentRunID]; ok {
		c.runCallbacks[childRunID] = entry
	}
}

// fireToolStartCallback fires the OnToolStart callback for a tool execution.
func (c *Client[TTx]) fireToolStartCallback(exec *driver.ToolExecution, sessionID uuid.UUID, iterationNumber int) {
	c.runCallbacksMu.Lock()
	entry := c.runCallbacks[exec.RunID]
	c.runCallbacksMu.Unlock()

	if entry == nil || entry.onToolStart == nil {
		return
	}

	event := ToolCallEvent{
		RunID:           exec.RunID,
		SessionID:       sessionID,
		ToolName:        exec.ToolName,
		ToolInput:       exec.ToolInput,
		IsAgentTool:     exec.IsAgentTool,
		IterationNumber: iterationNumber,
	}

	go func() {
		defer func() {
			if r := recover(); r != nil {
				c.log().Error("panic in OnToolStart callback", "tool", exec.ToolName, "panic", r)
			}
		}()
		entry.onToolStart(event)
	}()
}

// fireToolCompleteCallback fires the OnToolComplete callback for a tool execution.
func (c *Client[TTx]) fireToolCompleteCallback(exec *driver.ToolExecution, sessionID uuid.UUID, iterationNumber int, output string, isError bool, errorMsg string, duration time.Duration) {
	c.runCallbacksMu.Lock()
	entry := c.runCallbacks[exec.RunID]
	c.runCallbacksMu.Unlock()

	if entry == nil || entry.onToolComplete == nil {
		return
	}

	event := ToolCallEvent{
		RunID:           exec.RunID,
		SessionID:       sessionID,
		ToolName:        exec.ToolName,
		ToolInput:       exec.ToolInput,
		IsAgentTool:     exec.IsAgentTool,
		IterationNumber: iterationNumber,
		Output:          output,
		IsError:         isError,
		ErrorMessage:    errorMsg,
		Duration:        duration,
	}

	go func() {
		defer func() {
			if r := recover(); r != nil {
				c.log().Error("panic in OnToolComplete callback", "tool", exec.ToolName, "panic", r)
			}
		}()
		entry.onToolComplete(event)
	}()
}

// convertToolExecution converts a driver.ToolExecution to a ToolCall.
func convertToolExecution(exec *driver.ToolExecution, iterationNumber int) ToolCall {
	var duration time.Duration
	if exec.StartedAt != nil && exec.CompletedAt != nil {
		duration = exec.CompletedAt.Sub(*exec.StartedAt)
	}
	return ToolCall{
		Name:            exec.ToolName,
		Input:           exec.ToolInput,
		Output:          Deref(exec.ToolOutput),
		IsError:         exec.IsError,
		ErrorMessage:    Deref(exec.ErrorMessage),
		IsAgentTool:     exec.IsAgentTool,
		AgentID:         exec.AgentID,
		ChildRunID:      exec.ChildRunID,
		Duration:        duration,
		IterationNumber: iterationNumber,
		State:           ToolExecutionState(exec.State),
		StartedAt:       exec.StartedAt,
		CompletedAt:     exec.CompletedAt,
	}
}

// GetRunToolCalls returns all tool calls for a run, ordered by creation time.
func (c *Client[TTx]) GetRunToolCalls(ctx context.Context, runID uuid.UUID) ([]ToolCall, error) {
	store := c.driver.Store()

	executions, err := store.GetToolExecutionsByRun(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("failed to get tool executions: %w", err)
	}

	if len(executions) == 0 {
		return nil, nil
	}

	// Build iteration number map
	iterations, err := store.GetIterationsByRun(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("failed to get iterations: %w", err)
	}
	iterMap := make(map[uuid.UUID]int, len(iterations))
	for _, iter := range iterations {
		iterMap[iter.ID] = iter.IterationNumber
	}

	result := make([]ToolCall, len(executions))
	for i, exec := range executions {
		result[i] = convertToolExecution(exec, iterMap[exec.IterationID])
	}
	return result, nil
}

// GetIterationToolCalls returns tool calls for a specific iteration.
func (c *Client[TTx]) GetIterationToolCalls(ctx context.Context, iterationID uuid.UUID) ([]ToolCall, error) {
	store := c.driver.Store()

	executions, err := store.GetToolExecutionsByIteration(ctx, iterationID)
	if err != nil {
		return nil, fmt.Errorf("failed to get tool executions: %w", err)
	}

	if len(executions) == 0 {
		return nil, nil
	}

	// Get iteration number
	iter, err := store.GetIteration(ctx, iterationID)
	if err != nil {
		return nil, fmt.Errorf("failed to get iteration: %w", err)
	}
	iterNum := 0
	if iter != nil {
		iterNum = iter.IterationNumber
	}

	result := make([]ToolCall, len(executions))
	for i, exec := range executions {
		result[i] = convertToolExecution(exec, iterNum)
	}
	return result, nil
}

func (c *Client[TTx]) buildResponse(ctx context.Context, run *driver.Run) (*Response, error) {
	if run.State == string(RunStateFailed) {
		return nil, &AgentError{
			Op:        "run",
			Err:       errors.New(Deref(run.ErrorMessage)),
			RunID:     run.ID.String(),
			SessionID: run.SessionID.String(),
			Context: map[string]any{
				"error_type": Deref(run.ErrorType),
			},
		}
	}

	if run.State == string(RunStateCancelled) {
		return nil, &AgentError{
			Op:        "run",
			Err:       errors.New("run was cancelled"),
			RunID:     run.ID.String(),
			SessionID: run.SessionID.String(),
		}
	}

	// Get the final message
	var message *Message
	if run.CurrentIterationID != nil {
		iter, err := c.driver.Store().GetIteration(ctx, *run.CurrentIterationID)
		if err == nil && iter != nil && iter.ResponseMessageID != nil {
			msg, err := c.driver.Store().GetMessage(ctx, *iter.ResponseMessageID)
			if err == nil && msg != nil {
				message = convertMessage(msg)
			}
		}
	}

	// If no message from iteration, try to get from run's messages
	if message == nil {
		messages, err := c.driver.Store().GetMessagesByRun(ctx, run.ID)
		if err == nil && len(messages) > 0 {
			// Get last assistant message
			for i := len(messages) - 1; i >= 0; i-- {
				if messages[i].Role == string(MessageRoleAssistant) {
					message = convertMessage(messages[i])
					break
				}
			}
		}
	}

	// Populate tool calls (best-effort — failure logs warning but doesn't fail the response)
	var toolCalls []ToolCall
	if executions, err := c.driver.Store().GetToolExecutionsByRun(ctx, run.ID); err != nil {
		c.log().Warn("failed to get tool executions for response", "run_id", run.ID, "error", err)
	} else if len(executions) > 0 {
		iterations, iterErr := c.driver.Store().GetIterationsByRun(ctx, run.ID)
		iterMap := make(map[uuid.UUID]int)
		if iterErr != nil {
			c.log().Warn("failed to get iterations for tool call mapping", "run_id", run.ID, "error", iterErr)
		} else {
			for _, iter := range iterations {
				iterMap[iter.ID] = iter.IterationNumber
			}
		}
		toolCalls = make([]ToolCall, len(executions))
		for i, exec := range executions {
			toolCalls[i] = convertToolExecution(exec, iterMap[exec.IterationID])
		}
	}

	return &Response{
		Text:       Deref(run.ResponseText),
		StopReason: Deref(run.StopReason),
		Usage: Usage{
			InputTokens:              run.InputTokens,
			OutputTokens:             run.OutputTokens,
			CacheCreationInputTokens: run.CacheCreationInputTokens,
			CacheReadInputTokens:     run.CacheReadInputTokens,
		},
		Message:        message,
		IterationCount: run.IterationCount,
		ToolIterations: run.ToolIterations,
		ToolCalls:      toolCalls,
	}, nil
}

func (c *Client[TTx]) log() Logger {
	if c.config.Logger != nil {
		return c.config.Logger
	}
	return &noopLogger{}
}

// toolMaxAttempts returns the max attempts for tool executions from config.
func (c *Client[TTx]) toolMaxAttempts() int {
	if c.config.ToolRetryConfig != nil && c.config.ToolRetryConfig.MaxAttempts > 0 {
		return c.config.ToolRetryConfig.MaxAttempts
	}
	return DefaultToolRetryMaxAttempts
}

// Helper functions

// buildSystemPrompt constructs the system prompt for a run, applying run-level instruction overrides.
// If options contains "override_instructions", it replaces the agent's system prompt entirely.
// If options contains "append_instructions", it appends to the agent's system prompt.
// OverrideInstructions takes precedence if both are set.
func buildSystemPrompt(agent *AgentDefinition, runOptions map[string]any) []anthropic.TextBlockParam {
	systemPrompt := agent.SystemPrompt

	if runOptions != nil {
		if override, ok := runOptions["override_instructions"].(string); ok && override != "" {
			systemPrompt = override
		} else if appendInstr, ok := runOptions["append_instructions"].(string); ok && appendInstr != "" {
			if systemPrompt != "" {
				systemPrompt = systemPrompt + "\n\n" + appendInstr
			} else {
				systemPrompt = appendInstr
			}
		}
	}

	if systemPrompt == "" {
		return nil
	}
	return []anthropic.TextBlockParam{
		{Text: systemPrompt},
	}
}

func isTerminalState(state RunState) bool {
	return state == RunStateCompleted || state == RunStateFailed || state == RunStateCancelled
}

// runOptsVariables extracts Variables from RunOptions for storage in run metadata.
func runOptsVariables(opts *RunOptions) map[string]any {
	if opts == nil {
		return nil
	}
	return opts.Variables
}

// runOptsToMap converts RunOptions instruction fields to a map for storage in the options column.
func runOptsToMap(opts *RunOptions) map[string]any {
	if opts == nil {
		return nil
	}
	m := make(map[string]any)
	if opts.OverrideInstructions != "" {
		m["override_instructions"] = opts.OverrideInstructions
	}
	if opts.AppendInstructions != "" {
		m["append_instructions"] = opts.AppendInstructions
	}
	if len(m) == 0 {
		return nil
	}
	return m
}

// mapToRunOptions converts stored options map and metadata back to RunOptions.
func mapToRunOptions(options map[string]any, metadata map[string]any) *RunOptions {
	hasOptions := len(options) > 0
	hasMetadata := len(metadata) > 0
	if !hasOptions && !hasMetadata {
		return nil
	}
	opts := &RunOptions{}
	if hasMetadata {
		opts.Variables = metadata
	}
	if hasOptions {
		if v, ok := options["override_instructions"].(string); ok {
			opts.OverrideInstructions = v
		}
		if v, ok := options["append_instructions"].(string); ok {
			opts.AppendInstructions = v
		}
	}
	return opts
}

func convertRun(r *driver.Run) *Run {
	if r == nil {
		return nil
	}
	return &Run{
		ID:                       r.ID,
		SessionID:                r.SessionID,
		AgentID:                  r.AgentID,
		RunMode:                  RunMode(r.RunMode),
		ParentRunID:              r.ParentRunID,
		ParentToolExecutionID:    r.ParentToolExecutionID,
		Depth:                    r.Depth,
		State:                    RunState(r.State),
		PreviousState:            (*RunState)(r.PreviousState),
		Prompt:                   r.Prompt,
		CurrentIteration:         r.CurrentIteration,
		CurrentIterationID:       r.CurrentIterationID,
		ResponseText:             r.ResponseText,
		StopReason:               r.StopReason,
		InputTokens:              r.InputTokens,
		OutputTokens:             r.OutputTokens,
		CacheCreationInputTokens: r.CacheCreationInputTokens,
		CacheReadInputTokens:     r.CacheReadInputTokens,
		IterationCount:           r.IterationCount,
		ToolIterations:           r.ToolIterations,
		ErrorMessage:             r.ErrorMessage,
		ErrorType:                r.ErrorType,
		CreatedByInstanceID:      r.CreatedByInstanceID,
		ClaimedByInstanceID:      r.ClaimedByInstanceID,
		ClaimedAt:                r.ClaimedAt,
		Metadata:                 r.Metadata,
		Options:                  mapToRunOptions(r.Options, r.Metadata),
		CreatedAt:                r.CreatedAt,
		StartedAt:                r.StartedAt,
		FinalizedAt:              r.FinalizedAt,
	}
}

func convertMessage(m *driver.Message) *Message {
	if m == nil {
		return nil
	}
	content := make([]ContentBlock, len(m.Content))
	for i, c := range m.Content {
		content[i] = ContentBlock{
			Type:               c.Type,
			Text:               c.Text,
			ToolUseID:          c.ToolUseID,
			ToolName:           c.ToolName,
			ToolInput:          c.ToolInput,
			ToolResultForUseID: c.ToolResultForUseID,
			ToolContent:        c.ToolContent,
			IsError:            c.IsError,
			Source:             c.Source,
			SearchResults:      c.SearchResults,
			Metadata:           c.Metadata,
		}
	}
	return &Message{
		ID:          m.ID,
		SessionID:   m.SessionID,
		RunID:       m.RunID,
		Role:        MessageRole(m.Role),
		Content:     content,
		Usage:       Usage(m.Usage),
		IsPreserved: m.IsPreserved,
		IsSummary:   m.IsSummary,
		Metadata:    m.Metadata,
		CreatedAt:   m.CreatedAt,
		UpdatedAt:   m.UpdatedAt,
	}
}

func convertDriverAgent(a *driver.AgentDefinition) *AgentDefinition {
	if a == nil {
		return nil
	}
	return &AgentDefinition{
		ID:           a.ID,
		Name:         a.Name,
		Description:  a.Description,
		Model:        a.Model,
		SystemPrompt: a.SystemPrompt,
		Tools:        a.ToolNames,
		AgentIDs:     a.AgentIDs,
		MaxTokens:    a.MaxTokens,
		Temperature:  a.Temperature,
		TopK:         a.TopK,
		TopP:         a.TopP,
		Metadata:     a.Metadata,
		Config:       a.Config,
		CreatedAt:    a.CreatedAt,
		UpdatedAt:    a.UpdatedAt,
	}
}

// noopLogger is a no-op logger implementation
type noopLogger struct{}

func (l *noopLogger) Debug(msg string, args ...any) {}
func (l *noopLogger) Info(msg string, args ...any)  {}
func (l *noopLogger) Warn(msg string, args ...any)  {}
func (l *noopLogger) Error(msg string, args ...any) {}
