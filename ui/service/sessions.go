package service

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/youssefsiam38/agentpg/driver"
)

// ListSessions returns a paginated list of sessions.
func (s *Service[TTx]) ListSessions(ctx context.Context, params SessionListParams) (*SessionList, error) {
	// Apply validation and defaults
	if params.Limit <= 0 {
		params.Limit = 25
	}
	params.Limit = ValidateLimit(params.Limit)
	params.Offset = ValidateOffset(params.Offset)
	params.OrderBy = ValidateOrderBy(params.OrderBy, AllowedSessionOrderBy)
	params.OrderDir = ValidateOrderDir(params.OrderDir)

	// Use the driver's ListSessions method with filtering and pagination
	driverSessions, total, err := s.store.ListSessions(ctx, driver.ListSessionsParams{
		MetadataFilter: params.MetadataFilter,
		Limit:          params.Limit,
		Offset:         params.Offset,
		OrderBy:        params.OrderBy,
		OrderDir:       params.OrderDir,
	})
	if err != nil {
		return nil, err
	}

	// Convert to summaries with additional computed fields
	summaries := make([]*SessionSummary, 0, len(driverSessions))
	for _, session := range driverSessions {
		summary := &SessionSummary{
			ID:              session.ID,
			Metadata:        session.Metadata,
			Depth:           session.Depth,
			CompactionCount: session.CompactionCount,
			CreatedAt:       session.CreatedAt,
			LastActivityAt:  session.UpdatedAt, // Use UpdatedAt as last activity
		}

		// Get run count for this session
		runs, err := s.store.GetRunsBySession(ctx, session.ID, 1000)
		if err == nil {
			summary.RunCount = len(runs)
			// Get agent name from the first run (oldest = last in the slice since ordered by created_at desc)
			if len(runs) > 0 {
				firstRun := runs[len(runs)-1]
				if agent, agentErr := s.store.GetAgent(ctx, firstRun.AgentID); agentErr == nil {
					summary.AgentName = agent.Name
				}
			}
			// Update last activity from most recent run
			for _, run := range runs {
				if run.CreatedAt.After(summary.LastActivityAt) {
					summary.LastActivityAt = run.CreatedAt
				}
				if run.FinalizedAt != nil && run.FinalizedAt.After(summary.LastActivityAt) {
					summary.LastActivityAt = *run.FinalizedAt
				}
			}
		}

		// Get message count for this session
		messages, err := s.store.GetMessages(ctx, session.ID, 1000)
		if err == nil {
			summary.MessageCount = len(messages)
		}

		summaries = append(summaries, summary)
	}

	return &SessionList{
		Sessions:   summaries,
		TotalCount: total,
		HasMore:    params.Offset+len(summaries) < total,
	}, nil
}

// GetSession returns a session by ID.
func (s *Service[TTx]) GetSession(ctx context.Context, id uuid.UUID) (*driver.Session, error) {
	return s.store.GetSession(ctx, id)
}

// GetSessionDetail returns detailed information about a session.
func (s *Service[TTx]) GetSessionDetail(ctx context.Context, id uuid.UUID) (*SessionDetail, error) {
	session, err := s.store.GetSession(ctx, id)
	if err != nil {
		return nil, err
	}

	detail := &SessionDetail{
		Session: session,
	}

	// Get runs for this session
	runs, err := s.store.GetRunsBySession(ctx, id, 100)
	if err == nil {
		detail.RunCount = len(runs)

		// Calculate token usage
		for _, run := range runs {
			detail.TokenUsage.InputTokens += run.InputTokens
			detail.TokenUsage.OutputTokens += run.OutputTokens
		}
		detail.TokenUsage.TotalTokens = detail.TokenUsage.InputTokens + detail.TokenUsage.OutputTokens

		// Calculate cache hit rate
		totalCacheTokens := 0
		for _, run := range runs {
			totalCacheTokens += run.CacheReadInputTokens
		}
		if detail.TokenUsage.InputTokens > 0 {
			detail.TokenUsage.CacheHitRate = float64(totalCacheTokens) / float64(detail.TokenUsage.InputTokens)
		}

		// Recent runs
		for i, run := range runs {
			if i >= 10 {
				break
			}
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
			detail.RecentRuns = append(detail.RecentRuns, &RunSummary{
				ID:             run.ID,
				SessionID:      run.SessionID,
				AgentName:      runAgentName,
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
	}

	// Get messages for this session
	messages, err := s.store.GetMessages(ctx, id, 100)
	if err == nil {
		detail.MessageCount = len(messages)

		// Recent messages
		for i, msg := range messages {
			if i >= 5 {
				break
			}
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

			detail.RecentMessages = append(detail.RecentMessages, &MessageSummary{
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

	// Get parent session if exists
	if session.ParentSessionID != nil {
		parent, parentErr := s.store.GetSession(ctx, *session.ParentSessionID)
		if parentErr == nil {
			detail.ParentSession = &SessionSummary{
				ID:              parent.ID,
				Metadata:        parent.Metadata,
				Depth:           parent.Depth,
				CompactionCount: parent.CompactionCount,
				CreatedAt:       parent.CreatedAt,
			}
		}
	}

	// Get full conversation for the session
	conversation, err := s.GetConversation(ctx, id, 100)
	if err == nil {
		detail.Conversation = conversation
	}

	return detail, nil
}

// CreateSession creates a new session.
func (s *Service[TTx]) CreateSession(ctx context.Context, req CreateSessionRequest) (*driver.Session, error) {
	return s.store.CreateSession(ctx, driver.CreateSessionParams{
		Metadata: req.Metadata,
	})
}

// GetMetadataValues returns all unique values for a metadata key with session counts.
// Used for building filter dropdowns in the UI.
func (s *Service[TTx]) GetMetadataValues(ctx context.Context, key string) ([]*MetadataFilterOption, error) {
	driverValues, err := s.store.GetMetadataValues(ctx, key)
	if err != nil {
		return nil, err
	}

	options := make([]*MetadataFilterOption, 0, len(driverValues))
	for _, v := range driverValues {
		options = append(options, &MetadataFilterOption{
			Value:        v.Value,
			SessionCount: v.SessionCount,
		})
	}

	return options, nil
}
