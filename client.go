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

	// Registered agents and tools (pre-Start)
	agents map[string]*AgentDefinition
	tools  map[string]tool.Tool

	// Background workers
	runWorker   *runWorker[TTx]
	toolWorker  *toolWorker[TTx]
	batchPoller *batchPoller[TTx]

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Waiters for run completion
	runWaiters   map[uuid.UUID][]chan *Run
	runWaitersMu sync.Mutex

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

	return &Client[TTx]{
		driver:     drv,
		config:     config,
		anthropic:  anthropicClient,
		instanceID: instanceID,
		agents:     make(map[string]*AgentDefinition),
		tools:      make(map[string]tool.Tool),
		runWaiters: make(map[uuid.UUID][]chan *Run),
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

// RegisterAgent registers an agent definition with the client.
// Must be called before Start().
func (c *Client[TTx]) RegisterAgent(def *AgentDefinition) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.started {
		return ErrClientAlreadyStarted
	}

	if def == nil {
		return fmt.Errorf("%w: agent definition is nil", ErrInvalidConfig)
	}

	if def.Name == "" {
		return fmt.Errorf("%w: agent name is required", ErrInvalidConfig)
	}

	if def.Model == "" {
		return fmt.Errorf("%w: agent model is required for agent %q", ErrInvalidConfig, def.Name)
	}

	c.agents[def.Name] = def
	c.log().Debug("registered agent", "name", def.Name, "model", def.Model)
	return nil
}

// RegisterTool registers a tool with the client.
// Must be called before Start().
func (c *Client[TTx]) RegisterTool(t tool.Tool) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.started {
		return ErrClientAlreadyStarted
	}

	if t == nil {
		return fmt.Errorf("%w: tool is nil", ErrInvalidConfig)
	}

	name := t.Name()
	if name == "" {
		return fmt.Errorf("%w: tool name is required", ErrInvalidConfig)
	}

	c.tools[name] = t
	c.log().Debug("registered tool", "name", name)
	return nil
}

// GetAgent returns the registered agent definition by name.
func (c *Client[TTx]) GetAgent(name string) *AgentDefinition {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.agents[name]
}

// GetTool returns the registered tool by name.
func (c *Client[TTx]) GetTool(name string) tool.Tool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.tools[name]
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
	c.toolWorker = newToolWorker(c)
	c.batchPoller = newBatchPoller(c)

	c.wg.Add(3)
	go func() {
		defer c.wg.Done()
		c.runWorker.run(c.ctx)
	}()
	go func() {
		defer c.wg.Done()
		c.toolWorker.run(c.ctx)
	}()
	go func() {
		defer c.wg.Done()
		c.batchPoller.run(c.ctx)
	}()

	c.started = true
	c.log().Info("client started", "instance_id", c.instanceID, "agents", len(c.agents), "tools", len(c.tools))
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
		listener.Close()
	}

	c.log().Info("client stopped", "instance_id", c.instanceID)
	return nil
}

// NewSession creates a new conversation session.
func (c *Client[TTx]) NewSession(ctx context.Context, tenantID, identifier string, parentSessionID *uuid.UUID, metadata map[string]any) (uuid.UUID, error) {
	c.mu.RLock()
	started := c.started
	c.mu.RUnlock()

	if !started {
		return uuid.Nil, ErrClientNotStarted
	}

	session, err := c.driver.Store().CreateSession(ctx, driver.CreateSessionParams{
		TenantID:        tenantID,
		Identifier:      identifier,
		ParentSessionID: parentSessionID,
		Metadata:        metadata,
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("failed to create session: %w", err)
	}

	return session.ID, nil
}

// NewSessionTx creates a new conversation session within a transaction.
func (c *Client[TTx]) NewSessionTx(ctx context.Context, tx TTx, tenantID, identifier string, parentSessionID *uuid.UUID, metadata map[string]any) (uuid.UUID, error) {
	c.mu.RLock()
	started := c.started
	c.mu.RUnlock()

	if !started {
		return uuid.Nil, ErrClientNotStarted
	}

	session, err := c.driver.Store().CreateSessionTx(ctx, tx, driver.CreateSessionParams{
		TenantID:        tenantID,
		Identifier:      identifier,
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
		TenantID:        session.TenantID,
		Identifier:      session.Identifier,
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
func (c *Client[TTx]) Run(ctx context.Context, sessionID uuid.UUID, agentName, prompt string) (uuid.UUID, error) {
	c.mu.RLock()
	started := c.started
	agent := c.agents[agentName]
	c.mu.RUnlock()

	if !started {
		return uuid.Nil, ErrClientNotStarted
	}

	if agent == nil {
		return uuid.Nil, fmt.Errorf("%w: %s", ErrAgentNotRegistered, agentName)
	}

	run, err := c.driver.Store().CreateRun(ctx, driver.CreateRunParams{
		SessionID:           sessionID,
		AgentName:           agentName,
		Prompt:              prompt,
		Depth:               0,
		CreatedByInstanceID: c.instanceID,
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("failed to create run: %w", err)
	}

	return run.ID, nil
}

// RunTx creates a new asynchronous agent run within a transaction.
// The run won't be visible to workers until the transaction commits.
func (c *Client[TTx]) RunTx(ctx context.Context, tx TTx, sessionID uuid.UUID, agentName, prompt string) (uuid.UUID, error) {
	c.mu.RLock()
	started := c.started
	agent := c.agents[agentName]
	c.mu.RUnlock()

	if !started {
		return uuid.Nil, ErrClientNotStarted
	}

	if agent == nil {
		return uuid.Nil, fmt.Errorf("%w: %s", ErrAgentNotRegistered, agentName)
	}

	run, err := c.driver.Store().CreateRunTx(ctx, tx, driver.CreateRunParams{
		SessionID:           sessionID,
		AgentName:           agentName,
		Prompt:              prompt,
		Depth:               0,
		CreatedByInstanceID: c.instanceID,
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("failed to create run: %w", err)
	}

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
				AgentName:                finalRun.AgentName,
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
func (c *Client[TTx]) RunSync(ctx context.Context, sessionID uuid.UUID, agentName, prompt string) (*Response, error) {
	runID, err := c.Run(ctx, sessionID, agentName, prompt)
	if err != nil {
		return nil, err
	}

	return c.WaitForRun(ctx, runID)
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

// Internal methods

func (c *Client[TTx]) validateReferences() error {
	// Validate that agents reference only registered tools and agents
	for agentName, agent := range c.agents {
		// Check tool references
		for _, toolName := range agent.Tools {
			if _, ok := c.tools[toolName]; !ok {
				return fmt.Errorf("%w: agent %q references unknown tool %q", ErrInvalidConfig, agentName, toolName)
			}
		}

		// Check agent references (for agent-as-tool)
		for _, delegateName := range agent.Agents {
			if _, ok := c.agents[delegateName]; !ok {
				return fmt.Errorf("%w: agent %q references unknown agent %q", ErrInvalidConfig, agentName, delegateName)
			}
			// Prevent self-reference
			if delegateName == agentName {
				return fmt.Errorf("%w: agent %q cannot reference itself", ErrInvalidConfig, agentName)
			}
		}
	}
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
	store := c.driver.Store()

	// Sync agents to database
	for _, agent := range c.agents {
		toolNames := make([]string, 0, len(agent.Tools)+len(agent.Agents))
		toolNames = append(toolNames, agent.Tools...)
		toolNames = append(toolNames, agent.Agents...)

		if err := store.UpsertAgent(ctx, &driver.AgentDefinition{
			Name:         agent.Name,
			Description:  agent.Description,
			Model:        agent.Model,
			SystemPrompt: agent.SystemPrompt,
			ToolNames:    toolNames,
			MaxTokens:    agent.MaxTokens,
			Temperature:  agent.Temperature,
			TopK:         agent.TopK,
			TopP:         agent.TopP,
			Config:       agent.Config,
		}); err != nil {
			return fmt.Errorf("failed to upsert agent %q: %w", agent.Name, err)
		}

		// Register instance capability for this agent
		if err := store.RegisterInstanceAgent(ctx, c.instanceID, agent.Name); err != nil {
			return fmt.Errorf("failed to register instance agent %q: %w", agent.Name, err)
		}
	}

	// Sync regular tools to database
	for name, t := range c.tools {
		schema := t.InputSchema()
		schemaMap := map[string]any{
			"type":       schema.Type,
			"properties": schema.Properties,
			"required":   schema.Required,
		}

		if err := store.UpsertTool(ctx, &driver.ToolDefinition{
			Name:        name,
			Description: t.Description(),
			InputSchema: schemaMap,
			IsAgentTool: false,
		}); err != nil {
			return fmt.Errorf("failed to upsert tool %q: %w", name, err)
		}

		// Register instance capability for this tool
		if err := store.RegisterInstanceTool(ctx, c.instanceID, name); err != nil {
			return fmt.Errorf("failed to register instance tool %q: %w", name, err)
		}
	}

	// Create agent-as-tool entries
	for _, agent := range c.agents {
		for _, delegateName := range agent.Agents {
			delegateAgent := c.agents[delegateName]

			// Create tool entry for the agent
			schemaMap := map[string]any{
				"type": "object",
				"properties": map[string]any{
					"task": map[string]any{
						"type":        "string",
						"description": "The task to delegate to this agent",
					},
				},
				"required": []string{"task"},
			}

			if err := store.UpsertTool(ctx, &driver.ToolDefinition{
				Name:        delegateName,
				Description: delegateAgent.Description,
				InputSchema: schemaMap,
				IsAgentTool: true,
				AgentName:   &delegateName,
			}); err != nil {
				return fmt.Errorf("failed to upsert agent-tool %q: %w", delegateName, err)
			}

			// Register instance capability for agent-tool
			if err := store.RegisterInstanceTool(ctx, c.instanceID, delegateName); err != nil {
				return fmt.Errorf("failed to register instance agent-tool %q: %w", delegateName, err)
			}
		}
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
		// Signal run worker to check for new runs
		if c.runWorker != nil {
			c.runWorker.trigger()
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
	}, nil
}

func (c *Client[TTx]) log() Logger {
	if c.config.Logger != nil {
		return c.config.Logger
	}
	return &noopLogger{}
}

// Helper functions

func isTerminalState(state RunState) bool {
	return state == RunStateCompleted || state == RunStateFailed || state == RunStateCancelled
}

func convertRun(r *driver.Run) *Run {
	if r == nil {
		return nil
	}
	return &Run{
		ID:                       r.ID,
		SessionID:                r.SessionID,
		AgentName:                r.AgentName,
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

// noopLogger is a no-op logger implementation
type noopLogger struct{}

func (l *noopLogger) Debug(msg string, args ...any) {}
func (l *noopLogger) Info(msg string, args ...any)  {}
func (l *noopLogger) Warn(msg string, args ...any)  {}
func (l *noopLogger) Error(msg string, args ...any) {}
