package service

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/youssefsiam38/agentpg/driver"
)

// ListRuns returns a paginated list of runs.
func (s *Service[TTx]) ListRuns(ctx context.Context, params RunListParams) (*RunList, error) {
	// Apply validation and defaults
	if params.Limit <= 0 {
		params.Limit = 25
	}
	params.Limit = ValidateLimit(params.Limit)
	params.Offset = ValidateOffset(params.Offset)
	params.OrderBy = ValidateOrderBy(params.OrderBy, AllowedRunOrderBy)
	params.OrderDir = ValidateOrderDir(params.OrderDir)

	// Use the driver's ListRuns method with filtering and pagination
	runs, total, err := s.store.ListRuns(ctx, driver.ListRunsParams{
		TenantID:  params.TenantID,
		SessionID: params.SessionID,
		AgentName: params.AgentName,
		State:     params.State,
		RunMode:   params.RunMode,
		Limit:     params.Limit,
		Offset:    params.Offset,
	})
	if err != nil {
		return nil, err
	}

	// Convert to summaries
	summaries := make([]*RunSummary, 0, len(runs))
	for _, run := range runs {
		var duration *time.Duration
		if run.FinalizedAt != nil && run.StartedAt != nil {
			d := run.FinalizedAt.Sub(*run.StartedAt)
			duration = &d
		}
		summaries = append(summaries, &RunSummary{
			ID:             run.ID,
			SessionID:      run.SessionID,
			AgentName:      run.AgentName,
			RunMode:        run.RunMode,
			State:          string(run.State),
			Depth:          run.Depth,
			HasParent:      run.ParentRunID != nil,
			IterationCount: run.IterationCount,
			ToolIterations: run.ToolIterations,
			TotalTokens:    run.InputTokens + run.OutputTokens,
			Duration:       duration,
			ErrorMessage:   run.ErrorMessage,
			CreatedAt:      run.CreatedAt,
			FinalizedAt:    run.FinalizedAt,
		})
	}

	return &RunList{
		Runs:       summaries,
		TotalCount: total,
		HasMore:    params.Offset+len(summaries) < total,
	}, nil
}

// GetRun returns a run by ID.
func (s *Service[TTx]) GetRun(ctx context.Context, id uuid.UUID) (*driver.Run, error) {
	return s.store.GetRun(ctx, id)
}

// GetRunDetail returns detailed information about a run.
func (s *Service[TTx]) GetRunDetail(ctx context.Context, id uuid.UUID) (*RunDetail, error) {
	run, err := s.store.GetRun(ctx, id)
	if err != nil {
		return nil, err
	}

	detail := &RunDetail{
		Run:            run,
		HierarchyDepth: run.Depth,
	}

	// Get session summary
	session, err := s.store.GetSession(ctx, run.SessionID)
	if err == nil {
		detail.Session = &SessionSummary{
			ID:              session.ID,
			TenantID:        session.TenantID,
			UserID:          session.UserID,
			Depth:           session.Depth,
			CompactionCount: session.CompactionCount,
			CreatedAt:       session.CreatedAt,
		}
	}

	// Get iterations
	iterations, err := s.store.GetIterationsByRun(ctx, id)
	if err == nil {
		for _, iter := range iterations {
			var duration *time.Duration
			if iter.CompletedAt != nil && iter.StartedAt != nil {
				d := iter.CompletedAt.Sub(*iter.StartedAt)
				duration = &d
			}
			detail.Iterations = append(detail.Iterations, &IterationSummary{
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
	}

	// Get tool executions
	toolExecs, err := s.store.GetToolExecutionsByRun(ctx, id)
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

	// Get messages for this run
	messages, err := s.store.GetMessagesByRun(ctx, id)
	if err == nil {
		for _, msg := range messages {
			blocks, _ := s.store.GetContentBlocks(ctx, msg.ID)

			preview := ""
			hasToolUse := false
			hasToolResult := false

			for _, block := range blocks {
				if block.Type == "text" && preview == "" {
					preview = block.Text
					if len(preview) > 100 {
						preview = preview[:100] + "..."
					}
				}
				if block.Type == "tool_use" {
					hasToolUse = true
				}
				if block.Type == "tool_result" {
					hasToolResult = true
				}
			}

			detail.Messages = append(detail.Messages, &MessageSummary{
				ID:            msg.ID,
				SessionID:     msg.SessionID,
				RunID:         msg.RunID,
				Role:          string(msg.Role),
				PreviewText:   preview,
				BlockCount:    len(blocks),
				HasToolUse:    hasToolUse,
				HasToolResult: hasToolResult,
				IsPreserved:   msg.IsPreserved,
				IsSummary:     msg.IsSummary,
				TotalTokens:   msg.Usage.InputTokens + msg.Usage.OutputTokens,
				CreatedAt:     msg.CreatedAt,
			})
		}
	}

	// Get parent run if exists
	if run.ParentRunID != nil {
		parent, err := s.store.GetRun(ctx, *run.ParentRunID)
		if err == nil {
			var duration *time.Duration
			if parent.FinalizedAt != nil && parent.StartedAt != nil {
				d := parent.FinalizedAt.Sub(*parent.StartedAt)
				duration = &d
			}
			detail.ParentRun = &RunSummary{
				ID:             parent.ID,
				SessionID:      parent.SessionID,
				AgentName:      parent.AgentName,
				RunMode:        parent.RunMode,
				State:          string(parent.State),
				Depth:          parent.Depth,
				IterationCount: parent.IterationCount,
				TotalTokens:    parent.InputTokens + parent.OutputTokens,
				Duration:       duration,
				CreatedAt:      parent.CreatedAt,
				FinalizedAt:    parent.FinalizedAt,
			}
		}
	}

	// Get child runs by looking at tool executions with child_run_id
	// (reuse toolExecs from above if already fetched, otherwise fetch again)
	if toolExecs == nil {
		toolExecs, _ = s.store.GetToolExecutionsByRun(ctx, id)
	}
	for _, exec := range toolExecs {
		if exec.ChildRunID != nil {
			childRun, childErr := s.store.GetRun(ctx, *exec.ChildRunID)
			if childErr == nil && childRun != nil {
				var childDuration *time.Duration
				if childRun.FinalizedAt != nil && childRun.StartedAt != nil {
					d := childRun.FinalizedAt.Sub(*childRun.StartedAt)
					childDuration = &d
				}
				detail.ChildRuns = append(detail.ChildRuns, &RunSummary{
					ID:             childRun.ID,
					SessionID:      childRun.SessionID,
					AgentName:      childRun.AgentName,
					RunMode:        childRun.RunMode,
					State:          string(childRun.State),
					Depth:          childRun.Depth,
					HasParent:      childRun.ParentRunID != nil,
					IterationCount: childRun.IterationCount,
					ToolIterations: childRun.ToolIterations,
					TotalTokens:    childRun.InputTokens + childRun.OutputTokens,
					Duration:       childDuration,
					ErrorMessage:   childRun.ErrorMessage,
					CreatedAt:      childRun.CreatedAt,
					FinalizedAt:    childRun.FinalizedAt,
				})
			}
		}
	}

	return detail, nil
}

// GetRunHierarchy returns the hierarchy of runs starting from the root.
func (s *Service[TTx]) GetRunHierarchy(ctx context.Context, id uuid.UUID) (*RunHierarchy, error) {
	run, err := s.store.GetRun(ctx, id)
	if err != nil {
		return nil, err
	}

	// Find root run
	rootRun := run
	for rootRun.ParentRunID != nil {
		parent, err := s.store.GetRun(ctx, *rootRun.ParentRunID)
		if err != nil {
			break
		}
		rootRun = parent
	}

	// Build tree starting from root
	root := s.buildRunNode(ctx, rootRun)

	return &RunHierarchy{Root: root}, nil
}

func (s *Service[TTx]) buildRunNode(ctx context.Context, run *driver.Run) *RunNode {
	var duration *time.Duration
	if run.FinalizedAt != nil && run.StartedAt != nil {
		d := run.FinalizedAt.Sub(*run.StartedAt)
		duration = &d
	}

	node := &RunNode{
		Run: &RunSummary{
			ID:             run.ID,
			SessionID:      run.SessionID,
			AgentName:      run.AgentName,
			RunMode:        run.RunMode,
			State:          string(run.State),
			Depth:          run.Depth,
			HasParent:      run.ParentRunID != nil,
			IterationCount: run.IterationCount,
			ToolIterations: run.ToolIterations,
			TotalTokens:    run.InputTokens + run.OutputTokens,
			Duration:       duration,
			ErrorMessage:   run.ErrorMessage,
			CreatedAt:      run.CreatedAt,
			FinalizedAt:    run.FinalizedAt,
		},
	}

	// Get child runs by looking at tool executions with child_run_id
	toolExecs, err := s.store.GetToolExecutionsByRun(ctx, run.ID)
	if err == nil {
		for _, exec := range toolExecs {
			if exec.ChildRunID != nil {
				childRun, err := s.store.GetRun(ctx, *exec.ChildRunID)
				if err == nil {
					node.Children = append(node.Children, s.buildRunNode(ctx, childRun))
				}
			}
		}
	}

	return node
}

// GetRunIterations returns iterations for a run.
func (s *Service[TTx]) GetRunIterations(ctx context.Context, runID uuid.UUID) ([]*IterationSummary, error) {
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

// GetRunToolExecutions returns tool executions for a run.
func (s *Service[TTx]) GetRunToolExecutions(ctx context.Context, runID uuid.UUID) ([]*ToolExecutionSummary, error) {
	executions, err := s.store.GetToolExecutionsByRun(ctx, runID)
	if err != nil {
		return nil, err
	}

	summaries := make([]*ToolExecutionSummary, 0, len(executions))
	for _, exec := range executions {
		var duration *time.Duration
		if exec.CompletedAt != nil && exec.StartedAt != nil {
			d := exec.CompletedAt.Sub(*exec.StartedAt)
			duration = &d
		}
		summaries = append(summaries, &ToolExecutionSummary{
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

	return summaries, nil
}

// GetRunMessages returns messages for a run.
func (s *Service[TTx]) GetRunMessages(ctx context.Context, runID uuid.UUID) ([]*MessageWithBlocks, error) {
	messages, err := s.store.GetMessagesByRun(ctx, runID)
	if err != nil {
		return nil, err
	}

	result := make([]*MessageWithBlocks, 0, len(messages))
	for _, msg := range messages {
		blocks, _ := s.store.GetContentBlocks(ctx, msg.ID)
		result = append(result, &MessageWithBlocks{
			Message:       msg,
			ContentBlocks: blocks,
		})
	}

	return result, nil
}
