package compaction

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/google/uuid"
	"github.com/youssefsiam38/agentpg/types"
)

// SummarizationStrategy implements Claude Code's 8-section summarization
type SummarizationStrategy struct {
	client      *anthropic.Client
	counter     *TokenCounter
	partitioner *Partitioner
}

// NewSummarizationStrategy creates a new summarization strategy
func NewSummarizationStrategy(client *anthropic.Client) *SummarizationStrategy {
	return &SummarizationStrategy{
		client:      client,
		counter:     NewTokenCounter(client),
		partitioner: NewPartitioner(),
	}
}

func (s *SummarizationStrategy) Name() string {
	return "summarization"
}

func (s *SummarizationStrategy) ShouldCompact(messages []*types.Message, config CompactionConfig) bool {
	totalTokens := SumTokens(messages)
	threshold := int(float64(config.MaxContextTokens) * config.TriggerThreshold)
	return totalTokens >= threshold
}

// Claude Code's reverse-engineered compaction prompt (8-section structure)
const compactionPrompt = `Your task is to create a detailed summary of the conversation so far, paying close attention to the user's explicit requests and your previous actions.

This summary should be thorough in capturing technical details, code patterns, and architectural decisions that would be essential for continuing development work without losing context.

Before providing your final summary, wrap your analysis in <analysis> tags. In your analysis:

1. Chronologically analyze each message. For each, identify:
   - The user's explicit requests and intents
   - Your approach to addressing requests
   - Key decisions, technical concepts, code patterns
   - Specific details: file names, full code snippets, function signatures, file edits
   - Errors encountered and how they were fixed
   - Specific user feedback, especially corrections or different approaches requested

2. Double-check for technical accuracy and completeness.

Your summary MUST include these sections:
1. **Primary Request and Intent**: All user requests and intents in detail
2. **Key Technical Concepts**: Technologies, frameworks, patterns discussed
3. **Files and Code Sections**: Specific files examined/modified/created with full code snippets where applicable and why each change matters
4. **Errors and Fixes**: All errors encountered and resolutions, especially user corrections
5. **Problem Solving**: Problems solved and ongoing troubleshooting
6. **All User Messages**: List ALL user messages (not tool results) - critical for understanding feedback
7. **Pending Tasks**: Explicitly requested tasks not yet completed
8. **Current Work**: Precisely what was being worked on immediately before this summary, with file names and code snippets
9. **Optional Next Step**: The next step related to most recent work`

func (s *SummarizationStrategy) Compact(
	ctx context.Context,
	messages []*types.Message,
	config CompactionConfig,
) (*CompactionResult, error) {
	// Partition messages
	preserved, toSummarize := s.partitioner.Partition(messages, config)

	if len(toSummarize) == 0 {
		return nil, nil // Nothing to compact
	}

	// Build conversation text for summarization
	conversationText := s.buildConversationText(toSummarize)

	// Add custom instructions if provided
	prompt := compactionPrompt
	if config.CustomInstructions != "" {
		prompt += "\n\nADDITIONAL INSTRUCTIONS:\n" + config.CustomInstructions
	}

	// Call Claude for summarization
	resp, err := s.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(config.SummarizerModel),
		MaxTokens: 4096,
		System:    BuildSystemPrompt(prompt),
		Messages: []anthropic.MessageParam{
			{
				Role: anthropic.MessageParamRoleUser,
				Content: []anthropic.ContentBlockParamUnion{
					anthropic.NewTextBlock("Please summarize this conversation:\n\n" + conversationText),
				},
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("summarization failed: %w", err)
	}

	// Extract summary text
	summary := extractTextContent(resp.Content)

	// Count summary tokens
	summaryTokens, _ := s.counter.CountTokens(ctx, config.SummarizerModel, summary)

	// Create summary message
	summaryMsg := &types.Message{
		ID:        uuid.New().String(),
		SessionID: messages[0].SessionID, // Use same session
		Role:      types.RoleSystem,
		Content: []types.ContentBlock{
			{
				Type: types.ContentTypeText,
				Text: buildContinuationPrompt(summary),
			},
		},
		Usage: &types.Usage{
			InputTokens: summaryTokens,
		},
		IsSummary: true,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	originalTokens := SumTokens(messages)
	compactedTokens := summaryTokens + SumTokens(preserved)

	// Combine summary message with preserved messages
	finalMessages := append([]*types.Message{summaryMsg}, preserved...)

	return &CompactionResult{
		Summary:           summary,
		PreservedMessages: finalMessages,
		OriginalTokens:    originalTokens,
		CompactedTokens:   compactedTokens,
		MessagesRemoved:   len(toSummarize),
		Strategy:          s.Name(),
		CompactedAt:       time.Now(),
	}, nil
}

// buildConversationText creates text representation of messages
func (s *SummarizationStrategy) buildConversationText(messages []*types.Message) string {
	var builder strings.Builder

	for _, msg := range messages {
		builder.WriteString(fmt.Sprintf("\n=== %s MESSAGE ===\n", strings.ToUpper(string(msg.Role))))

		for _, block := range msg.Content {
			switch block.Type {
			case types.ContentTypeText:
				builder.WriteString(block.Text)
				builder.WriteString("\n")

			case types.ContentTypeToolUse:
				builder.WriteString(fmt.Sprintf("\n[TOOL CALL: %s]\n", block.ToolName))
				inputJSON, _ := json.Marshal(block.ToolInput)
				builder.WriteString(fmt.Sprintf("Input: %s\n", string(inputJSON)))

			case types.ContentTypeToolResult:
				builder.WriteString(fmt.Sprintf("\n[TOOL RESULT: %s]\n", block.ToolUseID))
				builder.WriteString(fmt.Sprintf("Output: %s\n", block.ToolContent))
				if block.IsError {
					builder.WriteString("[ERROR]\n")
				}
			}
		}
		builder.WriteString("\n")
	}

	return builder.String()
}

// buildContinuationPrompt wraps summary with continuation instruction (Cline pattern)
func buildContinuationPrompt(summary string) string {
	return fmt.Sprintf(`This session is being continued from a previous conversation that ran out of context. The conversation is summarized below:

%s

Please continue the conversation from where we left it off without asking the user any further questions. Continue with the last task that you were asked to work on.`, summary)
}

// extractTextContent extracts text from Anthropic content blocks
// BuildSystemPrompt creates a system prompt parameter
func BuildSystemPrompt(text string) []anthropic.TextBlockParam {
	return []anthropic.TextBlockParam{
		{
			Type: "text",
			Text: text,
		},
	}
}

func extractTextContent(content []anthropic.ContentBlockUnion) string {
	var texts []string
	for _, block := range content {
		if textBlock, ok := block.AsAny().(anthropic.TextBlock); ok {
			texts = append(texts, textBlock.Text)
		}
	}
	return strings.Join(texts, "\n")
}
