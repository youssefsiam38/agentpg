package service

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/youssefsiam38/agentpg/driver"
)

// ListToolExecutions returns a list of tool executions.
func (s *Service[TTx]) ListToolExecutions(ctx context.Context, params ToolExecutionListParams) ([]*ToolExecutionSummary, error) {
	if params.Limit <= 0 {
		params.Limit = 25
	}

	// Use the driver's ListToolExecutions method with filtering and pagination
	executions, _, err := s.store.ListToolExecutions(ctx, driver.ListToolExecutionsParams{
		RunID:       params.RunID,
		IterationID: params.IterationID,
		ToolName:    params.ToolName,
		State:       params.State,
		IsAgentTool: params.IsAgentTool,
		Limit:       params.Limit,
		Offset:      params.Offset,
	})
	if err != nil {
		return nil, err
	}

	// Convert to summaries
	summaries := make([]*ToolExecutionSummary, 0, len(executions))
	for _, exec := range executions {
		var duration *time.Duration
		if exec.CompletedAt != nil && exec.StartedAt != nil {
			d := exec.CompletedAt.Sub(*exec.StartedAt)
			duration = &d
		}
		// Look up agent name from ID if this is an agent tool
		var agentName *string
		if exec.IsAgentTool && exec.AgentID != nil {
			if agent, err := s.store.GetAgent(ctx, *exec.AgentID); err == nil {
				agentName = &agent.Name
			}
		}
		summaries = append(summaries, &ToolExecutionSummary{
			ID:           exec.ID,
			RunID:        exec.RunID,
			IterationID:  exec.IterationID,
			ToolName:     exec.ToolName,
			State:        string(exec.State),
			IsAgentTool:  exec.IsAgentTool,
			AgentName:    agentName,
			ChildRunID:   exec.ChildRunID,
			IsError:      exec.IsError,
			AttemptCount: exec.AttemptCount,
			MaxAttempts:  exec.MaxAttempts,
			Duration:     duration,
			CreatedAt:    exec.CreatedAt,
			CompletedAt:  exec.CompletedAt,
		})
	}

	return summaries, nil
}

// GetToolExecution returns a tool execution by ID.
func (s *Service[TTx]) GetToolExecution(ctx context.Context, id uuid.UUID) (*driver.ToolExecution, error) {
	return s.store.GetToolExecution(ctx, id)
}

// GetToolExecutionDetail returns detailed information about a tool execution.
func (s *Service[TTx]) GetToolExecutionDetail(ctx context.Context, id uuid.UUID) (*ToolExecutionDetail, error) {
	exec, err := s.store.GetToolExecution(ctx, id)
	if err != nil {
		return nil, err
	}

	detail := &ToolExecutionDetail{
		Execution: exec,
	}

	// Get run
	run, err := s.store.GetRun(ctx, exec.RunID)
	if err == nil {
		var duration *time.Duration
		if run.FinalizedAt != nil && run.StartedAt != nil {
			d := run.FinalizedAt.Sub(*run.StartedAt)
			duration = &d
		}
		// Look up agent name from ID
		runAgentName := ""
		if agent, agentErr := s.store.GetAgent(ctx, run.AgentID); agentErr == nil {
			runAgentName = agent.Name
		}
		detail.Run = &RunSummary{
			ID:             run.ID,
			SessionID:      run.SessionID,
			AgentName:      runAgentName,
			RunMode:        run.RunMode,
			State:          string(run.State),
			Depth:          run.Depth,
			IterationCount: run.IterationCount,
			TotalTokens:    run.InputTokens + run.OutputTokens,
			Duration:       duration,
			CreatedAt:      run.CreatedAt,
		}
	}

	// Get iteration
	iter, err := s.store.GetIteration(ctx, exec.IterationID)
	if err == nil {
		var duration *time.Duration
		if iter.CompletedAt != nil && iter.StartedAt != nil {
			d := iter.CompletedAt.Sub(*iter.StartedAt)
			duration = &d
		}
		detail.Iteration = &IterationSummary{
			ID:              iter.ID,
			RunID:           iter.RunID,
			IterationNumber: iter.IterationNumber,
			IsStreaming:     iter.IsStreaming,
			TriggerType:     iter.TriggerType,
			StopReason:      iter.StopReason,
			HasToolUse:      iter.HasToolUse,
			ToolCount:       iter.ToolExecutionCount,
			InputTokens:     iter.InputTokens,
			OutputTokens:    iter.OutputTokens,
			Duration:        duration,
			CreatedAt:       iter.CreatedAt,
		}
	}

	// Get child run if agent tool
	if exec.ChildRunID != nil {
		childRun, err := s.store.GetRun(ctx, *exec.ChildRunID)
		if err == nil {
			var duration *time.Duration
			if childRun.FinalizedAt != nil && childRun.StartedAt != nil {
				d := childRun.FinalizedAt.Sub(*childRun.StartedAt)
				duration = &d
			}
			// Look up agent name from ID
			childAgentName := ""
			if agent, agentErr := s.store.GetAgent(ctx, childRun.AgentID); agentErr == nil {
				childAgentName = agent.Name
			}
			detail.ChildRun = &RunSummary{
				ID:             childRun.ID,
				SessionID:      childRun.SessionID,
				AgentName:      childAgentName,
				State:          string(childRun.State),
				Depth:          childRun.Depth,
				IterationCount: childRun.IterationCount,
				TotalTokens:    childRun.InputTokens + childRun.OutputTokens,
				Duration:       duration,
				CreatedAt:      childRun.CreatedAt,
			}
		}
	}

	return detail, nil
}
