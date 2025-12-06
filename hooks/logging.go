package hooks

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/youssefsiam38/agentpg/compaction"
	"github.com/youssefsiam38/agentpg/types"
)

// LoggingHooks provides built-in logging hooks for observability
type LoggingHooks struct {
	logger *log.Logger
}

// NewLoggingHooks creates logging hooks with the provided logger
func NewLoggingHooks(logger *log.Logger) *LoggingHooks {
	return &LoggingHooks{logger: logger}
}

// DefaultLoggingHooks creates logging hooks with default logger
func DefaultLoggingHooks() *LoggingHooks {
	return &LoggingHooks{logger: log.Default()}
}

// BeforeMessage logs before sending messages to the API
func (h *LoggingHooks) BeforeMessage(ctx context.Context, messages []*types.Message) error {
	h.logger.Printf("[AgentPG] Sending %d messages to Anthropic API", len(messages))
	return nil
}

// AfterMessage logs after receiving a response
func (h *LoggingHooks) AfterMessage(ctx context.Context, response *types.Response) error {
	h.logger.Printf("[AgentPG] Received response from Anthropic API: stop_reason=%s", response.StopReason)
	return nil
}

// ToolCall logs tool execution
func (h *LoggingHooks) ToolCall(ctx context.Context, toolName string, input json.RawMessage, output string, err error) error {
	if err != nil {
		h.logger.Printf("[AgentPG] Tool '%s' failed: %v", toolName, err)
	} else {
		outputPreview := output
		if len(outputPreview) > 100 {
			outputPreview = outputPreview[:100] + "..."
		}
		h.logger.Printf("[AgentPG] Tool '%s' succeeded: %s", toolName, outputPreview)
	}
	return nil
}

// BeforeCompaction logs before context compaction
func (h *LoggingHooks) BeforeCompaction(ctx context.Context, sessionID string) error {
	h.logger.Printf("[AgentPG] Starting context compaction for session %s", sessionID)
	return nil
}

// AfterCompaction logs after context compaction
func (h *LoggingHooks) AfterCompaction(ctx context.Context, result *compaction.CompactionResult) error {
	reduction := float64(0)
	if result.OriginalTokens > 0 {
		reduction = float64(result.OriginalTokens-result.CompactedTokens) / float64(result.OriginalTokens) * 100
	}

	h.logger.Printf("[AgentPG] Compaction complete: %d → %d tokens (%.1f%% reduction, %d messages removed, strategy: %s)",
		result.OriginalTokens, result.CompactedTokens, reduction, result.MessagesRemoved, result.Strategy)
	return nil
}

// VerboseLoggingHooks provides detailed logging for debugging
type VerboseLoggingHooks struct {
	logger *log.Logger
}

// NewVerboseLoggingHooks creates verbose logging hooks
func NewVerboseLoggingHooks(logger *log.Logger) *VerboseLoggingHooks {
	return &VerboseLoggingHooks{logger: logger}
}

// BeforeMessage logs detailed message information
func (h *VerboseLoggingHooks) BeforeMessage(ctx context.Context, messages []*types.Message) error {
	h.logger.Printf("[AgentPG][VERBOSE] === Sending %d messages ===", len(messages))
	for i, msg := range messages {
		h.logger.Printf("[AgentPG][VERBOSE] Message %d: role=%s", i, msg.Role)
	}
	return nil
}

// AfterMessage logs detailed response information
func (h *VerboseLoggingHooks) AfterMessage(ctx context.Context, response *types.Response) error {
	h.logger.Printf("[AgentPG][VERBOSE] Response: stop_reason=%s", response.StopReason)

	if response.Usage != nil {
		h.logger.Printf("[AgentPG][VERBOSE] Usage: %d input + %d output = %d total tokens",
			response.Usage.InputTokens, response.Usage.OutputTokens,
			response.Usage.InputTokens+response.Usage.OutputTokens)
	}
	return nil
}

// ToolCall logs detailed tool execution information
func (h *VerboseLoggingHooks) ToolCall(ctx context.Context, toolName string, input json.RawMessage, output string, err error) error {
	start := time.Now()

	h.logger.Printf("[AgentPG][VERBOSE] === Tool Call: %s ===", toolName)
	h.logger.Printf("[AgentPG][VERBOSE] Input: %s", string(input))

	if err != nil {
		h.logger.Printf("[AgentPG][VERBOSE] Error: %v", err)
	} else {
		h.logger.Printf("[AgentPG][VERBOSE] Output: %s", output)
	}

	h.logger.Printf("[AgentPG][VERBOSE] Duration: %v", time.Since(start))
	return nil
}

// BeforeCompaction logs detailed compaction information
func (h *VerboseLoggingHooks) BeforeCompaction(ctx context.Context, sessionID string) error {
	h.logger.Printf("[AgentPG][VERBOSE] === Starting Compaction ===")
	h.logger.Printf("[AgentPG][VERBOSE] Session: %s", sessionID)
	return nil
}

// AfterCompaction logs detailed compaction results
func (h *VerboseLoggingHooks) AfterCompaction(ctx context.Context, result *compaction.CompactionResult) error {
	h.logger.Printf("[AgentPG][VERBOSE] === Compaction Complete ===")
	h.logger.Printf("[AgentPG][VERBOSE] Strategy: %s", result.Strategy)
	h.logger.Printf("[AgentPG][VERBOSE] Original tokens: %d", result.OriginalTokens)
	h.logger.Printf("[AgentPG][VERBOSE] Compacted tokens: %d", result.CompactedTokens)
	h.logger.Printf("[AgentPG][VERBOSE] Messages removed: %d", result.MessagesRemoved)

	if result.OriginalTokens > 0 {
		h.logger.Printf("[AgentPG][VERBOSE] Reduction: %.1f%%",
			float64(result.OriginalTokens-result.CompactedTokens)/float64(result.OriginalTokens)*100)
	}

	return nil
}

// MetricsHooks collects metrics for monitoring
type MetricsHooks struct {
	OnMetric func(name string, value float64, tags map[string]string)
}

// NewMetricsHooks creates metrics collection hooks
func NewMetricsHooks(onMetric func(string, float64, map[string]string)) *MetricsHooks {
	return &MetricsHooks{OnMetric: onMetric}
}

// AfterMessage records response metrics
func (h *MetricsHooks) AfterMessage(ctx context.Context, response *types.Response) error {
	if response.Usage != nil {
		h.OnMetric("agent.tokens.input", float64(response.Usage.InputTokens), nil)
		h.OnMetric("agent.tokens.output", float64(response.Usage.OutputTokens), nil)
		h.OnMetric("agent.tokens.total", float64(response.Usage.InputTokens+response.Usage.OutputTokens), nil)
	}
	return nil
}

// ToolCall records tool execution metrics
func (h *MetricsHooks) ToolCall(ctx context.Context, toolName string, input json.RawMessage, output string, err error) error {
	tags := map[string]string{"tool": toolName}

	if err != nil {
		h.OnMetric("agent.tool.error", 1, tags)
	} else {
		h.OnMetric("agent.tool.success", 1, tags)
	}

	return nil
}

// AfterCompaction records compaction metrics
func (h *MetricsHooks) AfterCompaction(ctx context.Context, result *compaction.CompactionResult) error {
	tags := map[string]string{"strategy": result.Strategy}

	h.OnMetric("agent.compaction.original_tokens", float64(result.OriginalTokens), tags)
	h.OnMetric("agent.compaction.compacted_tokens", float64(result.CompactedTokens), tags)

	if result.OriginalTokens > 0 {
		h.OnMetric("agent.compaction.reduction_pct",
			float64(result.OriginalTokens-result.CompactedTokens)/float64(result.OriginalTokens)*100, tags)
	}

	return nil
}
