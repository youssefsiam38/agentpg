package compaction

import (
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// Sentinel errors for compaction operations.
var (
	// ErrInvalidConfig indicates invalid compaction configuration.
	ErrInvalidConfig = errors.New("invalid compaction configuration")

	// ErrNoMessagesToCompact indicates there are no messages eligible for compaction.
	ErrNoMessagesToCompact = errors.New("no messages to compact")

	// ErrCompactionInProgress indicates compaction is already running for this session.
	ErrCompactionInProgress = errors.New("compaction already in progress")

	// ErrSummarizationFailed indicates the summarization API call failed.
	ErrSummarizationFailed = errors.New("summarization failed")

	// ErrTokenCountingFailed indicates token counting failed.
	ErrTokenCountingFailed = errors.New("token counting failed")

	// ErrSessionNotFound indicates the session was not found.
	ErrSessionNotFound = errors.New("session not found")

	// ErrStorageError indicates a database operation failed.
	ErrStorageError = errors.New("storage operation failed")
)

// CompactionError provides structured error context for compaction operations.
type CompactionError struct {
	// Op is the operation that failed (e.g., "Compact", "CountTokens", "Summarize")
	Op string

	// SessionID is the session ID if applicable
	SessionID uuid.UUID

	// Err is the underlying error
	Err error

	// Context holds additional key-value pairs for debugging
	Context map[string]any
}

// Error returns a formatted error message.
func (e *CompactionError) Error() string {
	msg := fmt.Sprintf("compaction %s failed", e.Op)
	if e.SessionID != uuid.Nil {
		msg += fmt.Sprintf(" for session %s", e.SessionID)
	}
	if e.Err != nil {
		msg += ": " + e.Err.Error()
	}
	return msg
}

// Unwrap returns the underlying error for errors.Is/errors.As support.
func (e *CompactionError) Unwrap() error {
	return e.Err
}

// NewCompactionError creates a new CompactionError with the given operation and underlying error.
func NewCompactionError(op string, err error) *CompactionError {
	return &CompactionError{
		Op:      op,
		Err:     err,
		Context: make(map[string]any),
	}
}

// WithSession sets the session ID on the error and returns the error for chaining.
func (e *CompactionError) WithSession(sessionID uuid.UUID) *CompactionError {
	e.SessionID = sessionID
	return e
}

// WithContext adds a key-value pair to the error context and returns the error for chaining.
func (e *CompactionError) WithContext(key string, value any) *CompactionError {
	if e.Context == nil {
		e.Context = make(map[string]any)
	}
	e.Context[key] = value
	return e
}

// WrapError wraps an error with operation context. If err is nil, returns nil.
func WrapError(op string, err error) error {
	if err == nil {
		return nil
	}
	return NewCompactionError(op, err)
}

// WrapErrorWithSession wraps an error with operation and session context.
func WrapErrorWithSession(op string, sessionID uuid.UUID, err error) error {
	if err == nil {
		return nil
	}
	return NewCompactionError(op, err).WithSession(sessionID)
}
