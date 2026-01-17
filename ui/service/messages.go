package service

import (
	"context"

	"github.com/google/uuid"
	"github.com/youssefsiam38/agentpg/driver"
)

// GetConversation returns the conversation for a session.
// Only includes messages from root-level runs (depth=0) - nested agent conversations are hidden.
// Use GetHierarchicalConversation to see the full conversation with all nested agents.
func (s *Service[TTx]) GetConversation(ctx context.Context, sessionID uuid.UUID, limit int) (*ConversationView, error) {
	if limit <= 0 {
		limit = 100
	}

	session, err := s.store.GetSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	// Use GetMessagesWithRunInfo to get run depth info for filtering
	messagesWithInfo, err := s.store.GetMessagesWithRunInfo(ctx, sessionID, 0) // 0 = no limit, we filter below
	if err != nil {
		return nil, err
	}

	// Get the agent name from the first root run in the session
	var agentName string
	runs, err := s.store.GetRunsBySession(ctx, sessionID, 1000)
	if err == nil && len(runs) > 0 {
		// Runs are ordered by created_at DESC, so first run is last in slice
		firstRun := runs[len(runs)-1]
		if agent, agentErr := s.store.GetAgent(ctx, firstRun.AgentID); agentErr == nil {
			agentName = agent.Name
		}
	}

	// Build run lookup map for RunInfo
	runMap := make(map[uuid.UUID]*driver.Run)
	for _, r := range runs {
		runMap[r.ID] = r
	}

	view := &ConversationView{
		SessionID: sessionID,
		AgentName: agentName,
		Session: &SessionSummary{
			ID:        session.ID,
			Metadata:  session.Metadata,
			AgentName: agentName,
			Depth:     session.Depth,
			CreatedAt: session.CreatedAt,
		},
		ToolResults: make(map[string]driver.ContentBlock),
	}

	// First pass: build tool_use_id -> tool_result map from ALL messages (including nested)
	for _, msgInfo := range messagesWithInfo {
		for _, block := range msgInfo.Content {
			if block.Type == "tool_result" && block.ToolResultForUseID != "" {
				view.ToolResults[block.ToolResultForUseID] = block
			}
		}
	}

	// Also add completed tool execution outputs to ToolResults
	// This handles agent-as-tool where the result is in tool_execution.tool_output
	// before the tool_result message is created
	for _, run := range runs {
		state := string(run.State)
		if state != "completed" && state != "failed" && state != "cancelled" {
			execs, err := s.GetRunToolExecutions(ctx, run.ID)
			if err == nil {
				for _, exec := range execs {
					if exec.ToolUseID != "" && exec.State == "completed" {
						if _, exists := view.ToolResults[exec.ToolUseID]; !exists {
							fullExec, err := s.store.GetToolExecution(ctx, exec.ID)
							if err == nil && fullExec.ToolOutput != nil {
								view.ToolResults[exec.ToolUseID] = driver.ContentBlock{
									Type:               "tool_result",
									ToolResultForUseID: exec.ToolUseID,
									ToolContent:        *fullExec.ToolOutput,
									IsError:            fullExec.IsError,
								}
							}
						}
					}
				}
			}
		}
	}

	// Second pass: filter to ONLY include messages from root-level runs (depth=0)
	// Skip: nested runs (depth > 0), orphan messages (no run), unknown depth
	count := 0
	for _, msgInfo := range messagesWithInfo {
		// Only show messages from root-level runs (depth must be exactly 0)
		if msgInfo.RunDepth == nil || *msgInfo.RunDepth != 0 {
			continue
		}

		// Skip messages that only contain tool_result blocks
		if isToolResultOnlyMessage(msgInfo.Content) {
			continue
		}

		// Apply limit after filtering
		if limit > 0 && count >= limit {
			break
		}
		count++

		msgWithBlocks := &MessageWithBlocks{
			Message: &driver.Message{
				ID:          msgInfo.ID,
				SessionID:   msgInfo.SessionID,
				RunID:       msgInfo.RunID,
				Role:        msgInfo.Role,
				Content:     msgInfo.Content,
				Usage:       msgInfo.Usage,
				IsPreserved: msgInfo.IsPreserved,
				IsSummary:   msgInfo.IsSummary,
				Metadata:    msgInfo.Metadata,
				CreatedAt:   msgInfo.CreatedAt,
				UpdatedAt:   msgInfo.UpdatedAt,
			},
			ContentBlocks: msgInfo.Content,
		}

		// Add run info if available
		if msgInfo.RunID != nil {
			if run, ok := runMap[*msgInfo.RunID]; ok {
				msgWithBlocks.RunInfo = s.runToSummary(ctx, run)
			}
		}

		view.Messages = append(view.Messages, msgWithBlocks)
		view.TotalTokens += msgInfo.Usage.InputTokens + msgInfo.Usage.OutputTokens
	}

	view.MessageCount = len(view.Messages)

	return view, nil
}

// isToolResultOnlyMessage returns true if the message only contains tool_result blocks.
func isToolResultOnlyMessage(blocks []driver.ContentBlock) bool {
	if len(blocks) == 0 {
		return false
	}
	for _, block := range blocks {
		if block.Type != "tool_result" {
			return false
		}
	}
	return true
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
			// Look up agent name from ID
			runAgentName := ""
			if agent, agentErr := s.store.GetAgent(ctx, run.AgentID); agentErr == nil {
				runAgentName = agent.Name
			}
			runInfo = &RunSummary{
				ID:        run.ID,
				AgentName: runAgentName,
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

// GetHierarchicalConversation returns the conversation grouped by run hierarchy.
// Messages are organized into RunMessageGroups that form a tree structure based on
// the parent/child relationships of runs (agent-as-tool pattern).
// Supports any depth of nesting.
func (s *Service[TTx]) GetHierarchicalConversation(ctx context.Context, sessionID uuid.UUID, limit int) (*HierarchicalConversationView, error) {
	if limit <= 0 {
		limit = 100
	}

	session, err := s.store.GetSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	// Get messages with run info in a single query (avoids N+1)
	messagesWithInfo, err := s.store.GetMessagesWithRunInfo(ctx, sessionID, limit)
	if err != nil {
		return nil, err
	}

	// Get all runs for hierarchy building
	runs, err := s.store.GetRunsBySession(ctx, sessionID, 1000)
	if err != nil {
		return nil, err
	}

	// Reverse runs to get chronological order (oldest first)
	// GetRunsBySession returns DESC order, but we need ASC for display
	for i, j := 0, len(runs)-1; i < j; i, j = i+1, j-1 {
		runs[i], runs[j] = runs[j], runs[i]
	}

	// Build run lookup map
	runMap := make(map[uuid.UUID]*driver.Run)
	for _, r := range runs {
		runMap[r.ID] = r
	}

	// Get agent name from first root run (runs are now in chronological order)
	var agentName string
	for _, r := range runs {
		if r.Depth == 0 {
			if agent, agentErr := s.store.GetAgent(ctx, r.AgentID); agentErr == nil {
				agentName = agent.Name
			}
			break
		}
	}

	// First pass: build tool_use_id -> tool_result map
	toolResults := make(map[string]driver.ContentBlock)
	for _, msgInfo := range messagesWithInfo {
		for _, block := range msgInfo.Content {
			if block.Type == "tool_result" && block.ToolResultForUseID != "" {
				toolResults[block.ToolResultForUseID] = block
			}
		}
	}

	// Second pass: group messages by run_id, skipping tool_result-only messages
	messagesByRun := make(map[uuid.UUID][]*MessageWithBlocks)
	var orphanMessages []*MessageWithBlocks
	totalTokens := 0
	messageCount := 0

	for _, msgInfo := range messagesWithInfo {
		// Skip messages that only contain tool_result blocks
		if isToolResultOnlyMessage(msgInfo.Content) {
			continue
		}
		messageCount++

		msgWithBlocks := &MessageWithBlocks{
			Message: &driver.Message{
				ID:          msgInfo.ID,
				SessionID:   msgInfo.SessionID,
				RunID:       msgInfo.RunID,
				Role:        msgInfo.Role,
				Content:     msgInfo.Content,
				Usage:       msgInfo.Usage,
				IsPreserved: msgInfo.IsPreserved,
				IsSummary:   msgInfo.IsSummary,
				Metadata:    msgInfo.Metadata,
				CreatedAt:   msgInfo.CreatedAt,
				UpdatedAt:   msgInfo.UpdatedAt,
			},
			ContentBlocks: msgInfo.Content,
		}

		// Add run info if available
		if msgInfo.RunID != nil {
			if run, ok := runMap[*msgInfo.RunID]; ok {
				msgWithBlocks.RunInfo = s.runToSummary(ctx, run)
			}
		}

		if msgInfo.RunID != nil {
			messagesByRun[*msgInfo.RunID] = append(messagesByRun[*msgInfo.RunID], msgWithBlocks)
		} else {
			orphanMessages = append(orphanMessages, msgWithBlocks)
		}

		totalTokens += msgInfo.Usage.InputTokens + msgInfo.Usage.OutputTokens
	}

	// Fetch tool executions for in-progress runs and add completed results to toolResults
	toolExecsByRun := make(map[uuid.UUID][]*ToolExecutionSummary)
	for _, run := range runs {
		state := string(run.State)
		// Fetch tool executions for in-progress runs
		if state != "completed" && state != "failed" && state != "cancelled" {
			execs, err := s.GetRunToolExecutions(ctx, run.ID)
			if err == nil && len(execs) > 0 {
				toolExecsByRun[run.ID] = execs

				// Add completed tool execution outputs to toolResults if not already present
				// This handles agent-as-tool where the result is in tool_execution.tool_output
				// before the tool_result message is created
				for _, exec := range execs {
					if exec.ToolUseID != "" && exec.State == "completed" {
						if _, exists := toolResults[exec.ToolUseID]; !exists {
							// Get full tool execution to access tool_output
							fullExec, err := s.store.GetToolExecution(ctx, exec.ID)
							if err == nil && fullExec.ToolOutput != nil {
								toolResults[exec.ToolUseID] = driver.ContentBlock{
									Type:               "tool_result",
									ToolResultForUseID: exec.ToolUseID,
									ToolContent:        *fullExec.ToolOutput,
									IsError:            fullExec.IsError,
								}
							}
						}
					}
				}
			}
		}
	}

	// Build hierarchical groups starting from root runs (depth=0)
	rootGroups := s.buildRunMessageGroupsAtDepth(ctx, runs, messagesByRun, toolExecsByRun, 0, nil)

	return &HierarchicalConversationView{
		SessionID: sessionID,
		Session: &SessionSummary{
			ID:        session.ID,
			Metadata:  session.Metadata,
			AgentName: agentName,
			Depth:     session.Depth,
			CreatedAt: session.CreatedAt,
		},
		AgentName:      agentName,
		RootGroups:     rootGroups,
		OrphanMessages: orphanMessages,
		TotalTokens:    totalTokens,
		MessageCount:   messageCount,
		ToolResults:    toolResults,
	}, nil
}

// buildRunMessageGroupsAtDepth recursively builds run message groups for runs at a given depth
// with the specified parent run ID. Supports unlimited depth.
func (s *Service[TTx]) buildRunMessageGroupsAtDepth(ctx context.Context, allRuns []*driver.Run, messagesByRun map[uuid.UUID][]*MessageWithBlocks, toolExecsByRun map[uuid.UUID][]*ToolExecutionSummary, depth int, parentRunID *uuid.UUID) []*RunMessageGroup {
	groups := make([]*RunMessageGroup, 0, len(allRuns))

	for _, run := range allRuns {
		// Match runs at this depth with the correct parent
		if run.Depth != depth {
			continue
		}

		// Check parent relationship
		if depth == 0 {
			// Root runs have no parent
			if run.ParentRunID != nil {
				continue
			}
		} else {
			// Child runs must match the parent ID
			if parentRunID == nil || run.ParentRunID == nil || *run.ParentRunID != *parentRunID {
				continue
			}
		}

		// Build set of ToolUseIDs already shown in messages to avoid duplication
		toolUseIDsInMessages := make(map[string]bool)
		for _, msg := range messagesByRun[run.ID] {
			for _, block := range msg.ContentBlocks {
				if block.Type == "tool_use" && block.ToolUseID != "" {
					toolUseIDsInMessages[block.ToolUseID] = true
				}
			}
		}

		// Filter out tool executions that already have tool_use blocks in messages
		var filteredExecs []*ToolExecutionSummary
		for _, exec := range toolExecsByRun[run.ID] {
			if exec.ToolUseID == "" || !toolUseIDsInMessages[exec.ToolUseID] {
				filteredExecs = append(filteredExecs, exec)
			}
		}

		group := &RunMessageGroup{
			Run:            s.runToSummary(ctx, run),
			Messages:       messagesByRun[run.ID],
			Depth:          run.Depth,
			ToolExecutions: filteredExecs, // Only tool executions not shown in messages
		}

		// Recursively build child groups (supports any depth)
		group.ChildGroups = s.buildRunMessageGroupsAtDepth(ctx, allRuns, messagesByRun, toolExecsByRun, depth+1, &run.ID)

		groups = append(groups, group)
	}

	return groups
}
