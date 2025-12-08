package agentpg

import (
	"fmt"
	"sync"

	"github.com/youssefsiam38/agentpg/tool"
)

// Global registries - these are package-level and populated at init() time
var (
	globalAgentsMu sync.RWMutex
	globalAgents   = make(map[string]*AgentDefinition)
	globalToolsMu  sync.RWMutex
	globalTools    = make(map[string]tool.Tool)
)

// AgentDefinition defines an agent's configuration for registration.
// This is used with the global Register function to define agents at package init time.
type AgentDefinition struct {
	// Name is the unique identifier for this agent (required)
	Name string

	// Description describes what this agent does
	Description string

	// Model is the Claude model to use (required)
	// Examples: "claude-sonnet-4-5-20250929", "claude-opus-4-5-20251101"
	Model string

	// SystemPrompt is the system prompt for this agent (required)
	SystemPrompt string

	// Tools is a list of tool names to attach to this agent
	// These must be registered via RegisterTool before the agent is used
	Tools []string

	// MaxTokens is the maximum tokens for responses (optional)
	// If not set, uses model defaults
	MaxTokens *int

	// Temperature controls randomness (optional)
	// 0.0 = deterministic, 1.0 = creative
	Temperature *float32

	// Config holds additional configuration
	Config map[string]any
}

// Validate validates the agent definition
func (d *AgentDefinition) Validate() error {
	if d.Name == "" {
		return fmt.Errorf("%w: agent name is required", ErrInvalidConfig)
	}
	if d.Model == "" {
		return fmt.Errorf("%w: agent model is required for %q", ErrInvalidConfig, d.Name)
	}
	if d.SystemPrompt == "" {
		return fmt.Errorf("%w: agent system prompt is required for %q", ErrInvalidConfig, d.Name)
	}
	return nil
}

// Register registers an agent definition globally.
// This should be called at package init time before creating a Client.
//
// Example:
//
//	func init() {
//	    agentpg.Register(&agentpg.AgentDefinition{
//	        Name:         "chat",
//	        Model:        "claude-sonnet-4-5-20250929",
//	        SystemPrompt: "You are a helpful assistant",
//	        Tools:        []string{"calculator", "weather"},
//	    })
//	}
func Register(def *AgentDefinition) error {
	if def == nil {
		return fmt.Errorf("%w: agent definition is nil", ErrInvalidConfig)
	}

	if err := def.Validate(); err != nil {
		return err
	}

	globalAgentsMu.Lock()
	defer globalAgentsMu.Unlock()

	if _, exists := globalAgents[def.Name]; exists {
		return fmt.Errorf("%w: agent %q already registered", ErrInvalidConfig, def.Name)
	}

	globalAgents[def.Name] = def
	return nil
}

// MustRegister is like Register but panics on error.
// This is useful for init() functions where errors should be fatal.
//
// Example:
//
//	func init() {
//	    agentpg.MustRegister(&agentpg.AgentDefinition{
//	        Name:         "chat",
//	        Model:        "claude-sonnet-4-5-20250929",
//	        SystemPrompt: "You are a helpful assistant",
//	    })
//	}
func MustRegister(def *AgentDefinition) {
	if err := Register(def); err != nil {
		panic(err)
	}
}

// GetRegisteredAgent returns a registered agent definition by name.
func GetRegisteredAgent(name string) (*AgentDefinition, bool) {
	globalAgentsMu.RLock()
	defer globalAgentsMu.RUnlock()

	def, ok := globalAgents[name]
	return def, ok
}

// ListRegisteredAgents returns all registered agent names.
func ListRegisteredAgents() []string {
	globalAgentsMu.RLock()
	defer globalAgentsMu.RUnlock()

	names := make([]string, 0, len(globalAgents))
	for name := range globalAgents {
		names = append(names, name)
	}
	return names
}

// RegisterTool registers a tool globally.
// This should be called at package init time before creating a Client.
//
// Example:
//
//	func init() {
//	    agentpg.RegisterTool(&CalculatorTool{})
//	    agentpg.RegisterTool(&WeatherTool{})
//	}
func RegisterTool(t tool.Tool) error {
	if t == nil {
		return fmt.Errorf("%w: tool is nil", ErrInvalidConfig)
	}

	name := t.Name()
	if name == "" {
		return fmt.Errorf("%w: tool name is empty", ErrInvalidConfig)
	}

	globalToolsMu.Lock()
	defer globalToolsMu.Unlock()

	if _, exists := globalTools[name]; exists {
		return fmt.Errorf("%w: tool %q already registered", ErrInvalidConfig, name)
	}

	globalTools[name] = t
	return nil
}

// MustRegisterTool is like RegisterTool but panics on error.
// This is useful for init() functions where errors should be fatal.
func MustRegisterTool(t tool.Tool) {
	if err := RegisterTool(t); err != nil {
		panic(err)
	}
}

// GetRegisteredTool returns a registered tool by name.
func GetRegisteredTool(name string) (tool.Tool, bool) {
	globalToolsMu.RLock()
	defer globalToolsMu.RUnlock()

	t, ok := globalTools[name]
	return t, ok
}

// ListRegisteredTools returns all registered tool names.
func ListRegisteredTools() []string {
	globalToolsMu.RLock()
	defer globalToolsMu.RUnlock()

	names := make([]string, 0, len(globalTools))
	for name := range globalTools {
		names = append(names, name)
	}
	return names
}

// ClearRegistry clears all registered agents and tools.
// This is mainly useful for testing.
func ClearRegistry() {
	globalAgentsMu.Lock()
	globalAgents = make(map[string]*AgentDefinition)
	globalAgentsMu.Unlock()

	globalToolsMu.Lock()
	globalTools = make(map[string]tool.Tool)
	globalToolsMu.Unlock()
}
