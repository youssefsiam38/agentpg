package databasesql

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/youssefsiam38/agentpg/driver"
	"github.com/youssefsiam38/agentpg/runstate"
	"github.com/youssefsiam38/agentpg/storage"
)

// Store implements storage.Store using the databasesql driver.
type Store struct {
	driver *Driver
}

// NewStore creates a new databasesql Store.
func NewStore(d *Driver) *Store {
	return &Store{driver: d}
}

// getExecutor returns the executor from context if present, otherwise the default pool executor.
func (s *Store) getExecutor(ctx context.Context) driver.Executor {
	if exec := driver.ExecutorFromContext(ctx); exec != nil {
		return exec
	}
	return s.driver.GetExecutor()
}

// CreateSession creates a new conversation session.
func (s *Store) CreateSession(ctx context.Context, tenantID, identifier string, parentSessionID *string, metadata map[string]any) (string, error) {
	if tenantID == "" {
		return "", fmt.Errorf("tenant_id is required")
	}
	if identifier == "" {
		return "", fmt.Errorf("identifier is required")
	}

	sessionID := uuid.New().String()

	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return "", fmt.Errorf("failed to marshal metadata: %w", err)
	}

	query := `
		INSERT INTO agentpg_sessions (id, tenant_id, identifier, parent_session_id, metadata, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, NOW(), NOW())
	`

	_, err = s.getExecutor(ctx).Exec(ctx, query, sessionID, tenantID, identifier, parentSessionID, metadataJSON)
	if err != nil {
		return "", fmt.Errorf("failed to create session: %w", err)
	}

	return sessionID, nil
}

// GetSession retrieves a session by ID.
func (s *Store) GetSession(ctx context.Context, sessionID string) (*storage.Session, error) {
	query := `
		SELECT id, tenant_id, identifier, parent_session_id, metadata, compaction_count,
		       created_at, updated_at
		FROM agentpg_sessions
		WHERE id = $1
	`

	var session storage.Session
	var metadataJSON []byte

	row := s.getExecutor(ctx).QueryRow(ctx, query, sessionID)
	err := row.Scan(
		&session.ID,
		&session.TenantID,
		&session.Identifier,
		&session.ParentSessionID,
		&metadataJSON,
		&session.CompactionCount,
		&session.CreatedAt,
		&session.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get session: %w", err)
	}

	if err := json.Unmarshal(metadataJSON, &session.Metadata); err != nil {
		return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
	}

	return &session, nil
}

// GetSessionsByTenant retrieves all sessions for a tenant.
func (s *Store) GetSessionsByTenant(ctx context.Context, tenantID string) ([]*storage.Session, error) {
	query := `
		SELECT id, tenant_id, identifier, parent_session_id, metadata, compaction_count,
		       created_at, updated_at
		FROM agentpg_sessions
		WHERE tenant_id = $1
		ORDER BY updated_at DESC
	`

	rows, err := s.getExecutor(ctx).Query(ctx, query, tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to query sessions: %w", err)
	}
	defer rows.Close()

	var sessions []*storage.Session
	for rows.Next() {
		var session storage.Session
		var metadataJSON []byte

		err := rows.Scan(
			&session.ID,
			&session.TenantID,
			&session.Identifier,
			&session.ParentSessionID,
			&metadataJSON,
			&session.CompactionCount,
			&session.CreatedAt,
			&session.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan session: %w", err)
		}

		if err := json.Unmarshal(metadataJSON, &session.Metadata); err != nil {
			return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
		}

		sessions = append(sessions, &session)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating sessions: %w", err)
	}

	return sessions, nil
}

// GetSessionByTenantAndIdentifier retrieves a session by tenant and identifier.
func (s *Store) GetSessionByTenantAndIdentifier(ctx context.Context, tenantID, identifier string) (*storage.Session, error) {
	query := `
		SELECT id, tenant_id, identifier, parent_session_id, metadata, compaction_count,
		       created_at, updated_at
		FROM agentpg_sessions
		WHERE tenant_id = $1 AND identifier = $2
	`

	var session storage.Session
	var metadataJSON []byte

	row := s.getExecutor(ctx).QueryRow(ctx, query, tenantID, identifier)
	err := row.Scan(
		&session.ID,
		&session.TenantID,
		&session.Identifier,
		&session.ParentSessionID,
		&metadataJSON,
		&session.CompactionCount,
		&session.CreatedAt,
		&session.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("session not found for tenant %s and identifier %s", tenantID, identifier)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get session: %w", err)
	}

	if err := json.Unmarshal(metadataJSON, &session.Metadata); err != nil {
		return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
	}

	return &session, nil
}

// GetSessionTokenCount calculates total tokens for a session from messages.
func (s *Store) GetSessionTokenCount(ctx context.Context, sessionID string) (int, error) {
	query := `
		SELECT COALESCE(
			SUM(
				COALESCE((usage->>'input_tokens')::INTEGER, 0) +
				COALESCE((usage->>'output_tokens')::INTEGER, 0)
			), 0
		)
		FROM agentpg_messages
		WHERE session_id = $1
	`

	var totalTokens int
	err := s.getExecutor(ctx).QueryRow(ctx, query, sessionID).Scan(&totalTokens)
	if err != nil {
		return 0, fmt.Errorf("failed to calculate session tokens: %w", err)
	}

	return totalTokens, nil
}

// UpdateSessionCompactionCount increments the compaction count.
func (s *Store) UpdateSessionCompactionCount(ctx context.Context, sessionID string) error {
	query := `
		UPDATE agentpg_sessions
		SET compaction_count = compaction_count + 1, updated_at = NOW()
		WHERE id = $1
	`

	_, err := s.getExecutor(ctx).Exec(ctx, query, sessionID)
	if err != nil {
		return fmt.Errorf("failed to update compaction count: %w", err)
	}

	return nil
}

// SaveMessage saves a single message.
func (s *Store) SaveMessage(ctx context.Context, msg *storage.Message) error {
	return s.SaveMessages(ctx, []*storage.Message{msg})
}

// SaveMessages saves multiple messages.
// Note: Content blocks are saved separately via SaveContentBlocks.
func (s *Store) SaveMessages(ctx context.Context, messages []*storage.Message) error {
	if len(messages) == 0 {
		return nil
	}

	query := `
		INSERT INTO agentpg_messages (id, session_id, run_id, role, usage, metadata,
		                     is_preserved, is_summary, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (id) DO UPDATE SET
			run_id = EXCLUDED.run_id,
			usage = EXCLUDED.usage,
			metadata = EXCLUDED.metadata,
			is_preserved = EXCLUDED.is_preserved,
			is_summary = EXCLUDED.is_summary,
			updated_at = NOW()
	`

	exec := s.getExecutor(ctx)

	// Execute messages sequentially (database/sql doesn't have native batching)
	for _, msg := range messages {
		usageJSON, err := json.Marshal(msg.Usage)
		if err != nil {
			return fmt.Errorf("failed to marshal usage: %w", err)
		}

		metadataJSON, err := json.Marshal(msg.Metadata)
		if err != nil {
			return fmt.Errorf("failed to marshal metadata: %w", err)
		}

		createdAt := msg.CreatedAt
		if createdAt.IsZero() {
			createdAt = time.Now()
		}

		updatedAt := msg.UpdatedAt
		if updatedAt.IsZero() {
			updatedAt = time.Now()
		}

		_, err = exec.Exec(ctx, query,
			msg.ID,
			msg.SessionID,
			msg.RunID,
			msg.Role,
			usageJSON,
			metadataJSON,
			msg.IsPreserved,
			msg.IsSummary,
			createdAt,
			updatedAt,
		)
		if err != nil {
			return fmt.Errorf("failed to save message: %w", err)
		}
	}

	return nil
}

// GetMessages retrieves all messages for a session ordered by creation time.
// Note: ContentBlocks are NOT populated by default. Use GetMessageContentBlocks separately.
func (s *Store) GetMessages(ctx context.Context, sessionID string) ([]*storage.Message, error) {
	query := `
		SELECT id, session_id, run_id, role, usage, metadata,
		       is_preserved, is_summary, created_at, updated_at
		FROM agentpg_messages
		WHERE session_id = $1
		ORDER BY created_at ASC
	`

	rows, err := s.getExecutor(ctx).Query(ctx, query, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to query messages: %w", err)
	}
	defer rows.Close()

	return s.scanMessagesWithRunID(rows)
}

// GetMessagesSince retrieves messages created after a specific time.
func (s *Store) GetMessagesSince(ctx context.Context, sessionID string, since time.Time) ([]*storage.Message, error) {
	query := `
		SELECT id, session_id, run_id, role, usage, metadata,
		       is_preserved, is_summary, created_at, updated_at
		FROM agentpg_messages
		WHERE session_id = $1 AND created_at > $2
		ORDER BY created_at ASC
	`

	rows, err := s.getExecutor(ctx).Query(ctx, query, sessionID, since)
	if err != nil {
		return nil, fmt.Errorf("failed to query messages: %w", err)
	}
	defer rows.Close()

	return s.scanMessagesWithRunID(rows)
}

// DeleteMessages deletes messages by their IDs.
func (s *Store) DeleteMessages(ctx context.Context, messageIDs []string) error {
	if len(messageIDs) == 0 {
		return nil
	}

	// Use pq.Array for PostgreSQL array parameter
	query := `DELETE FROM agentpg_messages WHERE id = ANY($1)`

	_, err := s.getExecutor(ctx).Exec(ctx, query, pq.Array(messageIDs))
	if err != nil {
		return fmt.Errorf("failed to delete messages: %w", err)
	}

	return nil
}

// SaveCompactionEvent saves a compaction event.
func (s *Store) SaveCompactionEvent(ctx context.Context, event *storage.CompactionEvent) error {
	if event.ID == "" {
		event.ID = uuid.New().String()
	}

	preservedIDsJSON, err := json.Marshal(event.PreservedMessageIDs)
	if err != nil {
		return fmt.Errorf("failed to marshal preserved IDs: %w", err)
	}

	query := `
		INSERT INTO agentpg_compaction_events
			(id, session_id, strategy, original_tokens, compacted_tokens,
			 messages_removed, summary_content, preserved_message_ids,
			 model_used, duration_ms, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, NOW())
	`

	_, err = s.getExecutor(ctx).Exec(ctx, query,
		event.ID,
		event.SessionID,
		event.Strategy,
		event.OriginalTokens,
		event.CompactedTokens,
		event.MessagesRemoved,
		event.SummaryContent,
		preservedIDsJSON,
		event.ModelUsed,
		event.DurationMs,
	)
	if err != nil {
		return fmt.Errorf("failed to save compaction event: %w", err)
	}

	return nil
}

// GetCompactionHistory retrieves compaction history for a session.
func (s *Store) GetCompactionHistory(ctx context.Context, sessionID string) ([]*storage.CompactionEvent, error) {
	query := `
		SELECT id, session_id, strategy, original_tokens, compacted_tokens,
		       messages_removed, summary_content, preserved_message_ids,
		       model_used, duration_ms, created_at
		FROM agentpg_compaction_events
		WHERE session_id = $1
		ORDER BY created_at DESC
	`

	rows, err := s.getExecutor(ctx).Query(ctx, query, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to query compaction history: %w", err)
	}
	defer rows.Close()

	var events []*storage.CompactionEvent

	for rows.Next() {
		var event storage.CompactionEvent
		var preservedIDsJSON []byte
		var summaryContent *string
		var modelUsed *string

		err := rows.Scan(
			&event.ID,
			&event.SessionID,
			&event.Strategy,
			&event.OriginalTokens,
			&event.CompactedTokens,
			&event.MessagesRemoved,
			&summaryContent,
			&preservedIDsJSON,
			&modelUsed,
			&event.DurationMs,
			&event.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan compaction event: %w", err)
		}

		if summaryContent != nil {
			event.SummaryContent = *summaryContent
		}

		if modelUsed != nil {
			event.ModelUsed = *modelUsed
		}

		if err := json.Unmarshal(preservedIDsJSON, &event.PreservedMessageIDs); err != nil {
			return nil, fmt.Errorf("failed to unmarshal preserved IDs: %w", err)
		}

		events = append(events, &event)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating compaction events: %w", err)
	}

	return events, nil
}

// ArchiveMessages archives messages that were removed during compaction.
func (s *Store) ArchiveMessages(ctx context.Context, compactionEventID string, messages []*storage.Message) error {
	if len(messages) == 0 {
		return nil
	}

	query := `
		INSERT INTO agentpg_message_archive (id, compaction_event_id, session_id, original_message, archived_at)
		VALUES ($1, $2, $3, $4, NOW())
	`

	exec := s.getExecutor(ctx)

	// Execute sequentially (database/sql doesn't have native batching)
	for _, msg := range messages {
		msgJSON, err := json.Marshal(msg)
		if err != nil {
			return fmt.Errorf("failed to marshal message: %w", err)
		}

		_, err = exec.Exec(ctx, query, msg.ID, compactionEventID, msg.SessionID, msgJSON)
		if err != nil {
			return fmt.Errorf("failed to archive message: %w", err)
		}
	}

	return nil
}

// =============================================================================
// Run operations
// =============================================================================

// CreateRun creates a new run record in the pending state.
// This triggers the agentpg_run_created notification.
func (s *Store) CreateRun(ctx context.Context, params *storage.CreateRunParams) (string, error) {
	runID := uuid.New().String()

	metadataJSON, err := json.Marshal(params.Metadata)
	if err != nil {
		return "", fmt.Errorf("failed to marshal metadata: %w", err)
	}

	query := `
		INSERT INTO agentpg_runs (id, session_id, agent_name, prompt, instance_id, metadata, state, started_at)
		VALUES ($1, $2, $3, $4, $5, $6, 'pending', NOW())
	`

	_, err = s.getExecutor(ctx).Exec(ctx, query,
		runID,
		params.SessionID,
		params.AgentName,
		params.Prompt,
		params.InstanceID,
		metadataJSON,
	)
	if err != nil {
		return "", fmt.Errorf("failed to create run: %w", err)
	}

	return runID, nil
}

// GetRun returns a run by ID.
func (s *Store) GetRun(ctx context.Context, runID string) (*storage.Run, error) {
	query := `
		SELECT id, session_id, state, agent_name, prompt, response_text, stop_reason,
		       input_tokens, output_tokens, iteration_count, tool_iterations,
		       error_message, error_type, instance_id, worker_instance_id,
		       last_api_call_at, continuation_required, metadata, started_at, finalized_at
		FROM agentpg_runs
		WHERE id = $1
	`

	var run storage.Run
	var metadataJSON []byte

	row := s.getExecutor(ctx).QueryRow(ctx, query, runID)
	err := row.Scan(
		&run.ID,
		&run.SessionID,
		&run.State,
		&run.AgentName,
		&run.Prompt,
		&run.ResponseText,
		&run.StopReason,
		&run.InputTokens,
		&run.OutputTokens,
		&run.IterationCount,
		&run.ToolIterations,
		&run.ErrorMessage,
		&run.ErrorType,
		&run.InstanceID,
		&run.WorkerInstanceID,
		&run.LastAPICallAt,
		&run.ContinuationRequired,
		&metadataJSON,
		&run.StartedAt,
		&run.FinalizedAt,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("run not found: %s", runID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get run: %w", err)
	}

	if err := json.Unmarshal(metadataJSON, &run.Metadata); err != nil {
		return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
	}

	return &run, nil
}

// GetSessionRuns returns all runs for a session, ordered by started_at DESC.
func (s *Store) GetSessionRuns(ctx context.Context, sessionID string) ([]*storage.Run, error) {
	query := `
		SELECT id, session_id, state, agent_name, prompt, response_text, stop_reason,
		       input_tokens, output_tokens, iteration_count, tool_iterations,
		       error_message, error_type, instance_id, worker_instance_id,
		       last_api_call_at, continuation_required, metadata, started_at, finalized_at
		FROM agentpg_runs
		WHERE session_id = $1
		ORDER BY started_at DESC
	`

	rows, err := s.getExecutor(ctx).Query(ctx, query, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to query runs: %w", err)
	}
	defer rows.Close()

	return s.scanRuns(rows)
}

// GetLatestSessionRun returns the most recent run for a session.
func (s *Store) GetLatestSessionRun(ctx context.Context, sessionID string) (*storage.Run, error) {
	query := `
		SELECT id, session_id, state, agent_name, prompt, response_text, stop_reason,
		       input_tokens, output_tokens, iteration_count, tool_iterations,
		       error_message, error_type, instance_id, worker_instance_id,
		       last_api_call_at, continuation_required, metadata, started_at, finalized_at
		FROM agentpg_runs
		WHERE session_id = $1
		ORDER BY started_at DESC
		LIMIT 1
	`

	var run storage.Run
	var metadataJSON []byte

	row := s.getExecutor(ctx).QueryRow(ctx, query, sessionID)
	err := row.Scan(
		&run.ID,
		&run.SessionID,
		&run.State,
		&run.AgentName,
		&run.Prompt,
		&run.ResponseText,
		&run.StopReason,
		&run.InputTokens,
		&run.OutputTokens,
		&run.IterationCount,
		&run.ToolIterations,
		&run.ErrorMessage,
		&run.ErrorType,
		&run.InstanceID,
		&run.WorkerInstanceID,
		&run.LastAPICallAt,
		&run.ContinuationRequired,
		&metadataJSON,
		&run.StartedAt,
		&run.FinalizedAt,
	)

	if err == sql.ErrNoRows {
		return nil, nil // No runs exist for this session
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get latest run: %w", err)
	}

	if err := json.Unmarshal(metadataJSON, &run.Metadata); err != nil {
		return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
	}

	return &run, nil
}

// UpdateRunState transitions a run to a new state.
// Supports transitions between workable states and to terminal states.
// If RequiredState is set, only updates if the current state matches (atomic transition).
func (s *Store) UpdateRunState(ctx context.Context, runID string, params *storage.UpdateRunStateParams) error {
	var query string
	var args []any

	// Build WHERE clause based on RequiredState
	whereClause := "WHERE id = $1 AND state NOT IN ('completed', 'cancelled', 'failed')"
	if params.RequiredState != "" {
		whereClause = "WHERE id = $1 AND state = $11 AND state NOT IN ('completed', 'cancelled', 'failed')"
	}

	if params.State.IsTerminal() {
		// Terminal state transitions
		query = fmt.Sprintf(`
			UPDATE agentpg_runs
			SET state = $2,
			    response_text = COALESCE($3, response_text),
			    stop_reason = COALESCE($4, stop_reason),
			    input_tokens = COALESCE(NULLIF($5, 0), input_tokens),
			    output_tokens = COALESCE(NULLIF($6, 0), output_tokens),
			    tool_iterations = COALESCE(NULLIF($7, 0), tool_iterations),
			    error_message = COALESCE($8, error_message),
			    error_type = COALESCE($9, error_type),
			    continuation_required = COALESCE($10, continuation_required),
			    finalized_at = NOW()
			%s
		`, whereClause)
		args = []any{
			runID,
			params.State,
			params.ResponseText,
			params.StopReason,
			params.InputTokens,
			params.OutputTokens,
			params.ToolIterations,
			params.ErrorMessage,
			params.ErrorType,
			params.ContinuationRequired,
		}
	} else {
		// Non-terminal state transitions (e.g., pending_api -> pending_tools)
		// Adjust parameter number for RequiredState
		nonTerminalWhereClause := "WHERE id = $1 AND state NOT IN ('completed', 'cancelled', 'failed')"
		if params.RequiredState != "" {
			nonTerminalWhereClause = "WHERE id = $1 AND state = $9 AND state NOT IN ('completed', 'cancelled', 'failed')"
		}
		query = fmt.Sprintf(`
			UPDATE agentpg_runs
			SET state = $2,
			    response_text = COALESCE($3, response_text),
			    stop_reason = COALESCE($4, stop_reason),
			    input_tokens = COALESCE(NULLIF($5, 0), input_tokens),
			    output_tokens = COALESCE(NULLIF($6, 0), output_tokens),
			    tool_iterations = COALESCE(NULLIF($7, 0), tool_iterations),
			    continuation_required = COALESCE($8, continuation_required)
			%s
		`, nonTerminalWhereClause)
		args = []any{
			runID,
			params.State,
			params.ResponseText,
			params.StopReason,
			params.InputTokens,
			params.OutputTokens,
			params.ToolIterations,
			params.ContinuationRequired,
		}
	}

	// Add RequiredState to args if specified
	if params.RequiredState != "" {
		args = append(args, params.RequiredState)
	}

	result, err := s.getExecutor(ctx).Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to update run state: %w", err)
	}

	if result == 0 {
		// Could be: run not found, already finalized, or RequiredState didn't match
		if params.RequiredState != "" {
			return storage.ErrStateTransitionFailed
		}
		return fmt.Errorf("run not found or already finalized: %s", runID)
	}

	return nil
}

// GetRunMessages returns all messages associated with a run.
func (s *Store) GetRunMessages(ctx context.Context, runID string) ([]*storage.Message, error) {
	query := `
		SELECT id, session_id, run_id, role, usage, metadata,
		       is_preserved, is_summary, created_at, updated_at
		FROM agentpg_messages
		WHERE run_id = $1
		ORDER BY created_at ASC
	`

	rows, err := s.getExecutor(ctx).Query(ctx, query, runID)
	if err != nil {
		return nil, fmt.Errorf("failed to query run messages: %w", err)
	}
	defer rows.Close()

	return s.scanMessagesWithRunID(rows)
}

// GetStuckRuns returns all runs that have been in workable states longer than the horizon.
// Used by the cleanup service to detect orphaned runs.
func (s *Store) GetStuckRuns(ctx context.Context, horizon time.Time) ([]*storage.Run, error) {
	query := `
		SELECT id, session_id, state, agent_name, prompt, response_text, stop_reason,
		       input_tokens, output_tokens, iteration_count, tool_iterations,
		       error_message, error_type, instance_id, worker_instance_id,
		       last_api_call_at, continuation_required, metadata, started_at, finalized_at
		FROM agentpg_runs
		WHERE state IN ('pending', 'pending_api', 'pending_tools', 'awaiting_continuation')
		  AND started_at < $1
		ORDER BY started_at ASC
	`

	rows, err := s.getExecutor(ctx).Query(ctx, query, horizon)
	if err != nil {
		return nil, fmt.Errorf("failed to query stuck runs: %w", err)
	}
	defer rows.Close()

	return s.scanRuns(rows)
}

// scanRuns is a helper to scan run rows.
func (s *Store) scanRuns(rows driver.Rows) ([]*storage.Run, error) {
	var runs []*storage.Run

	for rows.Next() {
		var run storage.Run
		var metadataJSON []byte

		err := rows.Scan(
			&run.ID,
			&run.SessionID,
			&run.State,
			&run.AgentName,
			&run.Prompt,
			&run.ResponseText,
			&run.StopReason,
			&run.InputTokens,
			&run.OutputTokens,
			&run.IterationCount,
			&run.ToolIterations,
			&run.ErrorMessage,
			&run.ErrorType,
			&run.InstanceID,
			&run.WorkerInstanceID,
			&run.LastAPICallAt,
			&run.ContinuationRequired,
			&metadataJSON,
			&run.StartedAt,
			&run.FinalizedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan run: %w", err)
		}

		if err := json.Unmarshal(metadataJSON, &run.Metadata); err != nil {
			return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
		}

		runs = append(runs, &run)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating runs: %w", err)
	}

	return runs, nil
}

// scanMessagesWithRunID is a helper to scan message rows including run_id.
// Note: Content blocks are NOT loaded by this function.
func (s *Store) scanMessagesWithRunID(rows driver.Rows) ([]*storage.Message, error) {
	var messages []*storage.Message

	for rows.Next() {
		var msg storage.Message
		var usageJSON []byte
		var metadataJSON []byte

		err := rows.Scan(
			&msg.ID,
			&msg.SessionID,
			&msg.RunID,
			&msg.Role,
			&usageJSON,
			&metadataJSON,
			&msg.IsPreserved,
			&msg.IsSummary,
			&msg.CreatedAt,
			&msg.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan message: %w", err)
		}

		if len(usageJSON) > 0 {
			msg.Usage = &storage.MessageUsage{}
			if err := json.Unmarshal(usageJSON, msg.Usage); err != nil {
				return nil, fmt.Errorf("failed to unmarshal usage: %w", err)
			}
		}

		if err := json.Unmarshal(metadataJSON, &msg.Metadata); err != nil {
			return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
		}

		messages = append(messages, &msg)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating messages: %w", err)
	}

	return messages, nil
}

// =============================================================================
// Instance operations
// =============================================================================

// RegisterInstance registers a new instance with the given ID and metadata.
func (s *Store) RegisterInstance(ctx context.Context, params *storage.RegisterInstanceParams) error {
	metadataJSON, err := json.Marshal(params.Metadata)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	query := `
		INSERT INTO agentpg_instances (id, hostname, pid, version, metadata, created_at, last_heartbeat_at)
		VALUES ($1, $2, $3, $4, $5, NOW(), NOW())
		ON CONFLICT (id) DO UPDATE SET
			hostname = EXCLUDED.hostname,
			pid = EXCLUDED.pid,
			version = EXCLUDED.version,
			metadata = EXCLUDED.metadata,
			last_heartbeat_at = NOW()
	`

	_, err = s.getExecutor(ctx).Exec(ctx, query,
		params.ID,
		params.Hostname,
		params.PID,
		params.Version,
		metadataJSON,
	)
	if err != nil {
		return fmt.Errorf("failed to register instance: %w", err)
	}

	return nil
}

// UpdateInstanceHeartbeat updates the last_heartbeat_at for an instance.
func (s *Store) UpdateInstanceHeartbeat(ctx context.Context, instanceID string) error {
	query := `
		UPDATE agentpg_instances
		SET last_heartbeat_at = NOW()
		WHERE id = $1
	`

	result, err := s.getExecutor(ctx).Exec(ctx, query, instanceID)
	if err != nil {
		return fmt.Errorf("failed to update heartbeat: %w", err)
	}

	if result == 0 {
		return fmt.Errorf("instance not found: %s", instanceID)
	}

	return nil
}

// GetStaleInstances returns instance IDs that haven't heartbeated since horizon.
func (s *Store) GetStaleInstances(ctx context.Context, horizon time.Time) ([]string, error) {
	query := `
		SELECT id FROM agentpg_instances
		WHERE last_heartbeat_at < $1
	`

	rows, err := s.getExecutor(ctx).Query(ctx, query, horizon)
	if err != nil {
		return nil, fmt.Errorf("failed to query stale instances: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("failed to scan instance id: %w", err)
		}
		ids = append(ids, id)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating instances: %w", err)
	}

	return ids, nil
}

// DeregisterInstance removes an instance and triggers orphan cleanup.
func (s *Store) DeregisterInstance(ctx context.Context, instanceID string) error {
	query := `DELETE FROM agentpg_instances WHERE id = $1`

	_, err := s.getExecutor(ctx).Exec(ctx, query, instanceID)
	if err != nil {
		return fmt.Errorf("failed to deregister instance: %w", err)
	}

	return nil
}

// GetInstance returns an instance by ID.
func (s *Store) GetInstance(ctx context.Context, instanceID string) (*storage.Instance, error) {
	query := `
		SELECT id, hostname, pid, version, metadata, created_at, last_heartbeat_at
		FROM agentpg_instances
		WHERE id = $1
	`

	var inst storage.Instance
	var metadataJSON []byte

	row := s.getExecutor(ctx).QueryRow(ctx, query, instanceID)
	err := row.Scan(
		&inst.ID,
		&inst.Hostname,
		&inst.PID,
		&inst.Version,
		&metadataJSON,
		&inst.CreatedAt,
		&inst.LastHeartbeatAt,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("instance not found: %s", instanceID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get instance: %w", err)
	}

	if err := json.Unmarshal(metadataJSON, &inst.Metadata); err != nil {
		return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
	}

	return &inst, nil
}

// GetActiveInstances returns all instances with heartbeat after horizon.
func (s *Store) GetActiveInstances(ctx context.Context, horizon time.Time) ([]*storage.Instance, error) {
	query := `
		SELECT id, hostname, pid, version, metadata, created_at, last_heartbeat_at
		FROM agentpg_instances
		WHERE last_heartbeat_at >= $1
		ORDER BY created_at DESC
	`

	rows, err := s.getExecutor(ctx).Query(ctx, query, horizon)
	if err != nil {
		return nil, fmt.Errorf("failed to query instances: %w", err)
	}
	defer rows.Close()

	var instances []*storage.Instance
	for rows.Next() {
		var inst storage.Instance
		var metadataJSON []byte

		err := rows.Scan(
			&inst.ID,
			&inst.Hostname,
			&inst.PID,
			&inst.Version,
			&metadataJSON,
			&inst.CreatedAt,
			&inst.LastHeartbeatAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan instance: %w", err)
		}

		if err := json.Unmarshal(metadataJSON, &inst.Metadata); err != nil {
			return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
		}

		instances = append(instances, &inst)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating instances: %w", err)
	}

	return instances, nil
}

// =============================================================================
// Leader election operations
// =============================================================================

// LeaderAttemptElect attempts to elect this instance as leader.
func (s *Store) LeaderAttemptElect(ctx context.Context, params *storage.LeaderElectParams) (bool, error) {
	now := time.Now()
	expiresAt := now.Add(params.TTL)

	query := `
		INSERT INTO agentpg_leader (name, leader_id, elected_at, expires_at)
		VALUES ('default', $1, $2, $3)
		ON CONFLICT (name) DO NOTHING
	`

	result, err := s.getExecutor(ctx).Exec(ctx, query, params.LeaderID, now, expiresAt)
	if err != nil {
		return false, fmt.Errorf("failed to attempt election: %w", err)
	}

	return result > 0, nil
}

// LeaderAttemptReelect attempts to renew leadership.
func (s *Store) LeaderAttemptReelect(ctx context.Context, params *storage.LeaderElectParams) (bool, error) {
	now := time.Now()
	expiresAt := now.Add(params.TTL)

	query := `
		UPDATE agentpg_leader
		SET elected_at = $2, expires_at = $3
		WHERE name = 'default' AND leader_id = $1
	`

	result, err := s.getExecutor(ctx).Exec(ctx, query, params.LeaderID, now, expiresAt)
	if err != nil {
		return false, fmt.Errorf("failed to attempt reelection: %w", err)
	}

	return result > 0, nil
}

// LeaderResign voluntarily gives up leadership.
func (s *Store) LeaderResign(ctx context.Context, leaderID string) error {
	query := `DELETE FROM agentpg_leader WHERE name = 'default' AND leader_id = $1`

	_, err := s.getExecutor(ctx).Exec(ctx, query, leaderID)
	if err != nil {
		return fmt.Errorf("failed to resign leadership: %w", err)
	}

	return nil
}

// LeaderDeleteExpired removes expired leader entries.
func (s *Store) LeaderDeleteExpired(ctx context.Context) (int, error) {
	query := `DELETE FROM agentpg_leader WHERE expires_at < NOW()`

	result, err := s.getExecutor(ctx).Exec(ctx, query)
	if err != nil {
		return 0, fmt.Errorf("failed to delete expired leader: %w", err)
	}

	return int(result), nil
}

// LeaderGetCurrent returns the current leader, or nil if none.
func (s *Store) LeaderGetCurrent(ctx context.Context) (*storage.Leader, error) {
	query := `
		SELECT name, leader_id, elected_at, expires_at
		FROM agentpg_leader
		WHERE name = 'default' AND expires_at > NOW()
	`

	var leader storage.Leader
	row := s.getExecutor(ctx).QueryRow(ctx, query)
	err := row.Scan(
		&leader.Name,
		&leader.LeaderID,
		&leader.ElectedAt,
		&leader.ExpiresAt,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get current leader: %w", err)
	}

	return &leader, nil
}

// =============================================================================
// Agent registration operations
// =============================================================================

// RegisterAgent upserts an agent definition.
func (s *Store) RegisterAgent(ctx context.Context, params *storage.RegisterAgentParams) error {
	configJSON, err := json.Marshal(params.Config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	query := `
		INSERT INTO agentpg_agents (name, description, model, system_prompt, max_tokens, temperature, config, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, NOW(), NOW())
		ON CONFLICT (name) DO UPDATE SET
			description = EXCLUDED.description,
			model = EXCLUDED.model,
			system_prompt = EXCLUDED.system_prompt,
			max_tokens = EXCLUDED.max_tokens,
			temperature = EXCLUDED.temperature,
			config = EXCLUDED.config,
			updated_at = NOW()
	`

	_, err = s.getExecutor(ctx).Exec(ctx, query,
		params.Name,
		params.Description,
		params.Model,
		params.SystemPrompt,
		params.MaxTokens,
		params.Temperature,
		configJSON,
	)
	if err != nil {
		return fmt.Errorf("failed to register agent: %w", err)
	}

	return nil
}

// RegisterInstanceAgent links an instance to an agent.
func (s *Store) RegisterInstanceAgent(ctx context.Context, instanceID, agentName string) error {
	query := `
		INSERT INTO agentpg_instance_agents (instance_id, agent_name, registered_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (instance_id, agent_name) DO NOTHING
	`

	_, err := s.getExecutor(ctx).Exec(ctx, query, instanceID, agentName)
	if err != nil {
		return fmt.Errorf("failed to register instance agent: %w", err)
	}

	return nil
}

// GetAgent returns an agent by name.
func (s *Store) GetAgent(ctx context.Context, name string) (*storage.RegisteredAgent, error) {
	query := `
		SELECT name, description, model, system_prompt, max_tokens, temperature, config, created_at, updated_at
		FROM agentpg_agents
		WHERE name = $1
	`

	var agent storage.RegisteredAgent
	var configJSON []byte

	row := s.getExecutor(ctx).QueryRow(ctx, query, name)
	err := row.Scan(
		&agent.Name,
		&agent.Description,
		&agent.Model,
		&agent.SystemPrompt,
		&agent.MaxTokens,
		&agent.Temperature,
		&configJSON,
		&agent.CreatedAt,
		&agent.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("agent not found: %s", name)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get agent: %w", err)
	}

	if err := json.Unmarshal(configJSON, &agent.Config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	return &agent, nil
}

// GetAvailableAgents returns all agents with at least one active instance.
func (s *Store) GetAvailableAgents(ctx context.Context, horizon time.Time) ([]*storage.RegisteredAgent, error) {
	query := `
		SELECT DISTINCT a.name, a.description, a.model, a.system_prompt, a.max_tokens, a.temperature, a.config, a.created_at, a.updated_at
		FROM agentpg_agents a
		JOIN agentpg_instance_agents ia ON a.name = ia.agent_name
		JOIN agentpg_instances i ON ia.instance_id = i.id
		WHERE i.last_heartbeat_at >= $1
		ORDER BY a.name
	`

	rows, err := s.getExecutor(ctx).Query(ctx, query, horizon)
	if err != nil {
		return nil, fmt.Errorf("failed to query available agents: %w", err)
	}
	defer rows.Close()

	var agents []*storage.RegisteredAgent
	for rows.Next() {
		var agent storage.RegisteredAgent
		var configJSON []byte

		err := rows.Scan(
			&agent.Name,
			&agent.Description,
			&agent.Model,
			&agent.SystemPrompt,
			&agent.MaxTokens,
			&agent.Temperature,
			&configJSON,
			&agent.CreatedAt,
			&agent.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan agent: %w", err)
		}

		if err := json.Unmarshal(configJSON, &agent.Config); err != nil {
			return nil, fmt.Errorf("failed to unmarshal config: %w", err)
		}

		agents = append(agents, &agent)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating agents: %w", err)
	}

	return agents, nil
}

// =============================================================================
// Tool registration operations
// =============================================================================

// RegisterTool upserts a tool definition.
func (s *Store) RegisterTool(ctx context.Context, params *storage.RegisterToolParams) error {
	schemaJSON, err := json.Marshal(params.InputSchema)
	if err != nil {
		return fmt.Errorf("failed to marshal input schema: %w", err)
	}

	metadataJSON, err := json.Marshal(params.Metadata)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	query := `
		INSERT INTO agentpg_tools (name, description, input_schema, metadata, created_at, updated_at)
		VALUES ($1, $2, $3, $4, NOW(), NOW())
		ON CONFLICT (name) DO UPDATE SET
			description = EXCLUDED.description,
			input_schema = EXCLUDED.input_schema,
			metadata = EXCLUDED.metadata,
			updated_at = NOW()
	`

	_, err = s.getExecutor(ctx).Exec(ctx, query,
		params.Name,
		params.Description,
		schemaJSON,
		metadataJSON,
	)
	if err != nil {
		return fmt.Errorf("failed to register tool: %w", err)
	}

	return nil
}

// RegisterInstanceTool links an instance to a tool.
func (s *Store) RegisterInstanceTool(ctx context.Context, instanceID, toolName string) error {
	query := `
		INSERT INTO agentpg_instance_tools (instance_id, tool_name, registered_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (instance_id, tool_name) DO NOTHING
	`

	_, err := s.getExecutor(ctx).Exec(ctx, query, instanceID, toolName)
	if err != nil {
		return fmt.Errorf("failed to register instance tool: %w", err)
	}

	return nil
}

// GetTool returns a tool by name.
func (s *Store) GetTool(ctx context.Context, name string) (*storage.RegisteredTool, error) {
	query := `
		SELECT name, description, input_schema, metadata, created_at, updated_at
		FROM agentpg_tools
		WHERE name = $1
	`

	var tool storage.RegisteredTool
	var schemaJSON []byte
	var metadataJSON []byte

	row := s.getExecutor(ctx).QueryRow(ctx, query, name)
	err := row.Scan(
		&tool.Name,
		&tool.Description,
		&schemaJSON,
		&metadataJSON,
		&tool.CreatedAt,
		&tool.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("tool not found: %s", name)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get tool: %w", err)
	}

	if err := json.Unmarshal(schemaJSON, &tool.InputSchema); err != nil {
		return nil, fmt.Errorf("failed to unmarshal input schema: %w", err)
	}

	if err := json.Unmarshal(metadataJSON, &tool.Metadata); err != nil {
		return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
	}

	return &tool, nil
}

// GetAvailableTools returns all tools with at least one active instance.
func (s *Store) GetAvailableTools(ctx context.Context, horizon time.Time) ([]*storage.RegisteredTool, error) {
	query := `
		SELECT DISTINCT t.name, t.description, t.input_schema, t.metadata, t.created_at, t.updated_at
		FROM agentpg_tools t
		JOIN agentpg_instance_tools it ON t.name = it.tool_name
		JOIN agentpg_instances i ON it.instance_id = i.id
		WHERE i.last_heartbeat_at >= $1
		ORDER BY t.name
	`

	rows, err := s.getExecutor(ctx).Query(ctx, query, horizon)
	if err != nil {
		return nil, fmt.Errorf("failed to query available tools: %w", err)
	}
	defer rows.Close()

	var tools []*storage.RegisteredTool
	for rows.Next() {
		var tool storage.RegisteredTool
		var schemaJSON []byte
		var metadataJSON []byte

		err := rows.Scan(
			&tool.Name,
			&tool.Description,
			&schemaJSON,
			&metadataJSON,
			&tool.CreatedAt,
			&tool.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan tool: %w", err)
		}

		if err := json.Unmarshal(schemaJSON, &tool.InputSchema); err != nil {
			return nil, fmt.Errorf("failed to unmarshal input schema: %w", err)
		}

		if err := json.Unmarshal(metadataJSON, &tool.Metadata); err != nil {
			return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
		}

		tools = append(tools, &tool)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating tools: %w", err)
	}

	return tools, nil
}

// =============================================================================
// Content Block operations
// =============================================================================

// SaveContentBlocks saves multiple content blocks atomically.
func (s *Store) SaveContentBlocks(ctx context.Context, blocks []*storage.ContentBlock) error {
	if len(blocks) == 0 {
		return nil
	}

	query := `
		INSERT INTO agentpg_content_blocks (
			id, message_id, block_index, type, text, tool_use_id, tool_name, tool_input,
			tool_result_for_id, tool_content, is_error, source, web_search_results, metadata, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
		ON CONFLICT (id) DO UPDATE SET
			block_index = EXCLUDED.block_index,
			type = EXCLUDED.type,
			text = EXCLUDED.text,
			tool_use_id = EXCLUDED.tool_use_id,
			tool_name = EXCLUDED.tool_name,
			tool_input = EXCLUDED.tool_input,
			tool_result_for_id = EXCLUDED.tool_result_for_id,
			tool_content = EXCLUDED.tool_content,
			is_error = EXCLUDED.is_error,
			source = EXCLUDED.source,
			web_search_results = EXCLUDED.web_search_results,
			metadata = EXCLUDED.metadata
	`

	exec := s.getExecutor(ctx)

	for _, block := range blocks {
		toolInputJSON, err := json.Marshal(block.ToolInput)
		if err != nil {
			return fmt.Errorf("failed to marshal tool input: %w", err)
		}

		sourceJSON, err := json.Marshal(block.Source)
		if err != nil {
			return fmt.Errorf("failed to marshal source: %w", err)
		}

		webSearchJSON, err := json.Marshal(block.WebSearchResults)
		if err != nil {
			return fmt.Errorf("failed to marshal web search results: %w", err)
		}

		metadataJSON, err := json.Marshal(block.Metadata)
		if err != nil {
			return fmt.Errorf("failed to marshal metadata: %w", err)
		}

		createdAt := block.CreatedAt
		if createdAt.IsZero() {
			createdAt = time.Now()
		}

		_, err = exec.Exec(ctx, query,
			block.ID,
			block.MessageID,
			block.BlockIndex,
			block.Type,
			block.Text,
			block.ToolUseID,
			block.ToolName,
			toolInputJSON,
			block.ToolResultForID,
			block.ToolContent,
			block.IsError,
			sourceJSON,
			webSearchJSON,
			metadataJSON,
			createdAt,
		)
		if err != nil {
			return fmt.Errorf("failed to save content block: %w", err)
		}
	}

	return nil
}

// GetMessageContentBlocks returns all content blocks for a message, ordered by block_index.
func (s *Store) GetMessageContentBlocks(ctx context.Context, messageID string) ([]*storage.ContentBlock, error) {
	query := `
		SELECT id, message_id, block_index, type, text, tool_use_id, tool_name, tool_input,
		       tool_result_for_id, tool_content, is_error, source, web_search_results,
		       metadata, created_at
		FROM agentpg_content_blocks
		WHERE message_id = $1
		ORDER BY block_index ASC
	`

	rows, err := s.getExecutor(ctx).Query(ctx, query, messageID)
	if err != nil {
		return nil, fmt.Errorf("failed to query content blocks: %w", err)
	}
	defer rows.Close()

	return s.scanContentBlocks(rows)
}

// GetToolUseBlock finds a tool_use content block by Claude's tool_use_id.
func (s *Store) GetToolUseBlock(ctx context.Context, toolUseID string) (*storage.ContentBlock, error) {
	query := `
		SELECT id, message_id, block_index, type, text, tool_use_id, tool_name, tool_input,
		       tool_result_for_id, tool_content, is_error, source, web_search_results,
		       metadata, created_at
		FROM agentpg_content_blocks
		WHERE tool_use_id = $1
	`

	var block storage.ContentBlock
	var toolInputJSON, sourceJSON, webSearchJSON, metadataJSON []byte

	row := s.getExecutor(ctx).QueryRow(ctx, query, toolUseID)
	err := row.Scan(
		&block.ID,
		&block.MessageID,
		&block.BlockIndex,
		&block.Type,
		&block.Text,
		&block.ToolUseID,
		&block.ToolName,
		&toolInputJSON,
		&block.ToolResultForID,
		&block.ToolContent,
		&block.IsError,
		&sourceJSON,
		&webSearchJSON,
		&metadataJSON,
		&block.CreatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("tool use block not found: %s", toolUseID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get tool use block: %w", err)
	}

	if len(toolInputJSON) > 0 {
		if err := json.Unmarshal(toolInputJSON, &block.ToolInput); err != nil {
			return nil, fmt.Errorf("failed to unmarshal tool input: %w", err)
		}
	}
	if len(sourceJSON) > 0 {
		if err := json.Unmarshal(sourceJSON, &block.Source); err != nil {
			return nil, fmt.Errorf("failed to unmarshal source: %w", err)
		}
	}
	if len(webSearchJSON) > 0 {
		if err := json.Unmarshal(webSearchJSON, &block.WebSearchResults); err != nil {
			return nil, fmt.Errorf("failed to unmarshal web search results: %w", err)
		}
	}
	if len(metadataJSON) > 0 {
		if err := json.Unmarshal(metadataJSON, &block.Metadata); err != nil {
			return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
		}
	}

	return &block, nil
}

// GetContentBlock returns a content block by its internal ID.
func (s *Store) GetContentBlock(ctx context.Context, blockID string) (*storage.ContentBlock, error) {
	query := `
		SELECT id, message_id, block_index, type, text, tool_use_id, tool_name, tool_input,
		       tool_result_for_id, tool_content, is_error, source, web_search_results,
		       metadata, created_at
		FROM agentpg_content_blocks
		WHERE id = $1
	`

	var block storage.ContentBlock
	var toolInputJSON, sourceJSON, webSearchJSON, metadataJSON []byte

	row := s.getExecutor(ctx).QueryRow(ctx, query, blockID)
	err := row.Scan(
		&block.ID,
		&block.MessageID,
		&block.BlockIndex,
		&block.Type,
		&block.Text,
		&block.ToolUseID,
		&block.ToolName,
		&toolInputJSON,
		&block.ToolResultForID,
		&block.ToolContent,
		&block.IsError,
		&sourceJSON,
		&webSearchJSON,
		&metadataJSON,
		&block.CreatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("content block not found: %s", blockID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get content block: %w", err)
	}

	if len(toolInputJSON) > 0 {
		if err := json.Unmarshal(toolInputJSON, &block.ToolInput); err != nil {
			return nil, fmt.Errorf("failed to unmarshal tool input: %w", err)
		}
	}
	if len(sourceJSON) > 0 {
		if err := json.Unmarshal(sourceJSON, &block.Source); err != nil {
			return nil, fmt.Errorf("failed to unmarshal source: %w", err)
		}
	}
	if len(webSearchJSON) > 0 {
		if err := json.Unmarshal(webSearchJSON, &block.WebSearchResults); err != nil {
			return nil, fmt.Errorf("failed to unmarshal web search results: %w", err)
		}
	}
	if len(metadataJSON) > 0 {
		if err := json.Unmarshal(metadataJSON, &block.Metadata); err != nil {
			return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
		}
	}

	return &block, nil
}

// LinkToolResult updates a tool_result block to reference its tool_use block.
func (s *Store) LinkToolResult(ctx context.Context, toolResultBlockID, toolUseBlockID string) error {
	query := `
		UPDATE agentpg_content_blocks
		SET tool_result_for_id = $2
		WHERE id = $1
	`

	result, err := s.getExecutor(ctx).Exec(ctx, query, toolResultBlockID, toolUseBlockID)
	if err != nil {
		return fmt.Errorf("failed to link tool result: %w", err)
	}

	if result == 0 {
		return fmt.Errorf("tool result block not found: %s", toolResultBlockID)
	}

	return nil
}

// scanContentBlocks is a helper to scan content block rows.
func (s *Store) scanContentBlocks(rows driver.Rows) ([]*storage.ContentBlock, error) {
	var blocks []*storage.ContentBlock

	for rows.Next() {
		var block storage.ContentBlock
		var toolInputJSON, sourceJSON, webSearchJSON, metadataJSON []byte

		err := rows.Scan(
			&block.ID,
			&block.MessageID,
			&block.BlockIndex,
			&block.Type,
			&block.Text,
			&block.ToolUseID,
			&block.ToolName,
			&toolInputJSON,
			&block.ToolResultForID,
			&block.ToolContent,
			&block.IsError,
			&sourceJSON,
			&webSearchJSON,
			&metadataJSON,
			&block.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan content block: %w", err)
		}

		if len(toolInputJSON) > 0 {
			if err := json.Unmarshal(toolInputJSON, &block.ToolInput); err != nil {
				return nil, fmt.Errorf("failed to unmarshal tool input: %w", err)
			}
		}
		if len(sourceJSON) > 0 {
			if err := json.Unmarshal(sourceJSON, &block.Source); err != nil {
				return nil, fmt.Errorf("failed to unmarshal source: %w", err)
			}
		}
		if len(webSearchJSON) > 0 {
			if err := json.Unmarshal(webSearchJSON, &block.WebSearchResults); err != nil {
				return nil, fmt.Errorf("failed to unmarshal web search results: %w", err)
			}
		}
		if len(metadataJSON) > 0 {
			if err := json.Unmarshal(metadataJSON, &block.Metadata); err != nil {
				return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
			}
		}

		blocks = append(blocks, &block)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating content blocks: %w", err)
	}

	return blocks, nil
}

// =============================================================================
// Async Run operations
// =============================================================================

// GetPendingRuns returns runs waiting for processing in the given states.
func (s *Store) GetPendingRuns(ctx context.Context, states []runstate.RunState, limit int) ([]*storage.Run, error) {
	if len(states) == 0 {
		return nil, nil
	}

	// Build the state list for IN clause
	stateStrings := make([]string, len(states))
	for i, state := range states {
		stateStrings[i] = string(state)
	}

	query := `
		SELECT id, session_id, state, agent_name, prompt, response_text, stop_reason,
		       input_tokens, output_tokens, iteration_count, tool_iterations,
		       error_message, error_type, instance_id, worker_instance_id,
		       last_api_call_at, continuation_required, metadata, started_at, finalized_at
		FROM agentpg_runs
		WHERE state = ANY($1)
		ORDER BY started_at ASC
		LIMIT $2
	`

	rows, err := s.getExecutor(ctx).Query(ctx, query, pq.Array(stateStrings), limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query pending runs: %w", err)
	}
	defer rows.Close()

	return s.scanRuns(rows)
}

// ClaimRun attempts to claim a run for this worker instance.
// Returns true if claimed, false if already claimed by another worker.
func (s *Store) ClaimRun(ctx context.Context, runID, instanceID string) (bool, error) {
	query := `
		UPDATE agentpg_runs
		SET worker_instance_id = $2,
		    state = 'pending_api',
		    last_api_call_at = NOW()
		WHERE id = $1
		  AND state = 'pending'
		  AND (worker_instance_id IS NULL OR worker_instance_id = $2)
	`

	result, err := s.getExecutor(ctx).Exec(ctx, query, runID, instanceID)
	if err != nil {
		return false, fmt.Errorf("failed to claim run: %w", err)
	}

	return result > 0, nil
}

// ReleaseRunClaim releases a run claim (resets worker_instance_id).
func (s *Store) ReleaseRunClaim(ctx context.Context, runID string) error {
	query := `
		UPDATE agentpg_runs
		SET worker_instance_id = NULL,
		    state = 'pending'
		WHERE id = $1
		  AND state NOT IN ('completed', 'cancelled', 'failed')
	`

	_, err := s.getExecutor(ctx).Exec(ctx, query, runID)
	if err != nil {
		return fmt.Errorf("failed to release run claim: %w", err)
	}

	return nil
}

// UpdateRunIteration records a new API iteration.
func (s *Store) UpdateRunIteration(ctx context.Context, runID string, params *storage.UpdateRunIterationParams) error {
	query := `
		UPDATE agentpg_runs
		SET iteration_count = iteration_count + CASE WHEN $2 THEN 1 ELSE 0 END,
		    tool_iterations = tool_iterations + CASE WHEN $3 THEN 1 ELSE 0 END,
		    input_tokens = input_tokens + $4,
		    output_tokens = output_tokens + $5,
		    last_api_call_at = $6
		WHERE id = $1
		  AND state NOT IN ('completed', 'cancelled', 'failed')
	`

	result, err := s.getExecutor(ctx).Exec(ctx, query,
		runID,
		params.IncrementIteration,
		params.IncrementTools,
		params.InputTokens,
		params.OutputTokens,
		params.LastAPICallAt,
	)
	if err != nil {
		return fmt.Errorf("failed to update run iteration: %w", err)
	}

	if result == 0 {
		return fmt.Errorf("run not found or already finalized: %s", runID)
	}

	return nil
}

// =============================================================================
// Tool Execution operations
// =============================================================================

// CreateToolExecutions creates multiple pending tool executions for a run.
func (s *Store) CreateToolExecutions(ctx context.Context, executions []*storage.CreateToolExecutionParams) error {
	if len(executions) == 0 {
		return nil
	}

	query := `
		INSERT INTO agentpg_tool_executions (id, run_id, tool_use_block_id, tool_name, tool_input, max_attempts, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, NOW())
	`

	exec := s.getExecutor(ctx)

	for _, e := range executions {
		execID := uuid.New().String()

		toolInputJSON, err := json.Marshal(e.ToolInput)
		if err != nil {
			return fmt.Errorf("failed to marshal tool input: %w", err)
		}

		maxAttempts := e.MaxAttempts
		if maxAttempts == 0 {
			maxAttempts = 3
		}

		_, err = exec.Exec(ctx, query,
			execID,
			e.RunID,
			e.ToolUseBlockID,
			e.ToolName,
			toolInputJSON,
			maxAttempts,
		)
		if err != nil {
			return fmt.Errorf("failed to create tool execution: %w", err)
		}
	}

	return nil
}

// GetPendingToolExecutions returns pending tool executions for pickup.
func (s *Store) GetPendingToolExecutions(ctx context.Context, limit int) ([]*storage.ToolExecution, error) {
	query := `
		SELECT id, run_id, state, tool_use_block_id, tool_result_block_id,
		       tool_name, tool_input, tool_output, error_message,
		       instance_id, attempt_count, max_attempts, created_at, started_at, completed_at
		FROM agentpg_tool_executions
		WHERE state = 'pending'
		ORDER BY created_at ASC
		LIMIT $1
	`

	rows, err := s.getExecutor(ctx).Query(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query pending tool executions: %w", err)
	}
	defer rows.Close()

	return s.scanToolExecutions(rows)
}

// GetRunToolExecutions returns all tool executions for a run.
func (s *Store) GetRunToolExecutions(ctx context.Context, runID string) ([]*storage.ToolExecution, error) {
	query := `
		SELECT id, run_id, state, tool_use_block_id, tool_result_block_id,
		       tool_name, tool_input, tool_output, error_message,
		       instance_id, attempt_count, max_attempts, created_at, started_at, completed_at
		FROM agentpg_tool_executions
		WHERE run_id = $1
		ORDER BY created_at ASC
	`

	rows, err := s.getExecutor(ctx).Query(ctx, query, runID)
	if err != nil {
		return nil, fmt.Errorf("failed to query run tool executions: %w", err)
	}
	defer rows.Close()

	return s.scanToolExecutions(rows)
}

// GetToolExecution returns a single tool execution by ID.
func (s *Store) GetToolExecution(ctx context.Context, executionID string) (*storage.ToolExecution, error) {
	query := `
		SELECT id, run_id, state, tool_use_block_id, tool_result_block_id,
		       tool_name, tool_input, tool_output, error_message,
		       instance_id, attempt_count, max_attempts, created_at, started_at, completed_at
		FROM agentpg_tool_executions
		WHERE id = $1
	`

	var exec storage.ToolExecution
	var toolInputJSON []byte

	err := s.getExecutor(ctx).QueryRow(ctx, query, executionID).Scan(
		&exec.ID,
		&exec.RunID,
		&exec.State,
		&exec.ToolUseBlockID,
		&exec.ToolResultBlockID,
		&exec.ToolName,
		&toolInputJSON,
		&exec.ToolOutput,
		&exec.ErrorMessage,
		&exec.InstanceID,
		&exec.AttemptCount,
		&exec.MaxAttempts,
		&exec.CreatedAt,
		&exec.StartedAt,
		&exec.CompletedAt,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("tool execution not found: %s", executionID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get tool execution: %w", err)
	}

	if err := json.Unmarshal(toolInputJSON, &exec.ToolInput); err != nil {
		return nil, fmt.Errorf("failed to unmarshal tool input: %w", err)
	}

	return &exec, nil
}

// ClaimToolExecution attempts to claim a tool execution for this instance.
// Returns true if claimed, false if already claimed.
func (s *Store) ClaimToolExecution(ctx context.Context, executionID, instanceID string) (bool, error) {
	query := `
		UPDATE agentpg_tool_executions
		SET state = 'running',
		    instance_id = $2,
		    attempt_count = attempt_count + 1,
		    started_at = NOW()
		WHERE id = $1
		  AND state = 'pending'
		  AND (instance_id IS NULL OR instance_id = $2)
	`

	result, err := s.getExecutor(ctx).Exec(ctx, query, executionID, instanceID)
	if err != nil {
		return false, fmt.Errorf("failed to claim tool execution: %w", err)
	}

	return result > 0, nil
}

// UpdateToolExecutionState updates tool execution state.
func (s *Store) UpdateToolExecutionState(ctx context.Context, executionID string, params *storage.UpdateToolExecutionStateParams) error {
	var query string
	var args []any

	if params.State.IsTerminal() {
		query = `
			UPDATE agentpg_tool_executions
			SET state = $2,
			    tool_output = COALESCE($3, tool_output),
			    error_message = COALESCE($4, error_message),
			    tool_result_block_id = COALESCE($5, tool_result_block_id),
			    completed_at = NOW()
			WHERE id = $1 AND state = 'running'
		`
		args = []any{
			executionID,
			params.State,
			params.ToolOutput,
			params.ErrorMessage,
			params.ToolResultBlockID,
		}
	} else {
		query = `
			UPDATE agentpg_tool_executions
			SET state = $2,
			    tool_output = COALESCE($3, tool_output),
			    error_message = COALESCE($4, error_message),
			    tool_result_block_id = COALESCE($5, tool_result_block_id)
			WHERE id = $1 AND state NOT IN ('completed', 'failed', 'skipped')
		`
		args = []any{
			executionID,
			params.State,
			params.ToolOutput,
			params.ErrorMessage,
			params.ToolResultBlockID,
		}
	}

	result, err := s.getExecutor(ctx).Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to update tool execution state: %w", err)
	}

	if result == 0 {
		return fmt.Errorf("tool execution not found or already finalized: %s", executionID)
	}

	return nil
}

// AreAllToolExecutionsComplete checks if all tool executions for a run are terminal.
func (s *Store) AreAllToolExecutionsComplete(ctx context.Context, runID string) (bool, error) {
	query := `
		SELECT COUNT(*) = 0 OR
		       COUNT(*) FILTER (WHERE state IN ('completed', 'failed', 'skipped')) = COUNT(*)
		FROM agentpg_tool_executions
		WHERE run_id = $1
	`

	var allComplete bool
	err := s.getExecutor(ctx).QueryRow(ctx, query, runID).Scan(&allComplete)
	if err != nil {
		return false, fmt.Errorf("failed to check tool executions: %w", err)
	}

	return allComplete, nil
}

// GetCompletedToolExecutions returns all completed tool executions for a run.
func (s *Store) GetCompletedToolExecutions(ctx context.Context, runID string) ([]*storage.ToolExecution, error) {
	query := `
		SELECT id, run_id, state, tool_use_block_id, tool_result_block_id,
		       tool_name, tool_input, tool_output, error_message,
		       instance_id, attempt_count, max_attempts, created_at, started_at, completed_at
		FROM agentpg_tool_executions
		WHERE run_id = $1 AND state IN ('completed', 'failed')
		ORDER BY created_at ASC
	`

	rows, err := s.getExecutor(ctx).Query(ctx, query, runID)
	if err != nil {
		return nil, fmt.Errorf("failed to query completed tool executions: %w", err)
	}
	defer rows.Close()

	return s.scanToolExecutions(rows)
}

// LinkToolExecutionToResultBlock updates the tool_result_block_id for a tool execution.
func (s *Store) LinkToolExecutionToResultBlock(ctx context.Context, executionID, resultBlockID string) error {
	query := `UPDATE agentpg_tool_executions SET tool_result_block_id = $1 WHERE id = $2`
	_, err := s.getExecutor(ctx).Exec(ctx, query, resultBlockID, executionID)
	if err != nil {
		return fmt.Errorf("failed to link tool execution to result block: %w", err)
	}
	return nil
}

// scanToolExecutions is a helper to scan tool execution rows.
func (s *Store) scanToolExecutions(rows driver.Rows) ([]*storage.ToolExecution, error) {
	var executions []*storage.ToolExecution

	for rows.Next() {
		var exec storage.ToolExecution
		var toolInputJSON []byte

		err := rows.Scan(
			&exec.ID,
			&exec.RunID,
			&exec.State,
			&exec.ToolUseBlockID,
			&exec.ToolResultBlockID,
			&exec.ToolName,
			&toolInputJSON,
			&exec.ToolOutput,
			&exec.ErrorMessage,
			&exec.InstanceID,
			&exec.AttemptCount,
			&exec.MaxAttempts,
			&exec.CreatedAt,
			&exec.StartedAt,
			&exec.CompletedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan tool execution: %w", err)
		}

		if len(toolInputJSON) > 0 {
			if err := json.Unmarshal(toolInputJSON, &exec.ToolInput); err != nil {
				return nil, fmt.Errorf("failed to unmarshal tool input: %w", err)
			}
		}

		executions = append(executions, &exec)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating tool executions: %w", err)
	}

	return executions, nil
}

// Ensure Store implements storage.Store
var _ storage.Store = (*Store)(nil)
