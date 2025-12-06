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
		INSERT INTO agentpg_messages (id, session_id, role, content, usage, metadata,
		                     is_preserved, is_summary, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (id) DO UPDATE SET
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
		SELECT id, session_id, role, content, usage, metadata,
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

	return s.scanMessages(rows)
}

// GetMessagesSince retrieves messages created after a specific time.
func (s *Store) GetMessagesSince(ctx context.Context, sessionID string, since time.Time) ([]*storage.Message, error) {
	query := `
		SELECT id, session_id, role, content, usage, metadata,
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

	return s.scanMessages(rows)
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

// scanMessages is a helper to scan message rows.
func (s *Store) scanMessages(rows driver.Rows) ([]*storage.Message, error) {
	var messages []*storage.Message

	for rows.Next() {
		var msg storage.Message
		var contentJSON []byte
		var usageJSON []byte
		var metadataJSON []byte

		err := rows.Scan(
			&msg.ID,
			&msg.SessionID,
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
