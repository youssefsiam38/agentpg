package storage

import (
	"context"
	"time"
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
}

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
