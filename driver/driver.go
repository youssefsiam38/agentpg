// Package driver defines the interfaces for database drivers used by AgentPG.
package driver

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Driver provides database connectivity with generic transaction support.
// The TTx type parameter represents the native transaction type for the driver
// (e.g., pgx.Tx for pgxv5, *sql.Tx for database/sql).
type Driver[TTx any] interface {
	// Store returns the store interface for database operations.
	Store() Store[TTx]

	// Listener returns the listener for LISTEN/NOTIFY.
	// May return nil if the driver doesn't support LISTEN/NOTIFY.
	Listener() Listener

	// BeginTx starts a new transaction.
	BeginTx(ctx context.Context) (TTx, error)

	// CommitTx commits a transaction.
	CommitTx(ctx context.Context, tx TTx) error

	// RollbackTx rolls back a transaction.
	RollbackTx(ctx context.Context, tx TTx) error

	// Close closes the driver and releases resources.
	Close() error
}

// Store provides all database operations.
// The TTx type parameter represents the native transaction type.
type Store[TTx any] interface {
	// Session operations
	CreateSession(ctx context.Context, params CreateSessionParams) (*Session, error)
	CreateSessionTx(ctx context.Context, tx TTx, params CreateSessionParams) (*Session, error)
	GetSession(ctx context.Context, id uuid.UUID) (*Session, error)
	UpdateSession(ctx context.Context, id uuid.UUID, updates map[string]any) error
	// ListSessions returns sessions with optional filtering and pagination.
	// Returns (sessions, totalCount, error). Used by admin UI for browsing all sessions.
	ListSessions(ctx context.Context, params ListSessionsParams) ([]*Session, int, error)
	// ListTenants returns distinct tenant IDs with session counts.
	ListTenants(ctx context.Context) ([]TenantInfo, error)

	// Agent operations
	UpsertAgent(ctx context.Context, agent *AgentDefinition) error
	GetAgent(ctx context.Context, name string) (*AgentDefinition, error)
	DeleteAgent(ctx context.Context, name string) error
	ListAgents(ctx context.Context) ([]*AgentDefinition, error)

	// Tool operations
	UpsertTool(ctx context.Context, tool *ToolDefinition) error
	GetTool(ctx context.Context, name string) (*ToolDefinition, error)
	DeleteTool(ctx context.Context, name string) error
	ListTools(ctx context.Context) ([]*ToolDefinition, error)

	// Run operations
	CreateRun(ctx context.Context, params CreateRunParams) (*Run, error)
	CreateRunTx(ctx context.Context, tx TTx, params CreateRunParams) (*Run, error)
	GetRun(ctx context.Context, id uuid.UUID) (*Run, error)
	UpdateRun(ctx context.Context, id uuid.UUID, updates map[string]any) error
	UpdateRunState(ctx context.Context, id uuid.UUID, state RunState, updates map[string]any) error
	// ClaimRuns claims pending runs for processing. runMode is optional ("batch", "streaming", or empty for any).
	ClaimRuns(ctx context.Context, instanceID string, maxCount int, runMode string) ([]*Run, error)
	GetRunsBySession(ctx context.Context, sessionID uuid.UUID, limit int) ([]*Run, error)
	GetStuckPendingToolsRuns(ctx context.Context, limit int) ([]*Run, error)
	// ListRuns returns runs with optional filtering and pagination.
	// Returns (runs, totalCount, error). Used by admin UI for browsing all runs.
	ListRuns(ctx context.Context, params ListRunsParams) ([]*Run, int, error)

	// Iteration operations
	CreateIteration(ctx context.Context, params CreateIterationParams) (*Iteration, error)
	GetIteration(ctx context.Context, id uuid.UUID) (*Iteration, error)
	UpdateIteration(ctx context.Context, id uuid.UUID, updates map[string]any) error
	GetIterationsForPoll(ctx context.Context, instanceID string, pollInterval time.Duration, maxCount int) ([]*Iteration, error)
	GetIterationsByRun(ctx context.Context, runID uuid.UUID) ([]*Iteration, error)

	// Tool execution operations
	CreateToolExecution(ctx context.Context, params CreateToolExecutionParams) (*ToolExecution, error)
	CreateToolExecutions(ctx context.Context, params []CreateToolExecutionParams) ([]*ToolExecution, error)
	// CreateToolExecutionsAndUpdateRunState atomically creates tool executions and updates the run state.
	// This prevents partial state on crash between tool creation and run state update.
	CreateToolExecutionsAndUpdateRunState(ctx context.Context, params []CreateToolExecutionParams, runID uuid.UUID, state RunState, runUpdates map[string]any) ([]*ToolExecution, error)
	GetToolExecution(ctx context.Context, id uuid.UUID) (*ToolExecution, error)
	UpdateToolExecution(ctx context.Context, id uuid.UUID, updates map[string]any) error
	ClaimToolExecutions(ctx context.Context, instanceID string, maxCount int) ([]*ToolExecution, error)
	CompleteToolExecution(ctx context.Context, id uuid.UUID, output string, isError bool, errorMsg string) error
	GetToolExecutionsByRun(ctx context.Context, runID uuid.UUID) ([]*ToolExecution, error)
	GetToolExecutionsByIteration(ctx context.Context, iterationID uuid.UUID) ([]*ToolExecution, error)
	GetPendingToolExecutionsForRun(ctx context.Context, runID uuid.UUID) ([]*ToolExecution, error)
	// ListToolExecutions returns tool executions with optional filtering and pagination.
	// Returns (executions, totalCount, error). Used by admin UI for browsing all tool executions.
	ListToolExecutions(ctx context.Context, params ListToolExecutionsParams) ([]*ToolExecution, int, error)

	// Tool execution retry operations
	// RetryToolExecution resets a failed tool execution to pending with a scheduled delay.
	// Used for regular errors that should be retried with exponential backoff.
	RetryToolExecution(ctx context.Context, id uuid.UUID, scheduledAt time.Time, lastError string) error

	// SnoozeToolExecution resets a tool execution to pending without consuming an attempt.
	// Decrements attempt_count and increments snooze_count.
	SnoozeToolExecution(ctx context.Context, id uuid.UUID, scheduledAt time.Time) error

	// DiscardToolExecution marks a tool execution as permanently failed (no retry).
	// Used for ToolCancel/ToolDiscard errors.
	DiscardToolExecution(ctx context.Context, id uuid.UUID, errorMsg string) error

	// CompleteToolsAndContinueRun atomically creates the tool_result message and transitions
	// the run back to pending state for the next iteration. This prevents partial state
	// on crash between message creation and run state update.
	CompleteToolsAndContinueRun(ctx context.Context, sessionID, runID uuid.UUID, contentBlocks []ContentBlock) (*Message, error)

	// Run rescue operations
	// GetStuckRuns returns runs stuck in non-terminal states eligible for rescue.
	GetStuckRuns(ctx context.Context, timeout time.Duration, maxRescueAttempts, limit int) ([]*Run, error)

	// RescueRun resets a stuck run to pending state for reprocessing.
	// Increments rescue_attempts and sets last_rescue_at.
	RescueRun(ctx context.Context, id uuid.UUID) error

	// Message operations
	CreateMessage(ctx context.Context, params CreateMessageParams) (*Message, error)
	GetMessage(ctx context.Context, id uuid.UUID) (*Message, error)
	GetMessages(ctx context.Context, sessionID uuid.UUID, limit int) ([]*Message, error)
	GetMessagesByRun(ctx context.Context, runID uuid.UUID) ([]*Message, error)
	GetMessagesForRunContext(ctx context.Context, runID uuid.UUID) ([]*Message, error)
	// GetMessagesWithRunInfo returns messages for a session with joined run information.
	// This is an optimized query that returns messages with agent name, depth, parent run ID,
	// and state in a single query, avoiding N+1 queries when building hierarchical views.
	GetMessagesWithRunInfo(ctx context.Context, sessionID uuid.UUID, limit int) ([]*MessageWithRunInfo, error)
	UpdateMessage(ctx context.Context, id uuid.UUID, updates map[string]any) error
	DeleteMessage(ctx context.Context, id uuid.UUID) error

	// Content block operations
	CreateContentBlocks(ctx context.Context, messageID uuid.UUID, blocks []ContentBlock) error
	GetContentBlocks(ctx context.Context, messageID uuid.UUID) ([]ContentBlock, error)

	// Instance operations
	RegisterInstance(ctx context.Context, params RegisterInstanceParams) error
	UnregisterInstance(ctx context.Context, instanceID string) error
	UpdateHeartbeat(ctx context.Context, instanceID string) error
	GetInstance(ctx context.Context, instanceID string) (*Instance, error)
	ListInstances(ctx context.Context) ([]*Instance, error)
	GetStaleInstances(ctx context.Context, ttl time.Duration) ([]string, error)
	DeleteStaleInstances(ctx context.Context, ttl time.Duration) (int, error)
	// GetInstanceActiveCounts returns the active run and tool counts for an instance.
	// Counts are calculated on-the-fly by querying runs and tool_executions tables.
	GetInstanceActiveCounts(ctx context.Context, instanceID string) (activeRuns, activeTools int, err error)
	// GetAllInstanceActiveCounts returns the active run and tool counts for all instances.
	// Returns a map of instance ID to [activeRuns, activeTools].
	GetAllInstanceActiveCounts(ctx context.Context) (map[string][2]int, error)

	// Instance capability operations
	RegisterInstanceAgent(ctx context.Context, instanceID, agentName string) error
	RegisterInstanceTool(ctx context.Context, instanceID, toolName string) error
	UnregisterInstanceAgent(ctx context.Context, instanceID, agentName string) error
	UnregisterInstanceTool(ctx context.Context, instanceID, toolName string) error
	GetInstanceAgents(ctx context.Context, instanceID string) ([]string, error)
	GetInstanceTools(ctx context.Context, instanceID string) ([]string, error)

	// Leader election
	TryAcquireLeader(ctx context.Context, instanceID string, ttl time.Duration) (bool, error)
	RefreshLeader(ctx context.Context, instanceID string, ttl time.Duration) error
	GetLeader(ctx context.Context) (string, error)
	IsLeader(ctx context.Context, instanceID string) (bool, error)
	ReleaseLeader(ctx context.Context, instanceID string) error

	// Compaction operations
	CreateCompactionEvent(ctx context.Context, params CreateCompactionEventParams) (*CompactionEvent, error)
	ArchiveMessage(ctx context.Context, compactionEventID, messageID, sessionID uuid.UUID, originalMessage map[string]any) error
	GetCompactionEvents(ctx context.Context, sessionID uuid.UUID, limit int) ([]*CompactionEvent, error)
	// GetCompactionStats returns aggregate statistics for all compaction events.
	GetCompactionStats(ctx context.Context) (*CompactionStats, error)
}

// CompactionStats contains aggregate compaction statistics.
type CompactionStats struct {
	TotalCompactions      int
	TotalTokensSaved      int
	TotalMessagesArchived int
	AvgReductionPercent   float64
}

// Listener provides LISTEN/NOTIFY functionality.
type Listener interface {
	// Listen starts listening for notifications on the specified channels.
	Listen(ctx context.Context, channels ...string) error

	// Notifications returns a channel for receiving notifications.
	// The channel is closed when the listener is closed.
	Notifications() <-chan Notification

	// Close stops listening and releases resources.
	Close() error
}

// Notification represents a PostgreSQL NOTIFY payload.
type Notification struct {
	Channel string
	Payload string
}

// Parameter structs for database operations

// CreateSessionParams contains parameters for creating a session.
type CreateSessionParams struct {
	TenantID        string
	UserID          string
	ParentSessionID *uuid.UUID
	Metadata        map[string]any
}

// CreateRunParams contains parameters for creating a run.
type CreateRunParams struct {
	SessionID             uuid.UUID
	AgentName             string
	Prompt                string
	RunMode               string // "batch" or "streaming", defaults to "batch"
	ParentRunID           *uuid.UUID
	ParentToolExecutionID *uuid.UUID
	Depth                 int
	CreatedByInstanceID   string
	Metadata              map[string]any
}

// CreateIterationParams contains parameters for creating an iteration.
type CreateIterationParams struct {
	RunID           uuid.UUID
	IterationNumber int
	TriggerType     string
	IsStreaming     bool // TRUE if using streaming API instead of batch API
}

// CreateToolExecutionParams contains parameters for creating a tool execution.
type CreateToolExecutionParams struct {
	RunID       uuid.UUID
	IterationID uuid.UUID
	ToolUseID   string
	ToolName    string
	ToolInput   []byte // JSON
	IsAgentTool bool
	AgentName   *string
	MaxAttempts int
}

// CreateMessageParams contains parameters for creating a message.
type CreateMessageParams struct {
	SessionID   uuid.UUID
	RunID       *uuid.UUID
	Role        MessageRole
	Content     []ContentBlock
	Usage       Usage
	IsPreserved bool
	IsSummary   bool
	Metadata    map[string]any
}

// RegisterInstanceParams contains parameters for registering an instance.
type RegisterInstanceParams struct {
	ID                 string
	Name               string
	Hostname           string
	PID                int
	Version            string
	MaxConcurrentRuns  int
	MaxConcurrentTools int
	Metadata           map[string]any
}

// CreateCompactionEventParams contains parameters for creating a compaction event.
type CreateCompactionEventParams struct {
	SessionID           uuid.UUID
	Strategy            string
	OriginalTokens      int
	CompactedTokens     int
	MessagesRemoved     int
	SummaryContent      *string
	PreservedMessageIDs []uuid.UUID
	ModelUsed           *string
	DurationMS          *int64
}

// ListRunsParams contains parameters for listing runs with optional filtering.
type ListRunsParams struct {
	TenantID  string     // Filter by tenant (requires join with sessions)
	SessionID *uuid.UUID // Filter by session
	AgentName string     // Filter by agent name
	State     string     // Filter by run state
	RunMode   string     // Filter by run mode ("batch" or "streaming")
	Limit     int        // Maximum number of results
	Offset    int        // Offset for pagination
}

// ListToolExecutionsParams contains parameters for listing tool executions with optional filtering.
type ListToolExecutionsParams struct {
	RunID       *uuid.UUID // Filter by run
	IterationID *uuid.UUID // Filter by iteration
	ToolName    string     // Filter by tool name
	State       string     // Filter by execution state
	IsAgentTool *bool      // Filter by agent tool flag
	Limit       int        // Maximum number of results
	Offset      int        // Offset for pagination
}

// ListSessionsParams contains parameters for listing sessions with optional filtering.
type ListSessionsParams struct {
	TenantID string // Filter by tenant
	Limit    int    // Maximum number of results
	Offset   int    // Offset for pagination
	OrderBy  string // Field to order by (created_at, updated_at, user_id)
	OrderDir string // Order direction (asc, desc)
}

// TenantInfo contains tenant information with session count.
type TenantInfo struct {
	TenantID     string
	SessionCount int
}

// Type aliases for convenience (re-exported from main package)
type (
	Session = struct {
		ID              uuid.UUID
		TenantID        string
		UserID          string
		ParentSessionID *uuid.UUID
		Depth           int
		Metadata        map[string]any
		CompactionCount int
		CreatedAt       time.Time
		UpdatedAt       time.Time
	}

	AgentDefinition = struct {
		Name         string
		Description  string
		Model        string
		SystemPrompt string
		ToolNames    []string
		MaxTokens    *int
		Temperature  *float64
		TopK         *int
		TopP         *float64
		Config       map[string]any
		CreatedAt    time.Time
		UpdatedAt    time.Time
	}

	ToolDefinition = struct {
		Name        string
		Description string
		InputSchema map[string]any
		IsAgentTool bool
		AgentName   *string
		Metadata    map[string]any
		CreatedAt   time.Time
		UpdatedAt   time.Time
	}

	Run = struct {
		ID                       uuid.UUID
		SessionID                uuid.UUID
		AgentName                string
		RunMode                  string // "batch" or "streaming"
		ParentRunID              *uuid.UUID
		ParentToolExecutionID    *uuid.UUID
		Depth                    int
		State                    RunState
		PreviousState            *RunState
		Prompt                   string
		CurrentIteration         int
		CurrentIterationID       *uuid.UUID
		ResponseText             *string
		StopReason               *string
		InputTokens              int
		OutputTokens             int
		CacheCreationInputTokens int
		CacheReadInputTokens     int
		IterationCount           int
		ToolIterations           int
		ErrorMessage             *string
		ErrorType                *string
		CreatedByInstanceID      *string
		ClaimedByInstanceID      *string
		ClaimedAt                *time.Time
		Metadata                 map[string]any
		CreatedAt                time.Time
		StartedAt                *time.Time
		FinalizedAt              *time.Time
		// Rescue tracking
		RescueAttempts int
		LastRescueAt   *time.Time
	}

	RunState = string

	Iteration = struct {
		ID                       uuid.UUID
		RunID                    uuid.UUID
		IterationNumber          int
		IsStreaming              bool // TRUE if using streaming API instead of batch API
		BatchID                  *string
		BatchRequestID           *string
		BatchStatus              *BatchStatus
		BatchSubmittedAt         *time.Time
		BatchCompletedAt         *time.Time
		BatchExpiresAt           *time.Time
		BatchPollCount           int
		BatchLastPollAt          *time.Time
		StreamingStartedAt       *time.Time // Only for streaming mode
		StreamingCompletedAt     *time.Time // Only for streaming mode
		TriggerType              string
		RequestMessageIDs        []uuid.UUID
		StopReason               *string
		ResponseMessageID        *uuid.UUID
		HasToolUse               bool
		ToolExecutionCount       int
		InputTokens              int
		OutputTokens             int
		CacheCreationInputTokens int
		CacheReadInputTokens     int
		ErrorMessage             *string
		ErrorType                *string
		CreatedAt                time.Time
		StartedAt                *time.Time
		CompletedAt              *time.Time
	}

	BatchStatus = string

	ToolExecution = struct {
		ID                  uuid.UUID
		RunID               uuid.UUID
		IterationID         uuid.UUID
		State               ToolExecutionState
		ToolUseID           string
		ToolName            string
		ToolInput           []byte
		IsAgentTool         bool
		AgentName           *string
		ChildRunID          *uuid.UUID
		ToolOutput          *string
		IsError             bool
		ErrorMessage        *string
		ClaimedByInstanceID *string
		ClaimedAt           *time.Time
		AttemptCount        int
		MaxAttempts         int
		// Retry scheduling
		ScheduledAt time.Time
		SnoozeCount int
		LastError   *string
		// Timestamps
		CreatedAt   time.Time
		StartedAt   *time.Time
		CompletedAt *time.Time
	}

	ToolExecutionState = string

	Message = struct {
		ID          uuid.UUID
		SessionID   uuid.UUID
		RunID       *uuid.UUID
		Role        MessageRole
		Content     []ContentBlock
		Usage       Usage
		IsPreserved bool
		IsSummary   bool
		Metadata    map[string]any
		CreatedAt   time.Time
		UpdatedAt   time.Time
	}

	MessageRole = string

	ContentBlock = struct {
		Type               string
		Text               string
		ToolUseID          string
		ToolName           string
		ToolInput          []byte
		ToolResultForUseID string
		ToolContent        string
		IsError            bool
		Source             []byte
		SearchResults      []byte
		Metadata           map[string]any
	}

	Usage = struct {
		InputTokens              int
		OutputTokens             int
		CacheCreationInputTokens int
		CacheReadInputTokens     int
	}

	Instance = struct {
		ID                 string
		Name               string
		Hostname           *string
		PID                *int
		Version            *string
		MaxConcurrentRuns  int
		MaxConcurrentTools int
		ActiveRunCount     int
		ActiveToolCount    int
		Metadata           map[string]any
		CreatedAt          time.Time
		LastHeartbeatAt    time.Time
	}

	CompactionEvent = struct {
		ID                  uuid.UUID
		SessionID           uuid.UUID
		Strategy            string
		OriginalTokens      int
		CompactedTokens     int
		MessagesRemoved     int
		SummaryContent      *string
		PreservedMessageIDs []uuid.UUID
		ModelUsed           *string
		DurationMS          *int64
		CreatedAt           time.Time
	}

	// MessageWithRunInfo contains a message with its associated run information.
	// Used for efficiently fetching messages with run context in a single query,
	// avoiding N+1 queries when building hierarchical conversation views.
	MessageWithRunInfo = struct {
		// Embedded Message fields
		ID          uuid.UUID
		SessionID   uuid.UUID
		RunID       *uuid.UUID
		Role        MessageRole
		Content     []ContentBlock
		Usage       Usage
		IsPreserved bool
		IsSummary   bool
		Metadata    map[string]any
		CreatedAt   time.Time
		UpdatedAt   time.Time
		// Run information (from LEFT JOIN with runs table)
		RunAgentName  *string    // Agent name from the run
		RunDepth      *int       // Depth level of the run (0=root, 1+=child)
		ParentRunID   *uuid.UUID // Parent run ID for child runs
		RunState      *string    // Current state of the run
	}
)
