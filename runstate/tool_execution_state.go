// Package runstate provides the state machine definition for tool executions.
package runstate

import (
	"database/sql/driver"
	"fmt"
)

// ToolExecutionState represents the current state of a tool execution.
type ToolExecutionState string

const (
	// ToolExecPending indicates the tool execution is waiting to be picked up by a worker.
	ToolExecPending ToolExecutionState = "pending"

	// ToolExecRunning indicates the tool is currently being executed.
	ToolExecRunning ToolExecutionState = "running"

	// ToolExecCompleted indicates the tool execution finished successfully.
	ToolExecCompleted ToolExecutionState = "completed"

	// ToolExecFailed indicates the tool execution failed with an error.
	// Failed executions may be retried if attempt_count < max_attempts.
	ToolExecFailed ToolExecutionState = "failed"

	// ToolExecSkipped indicates the tool execution was skipped.
	// This happens when the parent run is cancelled.
	ToolExecSkipped ToolExecutionState = "skipped"
)

// AllToolExecutionStates returns all possible tool execution states.
func AllToolExecutionStates() []ToolExecutionState {
	return []ToolExecutionState{
		ToolExecPending,
		ToolExecRunning,
		ToolExecCompleted,
		ToolExecFailed,
		ToolExecSkipped,
	}
}

// TerminalToolExecutionStates returns all terminal (final) tool execution states.
func TerminalToolExecutionStates() []ToolExecutionState {
	return []ToolExecutionState{
		ToolExecCompleted,
		ToolExecFailed,
		ToolExecSkipped,
	}
}

// IsValid returns true if the state is a valid ToolExecutionState value.
func (s ToolExecutionState) IsValid() bool {
	switch s {
	case ToolExecPending, ToolExecRunning, ToolExecCompleted, ToolExecFailed, ToolExecSkipped:
		return true
	default:
		return false
	}
}

// IsTerminal returns true if the state is a terminal (final) state.
// Terminal states indicate the execution is complete (success or failure).
func (s ToolExecutionState) IsTerminal() bool {
	switch s {
	case ToolExecCompleted, ToolExecFailed, ToolExecSkipped:
		return true
	default:
		return false
	}
}

// IsSuccess returns true if the execution completed successfully.
func (s ToolExecutionState) IsSuccess() bool {
	return s == ToolExecCompleted
}

// IsError returns true if the execution failed with an error.
func (s ToolExecutionState) IsError() bool {
	return s == ToolExecFailed
}

// CanRetry returns true if this execution state can be retried.
// Only failed executions can be retried (if under max_attempts).
func (s ToolExecutionState) CanRetry() bool {
	return s == ToolExecFailed
}

// IsWaitingForWork returns true if the execution is waiting for worker pickup.
func (s ToolExecutionState) IsWaitingForWork() bool {
	return s == ToolExecPending
}

// IsInProgress returns true if the execution is currently running.
func (s ToolExecutionState) IsInProgress() bool {
	return s == ToolExecRunning
}

// CanTransitionTo returns true if a transition from this state to the
// target state is valid.
//
// Valid transitions:
//   - pending -> running (worker claims execution)
//   - pending -> skipped (run cancelled)
//   - running -> completed (success)
//   - running -> failed (error)
//   - running -> skipped (run cancelled)
//   - failed -> pending (retry, if under max_attempts)
//
// Invalid transitions:
//   - completed -> any (terminal)
//   - skipped -> any (terminal)
//   - Same state to same state
func (s ToolExecutionState) CanTransitionTo(target ToolExecutionState) bool {
	// Same state is not a valid transition
	if s == target {
		return false
	}

	switch s {
	case ToolExecPending:
		return target == ToolExecRunning || target == ToolExecSkipped
	case ToolExecRunning:
		return target == ToolExecCompleted || target == ToolExecFailed || target == ToolExecSkipped
	case ToolExecFailed:
		// Can retry (back to pending)
		return target == ToolExecPending
	case ToolExecCompleted, ToolExecSkipped:
		// Terminal states
		return false
	}

	return false
}

// String returns the string representation of the state.
func (s ToolExecutionState) String() string {
	return string(s)
}

// Value implements driver.Valuer for database serialization.
func (s ToolExecutionState) Value() (driver.Value, error) {
	return string(s), nil
}

// Scan implements sql.Scanner for database deserialization.
func (s *ToolExecutionState) Scan(src any) error {
	switch v := src.(type) {
	case string:
		state := ToolExecutionState(v)
		if !state.IsValid() {
			return fmt.Errorf("runstate: invalid tool execution state %q", v)
		}
		*s = state
		return nil
	case []byte:
		state := ToolExecutionState(v)
		if !state.IsValid() {
			return fmt.Errorf("runstate: invalid tool execution state %q", v)
		}
		*s = state
		return nil
	default:
		return fmt.Errorf("runstate: cannot scan type %T into ToolExecutionState", src)
	}
}

// ToolExecutionTransition represents a tool execution state transition with validation.
type ToolExecutionTransition struct {
	From ToolExecutionState
	To   ToolExecutionState
}

// Validate returns an error if the transition is invalid.
func (t ToolExecutionTransition) Validate() error {
	if !t.From.IsValid() {
		return fmt.Errorf("runstate: invalid source tool execution state %q", t.From)
	}
	if !t.To.IsValid() {
		return fmt.Errorf("runstate: invalid target tool execution state %q", t.To)
	}
	if !t.From.CanTransitionTo(t.To) {
		return fmt.Errorf("runstate: invalid tool execution transition from %q to %q", t.From, t.To)
	}
	return nil
}

// ValidToolExecutionTransitions returns all valid tool execution state transitions.
func ValidToolExecutionTransitions() []ToolExecutionTransition {
	return []ToolExecutionTransition{
		{From: ToolExecPending, To: ToolExecRunning},
		{From: ToolExecPending, To: ToolExecSkipped},
		{From: ToolExecRunning, To: ToolExecCompleted},
		{From: ToolExecRunning, To: ToolExecFailed},
		{From: ToolExecRunning, To: ToolExecSkipped},
		{From: ToolExecFailed, To: ToolExecPending}, // Retry
	}
}
