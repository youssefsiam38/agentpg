package storage

import (
	"context"
	"time"

	"github.com/youssefsiam38/agentpg/runstate"
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

	// Compaction operations
	SaveCompactionEvent(ctx context.Context, event *CompactionEvent) error
	GetCompactionHistory(ctx context.Context, sessionID string) ([]*CompactionEvent, error)
	ArchiveMessages(ctx context.Context, compactionEventID string, messages []*Message) error

	// =========================================================================
	// Run operations
	// =========================================================================

	// CreateRun creates a new run record in the running state.
	CreateRun(ctx context.Context, params *CreateRunParams) (string, error)

	// GetRun returns a run by ID.
	GetRun(ctx context.Context, runID string) (*Run, error)

	// GetSessionRuns returns all runs for a session, ordered by started_at DESC.
	GetSessionRuns(ctx context.Context, sessionID string) ([]*Run, error)

	// UpdateRunState transitions a run to a new state.
	// Returns ErrRunNotFound if run doesn't exist or ErrInvalidTransition if
	// the transition is not allowed.
	UpdateRunState(ctx context.Context, runID string, params *UpdateRunStateParams) error

	// GetRunMessages returns all messages associated with a run.
	GetRunMessages(ctx context.Context, runID string) ([]*Message, error)

	// GetStuckRuns returns all runs that have been running longer than the horizon.
	// Used by the cleanup service to detect orphaned runs.
	GetStuckRuns(ctx context.Context, horizon time.Time) ([]*Run, error)

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
// Existing types
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

// Message represents a stored message
type Message struct {
	ID          string         `json:"id"`
	SessionID   string         `json:"session_id"`
	RunID       *string        `json:"run_id,omitempty"` // Link to run
	Role        string         `json:"role"`
	Content     any            `json:"content"` // Stored as JSONB
	Usage       *MessageUsage  `json:"usage"`   // Token usage breakdown
	Metadata    map[string]any `json:"metadata"`
	IsPreserved bool           `json:"is_preserved"`
	IsSummary   bool           `json:"is_summary"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

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
// Run types
// =========================================================================

// Run represents an agent run (a single prompt-response cycle).
type Run struct {
	ID             string            `json:"id"`
	SessionID      string            `json:"session_id"`
	State          runstate.RunState `json:"state"`
	AgentName      string            `json:"agent_name"`
	Prompt         string            `json:"prompt"`
	ResponseText   *string           `json:"response_text,omitempty"`
	StopReason     *string           `json:"stop_reason,omitempty"`
	InputTokens    int               `json:"input_tokens"`
	OutputTokens   int               `json:"output_tokens"`
	ToolIterations int               `json:"tool_iterations"`
	ErrorMessage   *string           `json:"error_message,omitempty"`
	ErrorType      *string           `json:"error_type,omitempty"`
	InstanceID     *string           `json:"instance_id,omitempty"`
	Metadata       map[string]any    `json:"metadata"`
	StartedAt      time.Time         `json:"started_at"`
	FinalizedAt    *time.Time        `json:"finalized_at,omitempty"`
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
	State          runstate.RunState
	ResponseText   *string
	StopReason     *string
	InputTokens    int
	OutputTokens   int
	ToolIterations int
	ErrorMessage   *string
	ErrorType      *string
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
