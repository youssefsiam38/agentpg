package agentpg

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Response is the result of a completed run.
type Response struct {
	// Text is the final text response (extracted from the last assistant message).
	Text string

	// StopReason indicates why the run stopped: "end_turn", "max_tokens", "tool_use", "stop_sequence".
	StopReason string

	// Usage contains cumulative token statistics across all iterations.
	Usage Usage

	// Message is the full final assistant message with content blocks.
	Message *Message

	// IterationCount is the total number of API calls made for this run (batch or streaming).
	IterationCount int

	// ToolIterations is the number of iterations that involved tool_use.
	ToolIterations int
}

// Usage contains token usage statistics from Claude API.
type Usage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
}

// Add combines two Usage values.
func (u Usage) Add(other Usage) Usage {
	return Usage{
		InputTokens:              u.InputTokens + other.InputTokens,
		OutputTokens:             u.OutputTokens + other.OutputTokens,
		CacheCreationInputTokens: u.CacheCreationInputTokens + other.CacheCreationInputTokens,
		CacheReadInputTokens:     u.CacheReadInputTokens + other.CacheReadInputTokens,
	}
}

// Total returns the total number of tokens (input + output).
func (u Usage) Total() int {
	return u.InputTokens + u.OutputTokens
}

// Message represents a conversation message stored in the database.
type Message struct {
	ID        uuid.UUID      `json:"id"`
	SessionID uuid.UUID      `json:"session_id"`
	RunID     *uuid.UUID     `json:"run_id,omitempty"`
	Role      MessageRole    `json:"role"`
	Content   []ContentBlock `json:"content"`
	Usage     Usage          `json:"usage"`

	// Compaction flags
	IsPreserved bool `json:"is_preserved"`
	IsSummary   bool `json:"is_summary"`

	Metadata  map[string]any `json:"metadata,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

// ContentBlock represents a single content block within a message.
// Different fields are populated based on the Type.
type ContentBlock struct {
	Type string `json:"type"`

	// Text content (for ContentTypeText, ContentTypeThinking)
	Text string `json:"text,omitempty"`

	// Tool use fields (for ContentTypeToolUse, ContentTypeServerToolUse)
	ToolUseID string          `json:"tool_use_id,omitempty"`
	ToolName  string          `json:"tool_name,omitempty"`
	ToolInput json.RawMessage `json:"tool_input,omitempty"`

	// Tool result fields (for ContentTypeToolResult)
	ToolResultForUseID string `json:"tool_result_for_use_id,omitempty"`
	ToolContent        string `json:"tool_content,omitempty"`
	IsError            bool   `json:"is_error,omitempty"`

	// Media/document fields (for ContentTypeImage, ContentTypeDocument)
	Source json.RawMessage `json:"source,omitempty"`

	// Web search results (for ContentTypeWebSearchResult)
	SearchResults json.RawMessage `json:"search_results,omitempty"`

	Metadata map[string]any `json:"metadata,omitempty"`
}

// Run represents the state of an agent run.
type Run struct {
	ID        uuid.UUID `json:"id"`
	SessionID uuid.UUID `json:"session_id"`
	AgentName string    `json:"agent_name"`

	// Run mode (batch or streaming API)
	RunMode RunMode `json:"run_mode"`

	// Hierarchical run support
	ParentRunID           *uuid.UUID `json:"parent_run_id,omitempty"`
	ParentToolExecutionID *uuid.UUID `json:"parent_tool_execution_id,omitempty"`
	Depth                 int        `json:"depth"`

	// State machine
	State         RunState  `json:"state"`
	PreviousState *RunState `json:"previous_state,omitempty"`

	// Request
	Prompt string `json:"prompt"`

	// Iteration tracking
	CurrentIteration   int        `json:"current_iteration"`
	CurrentIterationID *uuid.UUID `json:"current_iteration_id,omitempty"`

	// Final response (populated when run completes)
	ResponseText *string `json:"response_text,omitempty"`
	StopReason   *string `json:"stop_reason,omitempty"`

	// Token usage (cumulative across all iterations)
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`

	// Iteration counts
	IterationCount int `json:"iteration_count"`
	ToolIterations int `json:"tool_iterations"`

	// Error tracking
	ErrorMessage *string `json:"error_message,omitempty"`
	ErrorType    *string `json:"error_type,omitempty"`

	// Worker/claiming
	CreatedByInstanceID *string    `json:"created_by_instance_id,omitempty"`
	ClaimedByInstanceID *string    `json:"claimed_by_instance_id,omitempty"`
	ClaimedAt           *time.Time `json:"claimed_at,omitempty"`

	// Rescue tracking
	RescueAttempts int        `json:"rescue_attempts"`
	LastRescueAt   *time.Time `json:"last_rescue_at,omitempty"`

	// Metadata
	Metadata map[string]any `json:"metadata,omitempty"`

	// Timestamps
	CreatedAt   time.Time  `json:"created_at"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	FinalizedAt *time.Time `json:"finalized_at,omitempty"`
}

// Usage returns the cumulative token usage for this run.
func (r *Run) Usage() Usage {
	return Usage{
		InputTokens:              r.InputTokens,
		OutputTokens:             r.OutputTokens,
		CacheCreationInputTokens: r.CacheCreationInputTokens,
		CacheReadInputTokens:     r.CacheReadInputTokens,
	}
}

// Session represents a conversation context.
// App-specific fields (tenant_id, user_id, etc.) should be stored in Metadata.
type Session struct {
	ID              uuid.UUID      `json:"id"`
	ParentSessionID *uuid.UUID     `json:"parent_session_id,omitempty"`
	Depth           int            `json:"depth"`
	Metadata        map[string]any `json:"metadata,omitempty"`
	CompactionCount int            `json:"compaction_count"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
}

// AgentDefinition defines an agent's configuration.
type AgentDefinition struct {
	// Name is the unique identifier (required).
	Name string `json:"name"`

	// Description is shown when agent is used as a tool.
	Description string `json:"description,omitempty"`

	// Model is the Claude model ID (required), e.g., "claude-sonnet-4-5-20250929".
	Model string `json:"model"`

	// SystemPrompt defines the agent's behavior.
	SystemPrompt string `json:"system_prompt,omitempty"`

	// Tools is the list of tool names this agent can use.
	// Only tools listed here will be available to the agent.
	// Must reference tools registered via client.RegisterTool().
	Tools []string `json:"tools,omitempty"`

	// Agents is the list of agent names this agent can delegate to.
	// Listed agents become available as tools to this agent.
	// Enables multi-level agent hierarchies (PM -> Lead -> Worker pattern).
	Agents []string `json:"agents,omitempty"`

	// MaxTokens limits response length.
	MaxTokens *int `json:"max_tokens,omitempty"`

	// Temperature controls randomness (0.0 to 1.0).
	Temperature *float64 `json:"temperature,omitempty"`

	// TopK limits token selection.
	TopK *int `json:"top_k,omitempty"`

	// TopP (nucleus sampling) limits cumulative probability.
	TopP *float64 `json:"top_p,omitempty"`

	// Config holds additional settings as JSON.
	Config map[string]any `json:"config,omitempty"`

	// Timestamps (populated from database)
	CreatedAt time.Time `json:"created_at,omitempty"`
	UpdatedAt time.Time `json:"updated_at,omitempty"`
}

// AllToolNames returns all tool names available to this agent,
// including both regular tools and agent-as-tool names.
func (a *AgentDefinition) AllToolNames() []string {
	result := make([]string, 0, len(a.Tools)+len(a.Agents))
	result = append(result, a.Tools...)
	result = append(result, a.Agents...)
	return result
}

// Iteration represents a single Claude API call (batch or streaming) within a run.
type Iteration struct {
	ID              uuid.UUID `json:"id"`
	RunID           uuid.UUID `json:"run_id"`
	IterationNumber int       `json:"iteration_number"`

	// API mode
	IsStreaming bool `json:"is_streaming"`

	// Batch API tracking (only populated when IsStreaming = false)
	BatchID          *string      `json:"batch_id,omitempty"`
	BatchRequestID   *string      `json:"batch_request_id,omitempty"`
	BatchStatus      *BatchStatus `json:"batch_status,omitempty"`
	BatchSubmittedAt *time.Time   `json:"batch_submitted_at,omitempty"`
	BatchCompletedAt *time.Time   `json:"batch_completed_at,omitempty"`
	BatchExpiresAt   *time.Time   `json:"batch_expires_at,omitempty"`
	BatchPollCount   int          `json:"batch_poll_count"`
	BatchLastPollAt  *time.Time   `json:"batch_last_poll_at,omitempty"`

	// Streaming API tracking (only populated when IsStreaming = true)
	StreamingStartedAt   *time.Time `json:"streaming_started_at,omitempty"`
	StreamingCompletedAt *time.Time `json:"streaming_completed_at,omitempty"`

	// Request context
	TriggerType       string      `json:"trigger_type"` // "user_prompt", "tool_results", "continuation"
	RequestMessageIDs []uuid.UUID `json:"request_message_ids,omitempty"`

	// Response
	StopReason         *string    `json:"stop_reason,omitempty"`
	ResponseMessageID  *uuid.UUID `json:"response_message_id,omitempty"`
	HasToolUse         bool       `json:"has_tool_use"`
	ToolExecutionCount int        `json:"tool_execution_count"`

	// Token usage (for this iteration only)
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`

	// Error tracking
	ErrorMessage *string `json:"error_message,omitempty"`
	ErrorType    *string `json:"error_type,omitempty"`

	// Timestamps
	CreatedAt   time.Time  `json:"created_at"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

// Usage returns the token usage for this iteration.
func (i *Iteration) Usage() Usage {
	return Usage{
		InputTokens:              i.InputTokens,
		OutputTokens:             i.OutputTokens,
		CacheCreationInputTokens: i.CacheCreationInputTokens,
		CacheReadInputTokens:     i.CacheReadInputTokens,
	}
}

// ToolExecution represents a pending or completed tool execution.
type ToolExecution struct {
	ID          uuid.UUID          `json:"id"`
	RunID       uuid.UUID          `json:"run_id"`
	IterationID uuid.UUID          `json:"iteration_id"`
	State       ToolExecutionState `json:"state"`

	// Tool identification
	ToolUseID string          `json:"tool_use_id"`
	ToolName  string          `json:"tool_name"`
	ToolInput json.RawMessage `json:"tool_input"`

	// Agent-as-tool support
	IsAgentTool bool       `json:"is_agent_tool"`
	AgentName   *string    `json:"agent_name,omitempty"`
	ChildRunID  *uuid.UUID `json:"child_run_id,omitempty"`

	// Result
	ToolOutput   *string `json:"tool_output,omitempty"`
	IsError      bool    `json:"is_error"`
	ErrorMessage *string `json:"error_message,omitempty"`

	// Worker/claiming
	ClaimedByInstanceID *string    `json:"claimed_by_instance_id,omitempty"`
	ClaimedAt           *time.Time `json:"claimed_at,omitempty"`

	// Retry logic
	AttemptCount int `json:"attempt_count"`
	MaxAttempts  int `json:"max_attempts"`

	// Retry scheduling
	ScheduledAt time.Time `json:"scheduled_at"`
	SnoozeCount int       `json:"snooze_count"`
	LastError   *string   `json:"last_error,omitempty"`

	// Timestamps
	CreatedAt   time.Time  `json:"created_at"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

// ToolDefinition represents a tool stored in the database.
type ToolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
	IsAgentTool bool           `json:"is_agent_tool"`
	AgentName   *string        `json:"agent_name,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	CreatedAt   time.Time      `json:"created_at,omitempty"`
	UpdatedAt   time.Time      `json:"updated_at,omitempty"`
}

// Instance represents a running worker instance.
type Instance struct {
	ID                 string         `json:"id"`
	Name               string         `json:"name"`
	Hostname           *string        `json:"hostname,omitempty"`
	PID                *int           `json:"pid,omitempty"`
	Version            *string        `json:"version,omitempty"`
	MaxConcurrentRuns  int            `json:"max_concurrent_runs"`
	MaxConcurrentTools int            `json:"max_concurrent_tools"`
	ActiveRunCount     int            `json:"active_run_count"`
	ActiveToolCount    int            `json:"active_tool_count"`
	Metadata           map[string]any `json:"metadata,omitempty"`
	CreatedAt          time.Time      `json:"created_at"`
	LastHeartbeatAt    time.Time      `json:"last_heartbeat_at"`
}

// CompactionEvent represents a context compaction operation.
type CompactionEvent struct {
	ID                  uuid.UUID   `json:"id"`
	SessionID           uuid.UUID   `json:"session_id"`
	Strategy            string      `json:"strategy"`
	OriginalTokens      int         `json:"original_tokens"`
	CompactedTokens     int         `json:"compacted_tokens"`
	MessagesRemoved     int         `json:"messages_removed"`
	SummaryContent      *string     `json:"summary_content,omitempty"`
	PreservedMessageIDs []uuid.UUID `json:"preserved_message_ids,omitempty"`
	ModelUsed           *string     `json:"model_used,omitempty"`
	DurationMS          *int64      `json:"duration_ms,omitempty"`
	CreatedAt           time.Time   `json:"created_at"`
}

// Helper functions for working with pointers

// Ptr returns a pointer to the given value.
func Ptr[T any](v T) *T {
	return &v
}

// Deref returns the value pointed to by p, or the zero value if p is nil.
func Deref[T any](p *T) T {
	if p == nil {
		var zero T
		return zero
	}
	return *p
}

// DerefOr returns the value pointed to by p, or the default value if p is nil.
func DerefOr[T any](p *T, def T) T {
	if p == nil {
		return def
	}
	return *p
}
