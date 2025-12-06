package agentpg

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/packages/ssestream"
	"github.com/youssefsiam38/agentpg/compaction"
	"github.com/youssefsiam38/agentpg/driver"
	anthropicinternal "github.com/youssefsiam38/agentpg/internal/anthropic"
	"github.com/youssefsiam38/agentpg/internal/convert"
	"github.com/youssefsiam38/agentpg/storage"
	"github.com/youssefsiam38/agentpg/streaming"
	"github.com/youssefsiam38/agentpg/tool"
	"github.com/youssefsiam38/agentpg/types"
)

// Agent represents an AI agent instance.
// TTx is the native transaction type from the driver (e.g., pgx.Tx, *sql.Tx).
// The type parameter is automatically inferred when creating agents via New().
type Agent[TTx any] struct {
	config       *internalConfig
	driver       driver.Driver[TTx]
	store        storage.Store
	toolRegistry *tool.Registry
	toolExecutor *tool.Executor

	mu             sync.RWMutex
	currentSession string // Current active session ID
}

// New creates a new Agent with the given driver, configuration, and options.
// The transaction type TTx is inferred from the driver argument.
//
// Example:
//
//	drv := pgxv5.New(pool)
//	agent, err := agentpg.New(drv, agentpg.Config{
//	    Client:       &client,
//	    Model:        "claude-sonnet-4-5-20250929",
//	    SystemPrompt: "You are a helpful assistant",
//	}, agentpg.WithMaxTokens(4096))
func New[TTx any](drv driver.Driver[TTx], cfg Config, opts ...Option) (*Agent[TTx], error) {
	// Validate driver
	if drv == nil {
		return nil, fmt.Errorf("%w: driver is required", ErrInvalidConfig)
	}
	if !drv.PoolIsSet() {
		return nil, fmt.Errorf("%w: driver pool is not set", ErrInvalidConfig)
	}

	// Validate required configuration
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	// Create internal config with defaults
	internal := newInternalConfig(cfg)

	// Apply options
	for _, opt := range opts {
		if err := opt(internal); err != nil {
			return nil, fmt.Errorf("failed to apply option: %w", err)
		}
	}

	// Get storage layer from driver
	store := drv.GetStore()

	// Create tool registry and executor
	registry := tool.NewRegistry()
	if err := registry.RegisterAll(internal.tools); err != nil {
		return nil, fmt.Errorf("failed to register tools: %w", err)
	}
	executor := tool.NewExecutor(registry)
	executor.SetDefaultTimeout(internal.toolTimeout)

	// Create compaction manager with configurable values
	compactionConfig := compaction.CompactionConfig{
		TriggerThreshold: internal.compactionTrigger,
		TargetTokens:     internal.compactionTarget,
		PreserveLastN:    internal.compactionPreserveN,
		ProtectedTokens:  internal.compactionProtected,
		SummarizerModel:  internal.summarizerModel,
		MainModel:        internal.model,
		MaxContextTokens: internal.maxContextTokens,
	}
	compactionManager := compaction.NewManager(cfg.Client, store, drv, compactionConfig)
	internal.compactionManager = compactionManager

	agent := &Agent[TTx]{
		config:       internal,
		driver:       drv,
		store:        store,
		toolRegistry: registry,
		toolExecutor: executor,
	}

	return agent, nil
}

// GetModel returns the model being used by this agent
func (a *Agent[TTx]) GetModel() string {
	return a.config.model
}

// GetSystemPrompt returns the system prompt
func (a *Agent[TTx]) GetSystemPrompt() string {
	return a.config.systemPrompt
}

// CurrentSession returns the current session ID (thread-safe)
func (a *Agent[TTx]) CurrentSession() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.currentSession
}

// setCurrentSession sets the current session ID (thread-safe)
func (a *Agent[TTx]) setCurrentSession(sessionID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.currentSession = sessionID
}

// OnBeforeMessage registers a hook called before sending messages
func (a *Agent[TTx]) OnBeforeMessage(hook func(ctx context.Context, messages []*types.Message) error) {
	a.config.hooks.OnBeforeMessage(hook)
}

// OnAfterMessage registers a hook called after receiving a response
func (a *Agent[TTx]) OnAfterMessage(hook func(ctx context.Context, response *types.Response) error) {
	a.config.hooks.OnAfterMessage(hook)
}

// OnToolCall registers a hook called when a tool is executed
func (a *Agent[TTx]) OnToolCall(hook func(ctx context.Context, toolName string, input json.RawMessage, output string, err error) error) {
	a.config.hooks.OnToolCall(hook)
}

// OnBeforeCompaction registers a hook called before context compaction
func (a *Agent[TTx]) OnBeforeCompaction(hook func(ctx context.Context, sessionID string) error) {
	a.config.hooks.OnBeforeCompaction(hook)
}

// OnAfterCompaction registers a hook called after context compaction
func (a *Agent[TTx]) OnAfterCompaction(hook func(ctx context.Context, result *compaction.CompactionResult) error) {
	a.config.hooks.OnAfterCompaction(hook)
}

// Run executes the agent with the given prompt.
// Automatically wraps execution in a transaction for atomicity.
func (a *Agent[TTx]) Run(ctx context.Context, prompt string) (*Response, error) {
	// Begin transaction using the driver
	execTx, err := a.driver.Begin(ctx)
	if err != nil {
		return nil, NewAgentError("Run", fmt.Errorf("failed to begin transaction: %w", err))
	}

	// Ensure rollback on error
	committed := false
	defer func() {
		if !committed {
			if rollbackErr := execTx.Rollback(ctx); rollbackErr != nil {
				log.Printf("agentpg: failed to rollback transaction: %v", rollbackErr)
			}
		}
	}()

	// Get native transaction for RunTx
	tx := a.driver.UnwrapTx(execTx)

	// Execute with transaction
	response, err := a.RunTx(ctx, tx, prompt)
	if err != nil {
		return nil, err // Rollback via defer
	}

	// Commit transaction
	if err := execTx.Commit(ctx); err != nil {
		return nil, NewAgentError("Run", fmt.Errorf("failed to commit transaction: %w", err))
	}
	committed = true

	return response, nil
}

// RunTx executes the agent with the given prompt within a transaction.
// The caller is responsible for calling tx.Commit() or tx.Rollback().
// All database operations within this Run will use the provided transaction.
//
// This allows developers to create atomic operations that include both
// their own database operations and agent operations in a single transaction:
//
//	tx, _ := pool.Begin(ctx)
//	defer tx.Rollback(ctx)
//
//	// Your business logic in the same transaction
//	tx.Exec(ctx, "INSERT INTO orders ...")
//
//	// Agent operations in the same transaction
//	response, err := agent.RunTx(ctx, tx, "Process this order")
//	if err != nil {
//	    return err // Everything rolled back
//	}
//
//	tx.Commit(ctx)
func (a *Agent[TTx]) RunTx(ctx context.Context, tx TTx, prompt string) (*Response, error) {
	// Ensure we have a session
	if err := a.ensureSession(ctx); err != nil {
		return nil, err
	}

	sessionID := a.CurrentSession()

	// Wrap transaction in executor and inject into context
	execTx := a.driver.UnwrapExecutor(tx)
	txCtx := driver.WithExecutor(ctx, execTx)

	// Check for auto-compaction (within the same transaction)
	if a.config.autoCompaction {
		if err := a.checkAndCompact(txCtx, sessionID); err != nil {
			return nil, err
		}
	}

	// Add user message within transaction
	userMsg := NewUserMessage(sessionID, prompt)
	if err := a.store.SaveMessage(txCtx, convert.ToStorageMessage(userMsg)); err != nil {
		return nil, NewAgentErrorWithSession("RunTx", sessionID, err)
	}

	// Execute with tool loop (within transaction context)
	return a.runWithToolLoop(txCtx, sessionID, false)
}

// checkAndCompact checks if compaction is needed and performs it
func (a *Agent[TTx]) checkAndCompact(ctx context.Context, sessionID string) error {
	if a.config.compactionManager == nil {
		return nil
	}

	mgr, ok := a.config.compactionManager.(*compaction.Manager)
	if !ok {
		return nil
	}

	shouldCompact, err := mgr.ShouldCompact(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("failed to check compaction: %w", err)
	}

	if !shouldCompact {
		return nil
	}

	// Trigger compaction hook before compaction
	if hookErr := a.config.hooks.TriggerBeforeCompaction(ctx, sessionID); hookErr != nil {
		return fmt.Errorf("before-compaction hook failed: %w", hookErr)
	}

	// Perform compaction
	result, err := mgr.Compact(ctx, sessionID, a.config.compactionStrategy)
	if err != nil {
		return fmt.Errorf("compaction failed: %w", err)
	}

	// Trigger compaction hook after compaction
	if err := a.config.hooks.TriggerAfterCompaction(ctx, result); err != nil {
		return fmt.Errorf("after-compaction hook failed: %w", err)
	}

	return nil
}

// Compact manually triggers context compaction for the current session.
// Automatically wraps execution in a transaction for atomicity.
// Returns the compaction result, or nil if no compaction was needed.
func (a *Agent[TTx]) Compact(ctx context.Context) (*compaction.CompactionResult, error) {
	// Begin transaction using the driver
	execTx, err := a.driver.Begin(ctx)
	if err != nil {
		return nil, NewAgentError("Compact", fmt.Errorf("failed to begin transaction: %w", err))
	}

	// Ensure rollback on error
	committed := false
	defer func() {
		if !committed {
			if rollbackErr := execTx.Rollback(ctx); rollbackErr != nil {
				log.Printf("agentpg: failed to rollback transaction: %v", rollbackErr)
			}
		}
	}()

	// Get native transaction for CompactTx
	tx := a.driver.UnwrapTx(execTx)

	// Execute compaction within transaction
	result, err := a.CompactTx(ctx, tx)
	if err != nil {
		return nil, err // Rollback via defer
	}

	// Commit transaction
	if err := execTx.Commit(ctx); err != nil {
		return nil, NewAgentError("Compact", fmt.Errorf("failed to commit transaction: %w", err))
	}
	committed = true

	return result, nil
}

// CompactTx manually triggers context compaction within an existing transaction.
// The caller is responsible for calling tx.Commit() or tx.Rollback().
// Returns the compaction result, or nil if no compaction was needed.
//
// This allows developers to combine compaction with other database operations atomically:
//
//	tx, _ := pool.Begin(ctx)
//	defer tx.Rollback(ctx)
//
//	// Compact the session
//	result, err := agent.CompactTx(ctx, tx)
//	if err != nil {
//	    return err
//	}
//
//	// Your business logic in the same transaction
//	tx.Exec(ctx, "UPDATE usage_stats ...")
//
//	tx.Commit(ctx)
func (a *Agent[TTx]) CompactTx(ctx context.Context, tx TTx) (*compaction.CompactionResult, error) {
	return a.compactInternal(ctx, &tx)
}

// compactInternal is the internal implementation for manual compaction
func (a *Agent[TTx]) compactInternal(ctx context.Context, tx *TTx) (*compaction.CompactionResult, error) {
	// Ensure we have a session
	if err := a.ensureSession(ctx); err != nil {
		return nil, err
	}

	sessionID := a.CurrentSession()

	// Get compaction manager
	if a.config.compactionManager == nil {
		return nil, NewAgentError("Compact", fmt.Errorf("compaction manager not configured"))
	}

	mgr, ok := a.config.compactionManager.(*compaction.Manager)
	if !ok {
		return nil, NewAgentError("Compact", fmt.Errorf("invalid compaction manager type"))
	}

	// Inject transaction into context if provided
	txCtx := ctx
	if tx != nil {
		execTx := a.driver.UnwrapExecutor(*tx)
		txCtx = driver.WithExecutor(ctx, execTx)
	}

	opName := "Compact"
	if tx != nil {
		opName = "CompactTx"
	}

	// Trigger before-compaction hook
	if err := a.config.hooks.TriggerBeforeCompaction(txCtx, sessionID); err != nil {
		return nil, NewAgentErrorWithSession(opName, sessionID, fmt.Errorf("before-compaction hook failed: %w", err))
	}

	// Perform compaction
	result, err := mgr.Compact(txCtx, sessionID, a.config.compactionStrategy)
	if err != nil {
		return nil, NewAgentErrorWithSession(opName, sessionID, fmt.Errorf("compaction failed: %w", err))
	}

	// Trigger after-compaction hook
	if err := a.config.hooks.TriggerAfterCompaction(txCtx, result); err != nil {
		return nil, NewAgentErrorWithSession(opName, sessionID, fmt.Errorf("after-compaction hook failed: %w", err))
	}

	return result, nil
}

// GetCompactionStats returns compaction statistics for the current session
func (a *Agent[TTx]) GetCompactionStats(ctx context.Context) (*compaction.CompactionStats, error) {
	if err := a.ensureSession(ctx); err != nil {
		return nil, err
	}

	sessionID := a.CurrentSession()

	if a.config.compactionManager == nil {
		return nil, NewAgentError("GetCompactionStats", fmt.Errorf("compaction manager not configured"))
	}

	mgr, ok := a.config.compactionManager.(*compaction.Manager)
	if !ok {
		return nil, NewAgentError("GetCompactionStats", fmt.Errorf("invalid compaction manager type"))
	}

	return mgr.GetCompactionStats(ctx, sessionID)
}

// runWithToolLoop executes the agent with automatic tool calling
func (a *Agent[TTx]) runWithToolLoop(ctx context.Context, sessionID string, useExtendedContext bool) (*Response, error) {
	return a.runWithToolLoopInternal(ctx, sessionID, useExtendedContext, 0)
}

// runWithToolLoopInternal is the internal implementation with retry tracking
func (a *Agent[TTx]) runWithToolLoopInternal(ctx context.Context, sessionID string, useExtendedContext bool, extendedContextRetries int) (*Response, error) {
	const maxExtendedContextRetries = 1 // Only retry once with extended context
	iteration := 0

	for iteration < a.config.maxToolIterations {
		iteration++

		// Get message history
		messages, err := a.getMessageHistory(ctx, sessionID)
		if err != nil {
			return nil, err
		}

		// Trigger before-message hook
		if hookErr := a.config.hooks.TriggerBeforeMessage(ctx, messages); hookErr != nil {
			return nil, fmt.Errorf("before-message hook failed: %w", hookErr)
		}

		// Build Anthropic messages
		anthropicMsgs := anthropicinternal.ConvertToAnthropicMessages(messages)

		// Create streaming request
		stream, err := a.streamMessage(ctx, anthropicMsgs, useExtendedContext)
		if err != nil {
			// Check for max_tokens error
			if anthropicinternal.IsMaxTokensError(err) && a.config.extendedContext && !useExtendedContext && extendedContextRetries < maxExtendedContextRetries {
				// Retry with extended context (only once)
				return a.runWithToolLoopInternal(ctx, sessionID, true, extendedContextRetries+1)
			}

			// Check if retryable
			if anthropicinternal.IsRetryableError(err) && iteration < a.config.maxRetries {
				time.Sleep(time.Second * time.Duration(iteration)) // Exponential backoff
				continue
			}

			return nil, NewAgentErrorWithSession("Run", sessionID, err)
		}

		// Accumulate message
		accumulator := streaming.NewAccumulator()
		for stream.Next() {
			event := stream.Current()
			accumulator.ProcessAnthropicEvent(event)
		}

		if err := stream.Err(); err != nil {
			return nil, NewAgentErrorWithSession("Run", sessionID, err)
		}

		streamMsg := accumulator.Message()

		// Convert to agentpg message
		assistantMsg := anthropicinternal.ConvertStreamingMessage(streamMsg, sessionID)

		// Set usage from API response
		assistantMsg.Usage = anthropicinternal.ConvertUsage(streamMsg.Usage)

		// Save assistant message (store uses transaction from context if present)
		if err := a.store.SaveMessage(ctx, convert.ToStorageMessage(assistantMsg)); err != nil {
			return nil, NewAgentErrorWithSession("Run", sessionID, err)
		}

		// Create response
		response := &Response{
			Message:    assistantMsg,
			StopReason: streamMsg.StopReason,
			Usage:      anthropicinternal.ConvertUsage(streamMsg.Usage),
		}

		// Trigger after-message hook
		if err := a.config.hooks.TriggerAfterMessage(ctx, response); err != nil {
			return nil, fmt.Errorf("after-message hook failed: %w", err)
		}

		// Check for tool calls
		if anthropicinternal.HasToolCalls(assistantMsg) {
			// Execute tools and continue loop
			if err := a.executeToolCalls(ctx, sessionID, assistantMsg); err != nil {
				return nil, err
			}
			continue
		}

		// No tool calls - we're done
		return response, nil
	}

	return nil, fmt.Errorf("max tool iterations (%d) reached", a.config.maxToolIterations)
}

// stripTransaction creates a new context without the transaction value
// but preserving deadline, cancellation, and other values.
// Used when nested agents should have their own independent transaction.
func stripTransaction(ctx context.Context) context.Context {
	return driver.StripExecutor(ctx)
}

// streamMessage creates a streaming message request
func (a *Agent[TTx]) streamMessage(ctx context.Context, messages []anthropic.MessageParam, useExtendedContext bool) (*ssestream.Stream[anthropic.MessageStreamEventUnion], error) {
	// Build request parameters
	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(a.config.model),
		MaxTokens: a.config.maxTokens,
		Messages:  messages,
	}

	// Add system prompt
	if a.config.systemPrompt != "" {
		params.System = anthropicinternal.BuildSystemPrompt(a.config.systemPrompt)
	}

	// Add tools if available
	if a.toolRegistry.Count() > 0 {
		params.Tools = a.toolRegistry.ToAnthropicToolUnions()
	}

	// Add optional parameters
	if a.config.temperature != nil {
		params.Temperature = anthropic.Float(*a.config.temperature)
	}
	if a.config.topK != nil {
		params.TopK = anthropic.Int(*a.config.topK)
	}
	if a.config.topP != nil {
		params.TopP = anthropic.Float(*a.config.topP)
	}
	if len(a.config.stopSequences) > 0 {
		params.StopSequences = a.config.stopSequences
	}

	// Build options
	opts := []option.RequestOption{}

	// Add extended context header if needed
	if useExtendedContext {
		for key, value := range anthropicinternal.BuildExtendedContextHeaders() {
			opts = append(opts, option.WithHeader(key, value))
		}
	}

	// Create streaming request
	stream := a.config.client.Messages.NewStreaming(ctx, params, opts...)
	return stream, nil
}

// executeToolCalls executes all tool calls in a message
func (a *Agent[TTx]) executeToolCalls(ctx context.Context, sessionID string, msg *Message) error {
	toolCalls := anthropicinternal.ExtractToolCalls(msg.Content)
	if len(toolCalls) == 0 {
		return nil
	}

	// Build execution requests with validation
	requests := make([]tool.ToolCallRequest, len(toolCalls))
	for i, call := range toolCalls {
		// Validate input against tool schema before execution
		if err := a.toolExecutor.ValidateInput(call.Name, call.Input); err != nil {
			log.Printf("agentpg: tool input validation failed for %s: %v", call.Name, err)
			// Continue with execution - the tool can handle invalid input and return an error
		}

		requests[i] = tool.ToolCallRequest{
			ID:       call.ID,
			ToolName: call.Name,
			Input:    call.Input,
		}
	}

	// Execute tools (parallel execution if tool supports it)
	results := a.toolExecutor.ExecuteBatch(ctx, requests, false) // Sequential for safety

	// Trigger tool call hooks and collect results
	outputs := make([]string, len(results))
	errors := make([]error, len(results))

	for i, result := range results {
		// Trigger hook
		hookErr := a.config.hooks.TriggerToolCall(ctx, result.ToolName, result.Input, result.Output, result.Error)
		if hookErr != nil {
			return fmt.Errorf("tool call hook failed: %w", hookErr)
		}

		outputs[i] = result.Output
		errors[i] = result.Error
	}

	// Create tool result blocks
	resultBlocks := anthropicinternal.CreateToolResultBlocks(toolCalls, outputs, errors)

	// Create tool result message
	toolResultMsg := NewMessage(sessionID, RoleUser, resultBlocks)
	// Tool results are user input, so count as input tokens
	toolResultMsg.Usage = &Usage{
		InputTokens: anthropicinternal.CountTokens(resultBlocks),
	}

	// Save tool result message (store uses transaction from context if present)
	if err := a.store.SaveMessage(ctx, convert.ToStorageMessage(toolResultMsg)); err != nil {
		return NewAgentErrorWithSession("executeToolCalls", sessionID, err)
	}

	return nil
}

// getMessageHistory retrieves message history for the given session
func (a *Agent[TTx]) getMessageHistory(ctx context.Context, sessionID string) ([]*Message, error) {
	storageMessages, err := a.store.GetMessages(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	// Convert storage messages to agentpg messages
	messages := make([]*Message, len(storageMessages))
	for i, sm := range storageMessages {
		messages[i] = convert.FromStorageMessage(sm)
	}

	return messages, nil
}

// RegisterTool adds a new tool to the agent
func (a *Agent[TTx]) RegisterTool(t tool.Tool) error {
	return a.toolRegistry.Register(t)
}

// GetTools returns all registered tool names
func (a *Agent[TTx]) GetTools() []string {
	return a.toolRegistry.List()
}

// AsToolFor registers this agent as a tool for another agent.
// Both agents must use the same driver type.
// The nested agent will have its own dedicated session linked to the parent's session.
func (a *Agent[TTx]) AsToolFor(parent *Agent[TTx]) error {
	// Import here to avoid circular dependency
	// We'll use the builtin package
	toolName := fmt.Sprintf("agent_%s", sanitizeName(a.config.systemPrompt))

	// Create agent tool wrapper with reference to parent agent
	agentTool := createAgentTool(a, parent, toolName, a.config.systemPrompt)

	// Register with parent
	return parent.RegisterTool(agentTool)
}

// sanitizeName creates a valid tool name from a string
func sanitizeName(s string) string {
	// Take first 30 chars and replace spaces with underscores
	if len(s) > 30 {
		s = s[:30]
	}

	result := ""
	for _, c := range s {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' {
			result += string(c)
		} else if c == ' ' {
			result += "_"
		}
	}

	if result == "" {
		result = "agent"
	}

	return result
}

// createAgentTool creates an agent tool wrapper (defined inline to avoid import cycle)
func createAgentTool[TTx any](agent *Agent[TTx], parent *Agent[TTx], name string, description string) tool.Tool {
	return &agentToolWrapper[TTx]{
		agent:       agent,
		parentAgent: parent,
		name:        name,
		description: description,
	}
}

// agentToolWrapper implements tool.Tool for nested agents
type agentToolWrapper[TTx any] struct {
	agent       *Agent[TTx]
	parentAgent *Agent[TTx]
	name        string
	description string
	sessionID   string
}

func (a *agentToolWrapper[TTx]) Name() string {
	return a.name
}

func (a *agentToolWrapper[TTx]) Description() string {
	if a.description == "" {
		return fmt.Sprintf("Delegate task to %s agent", a.name)
	}
	return a.description
}

func (a *agentToolWrapper[TTx]) InputSchema() tool.ToolSchema {
	return tool.ToolSchema{
		Type: "object",
		Properties: map[string]tool.PropertyDef{
			"task": {
				Type:        "string",
				Description: "The task or question to delegate to this agent",
			},
			"context": {
				Type:        "string",
				Description: "Additional context for the task (optional)",
			},
		},
		Required: []string{"task"},
	}
}

func (a *agentToolWrapper[TTx]) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var params struct {
		Task    string `json:"task"`
		Context string `json:"context"`
	}

	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}

	if params.Task == "" {
		return "", fmt.Errorf("task is required")
	}

	// Strip parent transaction early - nested agent manages its own transaction
	// This ensures the nested session is created outside the parent's transaction
	// so it's immediately visible and has independent atomicity
	nestedCtx := stripTransaction(ctx)

	// Create new session for nested execution if not exists
	if a.sessionID == "" {
		// Get parent session info to inherit tenant_id and link parent_session_id
		parentSessionID := a.parentAgent.CurrentSession()
		if parentSessionID == "" {
			return "", fmt.Errorf("parent agent has no active session")
		}

		// Use nestedCtx to read parent session (outside parent's transaction)
		parentSession, err := a.parentAgent.store.GetSession(nestedCtx, parentSessionID)
		if err != nil {
			return "", fmt.Errorf("failed to get parent session: %w", err)
		}

		// Create nested session with parent's tenant_id and linked parent_session_id
		// Use nestedCtx so the session is committed immediately
		sessionID, err := a.agent.NewSession(nestedCtx, parentSession.TenantID, a.name, &parentSessionID, nil)
		if err != nil {
			return "", fmt.Errorf("failed to create nested session: %w", err)
		}
		a.sessionID = sessionID
	}

	// Load session
	if err := a.agent.LoadSession(nestedCtx, a.sessionID); err != nil {
		return "", fmt.Errorf("failed to load session: %w", err)
	}

	// Build prompt
	prompt := params.Task
	if params.Context != "" {
		prompt = fmt.Sprintf("Context: %s\n\nTask: %s", params.Context, params.Task)
	}

	// Execute nested agent (will create its own transaction)
	response, err := a.agent.Run(nestedCtx, prompt)
	if err != nil {
		return "", fmt.Errorf("nested agent failed: %w", err)
	}

	// Extract text from response
	return anthropicinternal.ExtractTextContent(response.Message.Content), nil
}
