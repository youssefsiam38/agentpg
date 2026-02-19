package mcp

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/youssefsiam38/agentpg/tool"
)

var (
	// ErrNotConnected is returned when a tool call is attempted on a disconnected server.
	ErrNotConnected = errors.New("mcp server not connected")

	// ErrAlreadyConnected is returned when Connect is called on an already-connected server.
	ErrAlreadyConnected = errors.New("mcp server already connected")

	// ErrInvalidConfig is returned when the server configuration is invalid.
	ErrInvalidConfig = errors.New("invalid mcp configuration")
)

// mapMCPError converts MCP-level errors to appropriate agentpg tool error types.
func mapMCPError(serverName, toolName string, err error) error {
	if err == nil {
		return nil
	}

	// Not connected -> snooze (server may reconnect)
	if errors.Is(err, ErrNotConnected) {
		return tool.ToolSnooze(10*time.Second,
			fmt.Errorf("mcp server %q not connected for tool %q: %w", serverName, toolName, err))
	}

	errStr := strings.ToLower(err.Error())

	// Connection errors -> snooze (transient, retry after delay)
	if isConnectionError(errStr) {
		return tool.ToolSnooze(10*time.Second,
			fmt.Errorf("mcp server %q connection error for tool %q: %w", serverName, toolName, err))
	}

	// Rate limiting -> snooze
	if strings.Contains(errStr, "rate limit") || strings.Contains(errStr, "429") {
		return tool.ToolSnooze(30*time.Second,
			fmt.Errorf("mcp server %q rate limited for tool %q: %w", serverName, toolName, err))
	}

	// Authentication/authorization errors -> cancel (no retry)
	if strings.Contains(errStr, "401") || strings.Contains(errStr, "403") ||
		strings.Contains(errStr, "unauthorized") || strings.Contains(errStr, "forbidden") {
		return tool.ToolCancel(
			fmt.Errorf("mcp server %q auth error for tool %q: %w", serverName, toolName, err))
	}

	// Invalid input -> discard (no retry)
	if strings.Contains(errStr, "invalid params") || strings.Contains(errStr, "-32602") {
		return tool.ToolDiscard(
			fmt.Errorf("mcp tool %q invalid input: %w", toolName, err))
	}

	// Default: regular error (retried by toolWorker with backoff)
	return fmt.Errorf("mcp tool %q on server %q failed: %w", toolName, serverName, err)
}

func isConnectionError(errStr string) bool {
	return strings.Contains(errStr, "connection refused") ||
		strings.Contains(errStr, "connection reset") ||
		strings.Contains(errStr, "eof") ||
		strings.Contains(errStr, "timeout") ||
		strings.Contains(errStr, "no such host") ||
		strings.Contains(errStr, "broken pipe")
}
