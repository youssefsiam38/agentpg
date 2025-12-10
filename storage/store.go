package storage

import (
	"context"
	"errors"
	"time"

	"github.com/youssefsiam38/agentpg/runstate"
)

// Sentinel errors for distributed operations
var (
	// ErrStateTransitionFailed indicates an atomic state transition failed
	// because the current state didn't match RequiredState.
	// This is expected in distributed systems when another worker has already
	// transitioned the state.
	ErrStateTransitionFailed = errors.New("state transition failed: current state does not match required state")
)

// Store defines the storage interface for agents
type Store interface {
	// Session operations
	CreateSession(ctx context.Context, tenantID, identifier string, parentSessionID *string, metadata map[string]any) (string, error)
	GetSession(ctx context.Context, sessionID string) (*Session, error)
	GetSessionsByTenant(ctx context.Context, tenantID string) ([]*Session, error)
	GetSessionByTenantAndIdentifier(ctx context.Context, tenantID, identifier string) (*Session, error)
	// GetSessionTokenCount calculates total tokens by summing usage from messages
	GetSessionTokenCount(ctx context.Context, sessionID string) (int, error)
	UpdateSessionCompactionCount(ctx context.Context, sessionID string) error

	// Message operations
	SaveMessage(ctx context.Context, msg *Message) error
	SaveMessages(ctx context.Context, messages []*Message) error
	GetMessages(ctx context.Context, sessionID string) ([]*Message, error)
	GetMessagesSince(ctx context.Context, sessionID string, since time.Time) ([]*Message, error)
	DeleteMessages(ctx context.Context, messageIDs []string) error

	// =========================================================================
	// Content Block operations (normalized content storage)
	// =========================================================================

	// SaveContentBlocks saves multiple content blocks for a message atomically.
	SaveContentBlocks(ctx context.Context, blocks []*ContentBlock) error

	// GetMessageContentBlocks returns all content blocks for a message, ordered by block_index.
	GetMessageContentBlocks(ctx context.Context, messageID string) ([]*ContentBlock, error)

	// GetToolUseBlock finds a tool_use content block by Claude's tool_use_id.
	GetToolUseBlock(ctx context.Context, toolUseID string) (*ContentBlock, error)

	// GetContentBlock returns a content block by its internal ID.
	GetContentBlock(ctx context.Context, blockID string) (*ContentBlock, error)

	// LinkToolResult updates a tool_result block to reference its tool_use block.
	LinkToolResult(ctx context.Context, toolResultBlockID, toolUseBlockID string) error

	// Compaction operations
	SaveCompactionEvent(ctx context.Context, event *CompactionEvent) error
	GetCompactionHistory(ctx context.Context, sessionID string) ([]*CompactionEvent, error)
	ArchiveMessages(ctx context.Context, compactionEventID string, messages []*Message) error

	// =========================================================================
	// Run operations (event-driven)
	// =========================================================================

	// CreateRun creates a new run record in the pending state.
	// Triggers agentpg_run_created notification.
	CreateRun(ctx context.Context, params *CreateRunParams) (string, error)

	// GetRun returns a run by ID.
	GetRun(ctx context.Context, runID string) (*Run, error)

	// GetSessionRuns returns all runs for a session, ordered by started_at DESC.
	GetSessionRuns(ctx context.Context, sessionID string) ([]*Run, error)

	// GetLatestSessionRun returns the most recent run for a session.
	GetLatestSessionRun(ctx context.Context, sessionID string) (*Run, error)

	// UpdateRunState transitions a run to a new state.
	// Returns ErrRunNotFound if run doesn't exist or ErrInvalidTransition if
	// the transition is not allowed.
	UpdateRunState(ctx context.Context, runID string, params *UpdateRunStateParams) error

	// GetRunMessages returns all messages associated with a run.
	GetRunMessages(ctx context.Context, runID string) ([]*Message, error)

	// GetStuckRuns returns all runs that have been in workable states longer than the horizon.
	// Used by the cleanup service to detect orphaned runs.
	GetStuckRuns(ctx context.Context, horizon time.Time) ([]*Run, error)

	// =========================================================================
	// Async Run operations (for event-driven workers)
	// =========================================================================

	// GetPendingRuns returns runs waiting for processing in the given states.
	// Used by workers to pick up work.
	GetPendingRuns(ctx context.Context, states []runstate.RunState, limit int) ([]*Run, error)

	// ClaimRun attempts to claim a run for this worker instance.
	// Returns true if claimed, false if already claimed by another worker.
	// Uses atomic UPDATE with WHERE clause for race-free claiming.
	ClaimRun(ctx context.Context, runID, instanceID string) (bool, error)

	// ReleaseRunClaim releases a run claim (resets worker_instance_id).
	// Used when a worker fails or times out.
	ReleaseRunClaim(ctx context.Context, runID string) error

	// UpdateRunIteration records a new API iteration.
	UpdateRunIteration(ctx context.Context, runID string, params *UpdateRunIterationParams) error

	// =========================================================================
	// Tool Execution operations
	// =========================================================================

	// CreateToolExecutions creates multiple pending tool executions for a run.
	// Triggers agentpg_tool_pending notifications.
	CreateToolExecutions(ctx context.Context, executions []*CreateToolExecutionParams) error

	// GetPendingToolExecutions returns pending tool executions for pickup.
	GetPendingToolExecutions(ctx context.Context, limit int) ([]*ToolExecution, error)

	// GetRunToolExecutions returns all tool executions for a run.
	GetRunToolExecutions(ctx context.Context, runID string) ([]*ToolExecution, error)

	// GetToolExecution returns a single tool execution by ID.
	GetToolExecution(ctx context.Context, executionID string) (*ToolExecution, error)

	// ClaimToolExecution attempts to claim a tool execution for this instance.
	// Returns true if claimed, false if already claimed.
	ClaimToolExecution(ctx context.Context, executionID, instanceID string) (bool, error)

	// UpdateToolExecutionState updates tool execution state.
	UpdateToolExecutionState(ctx context.Context, executionID string, params *UpdateToolExecutionStateParams) error

	// AreAllToolExecutionsComplete checks if all tool executions for a run are terminal.
	AreAllToolExecutionsComplete(ctx context.Context, runID string) (bool, error)

	// GetCompletedToolExecutions returns all completed tool executions for a run.
	// Used to build the tool_result message.
	GetCompletedToolExecutions(ctx context.Context, runID string) ([]*ToolExecution, error)

	// LinkToolExecutionToResultBlock updates the tool_result_block_id for a tool execution.
	// This links the execution to its corresponding tool_result content block.
	LinkToolExecutionToResultBlock(ctx context.Context, executionID, resultBlockID string) error

	// =========================================================================
	// Instance operations
	// =========================================================================

	// RegisterInstance registers a new instance with the given ID and metadata.
	RegisterInstance(ctx context.Context, params *RegisterInstanceParams) error

	// UpdateInstanceHeartbeat updates the last_heartbeat_at for an instance.
	UpdateInstanceHeartbeat(ctx context.Context, instanceID string) error

	// GetStaleInstances returns instance IDs that haven't heartbeated since horizon.
	GetStaleInstances(ctx context.Context, horizon time.Time) ([]string, error)

	// DeregisterInstance removes an instance and triggers orphan cleanup.
	DeregisterInstance(ctx context.Context, instanceID string) error

	// GetInstance returns an instance by ID.
	GetInstance(ctx context.Context, instanceID string) (*Instance, error)

	// GetActiveInstances returns all instances with heartbeat after horizon.
	GetActiveInstances(ctx context.Context, horizon time.Time) ([]*Instance, error)

	// =========================================================================
	// Leader election operations
	// =========================================================================

	// LeaderAttemptElect attempts to elect this instance as leader.
	// Returns true if elected, false if another leader exists.
	LeaderAttemptElect(ctx context.Context, params *LeaderElectParams) (bool, error)

	// LeaderAttemptReelect attempts to renew leadership.
	// Returns true if re-elected, false if leadership was lost.
	LeaderAttemptReelect(ctx context.Context, params *LeaderElectParams) (bool, error)

	// LeaderResign voluntarily gives up leadership.
	LeaderResign(ctx context.Context, leaderID string) error

	// LeaderDeleteExpired removes expired leader entries.
	// Returns the number of rows deleted.
	LeaderDeleteExpired(ctx context.Context) (int, error)

	// LeaderGetCurrent returns the current leader, or nil if none.
	LeaderGetCurrent(ctx context.Context) (*Leader, error)

	// =========================================================================
	// Agent registration operations
	// =========================================================================

	// RegisterAgent upserts an agent definition.
	RegisterAgent(ctx context.Context, params *RegisterAgentParams) error

	// RegisterInstanceAgent links an instance to an agent.
	RegisterInstanceAgent(ctx context.Context, instanceID, agentName string) error

	// GetAgent returns an agent by name.
	GetAgent(ctx context.Context, name string) (*RegisteredAgent, error)

	// GetAvailableAgents returns all agents with at least one active instance.
	GetAvailableAgents(ctx context.Context, horizon time.Time) ([]*RegisteredAgent, error)

	// =========================================================================
	// Tool registration operations
	// =========================================================================

	// RegisterTool upserts a tool definition.
	RegisterTool(ctx context.Context, params *RegisterToolParams) error

	// RegisterInstanceTool links an instance to a tool.
	RegisterInstanceTool(ctx context.Context, instanceID, toolName string) error

	// GetTool returns a tool by name.
	GetTool(ctx context.Context, name string) (*RegisteredTool, error)

	// GetAvailableTools returns all tools with at least one active instance.
	GetAvailableTools(ctx context.Context, horizon time.Time) ([]*RegisteredTool, error)
}

// =========================================================================
// Session types
// =========================================================================

// Session represents a conversation session
type Session struct {
	ID              string         `json:"id"`
	TenantID        string         `json:"tenant_id"`
	Identifier      string         `json:"identifier"`
	ParentSessionID *string        `json:"parent_session_id,omitempty"`
	Metadata        map[string]any `json:"metadata"`
	CompactionCount int            `json:"compaction_count"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
}

// =========================================================================
// Message types (content is normalized into ContentBlock)
// =========================================================================

// MessageUsage represents token usage for a message
// This is provider-agnostic and can store usage data from any LLM
type MessageUsage struct {
	InputTokens         int `json:"input_tokens"`
	OutputTokens        int `json:"output_tokens"`
	CacheCreationTokens int `json:"cache_creation_tokens,omitempty"`
	CacheReadTokens     int `json:"cache_read_tokens,omitempty"`
}

// TotalTokens returns the sum of input and output tokens
func (u *MessageUsage) TotalTokens() int {
	if u == nil {
		return 0
	}
	return u.InputTokens + u.OutputTokens
}

// Message represents a stored message (content is in ContentBlock table)
type Message struct {
	ID          string         `json:"id"`
	SessionID   string         `json:"session_id"`
	RunID       *string        `json:"run_id,omitempty"` // Link to run
	Role        string         `json:"role"`
	Usage       *MessageUsage  `json:"usage"` // Token usage breakdown
	Metadata    map[string]any `json:"metadata"`
	IsPreserved bool           `json:"is_preserved"`
	IsSummary   bool           `json:"is_summary"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`

	// ContentBlocks is populated when loading messages with content
	// This is not stored directly in the messages table
	ContentBlocks []*ContentBlock `json:"content_blocks,omitempty"`
}

// =========================================================================
// Content Block types (normalized content storage)
// =========================================================================

// ContentBlockType represents the type of content block
type ContentBlockType string

const (
	ContentBlockTypeText            ContentBlockType = "text"
	ContentBlockTypeToolUse         ContentBlockType = "tool_use"
	ContentBlockTypeToolResult      ContentBlockType = "tool_result"
	ContentBlockTypeImage           ContentBlockType = "image"
	ContentBlockTypeDocument        ContentBlockType = "document"
	ContentBlockTypeThinking        ContentBlockType = "thinking"
	ContentBlockTypeServerToolUse   ContentBlockType = "server_tool_use"
	ContentBlockTypeWebSearchResult ContentBlockType = "web_search_tool_result"
)

// ContentBlock represents a normalized content block
type ContentBlock struct {
	ID         string           `json:"id"`
	MessageID  string           `json:"message_id"`
	BlockIndex int              `json:"block_index"`
	Type       ContentBlockType `json:"type"`

	// Text content (for text, thinking blocks)
	Text *string `json:"text,omitempty"`

	// Tool use fields (for tool_use, server_tool_use blocks)
	ToolUseID *string        `json:"tool_use_id,omitempty"` // Claude's tool_use_id
	ToolName  *string        `json:"tool_name,omitempty"`
	ToolInput map[string]any `json:"tool_input,omitempty"`

	// Tool result fields (for tool_result blocks)
	ToolResultForID *string `json:"tool_result_for_id,omitempty"` // References tool_use block ID
	ToolContent     *string `json:"tool_content,omitempty"`
	IsError         bool    `json:"is_error"`

	// Image/Document source
	Source map[string]any `json:"source,omitempty"`

	// Web search results
	WebSearchResults map[string]any `json:"web_search_results,omitempty"`

	// Metadata
	Metadata  map[string]any `json:"metadata"`
	CreatedAt time.Time      `json:"created_at"`
}

// =========================================================================
// Compaction types
// =========================================================================

// CompactionEvent represents a context compaction event
type CompactionEvent struct {
	ID                  string    `json:"id"`
	SessionID           string    `json:"session_id"`
	Strategy            string    `json:"strategy"`
	OriginalTokens      int       `json:"original_tokens"`
	CompactedTokens     int       `json:"compacted_tokens"`
	MessagesRemoved     int       `json:"messages_removed"`
	SummaryContent      string    `json:"summary_content,omitempty"`
	PreservedMessageIDs []string  `json:"preserved_message_ids"`
	ModelUsed           string    `json:"model_used,omitempty"`
	DurationMs          int64     `json:"duration_ms"`
	CreatedAt           time.Time `json:"created_at"`
}

// =========================================================================
// Run types (event-driven)
// =========================================================================

// Run represents an agent run (a single prompt-response cycle).
type Run struct {
	ID                   string            `json:"id"`
	SessionID            string            `json:"session_id"`
	State                runstate.RunState `json:"state"`
	AgentName            string            `json:"agent_name"`
	Prompt               string            `json:"prompt"`
	ResponseText         *string           `json:"response_text,omitempty"`
	StopReason           *string           `json:"stop_reason,omitempty"`
	InputTokens          int               `json:"input_tokens"`
	OutputTokens         int               `json:"output_tokens"`
	IterationCount       int               `json:"iteration_count"`
	ToolIterations       int               `json:"tool_iterations"`
	ErrorMessage         *string           `json:"error_message,omitempty"`
	ErrorType            *string           `json:"error_type,omitempty"`
	InstanceID           *string           `json:"instance_id,omitempty"`        // Instance that created the run
	WorkerInstanceID     *string           `json:"worker_instance_id,omitempty"` // Instance processing the run
	LastAPICallAt        *time.Time        `json:"last_api_call_at,omitempty"`
	ContinuationRequired bool              `json:"continuation_required"`
	Metadata             map[string]any    `json:"metadata"`
	StartedAt            time.Time         `json:"started_at"`
	FinalizedAt          *time.Time        `json:"finalized_at,omitempty"`
}

// CreateRunParams contains parameters for creating a new run.
type CreateRunParams struct {
	SessionID  string
	AgentName  string
	Prompt     string
	InstanceID string
	Metadata   map[string]any
}

// UpdateRunStateParams contains parameters for updating run state.
type UpdateRunStateParams struct {
	State                runstate.RunState
	RequiredState        runstate.RunState // If set, only update if current state matches (for atomic transitions)
	ResponseText         *string
	StopReason           *string
	InputTokens          int
	OutputTokens         int
	ToolIterations       int
	ErrorMessage         *string
	ErrorType            *string
	ContinuationRequired *bool
}

// UpdateRunIterationParams contains parameters for updating run iteration.
type UpdateRunIterationParams struct {
	IncrementIteration bool
	IncrementTools     bool
	InputTokens        int
	OutputTokens       int
	LastAPICallAt      time.Time
}

// =========================================================================
// Tool Execution types
// =========================================================================

// ToolExecution represents a tool execution record.
type ToolExecution struct {
	ID                string                      `json:"id"`
	RunID             string                      `json:"run_id"`
	State             runstate.ToolExecutionState `json:"state"`
	ToolUseBlockID    string                      `json:"tool_use_block_id"`
	ToolResultBlockID *string                     `json:"tool_result_block_id,omitempty"`
	ToolName          string                      `json:"tool_name"`
	ToolInput         map[string]any              `json:"tool_input"`
	ToolOutput        *string                     `json:"tool_output,omitempty"`
	ErrorMessage      *string                     `json:"error_message,omitempty"`
	InstanceID        *string                     `json:"instance_id,omitempty"`
	AttemptCount      int                         `json:"attempt_count"`
	MaxAttempts       int                         `json:"max_attempts"`
	CreatedAt         time.Time                   `json:"created_at"`
	StartedAt         *time.Time                  `json:"started_at,omitempty"`
	CompletedAt       *time.Time                  `json:"completed_at,omitempty"`
}

// CreateToolExecutionParams contains parameters for creating a tool execution.
type CreateToolExecutionParams struct {
	RunID          string
	ToolUseBlockID string
	ToolName       string
	ToolInput      map[string]any
	MaxAttempts    int
}

// UpdateToolExecutionStateParams contains parameters for updating tool execution state.
type UpdateToolExecutionStateParams struct {
	State             runstate.ToolExecutionState
	ToolOutput        *string
	ErrorMessage      *string
	ToolResultBlockID *string
}

// =========================================================================
// Instance types
// =========================================================================

// Instance represents a running agentpg client instance.
type Instance struct {
	ID              string         `json:"id"`
	Hostname        *string        `json:"hostname,omitempty"`
	PID             *int           `json:"pid,omitempty"`
	Version         *string        `json:"version,omitempty"`
	Metadata        map[string]any `json:"metadata"`
	CreatedAt       time.Time      `json:"created_at"`
	LastHeartbeatAt time.Time      `json:"last_heartbeat_at"`
}

// RegisterInstanceParams contains parameters for registering an instance.
type RegisterInstanceParams struct {
	ID       string
	Hostname string
	PID      int
	Version  string
	Metadata map[string]any
}

// =========================================================================
// Leader types
// =========================================================================

// Leader represents the current leader for cleanup operations.
type Leader struct {
	Name      string    `json:"name"`
	LeaderID  string    `json:"leader_id"`
	ElectedAt time.Time `json:"elected_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

// LeaderElectParams contains parameters for leader election.
type LeaderElectParams struct {
	LeaderID string
	TTL      time.Duration
}

// =========================================================================
// Agent registration types
// =========================================================================

// RegisteredAgent represents a registered agent definition.
type RegisteredAgent struct {
	Name         string         `json:"name"`
	Description  *string        `json:"description,omitempty"`
	Model        string         `json:"model"`
	SystemPrompt *string        `json:"system_prompt,omitempty"`
	MaxTokens    *int           `json:"max_tokens,omitempty"`
	Temperature  *float32       `json:"temperature,omitempty"`
	Config       map[string]any `json:"config"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

// RegisterAgentParams contains parameters for registering an agent.
type RegisterAgentParams struct {
	Name         string
	Description  string
	Model        string
	SystemPrompt string
	MaxTokens    *int
	Temperature  *float32
	Config       map[string]any
}

// =========================================================================
// Tool registration types
// =========================================================================

// RegisteredTool represents a registered tool definition.
type RegisteredTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
	Metadata    map[string]any `json:"metadata"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

// RegisterToolParams contains parameters for registering a tool.
type RegisterToolParams struct {
	Name        string
	Description string
	InputSchema map[string]any
	Metadata    map[string]any
}
