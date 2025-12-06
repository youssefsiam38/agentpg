package agentpg

import (
	"context"
	"time"

	"github.com/youssefsiam38/agentpg/driver"
	"github.com/youssefsiam38/agentpg/internal/convert"
)

// withTxContext wraps a transaction in an executor and injects it into the context.
// If tx is nil, returns the original context unchanged.
func (a *Agent[TTx]) withTxContext(ctx context.Context, tx *TTx) context.Context {
	if tx == nil {
		return ctx
	}
	execTx := a.driver.UnwrapExecutor(*tx)
	return driver.WithExecutor(ctx, execTx)
}

// NewSession creates a new conversation session
// tenantID is used for multi-tenant isolation (use "1" for single-tenant apps)
// identifier is a custom identifier (e.g., user ID, conversation ID, project name)
// parentSessionID is optional - set to link this session as a child of another session (for nested agents)
func (a *Agent[TTx]) NewSession(ctx context.Context, tenantID, identifier string, parentSessionID *string, metadata map[string]any) (string, error) {
	return a.newSessionInternal(ctx, nil, tenantID, identifier, parentSessionID, metadata)
}

// NewSessionTx creates a new conversation session within an existing transaction
// This allows combining session creation with other database operations atomically
func (a *Agent[TTx]) NewSessionTx(ctx context.Context, tx TTx, tenantID, identifier string, parentSessionID *string, metadata map[string]any) (string, error) {
	return a.newSessionInternal(ctx, &tx, tenantID, identifier, parentSessionID, metadata)
}

// newSessionInternal is the internal implementation for creating sessions
func (a *Agent[TTx]) newSessionInternal(ctx context.Context, tx *TTx, tenantID, identifier string, parentSessionID *string, metadata map[string]any) (string, error) {
	if metadata == nil {
		metadata = make(map[string]any)
	}

	txCtx := a.withTxContext(ctx, tx)

	sessionID, err := a.store.CreateSession(txCtx, tenantID, identifier, parentSessionID, metadata)
	if err != nil {
		opName := "NewSession"
		if tx != nil {
			opName = "NewSessionTx"
		}
		return "", NewAgentError(opName, err)
	}

	// Set as current session (thread-safe)
	a.setCurrentSession(sessionID)

	return sessionID, nil
}

// NewSessionWithParent is an alias for NewSession that implements AgentToolInterface
// This enables agents to be used with tool/builtin.NewAgentTool
func (a *Agent[TTx]) NewSessionWithParent(ctx context.Context, tenantID, identifier string, parentSessionID *string, metadata map[string]any) (string, error) {
	return a.NewSession(ctx, tenantID, identifier, parentSessionID, metadata)
}

// LoadSession loads an existing session and sets it as the current session
func (a *Agent[TTx]) LoadSession(ctx context.Context, sessionID string) error {
	return a.loadSessionInternal(ctx, nil, sessionID)
}

// LoadSessionTx loads an existing session within an existing transaction
func (a *Agent[TTx]) LoadSessionTx(ctx context.Context, tx TTx, sessionID string) error {
	return a.loadSessionInternal(ctx, &tx, sessionID)
}

// loadSessionInternal is the internal implementation for loading sessions
func (a *Agent[TTx]) loadSessionInternal(ctx context.Context, tx *TTx, sessionID string) error {
	txCtx := a.withTxContext(ctx, tx)

	// Verify session exists
	session, err := a.store.GetSession(txCtx, sessionID)
	if err != nil {
		opName := "LoadSession"
		if tx != nil {
			opName = "LoadSessionTx"
		}
		return NewAgentError(opName, err)
	}

	a.setCurrentSession(session.ID)
	return nil
}

// GetSession retrieves session information
func (a *Agent[TTx]) GetSession(ctx context.Context, sessionID string) (*SessionInfo, error) {
	return a.getSessionInternal(ctx, nil, sessionID)
}

// GetSessionTx retrieves session information within an existing transaction
func (a *Agent[TTx]) GetSessionTx(ctx context.Context, tx TTx, sessionID string) (*SessionInfo, error) {
	return a.getSessionInternal(ctx, &tx, sessionID)
}

// getSessionInternal is the internal implementation for retrieving session info
func (a *Agent[TTx]) getSessionInternal(ctx context.Context, tx *TTx, sessionID string) (*SessionInfo, error) {
	txCtx := a.withTxContext(ctx, tx)
	opName := "GetSession"
	if tx != nil {
		opName = "GetSessionTx"
	}

	session, err := a.store.GetSession(txCtx, sessionID)
	if err != nil {
		return nil, NewAgentError(opName, err)
	}

	// Get message count
	messages, err := a.store.GetMessages(txCtx, sessionID)
	if err != nil {
		return nil, NewAgentError(opName, err)
	}

	// Calculate total tokens from messages
	totalTokens, err := a.store.GetSessionTokenCount(txCtx, sessionID)
	if err != nil {
		return nil, NewAgentError(opName, err)
	}

	return &SessionInfo{
		ID:              session.ID,
		TenantID:        session.TenantID,
		Identifier:      session.Identifier,
		ParentSessionID: session.ParentSessionID,
		Metadata:        session.Metadata,
		TotalTokens:     totalTokens,
		CompactionCount: session.CompactionCount,
		MessageCount:    len(messages),
		CreatedAt:       session.CreatedAt,
		UpdatedAt:       session.UpdatedAt,
	}, nil
}

// SessionInfo contains information about a session
type SessionInfo struct {
	ID              string
	TenantID        string
	Identifier      string
	ParentSessionID *string
	Metadata        map[string]any
	TotalTokens     int
	CompactionCount int
	MessageCount    int
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// GetMessages retrieves all messages for the current session
func (a *Agent[TTx]) GetMessages(ctx context.Context) ([]*Message, error) {
	return a.getMessagesInternal(ctx, nil)
}

// GetMessagesTx retrieves all messages for the current session within an existing transaction
func (a *Agent[TTx]) GetMessagesTx(ctx context.Context, tx TTx) ([]*Message, error) {
	return a.getMessagesInternal(ctx, &tx)
}

// getMessagesInternal is the internal implementation for retrieving messages
func (a *Agent[TTx]) getMessagesInternal(ctx context.Context, tx *TTx) ([]*Message, error) {
	sessionID := a.CurrentSession()
	if sessionID == "" {
		return nil, ErrNoSession
	}

	txCtx := a.withTxContext(ctx, tx)
	opName := "GetMessages"
	if tx != nil {
		opName = "GetMessagesTx"
	}

	messages, err := a.store.GetMessages(txCtx, sessionID)
	if err != nil {
		return nil, NewAgentErrorWithSession(opName, sessionID, err)
	}

	// Convert storage messages to agentpg messages
	return convert.FromStorageMessages(messages), nil
}

// ensureSession ensures that a session is loaded
func (a *Agent[TTx]) ensureSession(ctx context.Context) error {
	if a.CurrentSession() == "" {
		return ErrNoSession
	}
	return nil
}
