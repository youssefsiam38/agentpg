package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// txContextKey is the context key for storing pgx.Tx
type txContextKey struct{}

// WithTx returns a new context with the given transaction
func WithTx(ctx context.Context, tx pgx.Tx) context.Context {
	return context.WithValue(ctx, txContextKey{}, tx)
}

// TxFromContext retrieves the transaction from context, or nil if not present
func TxFromContext(ctx context.Context) pgx.Tx {
	if tx, ok := ctx.Value(txContextKey{}).(pgx.Tx); ok {
		return tx
	}
	return nil
}

// txStrippedContext is a context wrapper that hides the transaction from nested contexts
type txStrippedContext struct {
	context.Context
}

func (c *txStrippedContext) Value(key any) any {
	if _, ok := key.(txContextKey); ok {
		return nil
	}
	return c.Context.Value(key)
}

// StripTx creates a new context without the transaction value
// but preserving deadline, cancellation, and other values.
// Used when nested agents should have their own independent transaction.
func StripTx(ctx context.Context) context.Context {
	return &txStrippedContext{ctx}
}

// querier is a common interface for pgxpool.Pool and pgx.Tx
type querier interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults
}

// PostgresStore implements Store using PostgreSQL with pgx
type PostgresStore struct {
	pool *pgxpool.Pool
}

// NewPostgresStore creates a new PostgreSQL store
func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

// getQuerier returns the transaction from context if present, otherwise the pool
func (s *PostgresStore) getQuerier(ctx context.Context) querier {
	if tx := TxFromContext(ctx); tx != nil {
		return tx
	}
	return s.pool
}

// CreateSession creates a new conversation session
func (s *PostgresStore) CreateSession(ctx context.Context, tenantID, identifier string, parentSessionID *string, metadata map[string]any) (string, error) {
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

	_, err = s.getQuerier(ctx).Exec(ctx, query, sessionID, tenantID, identifier, parentSessionID, metadataJSON)
	if err != nil {
		return "", fmt.Errorf("failed to create session: %w", err)
	}

	return sessionID, nil
}

// GetSession retrieves a session by ID
func (s *PostgresStore) GetSession(ctx context.Context, sessionID string) (*Session, error) {
	query := `
		SELECT id, tenant_id, identifier, parent_session_id, metadata, compaction_count,
		       created_at, updated_at
		FROM agentpg_sessions
		WHERE id = $1
	`

	var session Session
	var metadataJSON []byte

	err := s.getQuerier(ctx).QueryRow(ctx, query, sessionID).Scan(
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

// GetSessionsByTenant retrieves all sessions for a tenant
func (s *PostgresStore) GetSessionsByTenant(ctx context.Context, tenantID string) ([]*Session, error) {
	query := `
		SELECT id, tenant_id, identifier, parent_session_id, metadata, compaction_count,
		       created_at, updated_at
		FROM agentpg_sessions
		WHERE tenant_id = $1
		ORDER BY updated_at DESC
	`

	rows, err := s.getQuerier(ctx).Query(ctx, query, tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to query sessions: %w", err)
	}
	defer rows.Close()

	var sessions []*Session
	for rows.Next() {
		var session Session
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

// GetSessionByTenantAndIdentifier retrieves a session by tenant and identifier
func (s *PostgresStore) GetSessionByTenantAndIdentifier(ctx context.Context, tenantID, identifier string) (*Session, error) {
	query := `
		SELECT id, tenant_id, identifier, parent_session_id, metadata, compaction_count,
		       created_at, updated_at
		FROM agentpg_sessions
		WHERE tenant_id = $1 AND identifier = $2
	`

	var session Session
	var metadataJSON []byte

	err := s.getQuerier(ctx).QueryRow(ctx, query, tenantID, identifier).Scan(
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

// GetSessionTokenCount calculates total tokens for a session from messages
func (s *PostgresStore) GetSessionTokenCount(ctx context.Context, sessionID string) (int, error) {
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
	err := s.getQuerier(ctx).QueryRow(ctx, query, sessionID).Scan(&totalTokens)
	if err != nil {
		return 0, fmt.Errorf("failed to calculate session tokens: %w", err)
	}

	return totalTokens, nil
}

// UpdateSessionCompactionCount increments the compaction count
func (s *PostgresStore) UpdateSessionCompactionCount(ctx context.Context, sessionID string) error {
	query := `
		UPDATE agentpg_sessions
		SET compaction_count = compaction_count + 1, updated_at = NOW()
		WHERE id = $1
	`

	_, err := s.getQuerier(ctx).Exec(ctx, query, sessionID)
	if err != nil {
		return fmt.Errorf("failed to update compaction count: %w", err)
	}

	return nil
}

// SaveMessage saves a single message
func (s *PostgresStore) SaveMessage(ctx context.Context, msg *Message) error {
	return s.SaveMessages(ctx, []*Message{msg})
}

// SaveMessages saves multiple messages in a batch
func (s *PostgresStore) SaveMessages(ctx context.Context, messages []*Message) error {
	if len(messages) == 0 {
		return nil
	}

	batch := &pgx.Batch{}

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

		batch.Queue(query,
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
	}

	results := s.getQuerier(ctx).SendBatch(ctx, batch)
	defer results.Close()

	for range messages {
		if _, err := results.Exec(); err != nil {
			return fmt.Errorf("failed to save message: %w", err)
		}
	}

	return nil
}

// GetMessages retrieves all messages for a session ordered by creation time
func (s *PostgresStore) GetMessages(ctx context.Context, sessionID string) ([]*Message, error) {
	query := `
		SELECT id, session_id, role, content, usage, metadata,
		       is_preserved, is_summary, created_at, updated_at
		FROM agentpg_messages
		WHERE session_id = $1
		ORDER BY created_at ASC
	`

	rows, err := s.getQuerier(ctx).Query(ctx, query, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to query messages: %w", err)
	}
	defer rows.Close()

	return s.scanMessages(rows)
}

// GetMessagesSince retrieves messages created after a specific time
func (s *PostgresStore) GetMessagesSince(ctx context.Context, sessionID string, since time.Time) ([]*Message, error) {
	query := `
		SELECT id, session_id, role, content, usage, metadata,
		       is_preserved, is_summary, created_at, updated_at
		FROM agentpg_messages
		WHERE session_id = $1 AND created_at > $2
		ORDER BY created_at ASC
	`

	rows, err := s.getQuerier(ctx).Query(ctx, query, sessionID, since)
	if err != nil {
		return nil, fmt.Errorf("failed to query messages: %w", err)
	}
	defer rows.Close()

	return s.scanMessages(rows)
}

// DeleteMessages deletes messages by their IDs
func (s *PostgresStore) DeleteMessages(ctx context.Context, messageIDs []string) error {
	if len(messageIDs) == 0 {
		return nil
	}

	query := `DELETE FROM agentpg_messages WHERE id = ANY($1)`

	_, err := s.getQuerier(ctx).Exec(ctx, query, messageIDs)
	if err != nil {
		return fmt.Errorf("failed to delete messages: %w", err)
	}

	return nil
}

// scanMessages is a helper to scan message rows
func (s *PostgresStore) scanMessages(rows pgx.Rows) ([]*Message, error) {
	var messages []*Message

	for rows.Next() {
		var msg Message
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
			msg.Usage = &MessageUsage{}
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

// SaveCompactionEvent saves a compaction event
func (s *PostgresStore) SaveCompactionEvent(ctx context.Context, event *CompactionEvent) error {
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

	_, err = s.getQuerier(ctx).Exec(ctx, query,
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

// GetCompactionHistory retrieves compaction history for a session
func (s *PostgresStore) GetCompactionHistory(ctx context.Context, sessionID string) ([]*CompactionEvent, error) {
	query := `
		SELECT id, session_id, strategy, original_tokens, compacted_tokens,
		       messages_removed, summary_content, preserved_message_ids,
		       model_used, duration_ms, created_at
		FROM agentpg_compaction_events
		WHERE session_id = $1
		ORDER BY created_at DESC
	`

	rows, err := s.getQuerier(ctx).Query(ctx, query, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to query compaction history: %w", err)
	}
	defer rows.Close()

	var events []*CompactionEvent

	for rows.Next() {
		var event CompactionEvent
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

// ArchiveMessages archives messages that were removed during compaction
func (s *PostgresStore) ArchiveMessages(ctx context.Context, compactionEventID string, messages []*Message) error {
	if len(messages) == 0 {
		return nil
	}

	batch := &pgx.Batch{}

	query := `
		INSERT INTO agentpg_message_archive (id, compaction_event_id, session_id, original_message, archived_at)
		VALUES ($1, $2, $3, $4, NOW())
	`

	for _, msg := range messages {
		msgJSON, err := json.Marshal(msg)
		if err != nil {
			return fmt.Errorf("failed to marshal message: %w", err)
		}

		batch.Queue(query, msg.ID, compactionEventID, msg.SessionID, msgJSON)
	}

	results := s.getQuerier(ctx).SendBatch(ctx, batch)
	defer results.Close()

	for range messages {
		if _, err := results.Exec(); err != nil {
			return fmt.Errorf("failed to archive message: %w", err)
		}
	}

	return nil
}

