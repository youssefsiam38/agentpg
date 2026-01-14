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

		// Get registered agents
		agentNames, _ := s.store.GetInstanceAgents(ctx, inst.ID)
		health.AgentNames = agentNames

		// Get registered tools
		toolNames, _ := s.store.GetInstanceTools(ctx, inst.ID)
		health.ToolNames = toolNames

		results = append(results, health)
	}

	return results, nil
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

	// Get registered agents
	agentNames, _ := s.store.GetInstanceAgents(ctx, id)
	detail.AgentNames = agentNames

	// Get registered tools
	toolNames, _ := s.store.GetInstanceTools(ctx, id)
	detail.ToolNames = toolNames

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
