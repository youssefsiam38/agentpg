// Package convert provides utilities for converting between message formats.
package convert

import (
	"encoding/json"
	"log"

	"github.com/youssefsiam38/agentpg/storage"
	"github.com/youssefsiam38/agentpg/types"
)

// ToStorageMessage converts a types.Message to storage.Message format.
func ToStorageMessage(msg *types.Message) *storage.Message {
	var usage *storage.MessageUsage
	if msg.Usage != nil {
		usage = &storage.MessageUsage{
			InputTokens:         msg.Usage.InputTokens,
			OutputTokens:        msg.Usage.OutputTokens,
			CacheCreationTokens: msg.Usage.CacheCreationTokens,
			CacheReadTokens:     msg.Usage.CacheReadTokens,
		}
	}
	return &storage.Message{
		ID:          msg.ID,
		SessionID:   msg.SessionID,
		Role:        string(msg.Role),
		Content:     msg.Content,
		Usage:       usage,
		Metadata:    msg.Metadata,
		IsPreserved: msg.IsPreserved,
		IsSummary:   msg.IsSummary,
		CreatedAt:   msg.CreatedAt,
		UpdatedAt:   msg.UpdatedAt,
	}
}

// FromStorageMessage converts a storage.Message to types.Message format.
func FromStorageMessage(sm *storage.Message) *types.Message {
	var usage *types.Usage
	if sm.Usage != nil {
		usage = &types.Usage{
			InputTokens:         sm.Usage.InputTokens,
			OutputTokens:        sm.Usage.OutputTokens,
			CacheCreationTokens: sm.Usage.CacheCreationTokens,
			CacheReadTokens:     sm.Usage.CacheReadTokens,
		}
	}
	msg := &types.Message{
		ID:          sm.ID,
		SessionID:   sm.SessionID,
		Role:        types.Role(sm.Role),
		Usage:       usage,
		Metadata:    sm.Metadata,
		IsPreserved: sm.IsPreserved,
		IsSummary:   sm.IsSummary,
		CreatedAt:   sm.CreatedAt,
		UpdatedAt:   sm.UpdatedAt,
	}

	// Convert content from storage format
	switch content := sm.Content.(type) {
	case []byte:
		// Raw JSON bytes - unmarshal directly
		var blocks []types.ContentBlock
		if err := json.Unmarshal(content, &blocks); err != nil {
			log.Printf("agentpg: failed to unmarshal content blocks for message %s: %v", sm.ID, err)
		} else {
			msg.Content = blocks
		}
	case []types.ContentBlock:
		// Already the right type
		msg.Content = content
	case []any:
		// Generic slice from JSON unmarshal - convert each element
		blocks := make([]types.ContentBlock, 0, len(content))
		for _, item := range content {
			if blockMap, ok := item.(map[string]any); ok {
				block := mapToContentBlock(blockMap)
				blocks = append(blocks, block)
			} else {
				log.Printf("agentpg: unexpected content block type in message %s: %T", sm.ID, item)
			}
		}
		msg.Content = blocks
	default:
		if sm.Content != nil {
			log.Printf("agentpg: unexpected content type in message %s: %T", sm.ID, sm.Content)
		}
	}

	return msg
}

// FromStorageMessages converts a slice of storage.Message to types.Message format.
func FromStorageMessages(storageMessages []*storage.Message) []*types.Message {
	messages := make([]*types.Message, len(storageMessages))
	for i, sm := range storageMessages {
		messages[i] = FromStorageMessage(sm)
	}
	return messages
}

// ToStorageMessages converts a slice of types.Message to storage.Message format.
func ToStorageMessages(messages []*types.Message) []*storage.Message {
	result := make([]*storage.Message, len(messages))
	for i, msg := range messages {
		result[i] = ToStorageMessage(msg)
	}
	return result
}

// mapToContentBlock converts a map to a ContentBlock.
// Field names match JSON tags in types.ContentBlock struct.
func mapToContentBlock(m map[string]any) types.ContentBlock {
	block := types.ContentBlock{}

	if t, ok := m["type"].(string); ok {
		block.Type = types.ContentType(t)
	}
	if text, ok := m["text"].(string); ok {
		block.Text = text
	}
	// Tool use fields (json tags: id, name, input)
	if id, ok := m["id"].(string); ok {
		block.ToolUseID = id
	}
	if name, ok := m["name"].(string); ok {
		block.ToolName = name
	}
	if input, ok := m["input"].(map[string]any); ok {
		block.ToolInput = input
		if raw, err := json.Marshal(input); err == nil {
			block.ToolInputRaw = raw
		}
	}
	// Tool result fields (json tags: tool_use_id, content, is_error)
	if id, ok := m["tool_use_id"].(string); ok {
		block.ToolResultID = id
	}
	if content, ok := m["content"].(string); ok {
		block.ToolContent = content
	}
	if isErr, ok := m["is_error"].(bool); ok {
		block.IsError = isErr
	}

	return block
}
