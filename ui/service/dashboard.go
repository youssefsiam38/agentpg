package service

import (
	"context"
	"sort"
	"time"
)

// GetDashboardStats returns aggregated statistics for the dashboard.
// metadataFilter filters sessions/runs by metadata key-value pairs.
// metadataCountKeys specifies which metadata keys to show in the breakdown (e.g., by tenant_id).
func (s *Service[TTx]) GetDashboardStats(ctx context.Context, metadataFilter map[string]any, metadataCountKeys []string) (*DashboardStats, error) {
	stats := &DashboardStats{
		RunsByState:    make(map[string]int),
		ToolsByState:   make(map[string]int),
		MetadataCounts: make(map[string]map[string]int),
		RunsByAgent:    make(map[string]int),
	}

	// Initialize metadata count maps
	for _, key := range metadataCountKeys {
		stats.MetadataCounts[key] = make(map[string]int)
	}

	now := time.Now()

	// Get sessions
	sessions, err := s.ListSessions(ctx, SessionListParams{
		MetadataFilter: metadataFilter,
		Limit:          1000,
		OrderBy:        "updated_at",
		OrderDir:       "desc",
	})
	if err != nil {
		return nil, err
	}
	stats.TotalSessions = sessions.TotalCount

	// Count active sessions and collect recent sessions
	for i, sess := range sessions.Sessions {
		if now.Sub(sess.LastActivityAt) < 24*time.Hour {
			stats.ActiveSessions++
		}
		if now.Sub(sess.CreatedAt) < 24*time.Hour {
			stats.SessionsToday++
		}
		// Track metadata counts for specified keys
		for _, key := range metadataCountKeys {
			if sess.Metadata != nil {
				if val, ok := sess.Metadata[key]; ok {
					if strVal, ok := val.(string); ok {
						stats.MetadataCounts[key][strVal]++
					}
				}
			}
		}

		// Collect top 5 recent sessions
		if i < 5 {
			stats.RecentSessions = append(stats.RecentSessions, sess)
		}
	}

	// Get runs
	runs, err := s.ListRuns(ctx, RunListParams{
		MetadataFilter: metadataFilter,
		Limit:          1000,
	})
	if err != nil {
		return nil, err
	}
	stats.TotalRuns = runs.TotalCount

	// Agent stats aggregation
	agentStats := make(map[string]*AgentStats)

	// Track 24h metrics
	var totalDurationMs24h int64
	var runsWithDuration24h int
	var totalIterations24h int

	// Categorize runs
	for _, run := range runs.Runs {
		stats.RunsByState[run.State]++
		stats.RunsByAgent[run.AgentName]++

		// Initialize agent stats if not exists
		if agentStats[run.AgentName] == nil {
			agentStats[run.AgentName] = &AgentStats{Name: run.AgentName}
		}
		agentStats[run.AgentName].RunCount++
		agentStats[run.AgentName].TotalTokens += run.TotalTokens

		switch run.State {
		case "pending":
			stats.ActiveRuns++
			stats.PendingRuns++
		case "batch_submitting", "batch_pending", "batch_processing", "streaming", "pending_tools":
			stats.ActiveRuns++
		case "completed":
			agentStats[run.AgentName].CompletedCount++
		case "failed":
			agentStats[run.AgentName].FailedCount++
		}

		// 24h metrics
		isRecent := false
		if run.FinalizedAt != nil && now.Sub(*run.FinalizedAt) < 24*time.Hour {
			isRecent = true
			if run.State == "completed" {
				stats.CompletedRuns24h++
			} else if run.State == "failed" {
				stats.FailedRuns24h++
			}
		} else if run.CreatedAt.After(now.Add(-24 * time.Hour)) {
			isRecent = true
		}

		if isRecent {
			stats.TotalTokens24h += run.TotalTokens
			totalIterations24h += run.IterationCount

			if run.Duration != nil {
				totalDurationMs24h += run.Duration.Milliseconds()
				runsWithDuration24h++
			}
		}
	}

	// Calculate averages
	total24hRuns := stats.CompletedRuns24h + stats.FailedRuns24h
	if total24hRuns > 0 {
		stats.SuccessRate24h = float64(stats.CompletedRuns24h) / float64(total24hRuns) * 100
		stats.AvgTokensPerRun = stats.TotalTokens24h / total24hRuns
		stats.AvgIterationsPerRun = float64(totalIterations24h) / float64(total24hRuns)
	}
	if runsWithDuration24h > 0 {
		stats.AvgRunDurationMs = totalDurationMs24h / int64(runsWithDuration24h)
	}

	// Calculate agent success rates and get top agents
	for _, as := range agentStats {
		total := as.CompletedCount + as.FailedCount
		if total > 0 {
			as.SuccessRate = float64(as.CompletedCount) / float64(total) * 100
		}
	}

	// Sort agents by run count and get top 5
	agentList := make([]*AgentStats, 0, len(agentStats))
	for _, as := range agentStats {
		agentList = append(agentList, as)
	}
	sort.Slice(agentList, func(i, j int) bool {
		return agentList[i].RunCount > agentList[j].RunCount
	})
	if len(agentList) > 5 {
		agentList = agentList[:5]
	}
	stats.TopAgents = agentList

	// Get tool executions
	toolExecs, err := s.ListToolExecutions(ctx, ToolExecutionListParams{
		Limit: 1000,
	})
	if err != nil {
		return nil, err
	}

	// Tool stats aggregation
	toolStats := make(map[string]*ToolStats)
	toolDurations := make(map[string][]int64)

	for _, exec := range toolExecs {
		stats.ToolsByState[exec.State]++

		// Initialize tool stats if not exists
		if toolStats[exec.ToolName] == nil {
			toolStats[exec.ToolName] = &ToolStats{Name: exec.ToolName}
		}
		toolStats[exec.ToolName].ExecutionCount++

		switch exec.State {
		case "pending":
			stats.PendingTools++
		case "running":
			stats.RunningTools++
		case "failed":
			toolStats[exec.ToolName].FailedCount++
		}

		// Track 24h tool executions
		if exec.CreatedAt.After(now.Add(-24 * time.Hour)) {
			stats.ToolExecutions24h++
		}

		if exec.State == "failed" && exec.CompletedAt != nil && now.Sub(*exec.CompletedAt) < 24*time.Hour {
			stats.FailedTools24h++
		}

		// Track durations for completed tools
		if exec.Duration != nil {
			toolDurations[exec.ToolName] = append(toolDurations[exec.ToolName], exec.Duration.Milliseconds())
		}
	}

	// Calculate average durations for tools
	for name, ts := range toolStats {
		durations := toolDurations[name]
		if len(durations) > 0 {
			var total int64
			for _, d := range durations {
				total += d
			}
			ts.AvgDurationMs = total / int64(len(durations))
		}
	}

	// Sort tools by execution count and get top 5
	toolList := make([]*ToolStats, 0, len(toolStats))
	for _, ts := range toolStats {
		toolList = append(toolList, ts)
	}
	sort.Slice(toolList, func(i, j int) bool {
		return toolList[i].ExecutionCount > toolList[j].ExecutionCount
	})
	if len(toolList) > 5 {
		toolList = toolList[:5]
	}
	stats.TopTools = toolList

	// Get instances
	instances, err := s.ListInstances(ctx)
	if err != nil {
		return nil, err
	}
	stats.ActiveInstances = len(instances)

	// Find leader
	leaderID, _ := s.store.GetLeader(ctx)
	stats.LeaderInstanceID = leaderID

	// Get recent runs
	recentRuns, err := s.ListRuns(ctx, RunListParams{
		MetadataFilter: metadataFilter,
		Limit:          10,
		OrderBy:        "created_at",
		OrderDir:       "desc",
	})
	if err == nil {
		stats.RecentRuns = recentRuns.Runs
	}

	// Get recent tool errors
	var recentToolErrors []*ToolExecutionSummary
	for _, exec := range toolExecs {
		if exec.State == "failed" {
			recentToolErrors = append(recentToolErrors, exec)
			if len(recentToolErrors) >= 5 {
				break
			}
		}
	}
	stats.RecentToolErrors = recentToolErrors

	return stats, nil
}
