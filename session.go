package agentpg

import (
	"context"
)

// NewSession creates a new conversation session
// tenantID is used for multi-tenant isolation (use "1" for single-tenant apps)
// identifier is a custom identifier (e.g., user ID, conversation ID, project name)
// parentSessionID is optional - set to link this session as a child of another session (for nested agents)
func (a *Agent) NewSession(ctx context.Context, tenantID, identifier string, parentSessionID *string, metadata map[string]any) (string, error) {
	if metadata == nil {
		metadata = make(map[string]any)
	}

	sessionID, err := a.store.CreateSession(ctx, tenantID, identifier, parentSessionID, metadata)
	if err != nil {
		return "", NewAgentError("NewSession", err)
	}

	// Set as current session (thread-safe)
	a.setCurrentSession(sessionID)

	return sessionID, nil
}

// NewSessionWithParent is an alias for NewSession that implements AgentToolInterface
// This enables agents to be used with tool/builtin.NewAgentTool
func (a *Agent) NewSessionWithParent(ctx context.Context, tenantID, identifier string, parentSessionID *string, metadata map[string]any) (string, error) {
	return a.NewSession(ctx, tenantID, identifier, parentSessionID, metadata)
}

// LoadSession loads an existing session and sets it as the current session
func (a *Agent) LoadSession(ctx context.Context, sessionID string) error {
	// Verify session exists
	session, err := a.store.GetSession(ctx, sessionID)
	if err != nil {
		return NewAgentError("LoadSession", err)
	}

	a.setCurrentSession(session.ID)
	return nil
}

// GetSession retrieves session information
func (a *Agent) GetSession(ctx context.Context, sessionID string) (*SessionInfo, error) {
	session, err := a.store.GetSession(ctx, sessionID)
	if err != nil {
		return nil, NewAgentError("GetSession", err)
	}

	// Get message count
	messages, err := a.store.GetMessages(ctx, sessionID)
	if err != nil {
		return nil, NewAgentError("GetSession", err)
	}

	// Calculate total tokens from messages
	totalTokens, err := a.store.GetSessionTokenCount(ctx, sessionID)
	if err != nil {
		return nil, NewAgentError("GetSession", err)
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
	CreatedAt       any
	UpdatedAt       any
}

// GetMessages retrieves all messages for the current session
func (a *Agent) GetMessages(ctx context.Context) ([]*Message, error) {
	sessionID := a.CurrentSession()
	if sessionID == "" {
		return nil, ErrNoSession
	}

	messages, err := a.store.GetMessages(ctx, sessionID)
	if err != nil {
		return nil, NewAgentErrorWithSession("GetMessages", sessionID, err)
	}

	// Convert storage messages to agentpg messages
	result := make([]*Message, len(messages))
	for i, msg := range messages {
		result[i] = a.convertFromStorageMessage(msg)
	}

	return result, nil
}

// ensureSession ensures that a session is loaded
func (a *Agent) ensureSession(ctx context.Context) error {
	if a.CurrentSession() == "" {
		return ErrNoSession
	}
	return nil
}
