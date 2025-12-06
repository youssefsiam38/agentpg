package agentpg

import (
	"time"

	"github.com/youssefsiam38/agentpg/tool"
)

// Option is a functional option for configuring an Agent
type Option func(*internalConfig) error

// WithMaxTokens sets the maximum number of tokens to generate
func WithMaxTokens(n int64) Option {
	return func(c *internalConfig) error {
		c.maxTokens = n
		return nil
	}
}

// WithTemperature sets the temperature for sampling (0.0 to 1.0)
func WithTemperature(t float64) Option {
	return func(c *internalConfig) error {
		c.temperature = &t
		return nil
	}
}

// WithTopK sets the top-k sampling parameter
func WithTopK(k int64) Option {
	return func(c *internalConfig) error {
		c.topK = &k
		return nil
	}
}

// WithTopP sets the nucleus sampling parameter
func WithTopP(p float64) Option {
	return func(c *internalConfig) error {
		c.topP = &p
		return nil
	}
}

// WithStopSequences sets custom stop sequences
func WithStopSequences(sequences ...string) Option {
	return func(c *internalConfig) error {
		c.stopSequences = sequences
		return nil
	}
}

// WithTools registers tools with the agent
func WithTools(tools ...tool.Tool) Option {
	return func(c *internalConfig) error {
		for _, t := range tools {
			// Validate tool schema
			schema := t.InputSchema()
			if schema.Type != "object" {
				return NewAgentError("WithTools", ErrInvalidToolSchema).
					WithContext("tool", t.Name()).
					WithContext("reason", "schema type must be 'object'")
			}
			c.tools = append(c.tools, t)
		}
		return nil
	}
}

// WithAutoCompaction enables or disables automatic context compaction
func WithAutoCompaction(enabled bool) Option {
	return func(c *internalConfig) error {
		c.autoCompaction = enabled
		return nil
	}
}

// CompactionStrategy represents a compaction strategy
type CompactionStrategy string

const (
	// SummarizationStrategy uses Claude to summarize the conversation
	SummarizationStrategy CompactionStrategy = "summarization"

	// HybridStrategy prunes tool outputs first, then summarizes if needed
	HybridStrategy CompactionStrategy = "hybrid"
)

// WithCompactionStrategy sets the context compaction strategy
func WithCompactionStrategy(strategy CompactionStrategy) Option {
	return func(c *internalConfig) error {
		c.compactionStrategy = string(strategy)
		return nil
	}
}

// WithExtendedContext enables 1M token context via beta header
// The agent will automatically use the anthropic-beta: context-1m-2025-08-07 header
// and retry with extended context if a max_tokens error occurs
func WithExtendedContext(enabled bool) Option {
	return func(c *internalConfig) error {
		c.extendedContext = enabled
		return nil
	}
}

// WithMaxRetries sets the maximum number of retry attempts for API calls
func WithMaxRetries(n int) Option {
	return func(c *internalConfig) error {
		c.maxRetries = n
		return nil
	}
}

// WithPreserveToolOutputs preserves full tool outputs instead of pruning them during compaction
func WithPreserveToolOutputs(preserve bool) Option {
	return func(c *internalConfig) error {
		c.preserveToolOutputs = preserve
		return nil
	}
}

// WithSummarizerModel sets the model used for summarization during compaction
func WithSummarizerModel(model string) Option {
	return func(c *internalConfig) error {
		c.summarizerModel = model
		return nil
	}
}

// WithMaxContextTokens overrides the model's default context window size
func WithMaxContextTokens(tokens int) Option {
	return func(c *internalConfig) error {
		c.maxContextTokens = tokens
		return nil
	}
}

// WithCompactionTrigger sets when compaction triggers (0.0-1.0, default 0.85)
func WithCompactionTrigger(threshold float64) Option {
	return func(c *internalConfig) error {
		if threshold <= 0 || threshold > 1 {
			return NewAgentError("WithCompactionTrigger", ErrInvalidConfig).
				WithContext("threshold", threshold).
				WithContext("reason", "threshold must be between 0 and 1")
		}
		c.compactionTrigger = threshold
		return nil
	}
}

// WithCompactionTarget sets the target token count after compaction
func WithCompactionTarget(tokens int) Option {
	return func(c *internalConfig) error {
		c.compactionTarget = tokens
		return nil
	}
}

// WithCompactionPreserveN sets how many recent messages to always preserve
func WithCompactionPreserveN(n int) Option {
	return func(c *internalConfig) error {
		c.compactionPreserveN = n
		return nil
	}
}

// WithCompactionProtectedTokens sets how many recent tokens to never compact
func WithCompactionProtectedTokens(tokens int) Option {
	return func(c *internalConfig) error {
		c.compactionProtected = tokens
		return nil
	}
}

// WithMaxToolIterations sets the maximum tool call iterations per Run (default 10)
func WithMaxToolIterations(n int) Option {
	return func(c *internalConfig) error {
		if n <= 0 {
			return NewAgentError("WithMaxToolIterations", ErrInvalidConfig).
				WithContext("n", n).
				WithContext("reason", "must be positive")
		}
		c.maxToolIterations = n
		return nil
	}
}

// WithToolTimeout sets the timeout for individual tool executions (default 30s)
func WithToolTimeout(timeout time.Duration) Option {
	return func(c *internalConfig) error {
		if timeout <= 0 {
			return NewAgentError("WithToolTimeout", ErrInvalidConfig).
				WithContext("timeout", timeout).
				WithContext("reason", "timeout must be positive")
		}
		c.toolTimeout = timeout
		return nil
	}
}
