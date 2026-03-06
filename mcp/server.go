package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/youssefsiam38/agentpg/tool"
)

// MCPServer represents a connection to a single MCP server.
// It manages the connection lifecycle and exposes discovered tools
// as tool.Tool implementations.
type MCPServer struct {
	config      *MCPServerConfig
	client      *mcpsdk.Client
	session     *mcpsdk.ClientSession // Discovery/default session
	tools       []*MCPTool
	mu          sync.RWMutex
	connected   bool
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
	sessionPool sync.Map      // URL -> *mcpsdk.ClientSession (for URLFunc routing)
	httpClient  *http.Client  // Shared HTTP client (with header injection) for pooled sessions
	registrar   ToolRegistrar // For lazy tool registration (set by RegisterServerLazy)
}

// RegisterServer connects to an MCP server, discovers tools, and registers
// them with the given registrar (typically *agentpg.Client).
// Returns the MCPServer for lifecycle management. The caller must call
// Close() when the server is no longer needed (typically via defer).
//
// Must be called before client.Start().
func RegisterServer(ctx context.Context, registrar ToolRegistrar, config *MCPServerConfig) (*MCPServer, error) {
	server, err := NewMCPServer(config)
	if err != nil {
		return nil, err
	}

	if err := server.Connect(ctx); err != nil {
		return nil, err
	}

	for _, t := range server.Tools() {
		if err := registrar.RegisterTool(t); err != nil {
			_ = server.Close()
			return nil, fmt.Errorf("failed to register MCP tool %q: %w", t.Name(), err)
		}
	}

	return server, nil
}

// RegisterServerLazy creates an MCPServer without connecting.
// Tools are discovered and registered lazily on the first tool call
// (or via an explicit EnsureConnected call).
// This is useful for multi-tenant setups where the MCP URL is only
// known at request time via URLFunc.
func RegisterServerLazy(registrar ToolRegistrar, config *MCPServerConfig) (*MCPServer, error) {
	server, err := NewMCPServer(config)
	if err != nil {
		return nil, err
	}
	server.registrar = registrar
	return server, nil
}

// EnsureConnected connects to the MCP server if not already connected.
// Idempotent and thread-safe — safe to call from multiple goroutines.
// When URLFunc is set and URL is empty, the first call resolves the
// discovery URL from URLFunc(ctx).
func (s *MCPServer) EnsureConnected(ctx context.Context) error {
	s.mu.RLock()
	if s.connected {
		s.mu.RUnlock()
		return nil
	}
	s.mu.RUnlock()

	s.mu.Lock()
	defer s.mu.Unlock()

	// Double-check after lock upgrade.
	if s.connected {
		return nil
	}

	// Resolve discovery URL from URLFunc if no static URL is set.
	if s.config.HTTP != nil && s.config.HTTP.URL == "" && s.config.HTTP.URLFunc != nil {
		url, err := s.config.HTTP.URLFunc(ctx)
		if err != nil {
			return fmt.Errorf("URLFunc failed for discovery: %w", err)
		}
		s.config.HTTP.URL = url
	}

	s.ctx, s.cancel = context.WithCancel(ctx)

	s.client = mcpsdk.NewClient(
		&mcpsdk.Implementation{
			Name:    "agentpg",
			Version: "1.0.0",
		},
		nil,
	)

	transport, err := s.buildTransport()
	if err != nil {
		return fmt.Errorf("failed to build transport for %q: %w", s.config.Name, err)
	}

	session, err := s.client.Connect(s.ctx, transport, nil)
	if err != nil {
		return fmt.Errorf("failed to connect to MCP server %q: %w", s.config.Name, err)
	}
	s.session = session

	if err := s.discoverTools(s.ctx); err != nil {
		_ = session.Close()
		s.session = nil
		return fmt.Errorf("failed to discover tools from %q: %w", s.config.Name, err)
	}

	s.connected = true

	// Register discovered tools with the registrar (safe after client.Start()).
	if s.registrar != nil {
		for _, t := range s.tools {
			if err := s.registrar.RegisterTool(t); err != nil {
				// Non-fatal: tool may already be registered from a previous connection.
				continue
			}
		}
	}

	if s.config.Reconnect != nil {
		s.wg.Add(1)
		go s.reconnectLoop()
	}

	return nil
}

// NewMCPServer creates a new MCP server connection manager.
// Does not connect yet — call Connect() to establish the connection,
// or use RegisterServer() for convenience.
func NewMCPServer(config *MCPServerConfig) (*MCPServer, error) {
	if config == nil {
		return nil, fmt.Errorf("%w: config is nil", ErrInvalidConfig)
	}
	if config.Name == "" {
		return nil, fmt.Errorf("%w: server name is required", ErrInvalidConfig)
	}
	if config.Stdio == nil && config.HTTP == nil {
		return nil, fmt.Errorf("%w: one of Stdio or HTTP transport must be configured", ErrInvalidConfig)
	}
	if config.Stdio != nil && config.HTTP != nil {
		return nil, fmt.Errorf("%w: only one of Stdio or HTTP transport can be configured", ErrInvalidConfig)
	}
	if config.Stdio != nil && config.Stdio.Command == "" {
		return nil, fmt.Errorf("%w: stdio command is required", ErrInvalidConfig)
	}
	if config.HTTP != nil && config.HTTP.URL == "" && config.HTTP.URLFunc == nil {
		return nil, fmt.Errorf("%w: HTTP URL or URLFunc is required", ErrInvalidConfig)
	}

	return &MCPServer{
		config: config,
	}, nil
}

// Connect establishes a connection to the MCP server and discovers tools.
func (s *MCPServer) Connect(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.connected {
		return ErrAlreadyConnected
	}

	s.ctx, s.cancel = context.WithCancel(ctx)

	s.client = mcpsdk.NewClient(
		&mcpsdk.Implementation{
			Name:    "agentpg",
			Version: "1.0.0",
		},
		nil,
	)

	transport, err := s.buildTransport()
	if err != nil {
		return fmt.Errorf("failed to build transport for %q: %w", s.config.Name, err)
	}

	session, err := s.client.Connect(s.ctx, transport, nil)
	if err != nil {
		return fmt.Errorf("failed to connect to MCP server %q: %w", s.config.Name, err)
	}
	s.session = session

	if err := s.discoverTools(s.ctx); err != nil {
		_ = session.Close()
		s.session = nil
		return fmt.Errorf("failed to discover tools from %q: %w", s.config.Name, err)
	}

	s.connected = true

	if s.config.Reconnect != nil {
		s.wg.Add(1)
		go s.reconnectLoop()
	}

	return nil
}

// Tools returns all discovered tools as tool.Tool implementations.
func (s *MCPServer) Tools() []tool.Tool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tools := make([]tool.Tool, len(s.tools))
	for i, t := range s.tools {
		tools[i] = t
	}
	return tools
}

// Close shuts down the MCP server connection and all pooled sessions.
func (s *MCPServer) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.connected {
		return nil
	}

	s.cancel()
	s.wg.Wait()

	if s.session != nil {
		_ = s.session.Close()
		s.session = nil
	}

	// Close all pooled sessions.
	s.sessionPool.Range(func(key, value any) bool {
		if session, ok := value.(*mcpsdk.ClientSession); ok {
			_ = session.Close()
		}
		s.sessionPool.Delete(key)
		return true
	})

	s.connected = false
	return nil
}

// Name returns the server name from the configuration.
func (s *MCPServer) Name() string {
	return s.config.Name
}

func (s *MCPServer) buildTransport() (mcpsdk.Transport, error) {
	if s.config.Stdio != nil {
		return s.buildStdioTransport()
	}
	return s.buildHTTPTransport()
}

func (s *MCPServer) buildStdioTransport() (mcpsdk.Transport, error) {
	cfg := s.config.Stdio
	cmd := exec.CommandContext(s.ctx, cfg.Command, cfg.Args...) //nolint:gosec // Command is user-configured, not tainted input
	if cfg.Dir != "" {
		cmd.Dir = cfg.Dir
	}
	if len(cfg.Env) > 0 {
		cmd.Env = append(os.Environ(), cfg.Env...)
	}
	return &mcpsdk.CommandTransport{Command: cmd}, nil
}

// buildHTTPClient constructs the shared HTTP client with header injection.
// This client is reused for both the discovery session and pooled sessions.
func (s *MCPServer) buildHTTPClient() *http.Client {
	cfg := s.config.HTTP

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{}
	}

	// Wrap transport to inject headers if static headers or HeaderFunc provided.
	if len(cfg.Headers) > 0 || cfg.HeaderFunc != nil {
		base := httpClient.Transport
		if base == nil {
			base = http.DefaultTransport
		}
		httpClient = &http.Client{
			Transport: &headerRoundTripper{
				base:       base,
				headers:    cfg.Headers,
				headerFunc: cfg.HeaderFunc,
			},
			Timeout:       httpClient.Timeout,
			CheckRedirect: httpClient.CheckRedirect,
			Jar:           httpClient.Jar,
		}
	}

	return httpClient
}

func (s *MCPServer) buildHTTPTransport() (mcpsdk.Transport, error) {
	httpClient := s.buildHTTPClient()
	s.httpClient = httpClient

	return &mcpsdk.StreamableClientTransport{
		Endpoint:   s.config.HTTP.URL,
		HTTPClient: httpClient,
	}, nil
}

// buildHTTPTransportForURL creates a transport for a specific URL,
// reusing the shared HTTP client. Used by the session pool for URLFunc routing.
func (s *MCPServer) buildHTTPTransportForURL(url string) mcpsdk.Transport {
	return &mcpsdk.StreamableClientTransport{
		Endpoint:   url,
		HTTPClient: s.httpClient,
	}
}

// discoverTools fetches tools from the MCP server and creates MCPTool bridges.
func (s *MCPServer) discoverTools(ctx context.Context) error {
	s.tools = nil

	for mcpTool, err := range s.session.Tools(ctx, nil) {
		if err != nil {
			return fmt.Errorf("failed to list tools: %w", err)
		}

		// Apply filter
		if s.config.ToolFilter != nil && !s.config.ToolFilter(mcpTool.Name) {
			continue
		}

		namespacedName := toolName(s.config.Name, mcpTool.Name, s.config.DisableToolPrefix)
		schema := convertMCPInputSchema(mcpTool.InputSchema)

		bridgeTool := &MCPTool{
			server:         s,
			mcpName:        mcpTool.Name,
			namespacedName: namespacedName,
			description:    mcpTool.Description,
			schema:         schema,
		}

		s.tools = append(s.tools, bridgeTool)
	}

	return nil
}

// getOrCreateSession returns a pooled session for the given URL, creating one if needed.
func (s *MCPServer) getOrCreateSession(ctx context.Context, url string) (*mcpsdk.ClientSession, error) {
	// Fast path: session already exists.
	if val, ok := s.sessionPool.Load(url); ok {
		return val.(*mcpsdk.ClientSession), nil
	}

	// Slow path: create new session.
	transport := s.buildHTTPTransportForURL(url)
	session, err := s.client.Connect(ctx, transport, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to MCP server at %q: %w", url, err)
	}

	// Store or return existing if another goroutine raced us.
	actual, loaded := s.sessionPool.LoadOrStore(url, session)
	if loaded {
		// Another goroutine created the session first; close ours.
		_ = session.Close()
		return actual.(*mcpsdk.ClientSession), nil
	}

	return session, nil
}

// callTool delegates a tool call to the MCP server.
// If the server was created via RegisterServerLazy, auto-connects on first call.
func (s *MCPServer) callTool(ctx context.Context, mcpToolName string, input json.RawMessage) (string, error) {
	// Auto-connect if lazy-registered and not yet connected.
	if err := s.EnsureConnected(ctx); err != nil {
		return "", mapMCPError(s.config.Name, mcpToolName, err)
	}

	s.mu.RLock()
	connected := s.connected
	s.mu.RUnlock()

	if !connected {
		return "", mapMCPError(s.config.Name, mcpToolName, ErrNotConnected)
	}

	var arguments map[string]any
	if len(input) > 0 {
		if err := json.Unmarshal(input, &arguments); err != nil {
			return "", mapMCPError(s.config.Name, mcpToolName,
				fmt.Errorf("invalid tool input: %w", err))
		}
	}

	// Resolve session: use URLFunc for dynamic routing, else default session.
	var session *mcpsdk.ClientSession
	if s.config.HTTP != nil && s.config.HTTP.URLFunc != nil {
		url, err := s.config.HTTP.URLFunc(ctx)
		if err != nil {
			return "", mapMCPError(s.config.Name, mcpToolName,
				fmt.Errorf("URLFunc failed: %w", err))
		}
		session, err = s.getOrCreateSession(ctx, url)
		if err != nil {
			return "", mapMCPError(s.config.Name, mcpToolName, err)
		}
	} else {
		s.mu.RLock()
		session = s.session
		s.mu.RUnlock()
		if session == nil {
			return "", mapMCPError(s.config.Name, mcpToolName, ErrNotConnected)
		}
	}

	result, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      mcpToolName,
		Arguments: arguments,
	})
	if err != nil {
		return "", mapMCPError(s.config.Name, mcpToolName, err)
	}

	// MCP tools report errors via IsError + Content, not protocol errors.
	// Return the error text as a tool result string so Claude can see it.
	if result.IsError {
		content := extractTextContent(result.Content)
		if content == "" {
			content = "MCP tool returned an error with no message"
		}
		return content, nil
	}

	return extractTextContent(result.Content), nil
}

// reconnectLoop monitors the connection and reconnects on failure.
func (s *MCPServer) reconnectLoop() {
	defer s.wg.Done()

	cfg := s.config.Reconnect
	delay := cfg.initialDelay()
	retries := 0

	for {
		// Wait for the session to close or context to be cancelled.
		s.mu.RLock()
		session := s.session
		s.mu.RUnlock()

		if session == nil {
			return
		}

		// session.Wait() blocks until the session is closed.
		err := session.Wait()
		if err == nil {
			// Clean shutdown.
			return
		}

		// Check if we should stop.
		select {
		case <-s.ctx.Done():
			return
		default:
		}

		// Check max retries.
		if cfg.MaxRetries > 0 && retries >= cfg.MaxRetries {
			return
		}

		// Wait before reconnecting.
		select {
		case <-time.After(delay):
		case <-s.ctx.Done():
			return
		}

		// Attempt reconnect.
		if err := s.reconnect(); err != nil {
			retries++
			// Exponential backoff.
			delay *= 2
			delay = min(delay, cfg.maxDelay())
			continue
		}

		// Successful reconnect — reset backoff.
		delay = cfg.initialDelay()
		retries = 0
	}
}

func (s *MCPServer) reconnect() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Close old session.
	if s.session != nil {
		_ = s.session.Close()
		s.session = nil
	}

	transport, err := s.buildTransport()
	if err != nil {
		return err
	}

	session, err := s.client.Connect(s.ctx, transport, nil)
	if err != nil {
		return err
	}
	s.session = session
	s.connected = true

	// Re-discover tools (they may have changed).
	if err := s.discoverTools(s.ctx); err != nil {
		_ = session.Close()
		s.session = nil
		s.connected = false
		return err
	}

	return nil
}

// headerRoundTripper injects headers into HTTP requests.
type headerRoundTripper struct {
	base       http.RoundTripper
	headers    map[string]string
	headerFunc func() (map[string]string, error)
}

func (rt *headerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	r := req.Clone(req.Context())

	for k, v := range rt.headers {
		r.Header.Set(k, v)
	}

	if rt.headerFunc != nil {
		dynamic, err := rt.headerFunc()
		if err != nil {
			return nil, fmt.Errorf("failed to get dynamic headers: %w", err)
		}
		for k, v := range dynamic {
			r.Header.Set(k, v)
		}
	}

	return rt.base.RoundTrip(r)
}
