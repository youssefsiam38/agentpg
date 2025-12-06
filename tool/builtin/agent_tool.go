package builtin

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/youssefsiam38/agentpg/tool"
)

// AgentToolInterface is the minimal interface needed for agent-as-tool
// This avoids circular import by not importing the full agentpg package
type AgentToolInterface interface {
	NewSessionWithParent(ctx context.Context, tenantID, identifier string, parentSessionID *string, metadata map[string]any) (string, error)
	LoadSession(ctx context.Context, sessionID string) error
	Run(ctx context.Context, prompt string) (AgentResponse, error)
	GetSystemPrompt() string
}

// AgentResponse is the minimal response interface
type AgentResponse interface {
	GetText() string
}

// AgentTool wraps an agent as a tool for use by other agents
type AgentTool struct {
	agent           AgentToolInterface
	name            string
	description     string
	parentTenantID  string
	parentSessionID *string
	sessionID       string
}

// NewAgentTool creates a new agent tool wrapper
// The nested agent will have its own dedicated session linked to the parent's session
// parentTenantID is the tenant_id to use for the nested session (inherited from parent)
// parentSessionID is the parent session's ID to link to (can be nil if no parent)
func NewAgentTool(agent AgentToolInterface, name, description, parentTenantID string, parentSessionID *string) (*AgentTool, error) {
	if agent == nil {
		return nil, fmt.Errorf("agent cannot be nil")
	}

	if name == "" {
		return nil, fmt.Errorf("name cannot be empty")
	}

	if parentTenantID == "" {
		return nil, fmt.Errorf("parentTenantID cannot be empty")
	}

	if description == "" {
		description = fmt.Sprintf("Delegate task to %s agent", name)
	}

	// Create dedicated session for nested agent with proper parent linkage
	sessionID, err := agent.NewSessionWithParent(context.Background(), parentTenantID, name, parentSessionID, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create nested session: %w", err)
	}

	return &AgentTool{
		agent:           agent,
		name:            name,
		description:     description,
		parentTenantID:  parentTenantID,
		parentSessionID: parentSessionID,
		sessionID:       sessionID,
	}, nil
}

// Name returns the tool name
func (a *AgentTool) Name() string {
	return a.name
}

// Description returns the tool description
func (a *AgentTool) Description() string {
	return a.description
}

// InputSchema returns the JSON schema for the tool's input
func (a *AgentTool) InputSchema() tool.ToolSchema {
	return tool.ToolSchema{
		Type: "object",
		Properties: map[string]tool.PropertyDef{
			"task": {
				Type:        "string",
				Description: "The task or question to delegate to this agent",
			},
			"context": {
				Type:        "string",
				Description: "Additional context for the task (optional)",
			},
		},
		Required: []string{"task"},
	}
}

// Execute runs the nested agent with the given task
func (a *AgentTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	// Parse input
	var params struct {
		Task    string `json:"task"`
		Context string `json:"context"`
	}

	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}

	if params.Task == "" {
		return "", fmt.Errorf("task is required")
	}

	// Load the dedicated session
	if err := a.agent.LoadSession(ctx, a.sessionID); err != nil {
		return "", fmt.Errorf("failed to load session: %w", err)
	}

	// Build prompt
	prompt := params.Task
	if params.Context != "" {
		prompt = fmt.Sprintf("Context: %s\n\nTask: %s", params.Context, params.Task)
	}

	// Execute nested agent
	response, err := a.agent.Run(ctx, prompt)
	if err != nil {
		return "", fmt.Errorf("nested agent failed: %w", err)
	}

	// Extract text from response
	return response.GetText(), nil
}

// GetSessionID returns the dedicated session ID for this nested agent
func (a *AgentTool) GetSessionID() string {
	return a.sessionID
}

// GetParentSessionID returns the parent session ID if this is a nested agent
func (a *AgentTool) GetParentSessionID() *string {
	return a.parentSessionID
}

// GetParentTenantID returns the parent tenant ID
func (a *AgentTool) GetParentTenantID() string {
	return a.parentTenantID
}
