package agentpg

import (
	"errors"
	"fmt"
)

// Common errors
var (
	// ErrInvalidConfig is returned when the agent configuration is invalid
	ErrInvalidConfig = errors.New("invalid configuration")

	// ErrSessionNotFound is returned when a session does not exist
	ErrSessionNotFound = errors.New("session not found")

	// ErrToolNotFound is returned when a tool cannot be found
	ErrToolNotFound = errors.New("tool not found")

	// ErrCompactionFailed is returned when context compaction fails
	ErrCompactionFailed = errors.New("context compaction failed")

	// ErrStorageError is returned when a storage operation failed
	ErrStorageError = errors.New("storage operation failed")

	// ErrNoSession is returned when no session is loaded
	ErrNoSession = errors.New("no session loaded")

	// ErrInvalidToolSchema is returned when a tool schema is invalid
	ErrInvalidToolSchema = errors.New("invalid tool schema")

	// ErrToolExecutionFailed is returned when tool execution fails
	ErrToolExecutionFailed = errors.New("tool execution failed")

	// =========================================================================
	// Run errors
	// =========================================================================

	// ErrRunNotFound is returned when a run does not exist
	ErrRunNotFound = errors.New("run not found")

	// ErrInvalidStateTransition is returned when a run state transition is invalid
	ErrInvalidStateTransition = errors.New("invalid state transition")

	// ErrRunAlreadyFinalized is returned when attempting to modify a finalized run
	ErrRunAlreadyFinalized = errors.New("run already finalized")

	// =========================================================================
	// Instance errors
	// =========================================================================

	// ErrInstanceNotFound is returned when an instance does not exist
	ErrInstanceNotFound = errors.New("instance not found")

	// ErrInstanceAlreadyExists is returned when registering a duplicate instance
	ErrInstanceAlreadyExists = errors.New("instance already exists")

	// =========================================================================
	// Agent registration errors
	// =========================================================================

	// ErrAgentNotFound is returned when an agent does not exist
	ErrAgentNotFound = errors.New("agent not found")

	// ErrAgentNotRegistered is returned when trying to use an unregistered agent
	ErrAgentNotRegistered = errors.New("agent not registered")

	// =========================================================================
	// Client errors
	// =========================================================================

	// ErrClientNotStarted is returned when calling methods before Start()
	ErrClientNotStarted = errors.New("client not started")

	// ErrClientAlreadyStarted is returned when Start() is called twice
	ErrClientAlreadyStarted = errors.New("client already started")
)

// AgentError represents an error with additional context
type AgentError struct {
	Op        string         // Operation that failed
	Err       error          // Underlying error
	SessionID string         // Session ID if applicable
	Context   map[string]any // Additional context
}

// Error implements the error interface
func (e *AgentError) Error() string {
	if e.SessionID != "" {
		return fmt.Sprintf("%s (session=%s): %v", e.Op, e.SessionID, e.Err)
	}
	return fmt.Sprintf("%s: %v", e.Op, e.Err)
}

// Unwrap returns the underlying error
func (e *AgentError) Unwrap() error {
	return e.Err
}

// WithContext adds additional context to the error
func (e *AgentError) WithContext(key string, value any) *AgentError {
	if e.Context == nil {
		e.Context = make(map[string]any)
	}
	e.Context[key] = value
	return e
}

// NewAgentError creates a new AgentError
func NewAgentError(op string, err error) *AgentError {
	return &AgentError{
		Op:  op,
		Err: err,
	}
}

// NewAgentErrorWithSession creates a new AgentError with session ID
func NewAgentErrorWithSession(op string, sessionID string, err error) *AgentError {
	return &AgentError{
		Op:        op,
		Err:       err,
		SessionID: sessionID,
	}
}
