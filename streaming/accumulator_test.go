package streaming

import (
	"testing"
)

func TestBuildContentBlocks_EmptyToolInput(t *testing.T) {
	tests := []struct {
		name           string
		toolInputStr   string
		wantRaw        string
		wantInputEmpty bool
	}{
		{
			name:           "empty tool input defaults to empty object",
			toolInputStr:   "",
			wantRaw:        "{}",
			wantInputEmpty: true,
		},
		{
			name:           "valid tool input preserved",
			toolInputStr:   `{"key":"value"}`,
			wantRaw:        `{"key":"value"}`,
			wantInputEmpty: false,
		},
		{
			name:           "empty object input preserved",
			toolInputStr:   "{}",
			wantRaw:        "{}",
			wantInputEmpty: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			acc := NewAccumulator()

			// Simulate a tool use block
			block := &ContentBlock{
				Type:     "tool_use",
				Index:    0,
				ToolID:   "test-id",
				ToolName: "test_tool",
			}
			block.ToolInput.WriteString(tt.toolInputStr)

			acc.content = append(acc.content, *block)

			// Build content blocks
			result := acc.buildContentBlocks()

			if len(result) != 1 {
				t.Fatalf("expected 1 block, got %d", len(result))
			}

			mcb := result[0]

			// Check raw JSON
			if string(mcb.ToolInputRaw) != tt.wantRaw {
				t.Errorf("ToolInputRaw = %q, want %q", string(mcb.ToolInputRaw), tt.wantRaw)
			}

			// Check parsed input
			if tt.wantInputEmpty {
				if len(mcb.ToolInput) != 0 {
					t.Errorf("expected empty ToolInput map, got %v", mcb.ToolInput)
				}
			} else {
				if len(mcb.ToolInput) == 0 {
					t.Error("expected non-empty ToolInput map")
				}
			}
		})
	}
}
