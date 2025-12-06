package streaming

// EventType represents the type of streaming event
type EventType string

const (
	// EventTypeMessageStart indicates the message has started
	EventTypeMessageStart EventType = "message_start"

	// EventTypeContentBlockStart indicates a content block has started
	EventTypeContentBlockStart EventType = "content_block_start"

	// EventTypeContentBlockDelta indicates new content in a block
	EventTypeContentBlockDelta EventType = "content_block_delta"

	// EventTypeContentBlockStop indicates a content block has ended
	EventTypeContentBlockStop EventType = "content_block_stop"

	// EventTypeMessageDelta indicates message metadata has changed
	EventTypeMessageDelta EventType = "message_delta"

	// EventTypeMessageStop indicates the message has ended
	EventTypeMessageStop EventType = "message_stop"
)

// Event represents a streaming event
type Event interface {
	Type() EventType
}

// MessageStartEvent is emitted when a message starts
type MessageStartEvent struct {
	MessageID string
	Model     string
}

func (e *MessageStartEvent) Type() EventType {
	return EventTypeMessageStart
}

// ContentBlockStartEvent is emitted when a content block starts
type ContentBlockStartEvent struct {
	Index     int
	BlockType string
}

func (e *ContentBlockStartEvent) Type() EventType {
	return EventTypeContentBlockStart
}

// TextDeltaEvent is emitted when text content arrives
type TextDeltaEvent struct {
	Index int
	Delta string
}

func (e *TextDeltaEvent) Type() EventType {
	return EventTypeContentBlockDelta
}

// ToolUseStartEvent is emitted when a tool use block starts
type ToolUseStartEvent struct {
	Index    int
	ToolID   string
	ToolName string
}

func (e *ToolUseStartEvent) Type() EventType {
	return EventTypeContentBlockStart
}

// ToolInputDeltaEvent is emitted when tool input arrives
type ToolInputDeltaEvent struct {
	Index int
	Delta string
}

func (e *ToolInputDeltaEvent) Type() EventType {
	return EventTypeContentBlockDelta
}

// ContentBlockStopEvent is emitted when a content block ends
type ContentBlockStopEvent struct {
	Index int
}

func (e *ContentBlockStopEvent) Type() EventType {
	return EventTypeContentBlockStop
}

// MessageDeltaEvent is emitted when message metadata changes
type MessageDeltaEvent struct {
	StopReason   string
	StopSequence string
}

func (e *MessageDeltaEvent) Type() EventType {
	return EventTypeMessageDelta
}

// MessageStopEvent is emitted when the message ends
type MessageStopEvent struct{}

func (e *MessageStopEvent) Type() EventType {
	return EventTypeMessageStop
}
