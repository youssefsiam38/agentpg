package agentpg

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/youssefsiam38/agentpg/tool"
)

func TestRegister(t *testing.T) {
	// Clean up registry before and after test
	ClearRegistry()
	defer ClearRegistry()

	def := &AgentDefinition{
		Name:         "test-agent",
		Model:        "claude-sonnet-4-5-20250929",
		SystemPrompt: "You are a test agent",
	}

	// Register should succeed
	if err := Register(def); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	// Should be able to retrieve it
	retrieved, ok := GetRegisteredAgent("test-agent")
	if !ok {
		t.Fatal("GetRegisteredAgent() returned false")
	}

	if retrieved.Name != def.Name {
		t.Errorf("Name = %v, want %v", retrieved.Name, def.Name)
	}
	if retrieved.Model != def.Model {
		t.Errorf("Model = %v, want %v", retrieved.Model, def.Model)
	}
}

func TestRegister_Duplicate(t *testing.T) {
	ClearRegistry()
	defer ClearRegistry()

	def := &AgentDefinition{
		Name:         "test-agent",
		Model:        "claude-sonnet-4-5-20250929",
		SystemPrompt: "You are a test agent",
	}

	// First registration should succeed
	if err := Register(def); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	// Second registration should fail
	if err := Register(def); err == nil {
		t.Error("Expected error for duplicate registration")
	}
}

func TestRegister_Invalid(t *testing.T) {
	ClearRegistry()
	defer ClearRegistry()

	tests := []struct {
		name string
		def  *AgentDefinition
	}{
		{"nil definition", nil},
		{"empty name", &AgentDefinition{Model: "x", SystemPrompt: "x"}},
		{"empty model", &AgentDefinition{Name: "x", SystemPrompt: "x"}},
		{"empty system prompt", &AgentDefinition{Name: "x", Model: "x"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := Register(tt.def); err == nil {
				t.Error("Expected error for invalid definition")
			}
		})
	}
}

func TestMustRegister_Panic(t *testing.T) {
	ClearRegistry()
	defer ClearRegistry()

	defer func() {
		if r := recover(); r == nil {
			t.Error("MustRegister should panic on invalid definition")
		}
	}()

	MustRegister(nil)
}

func TestListRegisteredAgents(t *testing.T) {
	ClearRegistry()
	defer ClearRegistry()

	// Register some agents
	MustRegister(&AgentDefinition{Name: "agent-a", Model: "x", SystemPrompt: "x"})
	MustRegister(&AgentDefinition{Name: "agent-b", Model: "x", SystemPrompt: "x"})

	names := ListRegisteredAgents()
	if len(names) != 2 {
		t.Errorf("ListRegisteredAgents() = %d agents, want 2", len(names))
	}
}

// mockTool for testing
type mockTool struct {
	name string
}

func (m *mockTool) Name() string        { return m.name }
func (m *mockTool) Description() string { return "A mock tool" }
func (m *mockTool) InputSchema() tool.ToolSchema {
	return tool.ToolSchema{
		Type:       "object",
		Properties: map[string]tool.PropertyDef{},
	}
}
func (m *mockTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	return "mock result", nil
}

func TestRegisterTool(t *testing.T) {
	ClearRegistry()
	defer ClearRegistry()

	mock := &mockTool{name: "test-tool"}

	// Register should succeed
	if err := RegisterTool(mock); err != nil {
		t.Fatalf("RegisterTool() error = %v", err)
	}

	// Should be able to retrieve it
	retrieved, ok := GetRegisteredTool("test-tool")
	if !ok {
		t.Fatal("GetRegisteredTool() returned false")
	}

	if retrieved.Name() != mock.name {
		t.Errorf("Name = %v, want %v", retrieved.Name(), mock.name)
	}
}

func TestRegisterTool_Duplicate(t *testing.T) {
	ClearRegistry()
	defer ClearRegistry()

	mock := &mockTool{name: "test-tool"}

	// First registration should succeed
	if err := RegisterTool(mock); err != nil {
		t.Fatalf("RegisterTool() error = %v", err)
	}

	// Second registration should fail
	if err := RegisterTool(mock); err == nil {
		t.Error("Expected error for duplicate tool registration")
	}
}

func TestRegisterTool_Invalid(t *testing.T) {
	ClearRegistry()
	defer ClearRegistry()

	// Nil tool
	if err := RegisterTool(nil); err == nil {
		t.Error("Expected error for nil tool")
	}

	// Empty name
	if err := RegisterTool(&mockTool{name: ""}); err == nil {
		t.Error("Expected error for empty tool name")
	}
}

func TestListRegisteredTools(t *testing.T) {
	ClearRegistry()
	defer ClearRegistry()

	// Register some tools
	MustRegisterTool(&mockTool{name: "tool-a"})
	MustRegisterTool(&mockTool{name: "tool-b"})

	names := ListRegisteredTools()
	if len(names) != 2 {
		t.Errorf("ListRegisteredTools() = %d tools, want 2", len(names))
	}
}

func TestClearRegistry(t *testing.T) {
	ClearRegistry()
	defer ClearRegistry()

	// Register agents and tools
	MustRegister(&AgentDefinition{Name: "agent", Model: "x", SystemPrompt: "x"})
	MustRegisterTool(&mockTool{name: "tool"})

	// Clear
	ClearRegistry()

	// Should be empty
	if len(ListRegisteredAgents()) != 0 {
		t.Error("Expected no agents after clear")
	}
	if len(ListRegisteredTools()) != 0 {
		t.Error("Expected no tools after clear")
	}
}
