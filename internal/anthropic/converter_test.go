package anthropic

import (
	"encoding/json"
	"testing"

	"github.com/youssefsiam38/agentpg/types"
)

func TestConvertContentBlock_ToolUseEmptyInput(t *testing.T) {
	tests := []struct {
		name  string
		block types.ContentBlock
	}{
		{
			name: "nil ToolInput and empty ToolInputRaw defaults to empty object",
			block: types.ContentBlock{
				Type:         types.ContentTypeToolUse,
				ToolUseID:    "test-id",
				ToolName:     "test_tool",
				ToolInput:    nil,
				ToolInputRaw: nil,
			},
		},
		{
			name: "empty ToolInputRaw defaults to empty object",
			block: types.ContentBlock{
				Type:         types.ContentTypeToolUse,
				ToolUseID:    "test-id",
				ToolName:     "test_tool",
				ToolInput:    nil,
				ToolInputRaw: json.RawMessage(""),
			},
		},
		{
			name: "valid ToolInputRaw preserved",
			block: types.ContentBlock{
				Type:         types.ContentTypeToolUse,
				ToolUseID:    "test-id",
				ToolName:     "test_tool",
				ToolInput:    nil,
				ToolInputRaw: json.RawMessage(`{"key":"value"}`),
			},
		},
		{
			name: "ToolInput map used when ToolInputRaw is empty",
			block: types.ContentBlock{
				Type:         types.ContentTypeToolUse,
				ToolUseID:    "test-id",
				ToolName:     "test_tool",
				ToolInput:    map[string]any{"foo": "bar"},
				ToolInputRaw: nil,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// The key test is that calling this function doesn't panic
			// when ToolInput is nil or ToolInputRaw is empty.
			// Previously this would pass nil to NewToolUseBlock,
			// causing API errors like "Input should be a valid dictionary"
			_ = convertContentBlock(tt.block)
		})
	}
}

func TestConvertToAnthropicMessages_WithToolUse(t *testing.T) {
	messages := []*types.Message{
		{
			Role: types.RoleAssistant,
			Content: []types.ContentBlock{
				{
					Type:         types.ContentTypeToolUse,
					ToolUseID:    "tool-123",
					ToolName:     "list_tasks",
					ToolInput:    nil,
					ToolInputRaw: nil, // Empty input
				},
			},
		},
	}

	// This should not panic
	result := ConvertToAnthropicMessages(messages)

	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result))
	}

	if len(result[0].Content) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(result[0].Content))
	}
}
