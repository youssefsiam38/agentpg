package compaction

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/youssefsiam38/agentpg/driver"
)

// Summarizer handles the creation of conversation summaries using Claude's streaming API.
type Summarizer struct {
	client    *anthropic.Client
	model     string
	maxTokens int
}

// NewSummarizer creates a new Summarizer with the given Anthropic client and configuration.
func NewSummarizer(client *anthropic.Client, model string, maxTokens int) *Summarizer {
	return &Summarizer{
		client:    client,
		model:     model,
		maxTokens: maxTokens,
	}
}

// Summarize generates a summary of the given messages using Claude's streaming API.
// It returns the summary text and any error encountered.
func (s *Summarizer) Summarize(ctx context.Context, messages []*driver.Message) (string, error) {
	return s.SummarizeWithContext(ctx, nil, messages)
}

// SummarizeWithContext generates a summary with additional context from previous summaries.
func (s *Summarizer) SummarizeWithContext(ctx context.Context, contextMsgs, toSummarize []*driver.Message) (string, error) {
	if len(toSummarize) == 0 {
		return "", ErrNoMessagesToCompact
	}

	// Convert messages to text format for summarization
	conversationText := s.formatMessagesForSummary(toSummarize)

	var userPrompt string
	if len(contextMsgs) > 0 {
		contextText := s.formatMessagesForSummary(contextMsgs)
		userPrompt = BuildSummarizationUserPromptWithContext(contextText, conversationText)
	} else {
		userPrompt = BuildSummarizationUserPrompt(conversationText)
	}

	// Create the streaming request
	stream := s.client.Messages.NewStreaming(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(s.model),
		MaxTokens: int64(s.maxTokens),
		System: []anthropic.TextBlockParam{
			{Text: SummarizationSystemPrompt},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(userPrompt)),
		},
	})

	// Accumulate the streamed response
	message := anthropic.Message{}
	for stream.Next() {
		event := stream.Current()
		if err := message.Accumulate(event); err != nil {
			return "", fmt.Errorf("%w: failed to accumulate stream: %v", ErrSummarizationFailed, err)
		}
	}

	if err := stream.Err(); err != nil {
		return "", fmt.Errorf("%w: %v", ErrSummarizationFailed, err)
	}

	// Extract text from the response
	var summary strings.Builder
	for _, block := range message.Content {
		if text, ok := block.AsAny().(anthropic.TextBlock); ok {
			summary.WriteString(text.Text)
		}
	}

	if summary.Len() == 0 {
		return "", fmt.Errorf("%w: empty response from summarizer", ErrSummarizationFailed)
	}

	return summary.String(), nil
}

// formatMessagesForSummary converts messages to a text format suitable for summarization.
func (s *Summarizer) formatMessagesForSummary(messages []*driver.Message) string {
	summaryMsgs := make([]MessageForSummary, 0, len(messages))

	for _, msg := range messages {
		content := s.extractMessageContent(msg)
		if content != "" {
			summaryMsgs = append(summaryMsgs, MessageForSummary{
				Role:    string(msg.Role),
				Content: content,
			})
		}
	}

	return FormatMessagesAsText(summaryMsgs)
}

// extractMessageContent extracts readable text content from a message.
func (s *Summarizer) extractMessageContent(msg *driver.Message) string {
	var parts []string

	for _, block := range msg.Content {
		switch block.Type {
		case "text":
			if block.Text != "" {
				parts = append(parts, block.Text)
			}
		case "tool_use":
			// Include tool invocations with their inputs
			toolInfo := fmt.Sprintf("[Tool: %s, Input: %s]", block.ToolName, string(block.ToolInput))
			parts = append(parts, toolInfo)
		case "tool_result":
			// Include tool results (abbreviated if very long)
			result := block.ToolContent
			if len(result) > 500 {
				result = result[:497] + "..."
			}
			toolResult := fmt.Sprintf("[Tool Result for %s: %s]", block.ToolResultForUseID, result)
			if block.IsError {
				toolResult = fmt.Sprintf("[Tool Error for %s: %s]", block.ToolResultForUseID, result)
			}
			parts = append(parts, toolResult)
		case "thinking":
			// Include thinking blocks as they contain useful context
			if block.Text != "" {
				parts = append(parts, fmt.Sprintf("[Thinking: %s]", block.Text))
			}
		}
	}

	return strings.Join(parts, "\n")
}

// SummarizationStrategy implements the StrategyExecutor interface using pure summarization.
type SummarizationStrategy struct {
	summarizer   *Summarizer
	tokenCounter *TokenCounter
}

// NewSummarizationStrategy creates a new summarization strategy.
func NewSummarizationStrategy(summarizer *Summarizer, tokenCounter *TokenCounter) *SummarizationStrategy {
	return &SummarizationStrategy{
		summarizer:   summarizer,
		tokenCounter: tokenCounter,
	}
}

// Name returns the strategy name.
func (s *SummarizationStrategy) Name() Strategy {
	return StrategySummarization
}

// Execute performs the summarization compaction.
func (s *SummarizationStrategy) Execute(ctx context.Context, partition *MessagePartition) (*StrategyResult, error) {
	if !partition.CanCompact() {
		return nil, ErrNoMessagesToCompact
	}

	start := time.Now()

	// Get context messages (previous summaries and preserved messages)
	contextMsgs := partition.ContextMessages()

	// Summarize the compactable messages
	summaryText, err := s.summarizer.SummarizeWithContext(ctx, contextMsgs, partition.Compactable)
	if err != nil {
		return nil, err
	}

	// Estimate tokens for the summary
	summaryTokens, err := s.tokenCounter.CountTokensForContent(ctx, summaryText)
	if err != nil {
		// Use approximation if counting fails
		summaryTokens = approximateTokens(summaryText)
	}

	// Calculate token reduction
	tokensRemoved := partition.Stats.CompactableTokens
	tokensAfter := partition.Stats.TotalTokens - tokensRemoved + summaryTokens

	return &StrategyResult{
		SummaryText:        summaryText,
		SummaryTokens:      summaryTokens,
		ArchivedMessageIDs: partition.CompactableIDs(),
		TokensRemoved:      tokensRemoved,
		TokensAfter:        tokensAfter,
		Duration:           time.Since(start),
	}, nil
}
