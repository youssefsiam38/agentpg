package service

import (
	"context"
	"time"
)

// GetDashboardStats returns aggregated statistics for the dashboard.
func (s *Service[TTx]) GetDashboardStats(ctx context.Context, tenantID string) (*DashboardStats, error) {
	stats := &DashboardStats{
		RunsByState:  make(map[string]int),
		ToolsByState: make(map[string]int),
		TenantCounts: make(map[string]int),
	}

	// Get sessions
	sessions, err := s.ListSessions(ctx, SessionListParams{
		TenantID: tenantID,
		Limit:    1000,
	})
	if err != nil {
		return nil, err
	}
	stats.TotalSessions = sessions.TotalCount

	// Count active sessions (sessions with activity in last 24h)
	now := time.Now()
	for _, sess := range sessions.Sessions {
		if now.Sub(sess.LastActivityAt) < 24*time.Hour {
			stats.ActiveSessions++
		}
		if now.Sub(sess.CreatedAt) < 24*time.Hour {
			stats.SessionsToday++
		}
		// Track tenant counts
		stats.TenantCounts[sess.TenantID]++
	}

	// Get runs
	runs, err := s.ListRuns(ctx, RunListParams{
		TenantID: tenantID,
		Limit:    1000,
	})
	if err != nil {
		return nil, err
	}
	stats.TotalRuns = runs.TotalCount

	// Categorize runs
	for _, run := range runs.Runs {
		stats.RunsByState[run.State]++

		switch run.State {
		case "pending":
			stats.ActiveRuns++
			stats.PendingRuns++
		case "batch_submitting", "batch_pending", "batch_processing", "streaming", "pending_tools":
			stats.ActiveRuns++
		}

		if run.FinalizedAt != nil && now.Sub(*run.FinalizedAt) < 24*time.Hour {
			if run.State == "completed" {
				stats.CompletedRuns24h++
			} else if run.State == "failed" {
				stats.FailedRuns24h++
			}
		}
	}

	// Get tool executions
	toolExecs, err := s.ListToolExecutions(ctx, ToolExecutionListParams{
		Limit: 1000,
	})
	if err != nil {
		return nil, err
	}

	for _, exec := range toolExecs {
		stats.ToolsByState[exec.State]++

		switch exec.State {
		case "pending":
			stats.PendingTools++
		case "running":
			stats.RunningTools++
		}

		if exec.State == "failed" && exec.CompletedAt != nil && now.Sub(*exec.CompletedAt) < 24*time.Hour {
			stats.FailedTools24h++
		}
	}

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
		TenantID: tenantID,
		Limit:    10,
		OrderBy:  "created_at",
		OrderDir: "desc",
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
