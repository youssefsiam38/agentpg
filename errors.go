package agentpg

import (
	"errors"
	"fmt"
)

// Sentinel errors for AgentPG operations.
var (
	// Configuration errors
	ErrInvalidConfig = errors.New("invalid configuration")

	// Resource not found errors
	ErrSessionNotFound       = errors.New("session not found")
	ErrRunNotFound           = errors.New("run not found")
	ErrAgentNotFound         = errors.New("agent not found")
	ErrToolNotFound          = errors.New("tool not found")
	ErrIterationNotFound     = errors.New("iteration not found")
	ErrToolExecutionNotFound = errors.New("tool execution not found")

	// Registration errors
	ErrAgentNotRegistered = errors.New("agent not registered on this client")
	ErrToolNotRegistered  = errors.New("tool not registered on this client")

	// Client lifecycle errors
	ErrClientNotStarted     = errors.New("client not started")
	ErrClientAlreadyStarted = errors.New("client already started")
	ErrClientStopping       = errors.New("client is stopping")

	// State errors
	ErrInvalidStateTransition = errors.New("invalid state transition")
	ErrRunAlreadyFinalized    = errors.New("run already finalized")
	ErrRunCancelled           = errors.New("run was cancelled")

	// Tool errors
	ErrInvalidToolSchema   = errors.New("invalid tool schema")
	ErrToolExecutionFailed = errors.New("tool execution failed")

	// Batch API errors
	ErrBatchAPIError = errors.New("claude batch API error")
	ErrBatchExpired  = errors.New("batch expired")
	ErrBatchFailed   = errors.New("batch processing failed")

	// Storage errors
	ErrStorageError = errors.New("storage operation failed")

	// Instance errors
	ErrInstanceDisconnected = errors.New("instance disconnected")
	ErrInstanceNotFound     = errors.New("instance not found")

	// Compaction errors
	ErrCompactionFailed = errors.New("context compaction failed")
)

// AgentError provides structured error context for AgentPG operations.
// It wraps an underlying error with additional context including operation name,
// session/run IDs, and arbitrary key-value context.
type AgentError struct {
	// Op is the operation that failed (e.g., "Run", "NewSession", "ExecuteTool")
	Op string

	// Err is the underlying error
	Err error

	// SessionID is the session ID if applicable
	SessionID string

	// RunID is the run ID if applicable
	RunID string

	// Context holds additional key-value pairs for debugging
	Context map[string]any
}

// Error returns a formatted error message.
func (e *AgentError) Error() string {
	msg := e.Op + ": " + e.Err.Error()
	if e.SessionID != "" {
		msg += fmt.Sprintf(" (session=%s)", e.SessionID)
	}
	if e.RunID != "" {
		msg += fmt.Sprintf(" (run=%s)", e.RunID)
	}
	return msg
}

// Unwrap returns the underlying error for errors.Is/errors.As support.
func (e *AgentError) Unwrap() error {
	return e.Err
}

// NewAgentError creates a new AgentError with the given operation and underlying error.
func NewAgentError(op string, err error) *AgentError {
	return &AgentError{
		Op:      op,
		Err:     err,
		Context: make(map[string]any),
	}
}

// WithSession sets the session ID on the error and returns the error for chaining.
func (e *AgentError) WithSession(sessionID string) *AgentError {
	e.SessionID = sessionID
	return e
}

// WithRun sets the run ID on the error and returns the error for chaining.
func (e *AgentError) WithRun(runID string) *AgentError {
	e.RunID = runID
	return e
}

// WithContext adds a key-value pair to the error context and returns the error for chaining.
func (e *AgentError) WithContext(key string, value any) *AgentError {
	e.Context[key] = value
	return e
}

// WrapError wraps an error with operation context. If err is nil, returns nil.
func WrapError(op string, err error) error {
	if err == nil {
		return nil
	}
	return NewAgentError(op, err)
}
