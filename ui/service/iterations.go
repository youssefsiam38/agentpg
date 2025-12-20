package service

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/youssefsiam38/agentpg/driver"
)

// ListIterations returns iterations for a run.
func (s *Service[TTx]) ListIterations(ctx context.Context, runID uuid.UUID) ([]*IterationSummary, error) {
	iterations, err := s.store.GetIterationsByRun(ctx, runID)
	if err != nil {
		return nil, err
	}

	summaries := make([]*IterationSummary, 0, len(iterations))
	for _, iter := range iterations {
		var duration *time.Duration
		if iter.CompletedAt != nil && iter.StartedAt != nil {
			d := iter.CompletedAt.Sub(*iter.StartedAt)
			duration = &d
		}
		summaries = append(summaries, &IterationSummary{
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
			ErrorMessage:    iter.ErrorMessage,
			CreatedAt:       iter.CreatedAt,
			CompletedAt:     iter.CompletedAt,
		})
	}

	return summaries, nil
}

// GetIteration returns an iteration by ID.
func (s *Service[TTx]) GetIteration(ctx context.Context, id uuid.UUID) (*driver.Iteration, error) {
	return s.store.GetIteration(ctx, id)
}

// GetIterationDetail returns detailed information about an iteration.
func (s *Service[TTx]) GetIterationDetail(ctx context.Context, id uuid.UUID) (*IterationDetail, error) {
	iter, err := s.store.GetIteration(ctx, id)
	if err != nil {
		return nil, err
	}

	detail := &IterationDetail{
		Iteration: iter,
	}

	// Get run info
	run, err := s.store.GetRun(ctx, iter.RunID)
	if err == nil {
		var duration *time.Duration
		if run.FinalizedAt != nil && run.StartedAt != nil {
			d := run.FinalizedAt.Sub(*run.StartedAt)
			duration = &d
		}
		detail.Run = &RunSummary{
			ID:             run.ID,
			SessionID:      run.SessionID,
			AgentName:      run.AgentName,
			RunMode:        run.RunMode,
			State:          string(run.State),
			Depth:          run.Depth,
			IterationCount: run.IterationCount,
			TotalTokens:    run.InputTokens + run.OutputTokens,
			Duration:       duration,
			CreatedAt:      run.CreatedAt,
		}
	}

	// Get tool executions for this iteration
	toolExecs, err := s.store.GetToolExecutionsByIteration(ctx, id)
	if err == nil {
		for _, exec := range toolExecs {
			var duration *time.Duration
			if exec.CompletedAt != nil && exec.StartedAt != nil {
				d := exec.CompletedAt.Sub(*exec.StartedAt)
				duration = &d
			}
			detail.ToolExecutions = append(detail.ToolExecutions, &ToolExecutionSummary{
				ID:           exec.ID,
				RunID:        exec.RunID,
				IterationID:  exec.IterationID,
				ToolName:     exec.ToolName,
				State:        string(exec.State),
				IsAgentTool:  exec.IsAgentTool,
				AgentName:    exec.AgentName,
				ChildRunID:   exec.ChildRunID,
				IsError:      exec.IsError,
				AttemptCount: exec.AttemptCount,
				MaxAttempts:  exec.MaxAttempts,
				Duration:     duration,
				CreatedAt:    exec.CreatedAt,
				CompletedAt:  exec.CompletedAt,
			})
		}
	}

	return detail, nil
}

// GetIterationTimeline returns iterations with their tool executions for visualization.
func (s *Service[TTx]) GetIterationTimeline(ctx context.Context, runID uuid.UUID) ([]*IterationTimelineItem, error) {
	iterations, err := s.store.GetIterationsByRun(ctx, runID)
	if err != nil {
		return nil, err
	}

	items := make([]*IterationTimelineItem, 0, len(iterations))
	for _, iter := range iterations {
		var duration *time.Duration
		if iter.CompletedAt != nil && iter.StartedAt != nil {
			d := iter.CompletedAt.Sub(*iter.StartedAt)
			duration = &d
		}

		item := &IterationTimelineItem{
			Iteration: &IterationSummary{
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
				ErrorMessage:    iter.ErrorMessage,
				CreatedAt:       iter.CreatedAt,
				CompletedAt:     iter.CompletedAt,
			},
		}

		// Get tool executions
		toolExecs, err := s.store.GetToolExecutionsByIteration(ctx, iter.ID)
		if err == nil {
			for _, exec := range toolExecs {
				var execDuration *time.Duration
				if exec.CompletedAt != nil && exec.StartedAt != nil {
					d := exec.CompletedAt.Sub(*exec.StartedAt)
					execDuration = &d
				}
				item.ToolExecutions = append(item.ToolExecutions, &ToolExecutionSummary{
					ID:           exec.ID,
					RunID:        exec.RunID,
					IterationID:  exec.IterationID,
					ToolName:     exec.ToolName,
					State:        string(exec.State),
					IsAgentTool:  exec.IsAgentTool,
					AgentName:    exec.AgentName,
					ChildRunID:   exec.ChildRunID,
					IsError:      exec.IsError,
					AttemptCount: exec.AttemptCount,
					MaxAttempts:  exec.MaxAttempts,
					Duration:     execDuration,
					CreatedAt:    exec.CreatedAt,
					CompletedAt:  exec.CompletedAt,
				})
			}
		}

		items = append(items, item)
	}

	return items, nil
}

// IterationTimelineItem represents an iteration with its tool executions for timeline view.
type IterationTimelineItem struct {
	Iteration      *IterationSummary
	ToolExecutions []*ToolExecutionSummary
}
