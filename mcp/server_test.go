package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/youssefsiam38/agentpg/tool"
)

// mockRegistrar captures tool registrations for testing.
type mockRegistrar struct {
	tools map[string]tool.Tool
}

func newMockRegistrar() *mockRegistrar {
	return &mockRegistrar{tools: make(map[string]tool.Tool)}
}

func (r *mockRegistrar) RegisterTool(t tool.Tool) error {
	if _, exists := r.tools[t.Name()]; exists {
		return errors.New("tool already registered: " + t.Name())
	}
	r.tools[t.Name()] = t
	return nil
}

// startTestServer creates an in-memory MCP server with the given tools
// and connects a client to it. Returns the client session and a cleanup func.
func startTestServer(t *testing.T, tools map[string]mcpsdk.ToolHandler) *mcpsdk.ClientSession {
	t.Helper()

	server := mcpsdk.NewServer(
		&mcpsdk.Implementation{Name: "test-server", Version: "1.0.0"},
		nil,
	)

	for name, handler := range tools {
		server.AddTool(&mcpsdk.Tool{
			Name:        name,
			Description: "Test tool: " + name,
			InputSchema: json.RawMessage(`{"type":"object","properties":{"input":{"type":"string","description":"test input"}},"required":["input"]}`),
		}, handler)
	}

	serverTransport, clientTransport := mcpsdk.NewInMemoryTransports()

	ctx := context.Background()
	go server.Run(ctx, serverTransport)

	client := mcpsdk.NewClient(
		&mcpsdk.Implementation{Name: "test-client", Version: "1.0.0"},
		nil,
	)

	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	t.Cleanup(func() { session.Close() })

	return session
}

func TestToolDiscovery(t *testing.T) {
	session := startTestServer(t, map[string]mcpsdk.ToolHandler{
		"greet": func(_ context.Context, req *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
			return &mcpsdk.CallToolResult{
				Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: "hello"}},
			}, nil
		},
		"farewell": func(_ context.Context, req *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
			return &mcpsdk.CallToolResult{
				Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: "goodbye"}},
			}, nil
		},
	})

	s := &MCPServer{
		config: &MCPServerConfig{
			Name: "test",
		},
		session: session,
	}

	if err := s.discoverTools(context.Background()); err != nil {
		t.Fatalf("discoverTools failed: %v", err)
	}

	if len(s.tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(s.tools))
	}

	toolNames := make(map[string]bool)
	for _, tool := range s.tools {
		toolNames[tool.Name()] = true
	}

	if !toolNames["test__greet"] {
		t.Error("expected tool test__greet")
	}
	if !toolNames["test__farewell"] {
		t.Error("expected tool test__farewell")
	}
}

func TestToolDiscoveryWithFilter(t *testing.T) {
	session := startTestServer(t, map[string]mcpsdk.ToolHandler{
		"greet": func(_ context.Context, req *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
			return &mcpsdk.CallToolResult{
				Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: "hello"}},
			}, nil
		},
		"secret": func(_ context.Context, req *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
			return &mcpsdk.CallToolResult{
				Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: "secret"}},
			}, nil
		},
	})

	s := &MCPServer{
		config: &MCPServerConfig{
			Name: "test",
			ToolFilter: func(name string) bool {
				return name == "greet"
			},
		},
		session: session,
	}

	if err := s.discoverTools(context.Background()); err != nil {
		t.Fatalf("discoverTools failed: %v", err)
	}

	if len(s.tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(s.tools))
	}

	if s.tools[0].Name() != "test__greet" {
		t.Errorf("expected test__greet, got %s", s.tools[0].Name())
	}
}

func TestToolDiscoveryDisablePrefix(t *testing.T) {
	session := startTestServer(t, map[string]mcpsdk.ToolHandler{
		"greet": func(_ context.Context, req *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
			return &mcpsdk.CallToolResult{
				Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: "hello"}},
			}, nil
		},
	})

	s := &MCPServer{
		config: &MCPServerConfig{
			Name:              "test",
			DisableToolPrefix: true,
		},
		session: session,
	}

	if err := s.discoverTools(context.Background()); err != nil {
		t.Fatalf("discoverTools failed: %v", err)
	}

	if len(s.tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(s.tools))
	}

	if s.tools[0].Name() != "greet" {
		t.Errorf("expected greet (no prefix), got %s", s.tools[0].Name())
	}
}

func TestToolExecution(t *testing.T) {
	session := startTestServer(t, map[string]mcpsdk.ToolHandler{
		"echo": func(_ context.Context, req *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
			var args struct {
				Input string `json:"input"`
			}
			json.Unmarshal(req.Params.Arguments, &args)
			return &mcpsdk.CallToolResult{
				Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: "echoed: " + args.Input}},
			}, nil
		},
	})

	s := &MCPServer{
		config:    &MCPServerConfig{Name: "test"},
		session:   session,
		connected: true,
	}

	result, err := s.callTool(context.Background(), "echo", json.RawMessage(`{"input":"hello"}`))
	if err != nil {
		t.Fatalf("callTool failed: %v", err)
	}

	if result != "echoed: hello" {
		t.Errorf("expected 'echoed: hello', got %q", result)
	}
}

func TestToolExecutionError(t *testing.T) {
	session := startTestServer(t, map[string]mcpsdk.ToolHandler{
		"fail": func(_ context.Context, req *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
			return &mcpsdk.CallToolResult{
				Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: "something went wrong"}},
				IsError: true,
			}, nil
		},
	})

	s := &MCPServer{
		config:    &MCPServerConfig{Name: "test"},
		session:   session,
		connected: true,
	}

	// MCP tool errors (IsError=true) are returned as text, not Go errors.
	// This lets Claude see the error and self-correct.
	result, err := s.callTool(context.Background(), "fail", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("expected no Go error for MCP tool error, got: %v", err)
	}

	if result != "something went wrong" {
		t.Errorf("expected 'something went wrong', got %q", result)
	}
}

func TestToolExecutionNotConnected(t *testing.T) {
	s := &MCPServer{
		config:    &MCPServerConfig{Name: "test"},
		connected: false,
	}

	_, err := s.callTool(context.Background(), "echo", json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected error when not connected")
	}

	// Should be mapped to a snooze error (connection issue = transient)
	if !tool.IsToolSnooze(err) {
		t.Errorf("expected ToolSnooze error, got: %v", err)
	}
}

func TestConvertMCPInputSchema(t *testing.T) {
	t.Run("nil schema", func(t *testing.T) {
		ts := convertMCPInputSchema(nil)
		if ts.Type != "object" {
			t.Errorf("expected type 'object', got %q", ts.Type)
		}
	})

	t.Run("simple properties", func(t *testing.T) {
		schema := map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{
					"type":        "string",
					"description": "User name",
				},
				"age": map[string]any{
					"type":    "integer",
					"minimum": float64(0),
					"maximum": float64(150),
				},
			},
			"required": []any{"name"},
		}

		ts := convertMCPInputSchema(schema)

		if ts.Type != "object" {
			t.Errorf("expected type 'object', got %q", ts.Type)
		}
		if len(ts.Properties) != 2 {
			t.Fatalf("expected 2 properties, got %d", len(ts.Properties))
		}

		nameProp := ts.Properties["name"]
		if nameProp.Type != "string" {
			t.Errorf("name type: expected 'string', got %q", nameProp.Type)
		}
		if nameProp.Description != "User name" {
			t.Errorf("name description: expected 'User name', got %q", nameProp.Description)
		}

		ageProp := ts.Properties["age"]
		if ageProp.Type != "integer" {
			t.Errorf("age type: expected 'integer', got %q", ageProp.Type)
		}
		if ageProp.Minimum == nil || *ageProp.Minimum != 0 {
			t.Errorf("age minimum: expected 0")
		}
		if ageProp.Maximum == nil || *ageProp.Maximum != 150 {
			t.Errorf("age maximum: expected 150")
		}

		if len(ts.Required) != 1 || ts.Required[0] != "name" {
			t.Errorf("expected required=[name], got %v", ts.Required)
		}
	})

	t.Run("nested object", func(t *testing.T) {
		schema := map[string]any{
			"type": "object",
			"properties": map[string]any{
				"address": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"city": map[string]any{
							"type": "string",
						},
					},
					"required": []any{"city"},
				},
			},
		}

		ts := convertMCPInputSchema(schema)
		addrProp := ts.Properties["address"]
		if addrProp.Type != "object" {
			t.Errorf("expected type 'object', got %q", addrProp.Type)
		}
		if len(addrProp.Properties) != 1 {
			t.Fatalf("expected 1 nested property, got %d", len(addrProp.Properties))
		}
		if addrProp.Properties["city"].Type != "string" {
			t.Errorf("expected city type 'string', got %q", addrProp.Properties["city"].Type)
		}
		if len(addrProp.Required) != 1 || addrProp.Required[0] != "city" {
			t.Errorf("expected required=[city], got %v", addrProp.Required)
		}
	})

	t.Run("array items", func(t *testing.T) {
		schema := map[string]any{
			"type": "object",
			"properties": map[string]any{
				"tags": map[string]any{
					"type": "array",
					"items": map[string]any{
						"type": "string",
					},
					"minItems": float64(1),
					"maxItems": float64(10),
				},
			},
		}

		ts := convertMCPInputSchema(schema)
		tagsProp := ts.Properties["tags"]
		if tagsProp.Type != "array" {
			t.Errorf("expected type 'array', got %q", tagsProp.Type)
		}
		if tagsProp.Items == nil {
			t.Fatal("expected items to be set")
		}
		if tagsProp.Items.Type != "string" {
			t.Errorf("expected items type 'string', got %q", tagsProp.Items.Type)
		}
		if tagsProp.MinItems == nil || *tagsProp.MinItems != 1 {
			t.Errorf("expected minItems=1")
		}
		if tagsProp.MaxItems == nil || *tagsProp.MaxItems != 10 {
			t.Errorf("expected maxItems=10")
		}
	})

	t.Run("enum values", func(t *testing.T) {
		schema := map[string]any{
			"type": "object",
			"properties": map[string]any{
				"color": map[string]any{
					"type": "string",
					"enum": []any{"red", "green", "blue"},
				},
			},
		}

		ts := convertMCPInputSchema(schema)
		colorProp := ts.Properties["color"]
		if len(colorProp.Enum) != 3 {
			t.Fatalf("expected 3 enum values, got %d", len(colorProp.Enum))
		}
		if colorProp.Enum[0] != "red" || colorProp.Enum[1] != "green" || colorProp.Enum[2] != "blue" {
			t.Errorf("unexpected enum values: %v", colorProp.Enum)
		}
	})

	t.Run("string constraints", func(t *testing.T) {
		schema := map[string]any{
			"type": "object",
			"properties": map[string]any{
				"email": map[string]any{
					"type":      "string",
					"minLength": float64(5),
					"maxLength": float64(100),
					"pattern":   "^[a-z]+@[a-z]+\\.[a-z]+$",
				},
			},
		}

		ts := convertMCPInputSchema(schema)
		emailProp := ts.Properties["email"]
		if emailProp.MinLength == nil || *emailProp.MinLength != 5 {
			t.Errorf("expected minLength=5")
		}
		if emailProp.MaxLength == nil || *emailProp.MaxLength != 100 {
			t.Errorf("expected maxLength=100")
		}
		if emailProp.Pattern != "^[a-z]+@[a-z]+\\.[a-z]+$" {
			t.Errorf("unexpected pattern: %q", emailProp.Pattern)
		}
	})
}

func TestToolName(t *testing.T) {
	tests := []struct {
		server  string
		tool    string
		disable bool
		want    string
	}{
		{"github", "search_code", false, "github__search_code"},
		{"slack", "send_message", false, "slack__send_message"},
		{"github", "search_code", true, "search_code"},
	}

	for _, tt := range tests {
		got := toolName(tt.server, tt.tool, tt.disable)
		if got != tt.want {
			t.Errorf("toolName(%q, %q, %v) = %q, want %q", tt.server, tt.tool, tt.disable, got, tt.want)
		}
	}
}

func TestRegisterServer(t *testing.T) {
	// Create an in-memory MCP server with a tool.
	server := mcpsdk.NewServer(
		&mcpsdk.Implementation{Name: "test-server", Version: "1.0.0"},
		nil,
	)
	server.AddTool(&mcpsdk.Tool{
		Name:        "ping",
		Description: "Ping pong",
		InputSchema: json.RawMessage(`{"type":"object"}`),
	}, func(_ context.Context, req *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		return &mcpsdk.CallToolResult{
			Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: "pong"}},
		}, nil
	})

	serverTransport, clientTransport := mcpsdk.NewInMemoryTransports()
	go server.Run(context.Background(), serverTransport)

	// Create MCPServer manually with the in-memory transport.
	registrar := newMockRegistrar()

	s := &MCPServer{
		config: &MCPServerConfig{Name: "myserver"},
	}
	s.client = mcpsdk.NewClient(
		&mcpsdk.Implementation{Name: "test-client", Version: "1.0.0"},
		nil,
	)

	ctx := context.Background()
	s.ctx, s.cancel = context.WithCancel(ctx)

	session, err := s.client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("connect failed: %v", err)
	}
	s.session = session
	t.Cleanup(func() { session.Close() })

	if err := s.discoverTools(ctx); err != nil {
		t.Fatalf("discoverTools failed: %v", err)
	}
	s.connected = true

	// Register tools with mock registrar.
	for _, tl := range s.Tools() {
		if err := registrar.RegisterTool(tl); err != nil {
			t.Fatalf("RegisterTool failed: %v", err)
		}
	}

	// Verify tool was registered.
	if len(registrar.tools) != 1 {
		t.Fatalf("expected 1 registered tool, got %d", len(registrar.tools))
	}

	registered, ok := registrar.tools["myserver__ping"]
	if !ok {
		t.Fatal("expected tool myserver__ping to be registered")
	}

	if registered.Description() != "Ping pong" {
		t.Errorf("expected description 'Ping pong', got %q", registered.Description())
	}

	// Execute the registered tool.
	result, err := registered.Execute(ctx, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if result != "pong" {
		t.Errorf("expected 'pong', got %q", result)
	}
}

func TestNewMCPServerValidation(t *testing.T) {
	tests := []struct {
		name    string
		config  *MCPServerConfig
		wantErr bool
	}{
		{"nil config", nil, true},
		{"empty name", &MCPServerConfig{}, true},
		{"no transport", &MCPServerConfig{Name: "test"}, true},
		{"both transports", &MCPServerConfig{
			Name:  "test",
			Stdio: &StdioTransportConfig{Command: "cmd"},
			HTTP:  &HTTPTransportConfig{URL: "http://localhost"},
		}, true},
		{"stdio no command", &MCPServerConfig{
			Name:  "test",
			Stdio: &StdioTransportConfig{},
		}, true},
		{"http no url", &MCPServerConfig{
			Name: "test",
			HTTP: &HTTPTransportConfig{},
		}, true},
		{"valid stdio", &MCPServerConfig{
			Name:  "test",
			Stdio: &StdioTransportConfig{Command: "cmd"},
		}, false},
		{"valid http", &MCPServerConfig{
			Name: "test",
			HTTP: &HTTPTransportConfig{URL: "http://localhost:3000"},
		}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewMCPServer(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewMCPServer() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestMapMCPError(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		isSnooze  bool
		isCancel  bool
		isDiscard bool
	}{
		{"connection refused", errors.New("connection refused"), true, false, false},
		{"timeout", errors.New("request timeout"), true, false, false},
		{"eof", errors.New("unexpected EOF"), true, false, false},
		{"rate limit", errors.New("429 rate limit exceeded"), true, false, false},
		{"unauthorized", errors.New("401 unauthorized"), false, true, false},
		{"forbidden", errors.New("403 forbidden"), false, true, false},
		{"invalid params", errors.New("invalid params: missing field"), false, false, true},
		{"jsonrpc invalid params", errors.New("-32602 invalid params"), false, false, true},
		{"generic error", errors.New("something else failed"), false, false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mapped := mapMCPError("server", "tool", tt.err)
			if tool.IsToolSnooze(mapped) != tt.isSnooze {
				t.Errorf("IsToolSnooze = %v, want %v", tool.IsToolSnooze(mapped), tt.isSnooze)
			}
			if tool.IsToolCancel(mapped) != tt.isCancel {
				t.Errorf("IsToolCancel = %v, want %v", tool.IsToolCancel(mapped), tt.isCancel)
			}
			if tool.IsToolDiscard(mapped) != tt.isDiscard {
				t.Errorf("IsToolDiscard = %v, want %v", tool.IsToolDiscard(mapped), tt.isDiscard)
			}
		})
	}
}

func TestExtractTextContent(t *testing.T) {
	t.Run("single text", func(t *testing.T) {
		result := extractTextContent([]mcpsdk.Content{
			&mcpsdk.TextContent{Text: "hello"},
		})
		if result != "hello" {
			t.Errorf("expected 'hello', got %q", result)
		}
	})

	t.Run("multiple texts", func(t *testing.T) {
		result := extractTextContent([]mcpsdk.Content{
			&mcpsdk.TextContent{Text: "line 1"},
			&mcpsdk.TextContent{Text: "line 2"},
		})
		if result != "line 1\nline 2" {
			t.Errorf("expected 'line 1\\nline 2', got %q", result)
		}
	})

	t.Run("empty", func(t *testing.T) {
		result := extractTextContent(nil)
		if result != "" {
			t.Errorf("expected empty string, got %q", result)
		}
	})

	t.Run("non-text content ignored", func(t *testing.T) {
		result := extractTextContent([]mcpsdk.Content{
			&mcpsdk.ImageContent{Data: []byte("image"), MIMEType: "image/png"},
			&mcpsdk.TextContent{Text: "only text"},
		})
		if result != "only text" {
			t.Errorf("expected 'only text', got %q", result)
		}
	})
}

func TestHeaderRoundTripper(t *testing.T) {
	t.Run("static headers", func(t *testing.T) {
		var capturedReq *http.Request
		base := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			capturedReq = req
			return &http.Response{StatusCode: 200}, nil
		})

		rt := &headerRoundTripper{
			base:    base,
			headers: map[string]string{"Authorization": "Bearer token123"},
		}

		req, _ := http.NewRequest("GET", "http://example.com", http.NoBody)
		rt.RoundTrip(req)

		if capturedReq.Header.Get("Authorization") != "Bearer token123" {
			t.Errorf("expected Authorization header, got %q", capturedReq.Header.Get("Authorization"))
		}
	})

	t.Run("dynamic headers override static", func(t *testing.T) {
		var capturedReq *http.Request
		base := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			capturedReq = req
			return &http.Response{StatusCode: 200}, nil
		})

		rt := &headerRoundTripper{
			base:    base,
			headers: map[string]string{"Authorization": "Bearer static"},
			headerFunc: func() (map[string]string, error) {
				return map[string]string{"Authorization": "Bearer dynamic"}, nil
			},
		}

		req, _ := http.NewRequest("GET", "http://example.com", http.NoBody)
		rt.RoundTrip(req)

		if capturedReq.Header.Get("Authorization") != "Bearer dynamic" {
			t.Errorf("expected dynamic header to override, got %q", capturedReq.Header.Get("Authorization"))
		}
	})

	t.Run("headerFunc error", func(t *testing.T) {
		base := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200}, nil
		})

		rt := &headerRoundTripper{
			base: base,
			headerFunc: func() (map[string]string, error) {
				return nil, errors.New("token refresh failed")
			},
		}

		req, _ := http.NewRequest("GET", "http://example.com", http.NoBody)
		_, err := rt.RoundTrip(req)
		if err == nil {
			t.Fatal("expected error from headerFunc")
		}
	})

	t.Run("does not mutate original request", func(t *testing.T) {
		base := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200}, nil
		})

		rt := &headerRoundTripper{
			base:    base,
			headers: map[string]string{"X-Custom": "value"},
		}

		req, _ := http.NewRequest("GET", "http://example.com", http.NoBody)
		rt.RoundTrip(req)

		if req.Header.Get("X-Custom") != "" {
			t.Error("original request was mutated")
		}
	})
}

// roundTripFunc adapts a function to http.RoundTripper for testing.
type roundTripFunc func(req *http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestMCPToolInterface(t *testing.T) {
	// Verify MCPTool satisfies tool.Tool interface at compile time.
	var _ tool.Tool = (*MCPTool)(nil)

	mt := &MCPTool{
		mcpName:        "original",
		namespacedName: "server__original",
		description:    "A test tool",
		schema: tool.ToolSchema{
			Type: "object",
			Properties: map[string]tool.PropertyDef{
				"input": {Type: "string", Description: "Test input"},
			},
			Required: []string{"input"},
		},
	}

	if mt.Name() != "server__original" {
		t.Errorf("Name() = %q, want %q", mt.Name(), "server__original")
	}
	if mt.Description() != "A test tool" {
		t.Errorf("Description() = %q, want %q", mt.Description(), "A test tool")
	}

	schema := mt.InputSchema()
	if schema.Type != "object" {
		t.Errorf("InputSchema().Type = %q, want %q", schema.Type, "object")
	}
	if len(schema.Properties) != 1 {
		t.Errorf("expected 1 property, got %d", len(schema.Properties))
	}
	if len(schema.Required) != 1 || schema.Required[0] != "input" {
		t.Errorf("expected required=[input], got %v", schema.Required)
	}
}
