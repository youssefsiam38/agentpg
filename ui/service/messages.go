package service

import (
	"context"

	"github.com/google/uuid"
	"github.com/youssefsiam38/agentpg/driver"
)

// GetConversation returns the conversation for a session.
func (s *Service[TTx]) GetConversation(ctx context.Context, sessionID uuid.UUID, limit int) (*ConversationView, error) {
	if limit <= 0 {
		limit = 100
	}

	session, err := s.store.GetSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	messages, err := s.store.GetMessages(ctx, sessionID, limit)
	if err != nil {
		return nil, err
	}

	// Get the agent name from the first run in the session
	var agentName string
	runs, err := s.store.GetRunsBySession(ctx, sessionID, 1000)
	if err == nil && len(runs) > 0 {
		// Runs are ordered by created_at DESC, so first run is last in slice
		agentName = runs[len(runs)-1].AgentName
	}

	view := &ConversationView{
		SessionID: sessionID,
		AgentName: agentName,
		Session: &SessionSummary{
			ID:         session.ID,
			TenantID:   session.TenantID,
			Identifier: session.Identifier,
			AgentName:  agentName,
			Depth:      session.Depth,
			CreatedAt:  session.CreatedAt,
		},
		MessageCount: len(messages),
	}

	// Get content blocks for each message
	for _, msg := range messages {
		blocks, _ := s.store.GetContentBlocks(ctx, msg.ID)

		// Get run info if available
		var runInfo *RunSummary
		if msg.RunID != nil {
			run, err := s.store.GetRun(ctx, *msg.RunID)
			if err == nil {
				runInfo = &RunSummary{
					ID:        run.ID,
					AgentName: run.AgentName,
					State:     string(run.State),
					CreatedAt: run.CreatedAt,
				}
			}
		}

		view.Messages = append(view.Messages, &MessageWithBlocks{
			Message:       msg,
			ContentBlocks: blocks,
			RunInfo:       runInfo,
		})

		view.TotalTokens += msg.Usage.InputTokens + msg.Usage.OutputTokens
	}

	return view, nil
}

// GetMessage returns a message by ID with its content blocks.
func (s *Service[TTx]) GetMessage(ctx context.Context, id uuid.UUID) (*MessageWithBlocks, error) {
	msg, err := s.store.GetMessage(ctx, id)
	if err != nil {
		return nil, err
	}

	blocks, err := s.store.GetContentBlocks(ctx, id)
	if err != nil {
		blocks = []driver.ContentBlock{}
	}

	var runInfo *RunSummary
	if msg.RunID != nil {
		run, err := s.store.GetRun(ctx, *msg.RunID)
		if err == nil {
			runInfo = &RunSummary{
				ID:        run.ID,
				AgentName: run.AgentName,
				State:     string(run.State),
				CreatedAt: run.CreatedAt,
			}
		}
	}

	return &MessageWithBlocks{
		Message:       msg,
		ContentBlocks: blocks,
		RunInfo:       runInfo,
	}, nil
}
