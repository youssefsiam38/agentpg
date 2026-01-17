package service

import (
	"context"

	"github.com/google/uuid"
	"github.com/youssefsiam38/agentpg/driver"
)

// ListAgents returns all agents with statistics.
func (s *Service[TTx]) ListAgents(ctx context.Context, metadataFilter map[string]any) ([]*AgentWithStats, error) {
	agents, _, err := s.store.ListAgents(ctx, driver.ListAgentsParams{
		MetadataFilter: metadataFilter,
		Limit:          1000, // Get all agents
	})
	if err != nil {
		return nil, err
	}

	// Build results
	results := make([]*AgentWithStats, 0, len(agents))
	for _, agent := range agents {
		stats := &AgentWithStats{
			Agent: agent,
		}

		// Get run statistics using ListRuns with agent filter
		runs, total, err := s.store.ListRuns(ctx, driver.ListRunsParams{
			AgentID: &agent.ID,
			Limit:   1000, // Get enough to compute stats
		})
		if err == nil {
			stats.TotalRuns = total

			var totalTokens int
			for _, run := range runs {
				totalTokens += run.InputTokens + run.OutputTokens
				switch run.State {
				case "pending", "batch_submitting", "batch_pending", "batch_processing", "streaming", "pending_tools":
					stats.ActiveRuns++
				case "completed":
					stats.CompletedRuns++
				case "failed":
					stats.FailedRuns++
				}
			}

			if len(runs) > 0 {
				stats.AvgTokensPerRun = totalTokens / len(runs)
			}
		}

		// Check if any instance can handle this agent (has all required tools)
		stats.IsActive = s.canAnyInstanceHandleAgent(ctx, agent)
		if stats.IsActive {
			stats.CapableInstances = s.getInstancesWithTools(ctx, agent.ToolNames)
		}

		results = append(results, stats)
	}

	return results, nil
}

// GetAgentWithStats returns an agent with its statistics.
func (s *Service[TTx]) GetAgentWithStats(ctx context.Context, agentID uuid.UUID) (*AgentWithStats, error) {
	agent, err := s.store.GetAgent(ctx, agentID)
	if err != nil {
		return nil, err
	}

	// Check which instances can handle this agent
	isActive := s.canAnyInstanceHandleAgent(ctx, agent)
	var capableInstances []string
	if isActive {
		capableInstances = s.getInstancesWithTools(ctx, agent.ToolNames)
	}

	return &AgentWithStats{
		Agent:            agent,
		CapableInstances: capableInstances,
		IsActive:         isActive,
	}, nil
}

// canAnyInstanceHandleAgent checks if any instance has all the tools required by an agent.
func (s *Service[TTx]) canAnyInstanceHandleAgent(ctx context.Context, agent *driver.AgentDefinition) bool {
	if len(agent.ToolNames) == 0 {
		// Agent doesn't need any tools, any instance can handle it
		return true
	}

	instances, _ := s.store.ListInstances(ctx)
	for _, inst := range instances {
		instanceTools, _ := s.store.GetInstanceTools(ctx, inst.ID)
		toolSet := make(map[string]bool)
		for _, t := range instanceTools {
			toolSet[t] = true
		}

		// Check if instance has all required tools
		hasAllTools := true
		for _, toolName := range agent.ToolNames {
			if !toolSet[toolName] {
				hasAllTools = false
				break
			}
		}
		if hasAllTools {
			return true
		}
	}
	return false
}

// getInstancesWithTools returns instance IDs that have all the specified tools.
func (s *Service[TTx]) getInstancesWithTools(ctx context.Context, toolNames []string) []string {
	if len(toolNames) == 0 {
		// Return all instances if no tools required
		instances, _ := s.store.ListInstances(ctx)
		result := make([]string, 0, len(instances))
		for _, inst := range instances {
			result = append(result, inst.ID)
		}
		return result
	}

	var result []string
	instances, _ := s.store.ListInstances(ctx)
	for _, inst := range instances {
		instanceTools, _ := s.store.GetInstanceTools(ctx, inst.ID)
		toolSet := make(map[string]bool)
		for _, t := range instanceTools {
			toolSet[t] = true
		}

		// Check if instance has all required tools
		hasAllTools := true
		for _, toolName := range toolNames {
			if !toolSet[toolName] {
				hasAllTools = false
				break
			}
		}
		if hasAllTools {
			result = append(result, inst.ID)
		}
	}
	return result
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
		registeredOn := toolInstances[tool.Name]
		stats := &ToolWithStats{
			Tool:         tool,
			RegisteredOn: registeredOn,
			IsActive:     len(registeredOn) > 0,
		}

		// Get execution statistics using ListToolExecutions with tool filter
		execs, total, err := s.store.ListToolExecutions(ctx, driver.ListToolExecutionsParams{
			ToolName: tool.Name,
			Limit:    1000, // Get enough to compute stats
		})
		if err == nil {
			stats.TotalExecutions = total

			for _, exec := range execs {
				switch exec.State {
				case "pending":
					stats.PendingCount++
				case "completed":
					stats.CompletedCount++
				case "failed":
					stats.FailedCount++
				}
			}
		}

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
		IsActive:     len(registeredOn) > 0,
	}, nil
}
