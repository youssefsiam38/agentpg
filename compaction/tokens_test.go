package compaction

import (
	"testing"

	"github.com/youssefsiam38/agentpg/types"
)

func TestApproximateTokens(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected int
	}{
		{
			name:     "empty string",
			content:  "",
			expected: 0,
		},
		{
			name:     "short string",
			content:  "hi",
			expected: 1, // (2 + 3) / 4 = 1
		},
		{
			name:     "4 chars",
			content:  "test",
			expected: 1, // (4 + 3) / 4 = 1
		},
		{
			name:     "8 chars",
			content:  "12345678",
			expected: 2, // (8 + 3) / 4 = 2
		},
		{
			name:     "longer text",
			content:  "This is a longer piece of text for testing token approximation.",
			expected: 16, // (63 + 3) / 4 = 16
		},
		{
			name:     "very short 1 char",
			content:  "a",
			expected: 1, // (1 + 3) / 4 = 1
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ApproximateTokens(tt.content)
			if got != tt.expected {
				t.Errorf("ApproximateTokens(%q) = %d, want %d", tt.content, got, tt.expected)
			}
		})
	}
}

func TestApproximateTokensNonZero(t *testing.T) {
	// Ensure that any non-empty string returns at least 1 token
	testCases := []string{
		"a",
		"ab",
		"abc",
		"1",
		".",
		" ",
	}

	for _, tc := range testCases {
		got := ApproximateTokens(tc)
		if got < 1 {
			t.Errorf("ApproximateTokens(%q) = %d, expected at least 1", tc, got)
		}
	}
}

func TestSumTokens(t *testing.T) {
	tests := []struct {
		name     string
		messages []*types.Message
		expected int
	}{
		{
			name:     "empty messages",
			messages: []*types.Message{},
			expected: 0,
		},
		{
			name:     "nil messages",
			messages: nil,
			expected: 0,
		},
		{
			name: "single message with usage",
			messages: []*types.Message{
				{
					Usage: &types.Usage{
						InputTokens:  100,
						OutputTokens: 50,
					},
				},
			},
			expected: 150,
		},
		{
			name: "multiple messages",
			messages: []*types.Message{
				{
					Usage: &types.Usage{
						InputTokens:  100,
						OutputTokens: 50,
					},
				},
				{
					Usage: &types.Usage{
						InputTokens:  200,
						OutputTokens: 100,
					},
				},
			},
			expected: 450,
		},
		{
			name: "message without usage",
			messages: []*types.Message{
				{
					Content: []types.ContentBlock{
						{Type: types.ContentTypeText, Text: "Hello world"},
					},
				},
			},
			expected: 0, // Falls back to message.TokenCount() which is 0 without usage
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SumTokens(tt.messages)
			if got != tt.expected {
				t.Errorf("SumTokens() = %d, want %d", got, tt.expected)
			}
		})
	}
}
