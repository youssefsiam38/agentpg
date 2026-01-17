package service

import (
	"context"
	"time"

	"github.com/youssefsiam38/agentpg/driver"
)

// ListInstances returns all registered instances with their health status.
func (s *Service[TTx]) ListInstances(ctx context.Context) ([]*InstanceWithHealth, error) {
	instances, err := s.store.ListInstances(ctx)
	if err != nil {
		return nil, err
	}

	// Get active counts for all instances in one query
	activeCounts, err := s.store.GetAllInstanceActiveCounts(ctx)
	if err != nil {
		// Log error but continue - counts will be 0
		activeCounts = make(map[string][2]int)
	}

	// Pre-fetch all agents to determine which agents each instance can handle
	agents, _, _ := s.store.ListAgents(ctx, driver.ListAgentsParams{Limit: 1000})

	results := make([]*InstanceWithHealth, 0, len(instances))
	for _, inst := range instances {
		// Populate active counts from the query results
		if counts, ok := activeCounts[inst.ID]; ok {
			inst.ActiveRunCount = counts[0]
			inst.ActiveToolCount = counts[1]
		}

		health := &InstanceWithHealth{
			Instance: inst,
		}

		// Calculate health status based on heartbeat
		timeSinceHeartbeat := time.Since(inst.LastHeartbeatAt)
		health.TimeSinceHeartbeat = &timeSinceHeartbeat

		// Healthy if heartbeat within last 30 seconds
		if timeSinceHeartbeat < 30*time.Second {
			health.Status = "healthy"
		} else if timeSinceHeartbeat < 60*time.Second {
			health.Status = "warning"
		} else {
			health.Status = "unhealthy"
		}

		// Get registered tools
		toolNames, _ := s.store.GetInstanceTools(ctx, inst.ID)
		health.ToolNames = toolNames

		// Compute which agents this instance can handle based on its tools
		health.AgentNames = computeHandleableAgents(agents, toolNames)

		results = append(results, health)
	}

	return results, nil
}

// computeHandleableAgents returns the names of agents that can be handled by an instance with the given tools.
func computeHandleableAgents(agents []*driver.AgentDefinition, instanceTools []string) []string {
	if len(agents) == 0 {
		return nil
	}

	toolSet := make(map[string]bool)
	for _, t := range instanceTools {
		toolSet[t] = true
	}

	var result []string
	for _, agent := range agents {
		// Agent can be handled if all its required tools are available
		canHandle := true
		for _, toolName := range agent.ToolNames {
			if !toolSet[toolName] {
				canHandle = false
				break
			}
		}
		if canHandle {
			result = append(result, agent.Name)
		}
	}
	return result
}

// GetInstance returns an instance by ID.
func (s *Service[TTx]) GetInstance(ctx context.Context, id string) (*driver.Instance, error) {
	instances, err := s.store.ListInstances(ctx)
	if err != nil {
		return nil, err
	}

	for _, inst := range instances {
		if inst.ID == id {
			return inst, nil
		}
	}

	return nil, ErrNotFound
}

// GetInstanceDetail returns detailed information about an instance.
func (s *Service[TTx]) GetInstanceDetail(ctx context.Context, id string) (*InstanceDetail, error) {
	inst, err := s.GetInstance(ctx, id)
	if err != nil {
		return nil, err
	}

	// Get active counts for this instance
	activeRuns, activeTools, err := s.store.GetInstanceActiveCounts(ctx, id)
	if err == nil {
		inst.ActiveRunCount = activeRuns
		inst.ActiveToolCount = activeTools
	}

	detail := &InstanceDetail{
		Instance: inst,
	}

	// Calculate health status
	timeSinceHeartbeat := time.Since(inst.LastHeartbeatAt)
	detail.TimeSinceHeartbeat = &timeSinceHeartbeat

	if timeSinceHeartbeat < 30*time.Second {
		detail.Status = "healthy"
	} else if timeSinceHeartbeat < 60*time.Second {
		detail.Status = "warning"
	} else {
		detail.Status = "unhealthy"
	}

	// Get registered tools
	toolNames, _ := s.store.GetInstanceTools(ctx, id)
	detail.ToolNames = toolNames

	// Compute which agents this instance can handle based on its tools
	agents, _, _ := s.store.ListAgents(ctx, driver.ListAgentsParams{Limit: 1000})
	detail.AgentNames = computeHandleableAgents(agents, toolNames)

	// Get current leader
	leaderID, _ := s.store.GetLeader(ctx)
	detail.IsLeader = leaderID == id

	return detail, nil
}

// GetLeaderInfo returns information about the current leader.
func (s *Service[TTx]) GetLeaderInfo(ctx context.Context) (*LeaderInfo, error) {
	leaderID, err := s.store.GetLeader(ctx)
	if err != nil {
		return nil, err
	}

	if leaderID == "" {
		return nil, nil
	}

	info := &LeaderInfo{
		LeaderID: leaderID,
	}

	// Get instance info for the leader
	instances, _ := s.store.ListInstances(ctx)
	for _, inst := range instances {
		if inst.ID == leaderID {
			info.InstanceName = inst.Name
			break
		}
	}

	return info, nil
}
