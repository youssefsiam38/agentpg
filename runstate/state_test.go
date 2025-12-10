package runstate

import (
	"testing"
)

func TestRunState_IsValid(t *testing.T) {
	tests := []struct {
		state RunState
		valid bool
	}{
		{RunStatePending, true},
		{RunStatePendingAPI, true},
		{RunStatePendingTools, true},
		{RunStateAwaitingContinuation, true},
		{RunStateCompleted, true},
		{RunStateCancelled, true},
		{RunStateFailed, true},
		{RunState("invalid"), false},
		{RunState(""), false},
	}

	for _, tt := range tests {
		t.Run(string(tt.state), func(t *testing.T) {
			if got := tt.state.IsValid(); got != tt.valid {
				t.Errorf("IsValid() = %v, want %v", got, tt.valid)
			}
		})
	}
}

func TestRunState_IsTerminal(t *testing.T) {
	tests := []struct {
		state    RunState
		terminal bool
	}{
		{RunStatePending, false},
		{RunStatePendingAPI, false},
		{RunStatePendingTools, false},
		{RunStateAwaitingContinuation, false},
		{RunStateCompleted, true},
		{RunStateCancelled, true},
		{RunStateFailed, true},
	}

	for _, tt := range tests {
		t.Run(string(tt.state), func(t *testing.T) {
			if got := tt.state.IsTerminal(); got != tt.terminal {
				t.Errorf("IsTerminal() = %v, want %v", got, tt.terminal)
			}
		})
	}
}

func TestRunState_CanTransitionTo(t *testing.T) {
	tests := []struct {
		from  RunState
		to    RunState
		valid bool
	}{
		// Valid transitions from pending
		{RunStatePending, RunStatePendingAPI, true},
		{RunStatePending, RunStateCancelled, true},
		{RunStatePending, RunStateFailed, true},

		// Valid transitions from pending_api
		{RunStatePendingAPI, RunStateCompleted, true},
		{RunStatePendingAPI, RunStatePendingTools, true},
		{RunStatePendingAPI, RunStateAwaitingContinuation, true},
		{RunStatePendingAPI, RunStateCancelled, true},
		{RunStatePendingAPI, RunStateFailed, true},

		// Valid transitions from pending_tools
		{RunStatePendingTools, RunStatePendingAPI, true},
		{RunStatePendingTools, RunStateCancelled, true},
		{RunStatePendingTools, RunStateFailed, true},

		// Valid transitions from awaiting_continuation
		{RunStateAwaitingContinuation, RunStatePendingAPI, true},
		{RunStateAwaitingContinuation, RunStateCancelled, true},
		{RunStateAwaitingContinuation, RunStateFailed, true},

		// Invalid: same state to same state
		{RunStatePending, RunStatePending, false},
		{RunStatePendingAPI, RunStatePendingAPI, false},

		// Invalid: terminal states cannot transition
		{RunStateCompleted, RunStatePending, false},
		{RunStateCompleted, RunStateFailed, false},
		{RunStateCancelled, RunStatePending, false},
		{RunStateFailed, RunStateCompleted, false},

		// Invalid: backwards transitions
		{RunStatePendingAPI, RunStatePending, false},
	}

	for _, tt := range tests {
		name := string(tt.from) + "->" + string(tt.to)
		t.Run(name, func(t *testing.T) {
			if got := tt.from.CanTransitionTo(tt.to); got != tt.valid {
				t.Errorf("CanTransitionTo() = %v, want %v", got, tt.valid)
			}
		})
	}
}

func TestTransition_Validate(t *testing.T) {
	tests := []struct {
		name    string
		tr      Transition
		wantErr bool
	}{
		{"valid: pending->pending_api", Transition{RunStatePending, RunStatePendingAPI}, false},
		{"valid: pending_api->completed", Transition{RunStatePendingAPI, RunStateCompleted}, false},
		{"valid: pending_api->pending_tools", Transition{RunStatePendingAPI, RunStatePendingTools}, false},
		{"valid: pending_tools->pending_api", Transition{RunStatePendingTools, RunStatePendingAPI}, false},
		{"invalid: completed->pending", Transition{RunStateCompleted, RunStatePending}, true},
		{"invalid: invalid source", Transition{RunState("bad"), RunStateCompleted}, true},
		{"invalid: invalid target", Transition{RunStatePending, RunState("bad")}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.tr.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestRunState_Scan(t *testing.T) {
	tests := []struct {
		name    string
		input   any
		want    RunState
		wantErr bool
	}{
		{"string pending", "pending", RunStatePending, false},
		{"string pending_api", "pending_api", RunStatePendingAPI, false},
		{"string completed", "completed", RunStateCompleted, false},
		{"bytes cancelled", []byte("cancelled"), RunStateCancelled, false},
		{"bytes failed", []byte("failed"), RunStateFailed, false},
		{"invalid string", "invalid", RunState(""), true},
		{"invalid type", 123, RunState(""), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var s RunState
			err := s.Scan(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("Scan() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && s != tt.want {
				t.Errorf("Scan() got = %v, want %v", s, tt.want)
			}
		})
	}
}

func TestAllStates(t *testing.T) {
	states := AllStates()
	if len(states) != 7 {
		t.Errorf("AllStates() returned %d states, want 7", len(states))
	}

	// Verify all states are valid
	for _, s := range states {
		if !s.IsValid() {
			t.Errorf("AllStates() returned invalid state: %s", s)
		}
	}
}

func TestTerminalStates(t *testing.T) {
	states := TerminalStates()
	if len(states) != 3 {
		t.Errorf("TerminalStates() returned %d states, want 3", len(states))
	}

	// Verify all are terminal
	for _, s := range states {
		if !s.IsTerminal() {
			t.Errorf("TerminalStates() returned non-terminal state: %s", s)
		}
	}
}

func TestValidTransitions(t *testing.T) {
	transitions := ValidTransitions()
	// pending -> pending_api, cancelled, failed (3)
	// pending_api -> completed, pending_tools, awaiting_continuation, cancelled, failed (5)
	// pending_tools -> pending_api, cancelled, failed (3)
	// awaiting_continuation -> pending_api, cancelled, failed (3)
	// Total: 14 transitions
	if len(transitions) != 14 {
		t.Errorf("ValidTransitions() returned %d transitions, want 14", len(transitions))
	}

	// All should be valid
	for _, tr := range transitions {
		if err := tr.Validate(); err != nil {
			t.Errorf("ValidTransitions() returned invalid transition: %v", err)
		}
	}
}

func TestWorkableStates(t *testing.T) {
	states := WorkableStates()
	if len(states) != 4 {
		t.Errorf("WorkableStates() returned %d states, want 4", len(states))
	}

	// Verify all are workable
	for _, s := range states {
		if !s.IsWorkable() {
			t.Errorf("WorkableStates() returned non-workable state: %s", s)
		}
	}
}

func TestStopReason_NextRunState(t *testing.T) {
	tests := []struct {
		reason StopReason
		want   RunState
	}{
		{StopReasonEndTurn, RunStateCompleted},
		{StopReasonStopSequence, RunStateCompleted},
		{StopReasonToolUse, RunStatePendingTools},
		{StopReasonMaxTokens, RunStateAwaitingContinuation},
		{StopReasonPauseTurn, RunStateAwaitingContinuation},
		{StopReasonRefusal, RunStateFailed},
	}

	for _, tt := range tests {
		t.Run(string(tt.reason), func(t *testing.T) {
			if got := tt.reason.NextRunState(); got != tt.want {
				t.Errorf("NextRunState() = %v, want %v", got, tt.want)
			}
		})
	}
}
