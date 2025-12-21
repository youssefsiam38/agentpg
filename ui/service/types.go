package service

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/youssefsiam38/agentpg/driver"
)

// Validation constants for query parameters
const (
	// MaxPageLimit is the maximum allowed page size to prevent resource exhaustion
	MaxPageLimit = 1000
	// MinPageLimit is the minimum allowed page size
	MinPageLimit = 1
)

// AllowedSessionOrderBy is the whitelist of valid OrderBy values for sessions
var AllowedSessionOrderBy = map[string]bool{
	"":           true, // empty means default ordering
	"created_at": true,
	"updated_at": true,
}

// AllowedRunOrderBy is the whitelist of valid OrderBy values for runs
var AllowedRunOrderBy = map[string]bool{
	"":             true, // empty means default ordering
	"created_at":   true,
	"finalized_at": true,
}

// AllowedOrderDir is the whitelist of valid OrderDir values
var AllowedOrderDir = map[string]bool{
	"":     true, // empty means default direction
	"asc":  true,
	"desc": true,
}

// ValidateOrderBy validates an OrderBy value against the allowed whitelist.
// Returns the validated value or an empty string if invalid.
func ValidateOrderBy(value string, allowed map[string]bool) string {
	if allowed[value] {
		return value
	}
	return ""
}

// ValidateOrderDir validates an OrderDir value.
// Returns the validated value or an empty string if invalid.
func ValidateOrderDir(value string) string {
	if AllowedOrderDir[value] {
		return value
	}
	return ""
}

// ValidateLimit ensures limit is within acceptable bounds.
func ValidateLimit(limit int) int {
	if limit < MinPageLimit {
		return MinPageLimit
	}
	if limit > MaxPageLimit {
		return MaxPageLimit
	}
	return limit
}

// ValidateOffset ensures offset is non-negative.
func ValidateOffset(offset int) int {
	if offset < 0 {
		return 0
	}
	return offset
}

// DashboardStats contains aggregated statistics for the dashboard.
type DashboardStats struct {
	// Session counts
	TotalSessions  int `json:"total_sessions"`
	ActiveSessions int `json:"active_sessions"`
	SessionsToday  int `json:"sessions_today"`

	// Run counts
	TotalRuns        int `json:"total_runs"`
	ActiveRuns       int `json:"active_runs"`
	PendingRuns      int `json:"pending_runs"`
	CompletedRuns24h int `json:"completed_runs_24h"`
	FailedRuns24h    int `json:"failed_runs_24h"`

	// Tool execution counts
	PendingTools   int `json:"pending_tools"`
	RunningTools   int `json:"running_tools"`
	FailedTools24h int `json:"failed_tools_24h"`

	// Instance counts
	ActiveInstances  int    `json:"active_instances"`
	LeaderInstanceID string `json:"leader_instance_id,omitempty"`

	// Queue depths by state
	RunsByState  map[string]int `json:"runs_by_state"`
	ToolsByState map[string]int `json:"tools_by_state"`

	// Recent activity
	RecentRuns       []*RunSummary           `json:"recent_runs"`
	RecentToolErrors []*ToolExecutionSummary `json:"recent_tool_errors"`

	// Tenant breakdown (for admin mode)
	TenantCounts map[string]int `json:"tenant_counts,omitempty"`

	// Token usage insights
	TotalTokens24h       int `json:"total_tokens_24h"`
	TotalInputTokens24h  int `json:"total_input_tokens_24h"`
	TotalOutputTokens24h int `json:"total_output_tokens_24h"`
	AvgTokensPerRun      int `json:"avg_tokens_per_run"`

	// Performance insights
	AvgRunDurationMs    int64   `json:"avg_run_duration_ms"`
	SuccessRate24h      float64 `json:"success_rate_24h"`
	AvgIterationsPerRun float64 `json:"avg_iterations_per_run"`

	// Agent breakdown
	RunsByAgent map[string]int `json:"runs_by_agent"`
	TopAgents   []*AgentStats  `json:"top_agents"`

	// Tool breakdown
	ToolExecutions24h int          `json:"tool_executions_24h"`
	TopTools          []*ToolStats `json:"top_tools"`

	// Recent sessions for quick access
	RecentSessions []*SessionSummary `json:"recent_sessions"`
}

// AgentStats contains statistics for a specific agent.
type AgentStats struct {
	Name           string  `json:"name"`
	RunCount       int     `json:"run_count"`
	CompletedCount int     `json:"completed_count"`
	FailedCount    int     `json:"failed_count"`
	SuccessRate    float64 `json:"success_rate"`
	TotalTokens    int     `json:"total_tokens"`
}

// ToolStats contains statistics for a specific tool.
type ToolStats struct {
	Name           string `json:"name"`
	ExecutionCount int    `json:"execution_count"`
	FailedCount    int    `json:"failed_count"`
	AvgDurationMs  int64  `json:"avg_duration_ms"`
}

// SessionListParams contains parameters for listing sessions.
type SessionListParams struct {
	TenantID string
	Limit    int
	Offset   int
	OrderBy  string // "created_at", "updated_at"
	OrderDir string // "asc", "desc"
}

// SessionList contains a paginated list of sessions.
type SessionList struct {
	Sessions   []*SessionSummary `json:"sessions"`
	TotalCount int               `json:"total_count"`
	HasMore    bool              `json:"has_more"`
}

// SessionSummary contains summary information about a session.
type SessionSummary struct {
	ID              uuid.UUID `json:"id"`
	TenantID        string    `json:"tenant_id"`
	Identifier      string    `json:"identifier"`
	AgentName       string    `json:"agent_name,omitempty"` // Agent from first run
	Depth           int       `json:"depth"`
	RunCount        int       `json:"run_count"`
	MessageCount    int       `json:"message_count"`
	CompactionCount int       `json:"compaction_count"`
	LastActivityAt  time.Time `json:"last_activity_at"`
	CreatedAt       time.Time `json:"created_at"`
}

// SessionDetail contains detailed information about a session.
type SessionDetail struct {
	Session        *driver.Session   `json:"session"`
	ParentSession  *SessionSummary   `json:"parent_session,omitempty"`
	ChildSessions  []*SessionSummary `json:"child_sessions,omitempty"`
	RunCount       int               `json:"run_count"`
	MessageCount   int               `json:"message_count"`
	TokenUsage     TokenUsageSummary `json:"token_usage"`
	RecentRuns     []*RunSummary     `json:"recent_runs"`
	RecentMessages []*MessageSummary `json:"recent_messages"`
	Conversation   *ConversationView `json:"conversation,omitempty"`
}

// RunListParams contains parameters for listing runs.
type RunListParams struct {
	SessionID *uuid.UUID
	TenantID  string
	AgentName string
	State     string
	RunMode   string // "batch", "streaming"
	Limit     int
	Offset    int
	OrderBy   string // "created_at", "finalized_at"
	OrderDir  string // "asc", "desc"
}

// RunList contains a paginated list of runs.
type RunList struct {
	Runs       []*RunSummary `json:"runs"`
	TotalCount int           `json:"total_count"`
	HasMore    bool          `json:"has_more"`
}

// RunSummary contains summary information about a run.
type RunSummary struct {
	ID             uuid.UUID      `json:"id"`
	SessionID      uuid.UUID      `json:"session_id"`
	AgentName      string         `json:"agent_name"`
	RunMode        string         `json:"run_mode"`
	State          string         `json:"state"`
	Depth          int            `json:"depth"`
	HasParent      bool           `json:"has_parent"`
	IterationCount int            `json:"iteration_count"`
	ToolIterations int            `json:"tool_iterations"`
	TotalTokens    int            `json:"total_tokens"`
	Duration       *time.Duration `json:"duration,omitempty"`
	ErrorMessage   *string        `json:"error_message,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
	FinalizedAt    *time.Time     `json:"finalized_at,omitempty"`
}

// RunDetail contains detailed information about a run.
type RunDetail struct {
	Run            *driver.Run             `json:"run"`
	Session        *SessionSummary         `json:"session"`
	Iterations     []*IterationSummary     `json:"iterations"`
	ToolExecutions []*ToolExecutionSummary `json:"tool_executions"`
	Messages       []*MessageSummary       `json:"messages"`

	// Hierarchy info
	ParentRun      *RunSummary   `json:"parent_run,omitempty"`
	ChildRuns      []*RunSummary `json:"child_runs,omitempty"`
	HierarchyDepth int           `json:"hierarchy_depth"`
}

// RunHierarchy represents the hierarchical structure of runs.
type RunHierarchy struct {
	Root *RunNode `json:"root"`
}

// RunNode represents a node in the run hierarchy tree.
type RunNode struct {
	Run      *RunSummary `json:"run"`
	Children []*RunNode  `json:"children,omitempty"`
}

// IterationListParams contains parameters for listing iterations.
type IterationListParams struct {
	RunID  uuid.UUID
	Limit  int
	Offset int
}

// IterationSummary contains summary information about an iteration.
type IterationSummary struct {
	ID              uuid.UUID      `json:"id"`
	RunID           uuid.UUID      `json:"run_id"`
	IterationNumber int            `json:"iteration_number"`
	IsStreaming     bool           `json:"is_streaming"`
	TriggerType     string         `json:"trigger_type"`
	StopReason      *string        `json:"stop_reason,omitempty"`
	HasToolUse      bool           `json:"has_tool_use"`
	ToolCount       int            `json:"tool_count"`
	InputTokens     int            `json:"input_tokens"`
	OutputTokens    int            `json:"output_tokens"`
	Duration        *time.Duration `json:"duration,omitempty"`
	ErrorMessage    *string        `json:"error_message,omitempty"`
	CreatedAt       time.Time      `json:"created_at"`
	CompletedAt     *time.Time     `json:"completed_at,omitempty"`
}

// IterationDetail contains detailed information about an iteration.
type IterationDetail struct {
	Iteration       *driver.Iteration       `json:"iteration"`
	Run             *RunSummary             `json:"run"`
	ToolExecutions  []*ToolExecutionSummary `json:"tool_executions"`
	RequestMessage  *MessageWithBlocks      `json:"request_message,omitempty"`
	ResponseMessage *MessageWithBlocks      `json:"response_message,omitempty"`
}

// ToolExecutionListParams contains parameters for listing tool executions.
type ToolExecutionListParams struct {
	RunID       *uuid.UUID
	IterationID *uuid.UUID
	ToolName    string
	State       string
	IsAgentTool *bool
	Limit       int
	Offset      int
}

// ToolExecutionSummary contains summary information about a tool execution.
type ToolExecutionSummary struct {
	ID           uuid.UUID      `json:"id"`
	RunID        uuid.UUID      `json:"run_id"`
	IterationID  uuid.UUID      `json:"iteration_id"`
	ToolName     string         `json:"tool_name"`
	State        string         `json:"state"`
	IsAgentTool  bool           `json:"is_agent_tool"`
	AgentName    *string        `json:"agent_name,omitempty"`
	ChildRunID   *uuid.UUID     `json:"child_run_id,omitempty"`
	IsError      bool           `json:"is_error"`
	AttemptCount int            `json:"attempt_count"`
	MaxAttempts  int            `json:"max_attempts"`
	Duration     *time.Duration `json:"duration,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
	CompletedAt  *time.Time     `json:"completed_at,omitempty"`
}

// ToolExecutionDetail contains detailed information about a tool execution.
type ToolExecutionDetail struct {
	Execution *driver.ToolExecution `json:"execution"`
	Run       *RunSummary           `json:"run"`
	Iteration *IterationSummary     `json:"iteration"`
	ChildRun  *RunSummary           `json:"child_run,omitempty"`
}

// MessageSummary contains summary information about a message.
type MessageSummary struct {
	ID            uuid.UUID  `json:"id"`
	SessionID     uuid.UUID  `json:"session_id"`
	RunID         *uuid.UUID `json:"run_id,omitempty"`
	Role          string     `json:"role"`
	PreviewText   string     `json:"preview_text"`
	BlockCount    int        `json:"block_count"`
	HasToolUse    bool       `json:"has_tool_use"`
	HasToolResult bool       `json:"has_tool_result"`
	IsPreserved   bool       `json:"is_preserved"`
	IsSummary     bool       `json:"is_summary"`
	TotalTokens   int        `json:"total_tokens"`
	CreatedAt     time.Time  `json:"created_at"`
}

// MessageWithBlocks contains a message with its content blocks.
type MessageWithBlocks struct {
	Message       *driver.Message       `json:"message"`
	ContentBlocks []driver.ContentBlock `json:"content_blocks"`
	RunInfo       *RunSummary           `json:"run_info,omitempty"`
}

// ConversationView contains a conversation with messages.
type ConversationView struct {
	SessionID    uuid.UUID            `json:"session_id"`
	Session      *SessionSummary      `json:"session"`
	AgentName    string               `json:"agent_name,omitempty"` // Agent from first run
	Messages     []*MessageWithBlocks `json:"messages"`
	TotalTokens  int                  `json:"total_tokens"`
	MessageCount int                  `json:"message_count"`
}

// AgentWithStats contains an agent definition with statistics.
type AgentWithStats struct {
	Agent           *driver.AgentDefinition `json:"agent"`
	TotalRuns       int                     `json:"total_runs"`
	ActiveRuns      int                     `json:"active_runs"`
	CompletedRuns   int                     `json:"completed_runs"`
	FailedRuns      int                     `json:"failed_runs"`
	AvgTokensPerRun int                     `json:"avg_tokens_per_run"`
	RegisteredOn    []string                `json:"registered_on"`
	IsActive        bool                    `json:"is_active"` // true if registered on at least one instance
}

// ToolWithStats contains a tool definition with statistics.
type ToolWithStats struct {
	Tool            *driver.ToolDefinition `json:"tool"`
	TotalExecutions int                    `json:"total_executions"`
	PendingCount    int                    `json:"pending_count"`
	CompletedCount  int                    `json:"completed_count"`
	FailedCount     int                    `json:"failed_count"`
	AvgDuration     *time.Duration         `json:"avg_duration,omitempty"`
	RegisteredOn    []string               `json:"registered_on"`
	IsActive        bool                   `json:"is_active"` // true if registered on at least one instance
}

// InstanceWithCapabilities contains instance info with its capabilities.
type InstanceWithCapabilities struct {
	Instance  *driver.Instance `json:"instance"`
	Agents    []string         `json:"agents"`
	Tools     []string         `json:"tools"`
	IsLeader  bool             `json:"is_leader"`
	IsHealthy bool             `json:"is_healthy"`
}

// CompactionEventWithSession contains a compaction event with session info.
type CompactionEventWithSession struct {
	Event   *driver.CompactionEvent `json:"event"`
	Session *SessionSummary         `json:"session"`
}

// TokenUsageSummary contains aggregated token usage.
type TokenUsageSummary struct {
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
	TotalTokens  int     `json:"total_tokens"`
	CacheHitRate float64 `json:"cache_hit_rate"`
}

// CreateSessionRequest contains parameters for creating a new session.
type CreateSessionRequest struct {
	TenantID   string         `json:"tenant_id"`
	Identifier string         `json:"identifier"`
	AgentName  string         `json:"agent_name"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

// SendMessageRequest contains parameters for sending a chat message.
type SendMessageRequest struct {
	SessionID uuid.UUID `json:"session_id"`
	AgentName string    `json:"agent_name"`
	Message   string    `json:"message"`
}

// SendMessageResponse contains the response from sending a message.
type SendMessageResponse struct {
	RunID      uuid.UUID `json:"run_id"`
	State      string    `json:"state"`
	Response   string    `json:"response,omitempty"`
	StopReason string    `json:"stop_reason,omitempty"`
}

// TenantInfo contains information about a tenant.
type TenantInfo struct {
	TenantID     string `json:"tenant_id"`
	SessionCount int    `json:"session_count"`
	RunCount     int    `json:"run_count"`
}

// ContentBlockView is a view-friendly content block.
type ContentBlockView struct {
	Type        string          `json:"type"`
	Text        string          `json:"text,omitempty"`
	ToolUseID   string          `json:"tool_use_id,omitempty"`
	ToolName    string          `json:"tool_name,omitempty"`
	ToolInput   json.RawMessage `json:"tool_input,omitempty"`
	ToolContent string          `json:"tool_content,omitempty"`
	IsError     bool            `json:"is_error,omitempty"`
}

// InstanceWithHealth contains instance info with health status.
type InstanceWithHealth struct {
	Instance           *driver.Instance `json:"instance"`
	Status             string           `json:"status"` // "healthy", "warning", "unhealthy", "unknown"
	TimeSinceHeartbeat *time.Duration   `json:"time_since_heartbeat,omitempty"`
	AgentNames         []string         `json:"agent_names"`
	ToolNames          []string         `json:"tool_names"`
}

// InstanceDetail contains detailed information about an instance.
type InstanceDetail struct {
	Instance           *driver.Instance `json:"instance"`
	Status             string           `json:"status"`
	TimeSinceHeartbeat *time.Duration   `json:"time_since_heartbeat,omitempty"`
	AgentNames         []string         `json:"agent_names"`
	ToolNames          []string         `json:"tool_names"`
	IsLeader           bool             `json:"is_leader"`
}

// LeaderInfo contains information about the current leader.
type LeaderInfo struct {
	LeaderID     string    `json:"leader_id"`
	InstanceName string    `json:"instance_name"`
	ElectedAt    time.Time `json:"elected_at"`
	ExpiresAt    time.Time `json:"expires_at"`
}

// CompactionEventSummary contains summary information about a compaction event.
type CompactionEventSummary struct {
	ID              uuid.UUID      `json:"id"`
	SessionID       uuid.UUID      `json:"session_id"`
	Strategy        string         `json:"strategy"`
	OriginalTokens  int            `json:"original_tokens"`
	CompactedTokens int            `json:"compacted_tokens"`
	TokenReduction  float64        `json:"token_reduction"` // Percentage reduced
	MessagesRemoved int            `json:"messages_removed"`
	SummaryCreated  bool           `json:"summary_created"`
	Duration        *time.Duration `json:"duration,omitempty"`
	CreatedAt       time.Time      `json:"created_at"`
}

// CompactionEventDetail contains detailed information about a compaction event.
type CompactionEventDetail struct {
	Event              *driver.CompactionEvent `json:"event"`
	Session            *SessionSummary         `json:"session"`
	TokenReduction     float64                 `json:"token_reduction"`
	Duration           *time.Duration          `json:"duration,omitempty"`
	ArchivedMessageIDs []uuid.UUID             `json:"archived_message_ids,omitempty"`
}

// CompactionStats contains overall compaction statistics.
type CompactionStats struct {
	TotalCompactions      int     `json:"total_compactions"`
	TotalTokensSaved      int     `json:"total_tokens_saved"`
	TotalMessagesArchived int     `json:"total_messages_archived"`
	AvgReductionPercent   float64 `json:"avg_reduction_percent"`
}
