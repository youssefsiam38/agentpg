package mcp

import (
	"context"
	"net/http"
	"time"

	"github.com/youssefsiam38/agentpg/tool"
)

// ToolRegistrar is the minimal interface for registering tools.
// Satisfied by *agentpg.Client[TTx].
type ToolRegistrar interface {
	RegisterTool(t tool.Tool) error
}

// MCPServerConfig configures a connection to an MCP server.
type MCPServerConfig struct {
	// Name is the unique identifier for this MCP server.
	// Used as namespace prefix for tools: "{Name}__{tool_name}".
	// Must be non-empty.
	Name string

	// Transport: exactly one of Stdio or HTTP must be set.
	Stdio *StdioTransportConfig
	HTTP  *HTTPTransportConfig

	// DisableToolPrefix disables tool name prefixing.
	// When false (default), tools are registered as "{Name}__{tool_name}".
	// When true, tools use their original MCP names (risk of collision).
	DisableToolPrefix bool

	// ToolFilter optionally filters which tools to expose from this server.
	// Receives the original MCP tool name (without prefix).
	// Return true to include the tool, false to exclude it.
	// If nil, all tools are exposed.
	ToolFilter func(toolName string) bool

	// Reconnect configures automatic reconnection behavior.
	// If nil, no automatic reconnection is performed.
	Reconnect *ReconnectConfig
}

// StdioTransportConfig configures a stdio (subprocess) MCP server connection.
type StdioTransportConfig struct {
	// Command is the path to the MCP server executable.
	Command string

	// Args are the command-line arguments.
	Args []string

	// Env are additional environment variables for the subprocess.
	// These are appended to os.Environ().
	// Use this for passing API keys, tokens, etc.
	Env []string

	// Dir is the working directory for the subprocess.
	// If empty, the current working directory is used.
	Dir string
}

// HTTPTransportConfig configures a Streamable HTTP MCP server connection.
type HTTPTransportConfig struct {
	// URL is the MCP server endpoint.
	// When URLFunc is also set, URL is used only for initial tool discovery
	// at startup; all subsequent tool calls route through URLFunc.
	URL string

	// URLFunc dynamically resolves the MCP server URL per tool execution.
	// It receives the tool execution context which carries run variables
	// (tenant_id, user_id, etc.) accessible via tool.GetVariable().
	// When set, the MCPServer pools and reuses sessions per resolved URL.
	// If nil, all tool calls use the static URL.
	URLFunc func(ctx context.Context) (string, error)

	// HTTPClient allows full control over the HTTP client used for requests.
	// Users can configure TLS, timeouts, and inject auth via custom
	// RoundTripper middleware.
	// If nil, a default client is used.
	HTTPClient *http.Client

	// Headers are additional HTTP headers sent with every request.
	// Use for static auth tokens: {"Authorization": "Bearer xxx"}.
	Headers map[string]string

	// HeaderFunc is called before each request to produce dynamic headers.
	// This takes precedence over static Headers for the same key.
	// Useful for OAuth tokens that rotate.
	HeaderFunc func() (map[string]string, error)
}

// ReconnectConfig controls automatic reconnection to MCP servers.
type ReconnectConfig struct {
	// MaxRetries is the maximum number of reconnection attempts.
	// 0 means unlimited retries.
	// Default: 0 (unlimited)
	MaxRetries int

	// InitialDelay is the delay before the first reconnection attempt.
	// Default: 1 second
	InitialDelay time.Duration

	// MaxDelay caps the exponential backoff.
	// Default: 30 seconds
	MaxDelay time.Duration
}

func (c *ReconnectConfig) initialDelay() time.Duration {
	if c.InitialDelay > 0 {
		return c.InitialDelay
	}
	return time.Second
}

func (c *ReconnectConfig) maxDelay() time.Duration {
	if c.MaxDelay > 0 {
		return c.MaxDelay
	}
	return 30 * time.Second
}
