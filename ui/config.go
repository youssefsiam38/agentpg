package ui

import (
	"time"
)

// Default configuration values.
const (
	DefaultRefreshInterval = 5 * time.Second
	DefaultPageSize        = 25
)

// Config holds UI package configuration.
type Config struct {
	// BasePath is the URL prefix where the UI is mounted.
	// For example, if mounted at "/ui/", set BasePath to "/ui".
	// All navigation links will be prefixed with this path.
	// Defaults to empty string (root mount).
	BasePath string

	// MetadataFilter filters all data to sessions matching these metadata key-value pairs.
	// Example: map[string]any{"tenant_id": "my-tenant"} to show only matching sessions.
	// If empty, shows all sessions.
	MetadataFilter map[string]any

	// MetadataDisplayKeys specifies which metadata keys to show in session lists.
	// Example: []string{"tenant_id", "user_id", "environment"}
	MetadataDisplayKeys []string

	// MetadataFilterKeys specifies which metadata keys to show filter dropdowns for.
	// Enables users to filter sessions by these metadata fields in the UI.
	MetadataFilterKeys []string

	// ReadOnly disables write operations (chat, session creation).
	// Useful for monitoring-only deployments.
	ReadOnly bool

	// Logger for structured logging.
	// If nil, logging is disabled.
	Logger Logger

	// RefreshInterval for SSE updates and auto-refresh.
	// Defaults to 5 seconds.
	RefreshInterval time.Duration

	// PageSize for pagination.
	// Defaults to 25.
	PageSize int
}

// Logger interface for structured logging.
// Compatible with agentpg.Logger.
type Logger interface {
	Debug(msg string, args ...any)
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
}

// DefaultConfig returns a new Config with default values.
func DefaultConfig() *Config {
	return &Config{
		RefreshInterval: DefaultRefreshInterval,
		PageSize:        DefaultPageSize,
	}
}

// applyDefaults fills in default values for zero-valued fields.
func (c *Config) applyDefaults() {
	if c.RefreshInterval == 0 {
		c.RefreshInterval = DefaultRefreshInterval
	}
	if c.PageSize == 0 {
		c.PageSize = DefaultPageSize
	}
}

// validate checks the configuration for errors.
func (c *Config) validate() error {
	if c.PageSize < 1 {
		return ErrInvalidConfig
	}
	if c.RefreshInterval < time.Second {
		return ErrInvalidConfig
	}
	return nil
}
