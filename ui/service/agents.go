package service

import (
	"context"

	"github.com/youssefsiam38/agentpg/driver"
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
		registeredOn := agentInstances[agent.Name]
		stats := &AgentWithStats{
			Agent:        agent,
			RegisteredOn: registeredOn,
			IsActive:     len(registeredOn) > 0,
		}

		// Get run statistics using ListRuns with agent filter
		runs, total, err := s.store.ListRuns(ctx, driver.ListRunsParams{
			AgentName: agent.Name,
			Limit:     1000, // Get enough to compute stats
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
		IsActive:     len(registeredOn) > 0,
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
