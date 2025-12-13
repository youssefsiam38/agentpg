package agentpg

// ContentType constants aligned with Claude API and database schema (agentpg_content_type enum).
const (
	ContentTypeText            = "text"
	ContentTypeToolUse         = "tool_use"
	ContentTypeToolResult      = "tool_result"
	ContentTypeImage           = "image"
	ContentTypeDocument        = "document"
	ContentTypeThinking        = "thinking"
	ContentTypeServerToolUse   = "server_tool_use"
	ContentTypeWebSearchResult = "web_search_result"
)

// RunState represents the lifecycle of a run (mirrors agentpg_run_state enum).
//
// State transitions:
//
//	pending ──────────────────┐
//	    │ (worker claims)     │
//	    v                     │
//	batch_submitting ─────────┤
//	    │ (batch created)     │
//	    v                     │
//	batch_pending ────────────┤
//	    │ (polling)           │
//	    v                     │
//	batch_processing ─────────┤
//	    │ (batch complete)    │
//	    ├──> pending_tools    │ (has tool_use blocks)
//	    ├──> completed        │ (stop_reason=end_turn)
//	    ├──> awaiting_input   │ (stop_reason=max_tokens, needs continuation)
//	    └──> failed           │ (error)
//
//	pending_tools ────────────┤
//	    │ (all tools done)    │
//	    └──> pending          │ (continue with tool_results)
//
// Terminal states: completed, cancelled, failed
type RunState string

const (
	RunStatePending         RunState = "pending"
	RunStateBatchSubmitting RunState = "batch_submitting"
	RunStateBatchPending    RunState = "batch_pending"
	RunStateBatchProcessing RunState = "batch_processing"
	RunStatePendingTools    RunState = "pending_tools"
	RunStateAwaitingInput   RunState = "awaiting_input"
	RunStateCompleted       RunState = "completed"
	RunStateCancelled       RunState = "cancelled"
	RunStateFailed          RunState = "failed"
)

// IsTerminal returns true if the run state is a terminal state.
func (s RunState) IsTerminal() bool {
	return s == RunStateCompleted || s == RunStateCancelled || s == RunStateFailed
}

// String returns the string representation of the run state.
func (s RunState) String() string {
	return string(s)
}

// ToolExecutionState represents the lifecycle of a tool execution (mirrors agentpg_tool_execution_state enum).
//
// State transitions:
//
//	pending ──────────────────┐
//	    │ (worker claims)     │
//	    v                     │
//	running ──────────────────┤
//	    ├──> completed        │ (success)
//	    ├──> failed           │ (error, may retry)
//	    └──> skipped          │ (run cancelled)
type ToolExecutionState string

const (
	ToolStatePending   ToolExecutionState = "pending"
	ToolStateRunning   ToolExecutionState = "running"
	ToolStateCompleted ToolExecutionState = "completed"
	ToolStateFailed    ToolExecutionState = "failed"
	ToolStateSkipped   ToolExecutionState = "skipped"
)

// IsTerminal returns true if the tool execution state is a terminal state.
func (s ToolExecutionState) IsTerminal() bool {
	return s == ToolStateCompleted || s == ToolStateFailed || s == ToolStateSkipped
}

// String returns the string representation of the tool execution state.
func (s ToolExecutionState) String() string {
	return string(s)
}

// BatchStatus represents the processing status of a Claude Batch API request (mirrors agentpg_batch_status enum).
type BatchStatus string

const (
	BatchStatusInProgress BatchStatus = "in_progress"
	BatchStatusCanceling  BatchStatus = "canceling"
	BatchStatusEnded      BatchStatus = "ended"
)

// String returns the string representation of the batch status.
func (s BatchStatus) String() string {
	return string(s)
}

// MessageRole represents the role of a message in a conversation (mirrors agentpg_message_role enum).
type MessageRole string

const (
	MessageRoleUser      MessageRole = "user"
	MessageRoleAssistant MessageRole = "assistant"
	MessageRoleSystem    MessageRole = "system"
)

// String returns the string representation of the message role.
func (s MessageRole) String() string {
	return string(s)
}

// TriggerType constants for iteration triggers.
const (
	TriggerTypeUserPrompt   = "user_prompt"
	TriggerTypeToolResults  = "tool_results"
	TriggerTypeContinuation = "continuation"
)

// LISTEN/NOTIFY channel names.
const (
	ChannelRunCreated    = "agentpg_run_created"
	ChannelRunState      = "agentpg_run_state"
	ChannelRunFinalized  = "agentpg_run_finalized"
	ChannelToolPending   = "agentpg_tool_pending"
	ChannelToolsComplete = "agentpg_tools_complete"
)
