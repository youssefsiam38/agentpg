package agentpg

import (
	"os"
	"time"

	"github.com/google/uuid"
)

// Logger interface for structured logging.
// Compatible with slog.Logger and other structured loggers.
type Logger interface {
	Debug(msg string, args ...any)
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
}

// ClientConfig holds all configuration options for a Client.
type ClientConfig struct {
	// APIKey is the Anthropic API key (required).
	// Falls back to ANTHROPIC_API_KEY environment variable if not set.
	APIKey string

	// Name identifies this service instance for logging and debugging.
	// Defaults to hostname if not set.
	Name string

	// ID is the unique identifier for this client instance.
	// Defaults to a generated UUID if not set.
	// Must be unique across all running instances.
	ID string

	// MaxConcurrentRuns limits concurrent batch run processing.
	// Defaults to DefaultMaxConcurrentRuns (10).
	MaxConcurrentRuns int

	// MaxConcurrentStreamingRuns limits concurrent streaming run processing.
	// Streaming runs hold connections longer, so this is typically lower than MaxConcurrentRuns.
	// Defaults to DefaultMaxConcurrentStreamingRuns (5).
	MaxConcurrentStreamingRuns int

	// MaxConcurrentTools limits concurrent tool executions.
	// Defaults to DefaultMaxConcurrentTools (50).
	MaxConcurrentTools int

	// BatchPollInterval is how often to poll Claude Batch API for status.
	// Defaults to DefaultBatchPollInterval (30 seconds).
	BatchPollInterval time.Duration

	// RunPollInterval is the polling fallback interval for new runs.
	// Used when LISTEN/NOTIFY is unavailable.
	// Defaults to DefaultRunPollInterval (1 second).
	RunPollInterval time.Duration

	// ToolPollInterval is the polling interval for tool executions.
	// Used when LISTEN/NOTIFY is unavailable.
	// Defaults to DefaultToolPollInterval (500 milliseconds).
	ToolPollInterval time.Duration

	// HeartbeatInterval for instance liveness.
	// Defaults to DefaultHeartbeatInterval (15 seconds).
	HeartbeatInterval time.Duration

	// LeaderTTL is the leader election lease duration.
	// Defaults to DefaultLeaderTTL (30 seconds).
	LeaderTTL time.Duration

	// StuckRunTimeout marks runs as stuck after this duration without progress.
	// The leader will attempt to recover stuck runs.
	// Defaults to DefaultStuckRunTimeout (5 minutes).
	StuckRunTimeout time.Duration

	// InstanceTTL is how long an instance can go without heartbeat before cleanup.
	// Should be > 2x HeartbeatInterval.
	// Defaults to DefaultInstanceTTL (2 minutes).
	InstanceTTL time.Duration

	// CleanupInterval is how often to run cleanup jobs (stale instances, etc.).
	// Defaults to DefaultCleanupInterval (1 minute).
	CleanupInterval time.Duration

	// Logger for structured logging.
	// If nil, logs are discarded.
	Logger Logger
}

// Default configuration values.
const (
	DefaultMaxConcurrentRuns          = 10
	DefaultMaxConcurrentStreamingRuns = 5 // Lower because streaming holds connections
	DefaultMaxConcurrentTools         = 50
	DefaultBatchPollInterval          = 30 * time.Second
	DefaultRunPollInterval            = 1 * time.Second
	DefaultToolPollInterval           = 500 * time.Millisecond
	DefaultHeartbeatInterval          = 15 * time.Second
	DefaultLeaderTTL                  = 30 * time.Second
	DefaultStuckRunTimeout            = 5 * time.Minute
	DefaultInstanceTTL                = 2 * time.Minute
	DefaultCleanupInterval            = 1 * time.Minute
	DefaultMaxToolRetries             = 3
)

// validate validates the configuration and sets defaults.
// Returns an error if required fields are missing.
func (c *ClientConfig) validate() error {
	// API key is required
	if c.APIKey == "" {
		c.APIKey = os.Getenv("ANTHROPIC_API_KEY")
	}
	if c.APIKey == "" {
		return NewAgentError("ValidateConfig", ErrInvalidConfig).
			WithContext("field", "APIKey").
			WithContext("reason", "API key is required, set via config or ANTHROPIC_API_KEY env var")
	}

	// Set defaults for optional fields
	if c.Name == "" {
		hostname, err := os.Hostname()
		if err != nil {
			hostname = "unknown"
		}
		c.Name = hostname
	}

	if c.ID == "" {
		c.ID = uuid.New().String()
	}

	if c.MaxConcurrentRuns <= 0 {
		c.MaxConcurrentRuns = DefaultMaxConcurrentRuns
	}

	if c.MaxConcurrentStreamingRuns <= 0 {
		c.MaxConcurrentStreamingRuns = DefaultMaxConcurrentStreamingRuns
	}

	if c.MaxConcurrentTools <= 0 {
		c.MaxConcurrentTools = DefaultMaxConcurrentTools
	}

	if c.BatchPollInterval <= 0 {
		c.BatchPollInterval = DefaultBatchPollInterval
	}

	if c.RunPollInterval <= 0 {
		c.RunPollInterval = DefaultRunPollInterval
	}

	if c.ToolPollInterval <= 0 {
		c.ToolPollInterval = DefaultToolPollInterval
	}

	if c.HeartbeatInterval <= 0 {
		c.HeartbeatInterval = DefaultHeartbeatInterval
	}

	if c.LeaderTTL <= 0 {
		c.LeaderTTL = DefaultLeaderTTL
	}

	if c.StuckRunTimeout <= 0 {
		c.StuckRunTimeout = DefaultStuckRunTimeout
	}

	if c.InstanceTTL <= 0 {
		c.InstanceTTL = DefaultInstanceTTL
	}

	if c.CleanupInterval <= 0 {
		c.CleanupInterval = DefaultCleanupInterval
	}

	return nil
}

// DefaultConfig returns a new ClientConfig with all default values.
// Note: APIKey must still be set before use.
func DefaultConfig() *ClientConfig {
	return &ClientConfig{
		MaxConcurrentRuns:          DefaultMaxConcurrentRuns,
		MaxConcurrentStreamingRuns: DefaultMaxConcurrentStreamingRuns,
		MaxConcurrentTools:         DefaultMaxConcurrentTools,
		BatchPollInterval:          DefaultBatchPollInterval,
		RunPollInterval:            DefaultRunPollInterval,
		ToolPollInterval:           DefaultToolPollInterval,
		HeartbeatInterval:          DefaultHeartbeatInterval,
		LeaderTTL:                  DefaultLeaderTTL,
		StuckRunTimeout:            DefaultStuckRunTimeout,
		InstanceTTL:                DefaultInstanceTTL,
		CleanupInterval:            DefaultCleanupInterval,
	}
}
