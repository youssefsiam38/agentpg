// Package convert provides utilities for converting between message formats.
package convert

import (
	"encoding/json"

	"github.com/google/uuid"
	"github.com/youssefsiam38/agentpg/storage"
	"github.com/youssefsiam38/agentpg/types"
)

// ToStorageMessage converts a types.Message to storage.Message format.
// Note: Content blocks are stored separately and must be saved via SaveContentBlocks.
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

	// Convert content blocks to storage format
	var contentBlocks []*storage.ContentBlock
	if len(msg.Content) > 0 {
		contentBlocks = ToStorageContentBlocks(msg.ID, msg.Content)
	}

	return &storage.Message{
		ID:            msg.ID,
		SessionID:     msg.SessionID,
		Role:          string(msg.Role),
		Usage:         usage,
		Metadata:      msg.Metadata,
		IsPreserved:   msg.IsPreserved,
		IsSummary:     msg.IsSummary,
		CreatedAt:     msg.CreatedAt,
		UpdatedAt:     msg.UpdatedAt,
		ContentBlocks: contentBlocks,
	}
}

// FromStorageMessage converts a storage.Message to types.Message format.
// Note: ContentBlocks should be pre-populated from GetMessageContentBlocks.
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

	// Convert content blocks from storage format
	if len(sm.ContentBlocks) > 0 {
		msg.Content = FromStorageContentBlocks(sm.ContentBlocks)
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

// ToStorageContentBlocks converts types.ContentBlock slice to storage.ContentBlock slice.
func ToStorageContentBlocks(messageID string, blocks []types.ContentBlock) []*storage.ContentBlock {
	result := make([]*storage.ContentBlock, len(blocks))
	for i, block := range blocks {
		result[i] = ToStorageContentBlock(messageID, i, block)
	}
	return result
}

// ToStorageContentBlock converts a types.ContentBlock to storage.ContentBlock.
func ToStorageContentBlock(messageID string, index int, block types.ContentBlock) *storage.ContentBlock {
	sb := &storage.ContentBlock{
		ID:         uuid.New().String(),
		MessageID:  messageID,
		BlockIndex: index,
		Type:       storage.ContentBlockType(block.Type),
		IsError:    block.IsError,
	}

	// Text content
	if block.Text != "" {
		sb.Text = &block.Text
	}

	// Tool use fields
	if block.ToolUseID != "" {
		sb.ToolUseID = &block.ToolUseID
	}
	if block.ToolName != "" {
		sb.ToolName = &block.ToolName
	}
	if block.ToolInput != nil {
		sb.ToolInput = block.ToolInput
	}

	// Tool result fields
	if block.ToolResultID != "" {
		sb.ToolResultForID = &block.ToolResultID
	}
	if block.ToolContent != "" {
		sb.ToolContent = &block.ToolContent
	}

	// Image source
	if block.ImageSource != nil {
		sb.Source = map[string]any{
			"type":       block.ImageSource.Type,
			"media_type": block.ImageSource.MediaType,
			"data":       block.ImageSource.Data,
		}
	}

	return sb
}

// FromStorageContentBlocks converts storage.ContentBlock slice to types.ContentBlock slice.
func FromStorageContentBlocks(blocks []*storage.ContentBlock) []types.ContentBlock {
	result := make([]types.ContentBlock, len(blocks))
	for i, block := range blocks {
		result[i] = FromStorageContentBlock(block)
	}
	return result
}

// FromStorageContentBlock converts a storage.ContentBlock to types.ContentBlock.
func FromStorageContentBlock(sb *storage.ContentBlock) types.ContentBlock {
	block := types.ContentBlock{
		Type:    types.ContentType(sb.Type),
		IsError: sb.IsError,
	}

	// Text content
	if sb.Text != nil {
		block.Text = *sb.Text
	}

	// Tool use fields
	if sb.ToolUseID != nil {
		block.ToolUseID = *sb.ToolUseID
	}
	if sb.ToolName != nil {
		block.ToolName = *sb.ToolName
	}
	if sb.ToolInput != nil {
		block.ToolInput = sb.ToolInput
		if raw, err := json.Marshal(sb.ToolInput); err == nil {
			block.ToolInputRaw = raw
		}
	}

	// Tool result fields
	// For tool_result blocks, ToolUseID contains Claude's tool_use_id (what the API needs)
	// ToolResultForID contains our internal block UUID (for database references)
	if sb.ToolUseID != nil && sb.Type == storage.ContentBlockTypeToolResult {
		block.ToolResultID = *sb.ToolUseID
	} else if sb.ToolResultForID != nil {
		block.ToolResultID = *sb.ToolResultForID
	}
	if sb.ToolContent != nil {
		block.ToolContent = *sb.ToolContent
	}

	// Image source
	if sb.Source != nil {
		if srcType, ok := sb.Source["type"].(string); ok {
			block.ImageSource = &types.ImageSource{
				Type: srcType,
			}
			if mediaType, ok := sb.Source["media_type"].(string); ok {
				block.ImageSource.MediaType = mediaType
			}
			if data, ok := sb.Source["data"].(string); ok {
				block.ImageSource.Data = data
			}
		}
	}

	return block
}
