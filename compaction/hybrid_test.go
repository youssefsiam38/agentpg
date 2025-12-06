package compaction

import (
	"testing"
	"time"

	"github.com/youssefsiam38/agentpg/types"
)

func TestHybridStrategy_copyMessage(t *testing.T) {
	h := &HybridStrategy{}

	original := &types.Message{
		ID:        "msg-123",
		SessionID: "session-456",
		Role:      types.RoleAssistant,
		Content: []types.ContentBlock{
			{Type: types.ContentTypeText, Text: "Hello"},
			{Type: types.ContentTypeToolUse, ToolName: "test_tool"},
		},
		Usage: &types.Usage{
			InputTokens:  100,
			OutputTokens: 50,
		},
		Metadata: map[string]any{
			"key1": "value1",
			"key2": 42,
		},
		IsPreserved: true,
		IsSummary:   false,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	copied := h.copyMessage(original)

	// Verify values are copied correctly
	if copied.ID != original.ID {
		t.Errorf("ID not copied: got %s, want %s", copied.ID, original.ID)
	}
	if copied.SessionID != original.SessionID {
		t.Errorf("SessionID not copied: got %s, want %s", copied.SessionID, original.SessionID)
	}
	if copied.Role != original.Role {
		t.Errorf("Role not copied: got %s, want %s", copied.Role, original.Role)
	}
	if len(copied.Content) != len(original.Content) {
		t.Errorf("Content length mismatch: got %d, want %d", len(copied.Content), len(original.Content))
	}

	// Verify content is deep copied
	if &copied.Content[0] == &original.Content[0] {
		t.Error("Content slice not deep copied - same underlying array")
	}

	// Verify usage is deep copied
	if copied.Usage == original.Usage {
		t.Error("Usage pointer not deep copied - points to same object")
	}
	if copied.Usage.InputTokens != original.Usage.InputTokens {
		t.Errorf("Usage.InputTokens not copied: got %d, want %d", copied.Usage.InputTokens, original.Usage.InputTokens)
	}
	if copied.Usage.OutputTokens != original.Usage.OutputTokens {
		t.Errorf("Usage.OutputTokens not copied: got %d, want %d", copied.Usage.OutputTokens, original.Usage.OutputTokens)
	}

	// Verify metadata is deep copied
	if len(copied.Metadata) != len(original.Metadata) {
		t.Errorf("Metadata length mismatch: got %d, want %d", len(copied.Metadata), len(original.Metadata))
	}

	// Modify copied metadata and ensure original is unchanged
	copied.Metadata["key3"] = "new value"
	if _, exists := original.Metadata["key3"]; exists {
		t.Error("Original metadata was modified when copy was changed")
	}

	// Modify copied usage and ensure original is unchanged
	copied.Usage.InputTokens = 999
	if original.Usage.InputTokens == 999 {
		t.Error("Original usage was modified when copy was changed")
	}
}

func TestHybridStrategy_copyMessage_nilUsage(t *testing.T) {
	h := &HybridStrategy{}

	original := &types.Message{
		ID:        "msg-123",
		SessionID: "session-456",
		Role:      types.RoleUser,
		Content: []types.ContentBlock{
			{Type: types.ContentTypeText, Text: "Hello"},
		},
		Usage:    nil, // No usage
		Metadata: nil, // No metadata
	}

	copied := h.copyMessage(original)

	if copied.Usage != nil {
		t.Error("Expected nil usage to remain nil in copy")
	}
	if copied.Metadata != nil {
		t.Error("Expected nil metadata to remain nil in copy")
	}
}

func TestHybridStrategy_calculateMessageTokens(t *testing.T) {
	h := &HybridStrategy{}

	tests := []struct {
		name     string
		message  *types.Message
		minTokens int // Minimum expected tokens
	}{
		{
			name: "text message",
			message: &types.Message{
				Content: []types.ContentBlock{
					{Type: types.ContentTypeText, Text: "Hello world"},
				},
			},
			minTokens: 1,
		},
		{
			name: "tool use message",
			message: &types.Message{
				Content: []types.ContentBlock{
					{Type: types.ContentTypeToolUse, ToolName: "test", ToolInput: map[string]any{"key": "value"}},
				},
			},
			minTokens: 10, // At least the overhead
		},
		{
			name: "tool result message",
			message: &types.Message{
				Content: []types.ContentBlock{
					{Type: types.ContentTypeToolResult, ToolContent: "Some result"},
				},
			},
			minTokens: 10, // At least the overhead
		},
		{
			name: "multiple blocks",
			message: &types.Message{
				Content: []types.ContentBlock{
					{Type: types.ContentTypeText, Text: "Hello"},
					{Type: types.ContentTypeText, Text: "World"},
				},
			},
			minTokens: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens := h.calculateMessageTokens(tt.message)
			if tokens < tt.minTokens {
				t.Errorf("calculateMessageTokens() = %d, want at least %d", tokens, tt.minTokens)
			}
		})
	}
}

func TestHybridStrategy_Name(t *testing.T) {
	h := &HybridStrategy{}
	if h.Name() != "hybrid" {
		t.Errorf("Name() = %s, want hybrid", h.Name())
	}
}
