package agentpg

import (
	"fmt"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/youssefsiam38/agentpg/hooks"
	"github.com/youssefsiam38/agentpg/tool"
)

// ModelInfo contains model-specific parameters
type ModelInfo struct {
	MaxContextTokens int
	DefaultMaxTokens int
}

// KnownModels maps model IDs to their capabilities
var KnownModels = map[string]ModelInfo{
	// Claude 4 models
	"claude-sonnet-4-5-20250929": {MaxContextTokens: 200000, DefaultMaxTokens: 16384},
	"claude-opus-4-5-20251101":   {MaxContextTokens: 200000, DefaultMaxTokens: 16384},
	// Claude 3.5 models
	"claude-3-5-sonnet-20241022": {MaxContextTokens: 200000, DefaultMaxTokens: 8192},
	"claude-3-5-haiku-20241022":  {MaxContextTokens: 200000, DefaultMaxTokens: 8192},
	// Claude 3 models
	"claude-3-opus-20240229":   {MaxContextTokens: 200000, DefaultMaxTokens: 4096},
	"claude-3-sonnet-20240229": {MaxContextTokens: 200000, DefaultMaxTokens: 4096},
	"claude-3-haiku-20240307":  {MaxContextTokens: 200000, DefaultMaxTokens: 4096},
}

// GetModelInfo returns model info, using sensible defaults for unknown models
func GetModelInfo(model string) ModelInfo {
	if info, ok := KnownModels[model]; ok {
		return info
	}
	// Sensible defaults for unknown models
	return ModelInfo{MaxContextTokens: 200000, DefaultMaxTokens: 8192}
}

// Config holds the required configuration for an agent.
// The database driver is passed separately to New() to enable type inference.
//
// Example:
//
//	drv := pgxv5.New(pool)
//	agent, _ := agentpg.New(drv, agentpg.Config{
//	    Client:       &client,
//	    Model:        "claude-sonnet-4-5-20250929",
//	    SystemPrompt: "You are a helpful assistant",
//	})
type Config struct {
	// Client is the Anthropic API client (required)
	Client *anthropic.Client

	// Model is the model ID to use (required)
	// Examples: "claude-sonnet-4-5-20250929", "claude-opus-4-5-20251101"
	Model string

	// SystemPrompt is the system prompt for the agent (required)
	SystemPrompt string
}

// Validate validates the configuration
func (c *Config) Validate() error {
	if c.Client == nil {
		return fmt.Errorf("%w: Anthropic client is required", ErrInvalidConfig)
	}

	if c.Model == "" {
		return fmt.Errorf("%w: Model is required", ErrInvalidConfig)
	}

	if c.SystemPrompt == "" {
		return fmt.Errorf("%w: SystemPrompt is required", ErrInvalidConfig)
	}

	return nil
}

// internalConfig holds the full agent configuration including optional parameters
type internalConfig struct {
	// Required from Config
	client       *anthropic.Client
	model        string
	systemPrompt string

	// Optional parameters
	maxTokens           int64
	temperature         *float64
	topK                *int64
	topP                *float64
	stopSequences       []string
	autoCompaction      bool
	compactionStrategy  string
	extendedContext     bool
	maxRetries          int
	preserveToolOutputs bool

	// Compaction configuration
	compactionTrigger   float64 // Threshold to trigger compaction (0.0-1.0)
	compactionTarget    int     // Target tokens after compaction
	compactionPreserveN int     // Always preserve last N messages
	compactionProtected int     // Never touch last N tokens
	summarizerModel     string  // Model for summarization
	maxContextTokens    int     // Max context window for the model
	maxToolIterations   int     // Max tool call iterations per Run

	// Tool execution configuration
	toolTimeout time.Duration // Timeout for individual tool executions

	// Internal state
	tools             []tool.Tool
	hooks             *hooks.Registry
	compactionManager interface{} // Will be set to *compaction.Manager after initialization
}

// newInternalConfig creates a new internal config from the public Config
func newInternalConfig(cfg Config) *internalConfig {
	modelInfo := GetModelInfo(cfg.Model)

	return &internalConfig{
		client:       cfg.Client,
		model:        cfg.Model,
		systemPrompt: cfg.SystemPrompt,

		// Defaults
		maxTokens:           int64(modelInfo.DefaultMaxTokens),
		autoCompaction:      true,
		compactionStrategy:  "hybrid",
		extendedContext:     false,
		maxRetries:          2,
		preserveToolOutputs: false,

		// Model-aware compaction defaults
		compactionTrigger:   0.85,                                           // Trigger at 85% utilization
		compactionTarget:    int(float64(modelInfo.MaxContextTokens) * 0.4), // Target 40% after compaction
		compactionPreserveN: 10,                                             // Always keep last 10 messages
		compactionProtected: 40000,                                          // Never touch last 40K tokens
		summarizerModel:     "claude-3-5-haiku-20241022",                    // Fast, cheap model for summaries
		maxContextTokens:    modelInfo.MaxContextTokens,
		maxToolIterations:   10,

		// Tool execution defaults
		toolTimeout: 5 * time.Minute, // Default 5 minute timeout for tools

		tools: []tool.Tool{},
		hooks: hooks.NewRegistry(),
	}
}
