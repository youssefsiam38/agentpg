package streaming

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
)

// Accumulator accumulates streaming events into a complete message
type Accumulator struct {
	messageID    string
	model        string
	role         string
	content      []ContentBlock
	stopReason   string
	stopSequence string
	usage        Usage

	// Internal state for building content blocks
	currentBlocks map[int]*ContentBlock
}

// ContentBlock represents a content block being accumulated
type ContentBlock struct {
	Type  string
	Index int

	// Text content
	Text string

	// Tool use content
	ToolID    string
	ToolName  string
	ToolInput strings.Builder
}

// Usage tracks token usage
type Usage struct {
	InputTokens         int
	OutputTokens        int
	CacheCreationTokens int
	CacheReadTokens     int
}

// NewAccumulator creates a new stream accumulator
func NewAccumulator() *Accumulator {
	return &Accumulator{
		content:       []ContentBlock{},
		currentBlocks: make(map[int]*ContentBlock),
	}
}

// ProcessAnthropicEvent processes an event from the Anthropic streaming API
func (a *Accumulator) ProcessAnthropicEvent(event anthropic.MessageStreamEventUnion) {
	switch e := event.AsAny().(type) {
	case anthropic.MessageStartEvent:
		a.messageID = e.Message.ID
		a.model = string(e.Message.Model)
		a.role = string(e.Message.Role)
		a.usage.InputTokens = int(e.Message.Usage.InputTokens)

	case anthropic.ContentBlockStartEvent:
		block := &ContentBlock{
			Index: int(e.Index),
		}

		switch content := e.ContentBlock.AsAny().(type) {
		case anthropic.TextBlock:
			block.Type = "text"
			block.Text = content.Text

		case anthropic.ToolUseBlock:
			block.Type = "tool_use"
			block.ToolID = content.ID
			block.ToolName = content.Name
		}

		a.currentBlocks[int(e.Index)] = block

	case anthropic.ContentBlockDeltaEvent:
		block, exists := a.currentBlocks[int(e.Index)]
		if !exists {
			return
		}

		switch delta := e.Delta.AsAny().(type) {
		case anthropic.TextDelta:
			block.Text += delta.Text

		case anthropic.InputJSONDelta:
			block.ToolInput.WriteString(delta.PartialJSON)
		}

	case anthropic.ContentBlockStopEvent:
		block, exists := a.currentBlocks[int(e.Index)]
		if exists {
			a.content = append(a.content, *block)
			delete(a.currentBlocks, int(e.Index))
		}

	case anthropic.MessageDeltaEvent:
		a.stopReason = string(e.Delta.StopReason)
		a.stopSequence = e.Delta.StopSequence
		a.usage.OutputTokens = int(e.Usage.OutputTokens)

	default:
		// Ignore unknown events
	}
}

// Message returns the accumulated message
// This can be called at any time to get the current state
func (a *Accumulator) Message() *Message {
	return &Message{
		ID:           a.messageID,
		Model:        a.model,
		Role:         a.role,
		Content:      a.buildContentBlocks(),
		StopReason:   a.stopReason,
		StopSequence: a.stopSequence,
		Usage:        a.usage,
	}
}

// buildContentBlocks converts accumulated blocks to the final format
func (a *Accumulator) buildContentBlocks() []MessageContentBlock {
	blocks := make([]MessageContentBlock, 0, len(a.content))

	for _, block := range a.content {
		mcb := MessageContentBlock{
			Type: block.Type,
		}

		switch block.Type {
		case "text":
			mcb.Text = block.Text

		case "tool_use":
			mcb.ToolUseID = block.ToolID
			mcb.ToolName = block.ToolName

			// Handle empty tool input - default to empty object
			inputStr := block.ToolInput.String()
			if inputStr == "" {
				inputStr = "{}"
			}
			mcb.ToolInputRaw = json.RawMessage(inputStr)

			// Parse tool input into map
			var input map[string]any
			if err := json.Unmarshal(mcb.ToolInputRaw, &input); err == nil {
				mcb.ToolInput = input
			}
		}

		blocks = append(blocks, mcb)
	}

	return blocks
}

// Message represents the accumulated message
type Message struct {
	ID           string
	Model        string
	Role         string
	Content      []MessageContentBlock
	StopReason   string
	StopSequence string
	Usage        Usage
	CreatedAt    time.Time
}

// MessageContentBlock represents a content block in the message
type MessageContentBlock struct {
	Type string

	// Text content
	Text string

	// Tool use content
	ToolUseID    string
	ToolName     string
	ToolInput    map[string]any
	ToolInputRaw json.RawMessage
}

// ToAgentPGMessage converts the accumulated message to an agentpg Message
// This is used to save the message to the database
func (m *Message) ToAgentPGMessage(sessionID string) interface{} {
	// This will be implemented when we integrate with the main agentpg package
	// For now, we return the message structure
	return map[string]interface{}{
		"id":            m.ID,
		"session_id":    sessionID,
		"role":          m.Role,
		"content":       m.Content,
		"stop_reason":   m.StopReason,
		"stop_sequence": m.StopSequence,
		"usage":         m.Usage,
		"created_at":    time.Now(),
	}
}
