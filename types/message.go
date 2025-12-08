package types

import (
	"encoding/json"
	"time"
)

// Role represents the message role
type Role string

const (
	// RoleUser represents a user message
	RoleUser Role = "user"

	// RoleAssistant represents an assistant message
	RoleAssistant Role = "assistant"

	// RoleSystem represents a system message
	RoleSystem Role = "system"
)

// Message represents a conversation message with metadata
type Message struct {
	ID        string         `json:"id"`
	SessionID string         `json:"session_id"`
	Role      Role           `json:"role"`
	Content   []ContentBlock `json:"content"`
	Usage     *Usage         `json:"usage,omitempty"`
	Metadata  map[string]any `json:"metadata"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`

	// Compaction metadata
	IsPreserved bool `json:"is_preserved"` // Never compact this message
	IsSummary   bool `json:"is_summary"`   // This is a compaction summary
}

// TokenCount returns the total token count from usage for backwards compatibility
func (m *Message) TokenCount() int {
	if m.Usage == nil {
		return 0
	}
	return m.Usage.InputTokens + m.Usage.OutputTokens
}

// ContentType represents the type of content block
type ContentType string

const (
	// ContentTypeText represents text content
	ContentTypeText ContentType = "text"

	// ContentTypeToolUse represents a tool use block
	ContentTypeToolUse ContentType = "tool_use"

	// ContentTypeToolResult represents a tool result block
	ContentTypeToolResult ContentType = "tool_result"

	// ContentTypeImage represents an image block
	ContentTypeImage ContentType = "image"

	// ContentTypeDocument represents a document block
	ContentTypeDocument ContentType = "document"
)

// ContentBlock represents a piece of content in a message
type ContentBlock struct {
	Type ContentType `json:"type"`

	// Text content
	Text string `json:"text,omitempty"`

	// Tool use content
	ToolUseID    string          `json:"id,omitempty"`
	ToolName     string          `json:"name,omitempty"`
	ToolInput    map[string]any  `json:"input,omitempty"`
	ToolInputRaw json.RawMessage `json:"input_raw,omitempty"`

	// Tool result content
	ToolResultID string `json:"tool_use_id,omitempty"`
	ToolContent  string `json:"content,omitempty"`
	IsError      bool   `json:"is_error,omitempty"`

	// Image content
	ImageSource *ImageSource `json:"source,omitempty"`

	// Document content
	DocumentSource *DocumentSource `json:"document,omitempty"`
}

// ImageSource represents an image source
type ImageSource struct {
	Type      string `json:"type"`       // "base64" or "url"
	MediaType string `json:"media_type"` // "image/jpeg", "image/png", etc.
	Data      string `json:"data,omitempty"`
	URL       string `json:"url,omitempty"`
}

// DocumentSource represents a document source
type DocumentSource struct {
	Type      string `json:"type"`       // "base64"
	MediaType string `json:"media_type"` // "application/pdf"
	Data      string `json:"data"`
}

// Usage represents token usage information
type Usage struct {
	InputTokens         int `json:"input_tokens"`
	OutputTokens        int `json:"output_tokens"`
	CacheCreationTokens int `json:"cache_creation_input_tokens,omitempty"`
	CacheReadTokens     int `json:"cache_read_input_tokens,omitempty"`
}

// Response represents an agent response
type Response struct {
	Message    *Message
	StopReason string
	Usage      *Usage
	RunID      string // The run ID for this execution (empty for legacy API)
}
