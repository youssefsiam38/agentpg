package agentpg

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
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
