package compaction

import (
	"github.com/youssefsiam38/agentpg/types"
)

// Partitioner handles message partitioning for compaction
type Partitioner struct{}

// NewPartitioner creates a new message partitioner
func NewPartitioner() *Partitioner {
	return &Partitioner{}
}

// Partition splits messages into preserved and to-summarize sets
func (p *Partitioner) Partition(
	messages []*types.Message,
	config CompactionConfig,
) (preserved, toSummarize []*types.Message) {
	// Always preserve last N messages
	if len(messages) <= config.PreserveLastN {
		return messages, nil
	}

	// Also protect recent tokens (OpenCode pattern: 40K protected)
	protectedIdx := p.findProtectedIndex(messages, config.ProtectedTokens)

	// Take the more conservative split
	splitIdx := min(len(messages)-config.PreserveLastN, protectedIdx)
	if splitIdx < 0 {
		splitIdx = 0
	}

	// Never split in the middle of a tool call/result pair
	splitIdx = p.adjustForToolPairs(messages, splitIdx)

	// Separate preserved messages (manually marked)
	var finalPreserved []*types.Message
	var finalToSummarize []*types.Message

	for i, msg := range messages {
		if i >= splitIdx || msg.IsPreserved {
			finalPreserved = append(finalPreserved, msg)
		} else {
			finalToSummarize = append(finalToSummarize, msg)
		}
	}

	return finalPreserved, finalToSummarize
}

// findProtectedIndex finds the index where protected tokens start
func (p *Partitioner) findProtectedIndex(messages []*types.Message, protectedTokens int) int {
	tokensSeen := 0
	for i := len(messages) - 1; i >= 0; i-- {
		tokensSeen += messages[i].TokenCount()
		if tokensSeen > protectedTokens {
			return i + 1
		}
	}
	return 0
}

// adjustForToolPairs ensures we don't break tool call/result pairs
func (p *Partitioner) adjustForToolPairs(messages []*types.Message, idx int) int {
	if idx <= 0 || idx >= len(messages) {
		return idx
	}

	// Check if we're splitting right after a tool_use block
	// Tool results always follow tool_use blocks
	msg := messages[idx-1]
	for _, block := range msg.Content {
		if block.Type == types.ContentTypeToolUse {
			// Move split point back to keep the pair together
			return idx - 1
		}
	}

	// Check if current message has tool results (keep with previous tool_use)
	currentMsg := messages[idx]
	for _, block := range currentMsg.Content {
		if block.Type == types.ContentTypeToolResult {
			// Keep this with the previous message
			return idx - 1
		}
	}

	return idx
}
