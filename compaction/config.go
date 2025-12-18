package compaction

import (
	"fmt"
)

// Strategy represents a compaction strategy.
type Strategy string

const (
	// StrategySummarization uses Claude to create a structured summary of older messages.
	StrategySummarization Strategy = "summarization"

	// StrategyHybrid prunes tool outputs first, then summarizes if still needed.
	// This is the recommended strategy as it's more cost-effective.
	StrategyHybrid Strategy = "hybrid"
)

// Default configuration values based on production patterns.
const (
	DefaultStrategy            = StrategyHybrid
	DefaultTrigger             = 0.85    // 85% context usage
	DefaultTargetTokens        = 80000   // Target 80K tokens after compaction
	DefaultPreserveLastN       = 10      // Always keep last 10 messages
	DefaultProtectedTokens     = 40000   // Never touch last 40K tokens
	DefaultSummarizerModel     = "claude-3-5-haiku-20241022"
	DefaultMaxTokensForModel   = 200000  // Claude Sonnet 4.5 context window
	DefaultPreserveToolOutputs = false   // Prune tool outputs by default
	DefaultUseTokenCountingAPI = true    // Use Claude API for accurate counts
	DefaultSummarizerMaxTokens = 4096    // Max tokens for summarization response
)

// Config holds compaction configuration.
type Config struct {
	// Strategy is the compaction strategy to use.
	// Default: StrategyHybrid
	Strategy Strategy

	// Trigger is the context usage threshold (0.0-1.0) that triggers compaction.
	// E.g., 0.85 means trigger at 85% context usage.
	// Default: 0.85
	Trigger float64

	// TargetTokens is the target token count after compaction.
	// The compactor will try to reduce context to this level.
	// Default: 80000
	TargetTokens int

	// PreserveLastN is the minimum number of recent messages to always preserve.
	// These messages are never summarized or removed.
	// Default: 10
	PreserveLastN int

	// ProtectedTokens is the token count at the end of context that is never summarized.
	// This protects the most recent conversation from compaction.
	// Default: 40000
	ProtectedTokens int

	// SummarizerModel is the Claude model to use for summarization.
	// Using a faster/cheaper model is recommended.
	// Default: "claude-3-5-haiku-20241022"
	SummarizerModel string

	// SummarizerMaxTokens is the maximum tokens for the summarization response.
	// Default: 4096
	SummarizerMaxTokens int

	// MaxTokensForModel is the maximum context window for the target model.
	// Used to calculate context usage percentage.
	// Default: 200000 (Sonnet 4.5 context window)
	MaxTokensForModel int

	// PreserveToolOutputs determines whether to keep tool outputs during hybrid compaction.
	// If false, tool outputs outside the protected zone are replaced with "[TOOL OUTPUT PRUNED]" placeholder.
	// Default: false
	PreserveToolOutputs bool

	// UseTokenCountingAPI determines whether to use Claude's token counting API.
	// If false or API fails, uses character-based approximation.
	// Default: true
	UseTokenCountingAPI bool
}

// DefaultConfig returns a Config with sensible defaults based on production patterns.
func DefaultConfig() *Config {
	return &Config{
		Strategy:            DefaultStrategy,
		Trigger:             DefaultTrigger,
		TargetTokens:        DefaultTargetTokens,
		PreserveLastN:       DefaultPreserveLastN,
		ProtectedTokens:     DefaultProtectedTokens,
		SummarizerModel:     DefaultSummarizerModel,
		SummarizerMaxTokens: DefaultSummarizerMaxTokens,
		MaxTokensForModel:   DefaultMaxTokensForModel,
		PreserveToolOutputs: DefaultPreserveToolOutputs,
		UseTokenCountingAPI: DefaultUseTokenCountingAPI,
	}
}

// Validate validates the configuration and returns an error if invalid.
func (c *Config) Validate() error {
	if c.Strategy != StrategySummarization && c.Strategy != StrategyHybrid {
		return fmt.Errorf("%w: unknown strategy %q, must be %q or %q",
			ErrInvalidConfig, c.Strategy, StrategySummarization, StrategyHybrid)
	}

	if c.Trigger <= 0 || c.Trigger > 1.0 {
		return fmt.Errorf("%w: trigger must be between 0 and 1, got %f", ErrInvalidConfig, c.Trigger)
	}

	if c.TargetTokens <= 0 {
		return fmt.Errorf("%w: target_tokens must be positive, got %d", ErrInvalidConfig, c.TargetTokens)
	}

	if c.PreserveLastN < 0 {
		return fmt.Errorf("%w: preserve_last_n must be non-negative, got %d", ErrInvalidConfig, c.PreserveLastN)
	}

	if c.ProtectedTokens < 0 {
		return fmt.Errorf("%w: protected_tokens must be non-negative, got %d", ErrInvalidConfig, c.ProtectedTokens)
	}

	if c.SummarizerModel == "" {
		return fmt.Errorf("%w: summarizer_model is required", ErrInvalidConfig)
	}

	if c.MaxTokensForModel <= 0 {
		return fmt.Errorf("%w: max_tokens_for_model must be positive, got %d", ErrInvalidConfig, c.MaxTokensForModel)
	}

	if c.SummarizerMaxTokens <= 0 {
		return fmt.Errorf("%w: summarizer_max_tokens must be positive, got %d", ErrInvalidConfig, c.SummarizerMaxTokens)
	}

	if c.TargetTokens >= c.MaxTokensForModel {
		return fmt.Errorf("%w: target_tokens (%d) must be less than max_tokens_for_model (%d)",
			ErrInvalidConfig, c.TargetTokens, c.MaxTokensForModel)
	}

	return nil
}

// ApplyDefaults fills in zero values with defaults.
func (c *Config) ApplyDefaults() {
	if c.Strategy == "" {
		c.Strategy = DefaultStrategy
	}
	if c.Trigger == 0 {
		c.Trigger = DefaultTrigger
	}
	if c.TargetTokens == 0 {
		c.TargetTokens = DefaultTargetTokens
	}
	if c.PreserveLastN == 0 {
		c.PreserveLastN = DefaultPreserveLastN
	}
	if c.ProtectedTokens == 0 {
		c.ProtectedTokens = DefaultProtectedTokens
	}
	if c.SummarizerModel == "" {
		c.SummarizerModel = DefaultSummarizerModel
	}
	if c.SummarizerMaxTokens == 0 {
		c.SummarizerMaxTokens = DefaultSummarizerMaxTokens
	}
	if c.MaxTokensForModel == 0 {
		c.MaxTokensForModel = DefaultMaxTokensForModel
	}
	// UseTokenCountingAPI defaults to true, so we don't need to check for zero
}

// TriggerThreshold returns the absolute token count that triggers compaction.
func (c *Config) TriggerThreshold() int {
	return int(float64(c.MaxTokensForModel) * c.Trigger)
}
