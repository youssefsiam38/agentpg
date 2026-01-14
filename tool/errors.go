// Package tool defines error types for controlling tool execution retry behavior.
package tool

import (
	"errors"
	"fmt"
	"time"
)

// Tool error sentinel values for type checking
var (
	// ErrToolCancelled is a sentinel error for cancelled tools.
	ErrToolCancelled = errors.New("tool cancelled")

	// ErrToolDiscarded is a sentinel error for discarded tools.
	ErrToolDiscarded = errors.New("tool discarded")

	// ErrToolSnoozed is a sentinel error for snoozed tools.
	ErrToolSnoozed = errors.New("tool snoozed")
)

// ToolCancelError signals immediate cancellation of the tool execution.
// The tool will not be retried regardless of remaining attempts.
// Use this when the tool encounters an unrecoverable error that should
// not be retried (e.g., authentication failures, permission denied).
type ToolCancelError struct {
	err error
}

// Error returns the error message.
func (e *ToolCancelError) Error() string {
	if e.err == nil {
		return "tool cancelled"
	}
	return fmt.Sprintf("tool cancelled: %s", e.err.Error())
}

// Is reports whether the target matches this error type.
func (e *ToolCancelError) Is(target error) bool {
	if target == ErrToolCancelled {
		return true
	}
	_, ok := target.(*ToolCancelError)
	return ok
}

// Unwrap returns the underlying error.
func (e *ToolCancelError) Unwrap() error {
	return e.err
}

// ToolCancel wraps an error to indicate the tool should be cancelled immediately.
// No more retries will be attempted regardless of remaining attempts.
//
// Example:
//
//	func (t *MyTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
//	    if authFailed {
//	        return "", tool.ToolCancel(errors.New("authentication failed"))
//	    }
//	    return "result", nil
//	}
func ToolCancel(err error) error {
	return &ToolCancelError{err: err}
}

// ToolDiscardError signals permanent failure without retry.
// Similar to ToolCancel but semantically indicates the input was invalid
// or the operation is fundamentally unsatisfiable.
// Use this when the tool input is malformed or the requested operation
// cannot possibly succeed.
type ToolDiscardError struct {
	err error
}

// Error returns the error message.
func (e *ToolDiscardError) Error() string {
	if e.err == nil {
		return "tool discarded"
	}
	return fmt.Sprintf("tool discarded: %s", e.err.Error())
}

// Is reports whether the target matches this error type.
func (e *ToolDiscardError) Is(target error) bool {
	if target == ErrToolDiscarded {
		return true
	}
	_, ok := target.(*ToolDiscardError)
	return ok
}

// Unwrap returns the underlying error.
func (e *ToolDiscardError) Unwrap() error {
	return e.err
}

// ToolDiscard wraps an error to indicate the tool should be discarded.
// Use when the input is fundamentally invalid and retrying won't help.
//
// Example:
//
//	func (t *MyTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
//	    var params struct{ UserID string `json:"user_id"` }
//	    if err := json.Unmarshal(input, &params); err != nil {
//	        return "", tool.ToolDiscard(fmt.Errorf("invalid input: %w", err))
//	    }
//	    return "result", nil
//	}
func ToolDiscard(err error) error {
	return &ToolDiscardError{err: err}
}

// ToolSnoozeError signals the tool should be retried after a duration.
// This does NOT consume an attempt, allowing the tool to be retried
// without penalty. Use for transient failures like rate limits,
// temporary unavailability, or waiting for external resources.
type ToolSnoozeError struct {
	// Duration is how long to wait before retrying.
	Duration time.Duration

	err error
}

// Error returns the error message.
func (e *ToolSnoozeError) Error() string {
	if e.err == nil {
		return fmt.Sprintf("tool snoozed for %s", e.Duration)
	}
	return fmt.Sprintf("tool snoozed for %s: %s", e.Duration, e.err.Error())
}

// Is reports whether the target matches this error type.
func (e *ToolSnoozeError) Is(target error) bool {
	if target == ErrToolSnoozed {
		return true
	}
	_, ok := target.(*ToolSnoozeError)
	return ok
}

// Unwrap returns the underlying error.
func (e *ToolSnoozeError) Unwrap() error {
	return e.err
}

// ToolSnooze returns an error that causes the tool to be retried after duration.
// This does NOT consume an attempt. The snooze count is tracked separately.
// There is no limit on the number of snoozes.
//
// Example:
//
//	func (t *MyTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
//	    result, err := callAPI()
//	    if isRateLimited(err) {
//	        return "", tool.ToolSnooze(30*time.Second, err)
//	    }
//	    return result, nil
//	}
func ToolSnooze(duration time.Duration, err error) error {
	if duration < 0 {
		duration = 0
	}
	return &ToolSnoozeError{Duration: duration, err: err}
}

// IsToolCancel reports whether err is a ToolCancelError.
func IsToolCancel(err error) bool {
	return errors.Is(err, ErrToolCancelled)
}

// IsToolDiscard reports whether err is a ToolDiscardError.
func IsToolDiscard(err error) bool {
	return errors.Is(err, ErrToolDiscarded)
}

// IsToolSnooze reports whether err is a ToolSnoozeError.
func IsToolSnooze(err error) bool {
	return errors.Is(err, ErrToolSnoozed)
}

// GetSnoozeDuration extracts the snooze duration from a ToolSnoozeError.
// Returns 0 and false if err is not a ToolSnoozeError.
func GetSnoozeDuration(err error) (time.Duration, bool) {
	var snoozeErr *ToolSnoozeError
	if errors.As(err, &snoozeErr) {
		return snoozeErr.Duration, true
	}
	return 0, false
}
