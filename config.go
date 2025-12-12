package agentpg

import (
	"time"
)

// =============================================================================
// MODEL CONFIGURATION
// =============================================================================

// ModelInfo contains model-specific parameters.
type ModelInfo struct {
	// MaxContextTokens is the maximum context window size in tokens.
	MaxContextTokens int

	// DefaultMaxTokens is the default maximum tokens to generate.
	DefaultMaxTokens int

	// SupportsBatchAPI indicates if this model supports the Batch API.
	SupportsBatchAPI bool
}

// KnownModels maps model IDs to their capabilities.
var KnownModels = map[string]ModelInfo{
	// Claude 4 models
	"claude-sonnet-4-5-20250929": {MaxContextTokens: 200000, DefaultMaxTokens: 16384, SupportsBatchAPI: true},
	"claude-opus-4-5-20251101":   {MaxContextTokens: 200000, DefaultMaxTokens: 16384, SupportsBatchAPI: true},
	// Claude 3.5 models
	"claude-3-5-sonnet-20241022": {MaxContextTokens: 200000, DefaultMaxTokens: 8192, SupportsBatchAPI: true},
	"claude-3-5-haiku-20241022":  {MaxContextTokens: 200000, DefaultMaxTokens: 8192, SupportsBatchAPI: true},
	// Claude 3 models
	"claude-3-opus-20240229":   {MaxContextTokens: 200000, DefaultMaxTokens: 4096, SupportsBatchAPI: true},
	"claude-3-sonnet-20240229": {MaxContextTokens: 200000, DefaultMaxTokens: 4096, SupportsBatchAPI: true},
	"claude-3-haiku-20240307":  {MaxContextTokens: 200000, DefaultMaxTokens: 4096, SupportsBatchAPI: true},
}

// GetModelInfo returns model info, using sensible defaults for unknown models.
func GetModelInfo(model string) ModelInfo {
	if info, ok := KnownModels[model]; ok {
		return info
	}
	// Sensible defaults for unknown models
	return ModelInfo{
		MaxContextTokens: 200000,
		DefaultMaxTokens: 8192,
		SupportsBatchAPI: true, // Assume Batch API support
	}
}

// =============================================================================
// COMPACTION CONFIGURATION
// =============================================================================

// CompactionConfig holds configuration for context compaction.
type CompactionConfig struct {
	// Enabled controls whether automatic compaction is enabled.
	// Defaults to true.
	Enabled bool

	// Strategy is the compaction strategy to use.
	// Options: "summarization", "hybrid"
	// Defaults to "hybrid".
	Strategy string

	// Trigger is the context utilization threshold (0.0-1.0) at which
	// compaction is triggered. Defaults to 0.85 (85%).
	Trigger float64

	// TargetTokens is the target token count after compaction.
	// Defaults to 40% of max context.
	TargetTokens int

	// PreserveLastN is the number of recent messages to always preserve.
	// Defaults to 10.
	PreserveLastN int

	// ProtectedTokens is the number of recent tokens to never compact.
	// Defaults to 40000.
	ProtectedTokens int

	// SummarizerModel is the model to use for generating summaries.
	// Defaults to "claude-3-5-haiku-20241022".
	SummarizerModel string

	// PreserveToolOutputs keeps full tool outputs instead of pruning them.
	// Defaults to false.
	PreserveToolOutputs bool
}

// DefaultCompactionConfig returns default compaction settings for a model.
func DefaultCompactionConfig(model string) CompactionConfig {
	modelInfo := GetModelInfo(model)
	return CompactionConfig{
		Enabled:             true,
		Strategy:            "hybrid",
		Trigger:             0.85,
		TargetTokens:        int(float64(modelInfo.MaxContextTokens) * 0.4),
		PreserveLastN:       10,
		ProtectedTokens:     40000,
		SummarizerModel:     "claude-3-5-haiku-20241022",
		PreserveToolOutputs: false,
	}
}

// =============================================================================
// RETRY CONFIGURATION
// =============================================================================

// RetryConfig holds configuration for retry behavior.
type RetryConfig struct {
	// MaxAttempts is the maximum number of retry attempts for API calls.
	// Defaults to 3.
	MaxAttempts int

	// InitialDelay is the initial delay before the first retry.
	// Defaults to 1 second.
	InitialDelay time.Duration

	// MaxDelay is the maximum delay between retries.
	// Defaults to 30 seconds.
	MaxDelay time.Duration

	// Multiplier is the backoff multiplier.
	// Defaults to 2.0.
	Multiplier float64
}

// DefaultRetryConfig returns default retry settings.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts:  3,
		InitialDelay: 1 * time.Second,
		MaxDelay:     30 * time.Second,
		Multiplier:   2.0,
	}
}

// =============================================================================
// TOOL CONFIGURATION
// =============================================================================

// ToolConfig holds configuration for tool execution.
type ToolConfig struct {
	// Timeout is the timeout for individual tool executions.
	// Defaults to 5 minutes.
	Timeout time.Duration

	// MaxIterations is the maximum tool call iterations per run.
	// Defaults to 10.
	MaxIterations int

	// RetryOnFailure controls whether to retry failed tool executions.
	// Defaults to true.
	RetryOnFailure bool

	// MaxRetries is the maximum retry attempts for a single tool execution.
	// Defaults to 3.
	MaxRetries int
}

// DefaultToolConfig returns default tool execution settings.
func DefaultToolConfig() ToolConfig {
	return ToolConfig{
		Timeout:        5 * time.Minute,
		MaxIterations:  10,
		RetryOnFailure: true,
		MaxRetries:     3,
	}
}
