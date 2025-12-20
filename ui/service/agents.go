package service

import (
	"context"
)

// ListAgents returns all agents with statistics.
func (s *Service[TTx]) ListAgents(ctx context.Context) ([]*AgentWithStats, error) {
	agents, err := s.store.ListAgents(ctx)
	if err != nil {
		return nil, err
	}

	// Get instances to find which agents are registered where
	instances, _ := s.store.ListInstances(ctx)
	agentInstances := make(map[string][]string)
	for _, inst := range instances {
		agentNames, _ := s.store.GetInstanceAgents(ctx, inst.ID)
		for _, name := range agentNames {
			agentInstances[name] = append(agentInstances[name], inst.ID)
		}
	}

	// Build results
	results := make([]*AgentWithStats, 0, len(agents))
	for _, agent := range agents {
		stats := &AgentWithStats{
			Agent:        agent,
			RegisteredOn: agentInstances[agent.Name],
		}

		// Get run statistics
		// TODO: Add queries to count runs by agent
		// For now, we leave the stats as zeros

		results = append(results, stats)
	}

	return results, nil
}

// GetAgentWithStats returns an agent with its statistics.
func (s *Service[TTx]) GetAgentWithStats(ctx context.Context, name string) (*AgentWithStats, error) {
	agent, err := s.store.GetAgent(ctx, name)
	if err != nil {
		return nil, err
	}

	// Get instances
	instances, _ := s.store.ListInstances(ctx)
	var registeredOn []string
	for _, inst := range instances {
		agentNames, _ := s.store.GetInstanceAgents(ctx, inst.ID)
		for _, n := range agentNames {
			if n == name {
				registeredOn = append(registeredOn, inst.ID)
				break
			}
		}
	}

	return &AgentWithStats{
		Agent:        agent,
		RegisteredOn: registeredOn,
	}, nil
}

// ListTools returns all tools with statistics.
func (s *Service[TTx]) ListTools(ctx context.Context) ([]*ToolWithStats, error) {
	tools, err := s.store.ListTools(ctx)
	if err != nil {
		return nil, err
	}

	// Get instances to find which tools are registered where
	instances, _ := s.store.ListInstances(ctx)
	toolInstances := make(map[string][]string)
	for _, inst := range instances {
		toolNames, _ := s.store.GetInstanceTools(ctx, inst.ID)
		for _, name := range toolNames {
			toolInstances[name] = append(toolInstances[name], inst.ID)
		}
	}

	// Build results
	results := make([]*ToolWithStats, 0, len(tools))
	for _, tool := range tools {
		stats := &ToolWithStats{
			Tool:         tool,
			RegisteredOn: toolInstances[tool.Name],
		}

		// Get execution statistics
		// TODO: Add queries to count executions by tool

		results = append(results, stats)
	}

	return results, nil
}

// GetToolWithStats returns a tool with its statistics.
func (s *Service[TTx]) GetToolWithStats(ctx context.Context, name string) (*ToolWithStats, error) {
	tool, err := s.store.GetTool(ctx, name)
	if err != nil {
		return nil, err
	}

	// Get instances
	instances, _ := s.store.ListInstances(ctx)
	var registeredOn []string
	for _, inst := range instances {
		toolNames, _ := s.store.GetInstanceTools(ctx, inst.ID)
		for _, n := range toolNames {
			if n == name {
				registeredOn = append(registeredOn, inst.ID)
				break
			}
		}
	}

	return &ToolWithStats{
		Tool:         tool,
		RegisteredOn: registeredOn,
	}, nil
}
