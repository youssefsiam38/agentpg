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
	GetSessionByIdentifier(ctx context.Context, tenantID, identifier string) (*Session, error)
	UpdateSession(ctx context.Context, id uuid.UUID, updates map[string]any) error

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

	// Iteration operations
	CreateIteration(ctx context.Context, params CreateIterationParams) (*Iteration, error)
	GetIteration(ctx context.Context, id uuid.UUID) (*Iteration, error)
	UpdateIteration(ctx context.Context, id uuid.UUID, updates map[string]any) error
	GetIterationsForPoll(ctx context.Context, instanceID string, pollInterval time.Duration, maxCount int) ([]*Iteration, error)
	GetIterationsByRun(ctx context.Context, runID uuid.UUID) ([]*Iteration, error)

	// Tool execution operations
	CreateToolExecution(ctx context.Context, params CreateToolExecutionParams) (*ToolExecution, error)
	CreateToolExecutions(ctx context.Context, params []CreateToolExecutionParams) ([]*ToolExecution, error)
	GetToolExecution(ctx context.Context, id uuid.UUID) (*ToolExecution, error)
	UpdateToolExecution(ctx context.Context, id uuid.UUID, updates map[string]any) error
	ClaimToolExecutions(ctx context.Context, instanceID string, maxCount int) ([]*ToolExecution, error)
	CompleteToolExecution(ctx context.Context, id uuid.UUID, output string, isError bool, errorMsg string) error
	GetToolExecutionsByRun(ctx context.Context, runID uuid.UUID) ([]*ToolExecution, error)
	GetToolExecutionsByIteration(ctx context.Context, iterationID uuid.UUID) ([]*ToolExecution, error)
	GetPendingToolExecutionsForRun(ctx context.Context, runID uuid.UUID) ([]*ToolExecution, error)

	// Message operations
	CreateMessage(ctx context.Context, params CreateMessageParams) (*Message, error)
	GetMessage(ctx context.Context, id uuid.UUID) (*Message, error)
	GetMessages(ctx context.Context, sessionID uuid.UUID, limit int) ([]*Message, error)
	GetMessagesByRun(ctx context.Context, runID uuid.UUID) ([]*Message, error)
	GetMessagesForRunContext(ctx context.Context, runID uuid.UUID) ([]*Message, error)
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
	UpdateInstanceCounts(ctx context.Context, instanceID string, runDelta, toolDelta int) error

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
	Identifier      string
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

// Type aliases for convenience (re-exported from main package)
type (
	Session = struct {
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
		CreatedAt           time.Time
		StartedAt           *time.Time
		CompletedAt         *time.Time
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
)
