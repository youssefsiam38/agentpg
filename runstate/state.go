// Package runstate provides the state machine definition for agent runs.
//
// A run represents a single prompt-response cycle, potentially including
// multiple tool call iterations. Each run has a state that progresses
// through the state machine until reaching a terminal state.
//
// State Machine:
//
//	running -> completed  (successful completion)
//	running -> cancelled  (user/system cancellation)
//	running -> failed     (error during execution)
//
// Terminal states (completed, cancelled, failed) cannot transition further.
package runstate

import (
	"database/sql/driver"
	"fmt"
)

// RunState represents the current state of an agent run.
type RunState string

const (
	// RunStateRunning indicates the run is currently in progress.
	// This is the initial state when a run is created.
	RunStateRunning RunState = "running"

	// RunStateCompleted indicates the run finished successfully.
	// The response_text and stop_reason fields will be populated.
	RunStateCompleted RunState = "completed"

	// RunStateCancelled indicates the run was cancelled.
	// This can happen due to context cancellation or explicit cancel request.
	RunStateCancelled RunState = "cancelled"

	// RunStateFailed indicates the run failed with an error.
	// The error_message and error_type fields will be populated.
	RunStateFailed RunState = "failed"
)

// AllStates returns all possible run states.
func AllStates() []RunState {
	return []RunState{
		RunStateRunning,
		RunStateCompleted,
		RunStateCancelled,
		RunStateFailed,
	}
}

// TerminalStates returns all terminal (final) states.
func TerminalStates() []RunState {
	return []RunState{
		RunStateCompleted,
		RunStateCancelled,
		RunStateFailed,
	}
}

// IsValid returns true if the state is a valid RunState value.
func (s RunState) IsValid() bool {
	switch s {
	case RunStateRunning, RunStateCompleted, RunStateCancelled, RunStateFailed:
		return true
	default:
		return false
	}
}

// IsTerminal returns true if the state is a terminal (final) state.
// Terminal states cannot transition to any other state.
func (s RunState) IsTerminal() bool {
	switch s {
	case RunStateCompleted, RunStateCancelled, RunStateFailed:
		return true
	default:
		return false
	}
}

// CanTransitionTo returns true if a transition from this state to the
// target state is valid.
//
// Valid transitions:
//   - running -> completed
//   - running -> cancelled
//   - running -> failed
//
// Invalid transitions:
//   - Any terminal state to any other state
//   - running -> running (no-op, not a valid transition)
func (s RunState) CanTransitionTo(target RunState) bool {
	// Terminal states cannot transition
	if s.IsTerminal() {
		return false
	}

	// From running, can only go to terminal states
	if s == RunStateRunning && target.IsTerminal() {
		return true
	}

	return false
}

// String returns the string representation of the state.
func (s RunState) String() string {
	return string(s)
}

// Value implements driver.Valuer for database serialization.
func (s RunState) Value() (driver.Value, error) {
	return string(s), nil
}

// Scan implements sql.Scanner for database deserialization.
func (s *RunState) Scan(src any) error {
	switch v := src.(type) {
	case string:
		state := RunState(v)
		if !state.IsValid() {
			return fmt.Errorf("runstate: invalid state %q", v)
		}
		*s = state
		return nil
	case []byte:
		state := RunState(v)
		if !state.IsValid() {
			return fmt.Errorf("runstate: invalid state %q", v)
		}
		*s = state
		return nil
	default:
		return fmt.Errorf("runstate: cannot scan type %T into RunState", src)
	}
}

// Transition represents a state transition with validation.
type Transition struct {
	From RunState
	To   RunState
}

// Validate returns an error if the transition is invalid.
func (t Transition) Validate() error {
	if !t.From.IsValid() {
		return fmt.Errorf("runstate: invalid source state %q", t.From)
	}
	if !t.To.IsValid() {
		return fmt.Errorf("runstate: invalid target state %q", t.To)
	}
	if !t.From.CanTransitionTo(t.To) {
		return fmt.Errorf("runstate: invalid transition from %q to %q", t.From, t.To)
	}
	return nil
}

// ValidTransitions returns all valid state transitions.
func ValidTransitions() []Transition {
	return []Transition{
		{From: RunStateRunning, To: RunStateCompleted},
		{From: RunStateRunning, To: RunStateCancelled},
		{From: RunStateRunning, To: RunStateFailed},
	}
}

// ErrorType represents the classification of a run error.
type ErrorType string

const (
	// ErrorTypeOrphan indicates the run was orphaned due to instance disconnect.
	ErrorTypeOrphan ErrorType = "orphan"

	// ErrorTypeTimeout indicates the run timed out.
	ErrorTypeTimeout ErrorType = "timeout"

	// ErrorTypeAPI indicates an error from the Claude API.
	ErrorTypeAPI ErrorType = "api"

	// ErrorTypeTool indicates an error during tool execution.
	ErrorTypeTool ErrorType = "tool"

	// ErrorTypeInternal indicates an internal agentpg error.
	ErrorTypeInternal ErrorType = "internal"

	// ErrorTypeCancelled indicates the run was cancelled by context.
	ErrorTypeCancelled ErrorType = "cancelled"
)

// String returns the string representation of the error type.
func (e ErrorType) String() string {
	return string(e)
}

// StopReason represents why a run stopped (from Claude API).
type StopReason string

const (
	// StopReasonEndTurn indicates the model finished generating.
	StopReasonEndTurn StopReason = "end_turn"

	// StopReasonToolUse indicates the model wants to use a tool.
	StopReasonToolUse StopReason = "tool_use"

	// StopReasonMaxTokens indicates max tokens was reached.
	StopReasonMaxTokens StopReason = "max_tokens"

	// StopReasonStopSequence indicates a stop sequence was hit.
	StopReasonStopSequence StopReason = "stop_sequence"
)

// String returns the string representation of the stop reason.
func (r StopReason) String() string {
	return string(r)
}
