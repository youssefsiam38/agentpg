package agentpg

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/youssefsiam38/agentpg/runstate"
	"github.com/youssefsiam38/agentpg/types"
)

// Re-export types from types package for backwards compatibility
type (
	Role           = types.Role
	Message        = types.Message
	ContentType    = types.ContentType
	ContentBlock   = types.ContentBlock
	ImageSource    = types.ImageSource
	DocumentSource = types.DocumentSource
	Usage          = types.Usage
	Response       = types.Response
)

// Re-export constants
const (
	RoleUser      = types.RoleUser
	RoleAssistant = types.RoleAssistant
	RoleSystem    = types.RoleSystem

	ContentTypeText       = types.ContentTypeText
	ContentTypeToolUse    = types.ContentTypeToolUse
	ContentTypeToolResult = types.ContentTypeToolResult
	ContentTypeImage      = types.ContentTypeImage
	ContentTypeDocument   = types.ContentTypeDocument
)

// NewMessage creates a new message
func NewMessage(sessionID string, role Role, content []ContentBlock) *Message {
	return &Message{
		ID:        uuid.New().String(),
		SessionID: sessionID,
		Role:      role,
		Content:   content,
		Metadata:  make(map[string]any),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
}

// NewUserMessage creates a new user message with text content
func NewUserMessage(sessionID string, text string) *Message {
	return NewMessage(sessionID, RoleUser, []ContentBlock{
		{Type: ContentTypeText, Text: text},
	})
}

// NewAssistantMessage creates a new assistant message
func NewAssistantMessage(sessionID string, content []ContentBlock) *Message {
	return NewMessage(sessionID, RoleAssistant, content)
}

// NewTextBlock creates a text content block
func NewTextBlock(text string) ContentBlock {
	return ContentBlock{
		Type: ContentTypeText,
		Text: text,
	}
}

// NewToolUseBlock creates a tool use content block
func NewToolUseBlock(id, name string, input map[string]any) ContentBlock {
	inputRaw, _ := json.Marshal(input)
	return ContentBlock{
		Type:         ContentTypeToolUse,
		ToolUseID:    id,
		ToolName:     name,
		ToolInput:    input,
		ToolInputRaw: inputRaw,
	}
}

// NewToolResultBlock creates a tool result content block
func NewToolResultBlock(toolUseID string, content string, isError bool) ContentBlock {
	return ContentBlock{
		Type:         ContentTypeToolResult,
		ToolResultID: toolUseID,
		ToolContent:  content,
		IsError:      isError,
	}
}

// UnmarshalJSON is a helper for unmarshaling content blocks
func UnmarshalJSON(data []byte, v any) error {
	return json.Unmarshal(data, v)
}

// RunStatus represents the current status of an async run.
type RunStatus struct {
	// ID is the unique run identifier.
	ID string `json:"id"`

	// State is the current state of the run.
	State runstate.RunState `json:"state"`

	// StopReason is the stop reason from the Claude API (set when completed).
	StopReason *string `json:"stop_reason,omitempty"`

	// ResponseText is the final response text (set when completed).
	ResponseText *string `json:"response_text,omitempty"`

	// ErrorMessage is the error message (set when failed).
	ErrorMessage *string `json:"error_message,omitempty"`

	// InputTokens is the total input tokens used.
	InputTokens int `json:"input_tokens"`

	// OutputTokens is the total output tokens used.
	OutputTokens int `json:"output_tokens"`

	// IterationCount is how many API calls have been made.
	IterationCount int `json:"iteration_count"`

	// CreatedAt is when the run was created.
	CreatedAt time.Time `json:"created_at"`

	// FinalizedAt is when the run reached a terminal state.
	FinalizedAt *time.Time `json:"finalized_at,omitempty"`
}

// IsComplete returns true if the run has completed successfully.
func (s *RunStatus) IsComplete() bool {
	return s.State == runstate.RunStateCompleted
}

// IsFailed returns true if the run has failed.
func (s *RunStatus) IsFailed() bool {
	return s.State == runstate.RunStateFailed
}

// IsCancelled returns true if the run was cancelled.
func (s *RunStatus) IsCancelled() bool {
	return s.State == runstate.RunStateCancelled
}

// IsTerminal returns true if the run is in a terminal state.
func (s *RunStatus) IsTerminal() bool {
	return s.State.IsTerminal()
}

// IsPending returns true if the run is waiting to be processed.
func (s *RunStatus) IsPending() bool {
	return s.State == runstate.RunStatePending
}

// IsInProgress returns true if the run is actively being processed.
func (s *RunStatus) IsInProgress() bool {
	return s.State == runstate.RunStatePendingAPI ||
		s.State == runstate.RunStatePendingTools ||
		s.State == runstate.RunStateAwaitingContinuation
}
