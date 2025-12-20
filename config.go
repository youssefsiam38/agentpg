package agentpg

import (
	"math"
	"math/rand"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/youssefsiam38/agentpg/compaction"
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

	// AutoCompactionEnabled enables automatic context compaction in workers.
	// When enabled, workers will check if compaction is needed after each run
	// completes and trigger compaction if the context exceeds the threshold.
	// Defaults to false (manual compaction only).
	AutoCompactionEnabled bool

	// CompactionConfig is the configuration for context compaction.
	// If nil, default compaction configuration is used.
	// Only used if AutoCompactionEnabled is true or when calling Compact() manually.
	CompactionConfig *compaction.Config

	// ToolRetryConfig configures tool execution retry behavior.
	// If nil, default retry configuration is used.
	ToolRetryConfig *ToolRetryConfig

	// RunRescueConfig configures run rescue behavior for stuck runs.
	// If nil, default rescue configuration is used.
	RunRescueConfig *RunRescueConfig
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

	// Default tool retry configuration
	DefaultToolRetryMaxAttempts = 2   // Fast default: 2 attempts total (1 retry)
	DefaultToolRetryJitter      = 0.0 // No jitter by default for instant retry

	// Default run rescue configuration
	DefaultRescueInterval    = 1 * time.Minute
	DefaultRescueTimeout     = 5 * time.Minute // Should match StuckRunTimeout
	DefaultMaxRescueAttempts = 3
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

// ToolRetryConfig configures tool execution retry behavior.
// Uses River's attempt^4 formula for exponential backoff.
type ToolRetryConfig struct {
	// MaxAttempts is the maximum number of execution attempts.
	// After this many attempts, the tool is marked as permanently failed.
	// Default: 3
	MaxAttempts int

	// Jitter adds randomness to prevent thundering herd.
	// Range: 0.0 to 1.0 (proportion of delay to randomize).
	// Default: 0.1 (10% jitter)
	Jitter float64
}

// DefaultToolRetryConfig returns the default tool retry configuration.
func DefaultToolRetryConfig() *ToolRetryConfig {
	return &ToolRetryConfig{
		MaxAttempts: DefaultToolRetryMaxAttempts,
		Jitter:      DefaultToolRetryJitter,
	}
}

// NextRetryDelay calculates the delay before the next retry attempt.
// By default returns 0 for instant retry (snappy user experience).
// If Jitter > 0, uses River's attempt^4 formula with jitter for backoff.
func (c *ToolRetryConfig) NextRetryDelay(attemptCount int) time.Duration {
	// Default: instant retry (no delay) for snappy experience
	if c.Jitter <= 0 {
		return 0
	}

	// If jitter is configured, use exponential backoff
	if attemptCount <= 0 {
		attemptCount = 1
	}

	// River's attempt^4 formula: delay = attempt^4 seconds
	base := math.Pow(float64(attemptCount), 4)
	delay := time.Duration(base) * time.Second

	// Apply jitter (±jitter%)
	jitterRange := float64(delay) * c.Jitter
	jitterOffset := (rand.Float64() * 2 * jitterRange) - jitterRange
	delay = time.Duration(float64(delay) + jitterOffset)

	// Ensure delay is at least 1 second when using backoff
	if delay < time.Second {
		delay = time.Second
	}

	return delay
}

// RunRescueConfig configures run rescue behavior for stuck runs.
// Runs stuck in non-terminal states are periodically rescued by the leader.
type RunRescueConfig struct {
	// RescueInterval is how often to check for stuck runs.
	// Only the leader performs rescue operations.
	// Default: 1 minute
	RescueInterval time.Duration

	// RescueTimeout is how long a run can be stuck before rescue.
	// A run is considered stuck if it's been in a non-terminal state
	// (batch_submitting, batch_pending, batch_processing, streaming, pending_tools)
	// for longer than this duration.
	// Default: 5 minutes (matches StuckRunTimeout)
	RescueTimeout time.Duration

	// MaxRescueAttempts is the maximum times a run can be rescued.
	// After this many rescue attempts, the run is marked as permanently failed.
	// Default: 3
	MaxRescueAttempts int
}

// DefaultRunRescueConfig returns the default run rescue configuration.
func DefaultRunRescueConfig() *RunRescueConfig {
	return &RunRescueConfig{
		RescueInterval:    DefaultRescueInterval,
		RescueTimeout:     DefaultRescueTimeout,
		MaxRescueAttempts: DefaultMaxRescueAttempts,
	}
}
