// Package runstate provides the state machine definition for agent runs.
//
// A run represents a single prompt-response cycle, potentially including
// multiple tool call iterations. Each run has a state that progresses
// through the state machine until reaching a terminal state.
//
// Event-Driven State Machine:
//
//	pending -> pending_api              (worker claims run)
//	pending_api -> completed            (API returns end_turn or stop_sequence)
//	pending_api -> pending_tools        (API returns tool_use)
//	pending_api -> awaiting_continuation (API returns pause_turn or max_tokens)
//	pending_api -> failed               (API returns refusal or error)
//	pending_tools -> pending_api        (all tools complete)
//	awaiting_continuation -> pending_api (continuation requested)
//	* -> cancelled                      (user/system cancellation)
//	* -> failed                         (error during execution)
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
	// RunStatePending indicates the run is created but not yet picked up by a worker.
	// This is the initial state when a run is created via Run().
	RunStatePending RunState = "pending"

	// RunStatePendingAPI indicates a worker has claimed the run and is calling the Claude API.
	RunStatePendingAPI RunState = "pending_api"

	// RunStatePendingTools indicates the API returned tool_use and we're waiting for
	// all tool executions to complete before sending tool results.
	RunStatePendingTools RunState = "pending_tools"

	// RunStateAwaitingContinuation indicates the run needs continuation.
	// This happens when the API returns pause_turn or max_tokens with more content expected.
	RunStateAwaitingContinuation RunState = "awaiting_continuation"

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
		RunStatePending,
		RunStatePendingAPI,
		RunStatePendingTools,
		RunStateAwaitingContinuation,
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

// WorkableStates returns all states that can be picked up by workers.
func WorkableStates() []RunState {
	return []RunState{
		RunStatePending,
		RunStatePendingAPI,
		RunStatePendingTools,
		RunStateAwaitingContinuation,
	}
}

// IsValid returns true if the state is a valid RunState value.
func (s RunState) IsValid() bool {
	switch s {
	case RunStatePending, RunStatePendingAPI, RunStatePendingTools,
		RunStateAwaitingContinuation, RunStateCompleted, RunStateCancelled, RunStateFailed:
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

// IsWorkable returns true if the state can be picked up by workers for processing.
func (s RunState) IsWorkable() bool {
	switch s {
	case RunStatePending, RunStatePendingAPI, RunStatePendingTools, RunStateAwaitingContinuation:
		return true
	default:
		return false
	}
}

// IsWaitingForWork returns true if the state is waiting for worker pickup.
// This is a subset of workable states that haven't been claimed yet.
func (s RunState) IsWaitingForWork() bool {
	return s == RunStatePending
}

// NeedsAPICall returns true if this state requires a Claude API call.
func (s RunState) NeedsAPICall() bool {
	switch s {
	case RunStatePendingAPI, RunStateAwaitingContinuation:
		return true
	default:
		return false
	}
}

// NeedsToolExecution returns true if this state is waiting for tool executions.
func (s RunState) NeedsToolExecution() bool {
	return s == RunStatePendingTools
}

// CanTransitionTo returns true if a transition from this state to the
// target state is valid.
//
// Valid transitions:
//   - pending -> pending_api (worker claims run)
//   - pending -> cancelled
//   - pending -> failed
//   - pending_api -> completed (end_turn, stop_sequence)
//   - pending_api -> pending_tools (tool_use)
//   - pending_api -> awaiting_continuation (pause_turn, max_tokens)
//   - pending_api -> cancelled
//   - pending_api -> failed
//   - pending_tools -> pending_api (all tools complete)
//   - pending_tools -> cancelled
//   - pending_tools -> failed
//   - awaiting_continuation -> pending_api (continuation)
//   - awaiting_continuation -> cancelled
//   - awaiting_continuation -> failed
//
// Invalid transitions:
//   - Any terminal state to any other state
//   - Same state to same state (no-op)
func (s RunState) CanTransitionTo(target RunState) bool {
	// Terminal states cannot transition
	if s.IsTerminal() {
		return false
	}

	// Same state is not a valid transition
	if s == target {
		return false
	}

	// Any workable state can transition to terminal states
	if target.IsTerminal() {
		return true
	}

	// Specific transitions
	switch s {
	case RunStatePending:
		return target == RunStatePendingAPI
	case RunStatePendingAPI:
		return target == RunStatePendingTools || target == RunStateAwaitingContinuation
	case RunStatePendingTools:
		return target == RunStatePendingAPI
	case RunStateAwaitingContinuation:
		return target == RunStatePendingAPI
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
		// From pending
		{From: RunStatePending, To: RunStatePendingAPI},
		{From: RunStatePending, To: RunStateCancelled},
		{From: RunStatePending, To: RunStateFailed},
		// From pending_api
		{From: RunStatePendingAPI, To: RunStateCompleted},
		{From: RunStatePendingAPI, To: RunStatePendingTools},
		{From: RunStatePendingAPI, To: RunStateAwaitingContinuation},
		{From: RunStatePendingAPI, To: RunStateCancelled},
		{From: RunStatePendingAPI, To: RunStateFailed},
		// From pending_tools
		{From: RunStatePendingTools, To: RunStatePendingAPI},
		{From: RunStatePendingTools, To: RunStateCancelled},
		{From: RunStatePendingTools, To: RunStateFailed},
		// From awaiting_continuation
		{From: RunStateAwaitingContinuation, To: RunStatePendingAPI},
		{From: RunStateAwaitingContinuation, To: RunStateCancelled},
		{From: RunStateAwaitingContinuation, To: RunStateFailed},
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

	// ErrorTypeRefusal indicates the model refused to respond (content policy).
	ErrorTypeRefusal ErrorType = "refusal"
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

	// StopReasonPauseTurn indicates a long-running turn was paused.
	// The response can be sent back as-is to continue.
	StopReasonPauseTurn StopReason = "pause_turn"

	// StopReasonRefusal indicates streaming classifiers intervened
	// to handle potential policy violations.
	StopReasonRefusal StopReason = "refusal"
)

// String returns the string representation of the stop reason.
func (r StopReason) String() string {
	return string(r)
}

// IsValid returns true if the stop reason is a known value.
func (r StopReason) IsValid() bool {
	switch r {
	case StopReasonEndTurn, StopReasonToolUse, StopReasonMaxTokens,
		StopReasonStopSequence, StopReasonPauseTurn, StopReasonRefusal:
		return true
	default:
		return false
	}
}

// RequiresContinuation returns true if this stop reason means the run
// should continue with another API call.
func (r StopReason) RequiresContinuation() bool {
	switch r {
	case StopReasonPauseTurn, StopReasonMaxTokens:
		return true
	default:
		return false
	}
}

// RequiresToolExecution returns true if this stop reason means
// tool executions need to be processed.
func (r StopReason) RequiresToolExecution() bool {
	return r == StopReasonToolUse
}

// IsTerminal returns true if this stop reason indicates the run is complete.
func (r StopReason) IsTerminal() bool {
	switch r {
	case StopReasonEndTurn, StopReasonStopSequence:
		return true
	default:
		return false
	}
}

// IsError returns true if this stop reason indicates an error.
func (r StopReason) IsError() bool {
	return r == StopReasonRefusal
}

// NextRunState returns the appropriate next run state based on this stop reason.
func (r StopReason) NextRunState() RunState {
	switch r {
	case StopReasonEndTurn, StopReasonStopSequence:
		return RunStateCompleted
	case StopReasonToolUse:
		return RunStatePendingTools
	case StopReasonMaxTokens, StopReasonPauseTurn:
		return RunStateAwaitingContinuation
	case StopReasonRefusal:
		return RunStateFailed
	default:
		// Unknown stop reason - treat as completed
		return RunStateCompleted
	}
}

// NextRunStateForStopReason returns the appropriate next run state
// based on the given stop reason string.
func NextRunStateForStopReason(reason string) RunState {
	return StopReason(reason).NextRunState()
}
