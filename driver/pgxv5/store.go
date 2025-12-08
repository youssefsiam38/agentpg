package pgxv5

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/youssefsiam38/agentpg/driver"
	"github.com/youssefsiam38/agentpg/storage"
)

// Store implements storage.Store using the pgxv5 driver.
type Store struct {
	driver *Driver
}

// NewStore creates a new pgxv5 Store.
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

	if err == pgx.ErrNoRows {
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

	if err == pgx.ErrNoRows {
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

// SaveMessages saves multiple messages in a batch.
func (s *Store) SaveMessages(ctx context.Context, messages []*storage.Message) error {
	if len(messages) == 0 {
		return nil
	}

	query := `
		INSERT INTO agentpg_messages (id, session_id, run_id, role, content, usage, metadata,
		                     is_preserved, is_summary, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (id) DO UPDATE SET
			run_id = EXCLUDED.run_id,
			content = EXCLUDED.content,
			usage = EXCLUDED.usage,
			metadata = EXCLUDED.metadata,
			is_preserved = EXCLUDED.is_preserved,
			is_summary = EXCLUDED.is_summary,
			updated_at = NOW()
	`

	// Check if executor supports batch operations
	exec := s.getExecutor(ctx)
	if batchExec, ok := exec.(interface {
		SendBatch(ctx context.Context, items []driver.BatchItem) ([]int64, error)
	}); ok {
		// Use batch operations
		items := make([]driver.BatchItem, 0, len(messages))
		for _, msg := range messages {
			contentJSON, err := json.Marshal(msg.Content)
			if err != nil {
				return fmt.Errorf("failed to marshal content: %w", err)
			}

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

			items = append(items, driver.BatchItem{
				Query: query,
				Args: []any{
					msg.ID,
					msg.SessionID,
					msg.RunID,
					msg.Role,
					contentJSON,
					usageJSON,
					metadataJSON,
					msg.IsPreserved,
					msg.IsSummary,
					createdAt,
					updatedAt,
				},
			})
		}

		_, err := batchExec.SendBatch(ctx, items)
		if err != nil {
			return fmt.Errorf("failed to save messages: %w", err)
		}
		return nil
	}

	// Fallback to sequential execution
	for _, msg := range messages {
		contentJSON, err := json.Marshal(msg.Content)
		if err != nil {
			return fmt.Errorf("failed to marshal content: %w", err)
		}

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
			contentJSON,
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
func (s *Store) GetMessages(ctx context.Context, sessionID string) ([]*storage.Message, error) {
	query := `
		SELECT id, session_id, run_id, role, content, usage, metadata,
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
		SELECT id, session_id, run_id, role, content, usage, metadata,
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

	query := `DELETE FROM agentpg_messages WHERE id = ANY($1)`

	_, err := s.getExecutor(ctx).Exec(ctx, query, messageIDs)
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

	// Check if executor supports batch operations
	exec := s.getExecutor(ctx)
	if batchExec, ok := exec.(interface {
		SendBatch(ctx context.Context, items []driver.BatchItem) ([]int64, error)
	}); ok {
		// Use batch operations
		items := make([]driver.BatchItem, 0, len(messages))
		for _, msg := range messages {
			msgJSON, err := json.Marshal(msg)
			if err != nil {
				return fmt.Errorf("failed to marshal message: %w", err)
			}

			items = append(items, driver.BatchItem{
				Query: query,
				Args:  []any{msg.ID, compactionEventID, msg.SessionID, msgJSON},
			})
		}

		_, err := batchExec.SendBatch(ctx, items)
		if err != nil {
			return fmt.Errorf("failed to archive messages: %w", err)
		}
		return nil
	}

	// Fallback to sequential execution
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

// CreateRun creates a new run record in the running state.
func (s *Store) CreateRun(ctx context.Context, params *storage.CreateRunParams) (string, error) {
	runID := uuid.New().String()

	metadataJSON, err := json.Marshal(params.Metadata)
	if err != nil {
		return "", fmt.Errorf("failed to marshal metadata: %w", err)
	}

	query := `
		INSERT INTO agentpg_runs (id, session_id, agent_name, prompt, instance_id, metadata, state, started_at)
		VALUES ($1, $2, $3, $4, $5, $6, 'running', NOW())
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
		       input_tokens, output_tokens, tool_iterations, error_message, error_type,
		       instance_id, metadata, started_at, finalized_at
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
		&run.ToolIterations,
		&run.ErrorMessage,
		&run.ErrorType,
		&run.InstanceID,
		&metadataJSON,
		&run.StartedAt,
		&run.FinalizedAt,
	)

	if err == pgx.ErrNoRows {
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
		       input_tokens, output_tokens, tool_iterations, error_message, error_type,
		       instance_id, metadata, started_at, finalized_at
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

// UpdateRunState transitions a run to a new state.
func (s *Store) UpdateRunState(ctx context.Context, runID string, params *storage.UpdateRunStateParams) error {
	// Validate the transition
	if !params.State.IsTerminal() {
		return fmt.Errorf("can only transition to terminal states")
	}

	query := `
		UPDATE agentpg_runs
		SET state = $2,
		    response_text = COALESCE($3, response_text),
		    stop_reason = COALESCE($4, stop_reason),
		    input_tokens = $5,
		    output_tokens = $6,
		    tool_iterations = $7,
		    error_message = COALESCE($8, error_message),
		    error_type = COALESCE($9, error_type),
		    finalized_at = NOW()
		WHERE id = $1 AND state = 'running'
	`

	result, err := s.getExecutor(ctx).Exec(ctx, query,
		runID,
		params.State,
		params.ResponseText,
		params.StopReason,
		params.InputTokens,
		params.OutputTokens,
		params.ToolIterations,
		params.ErrorMessage,
		params.ErrorType,
	)
	if err != nil {
		return fmt.Errorf("failed to update run state: %w", err)
	}

	if result == 0 {
		return fmt.Errorf("run not found or already finalized: %s", runID)
	}

	return nil
}

// GetRunMessages returns all messages associated with a run.
func (s *Store) GetRunMessages(ctx context.Context, runID string) ([]*storage.Message, error) {
	query := `
		SELECT id, session_id, run_id, role, content, usage, metadata,
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

// GetStuckRuns returns all runs that have been running longer than the horizon.
func (s *Store) GetStuckRuns(ctx context.Context, horizon time.Time) ([]*storage.Run, error) {
	query := `
		SELECT id, session_id, state, agent_name, prompt, response_text, stop_reason,
		       input_tokens, output_tokens, tool_iterations, error_message, error_type,
		       instance_id, metadata, started_at, finalized_at
		FROM agentpg_runs
		WHERE state = 'running' AND started_at < $1
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
			&run.ToolIterations,
			&run.ErrorMessage,
			&run.ErrorType,
			&run.InstanceID,
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
func (s *Store) scanMessagesWithRunID(rows driver.Rows) ([]*storage.Message, error) {
	var messages []*storage.Message

	for rows.Next() {
		var msg storage.Message
		var contentJSON []byte
		var usageJSON []byte
		var metadataJSON []byte

		err := rows.Scan(
			&msg.ID,
			&msg.SessionID,
			&msg.RunID,
			&msg.Role,
			&contentJSON,
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

		if err := json.Unmarshal(contentJSON, &msg.Content); err != nil {
			return nil, fmt.Errorf("failed to unmarshal content: %w", err)
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

	if err == pgx.ErrNoRows {
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

	if err == pgx.ErrNoRows {
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

	if err == pgx.ErrNoRows {
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

	if err == pgx.ErrNoRows {
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

// Ensure Store implements storage.Store
var _ storage.Store = (*Store)(nil)
