package runstate

import (
	"testing"
)

func TestRunState_IsValid(t *testing.T) {
	tests := []struct {
		state RunState
		valid bool
	}{
		{RunStateRunning, true},
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
		{RunStateRunning, false},
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
		// Valid transitions from running
		{RunStateRunning, RunStateCompleted, true},
		{RunStateRunning, RunStateCancelled, true},
		{RunStateRunning, RunStateFailed, true},

		// Invalid: running to running
		{RunStateRunning, RunStateRunning, false},

		// Invalid: terminal states cannot transition
		{RunStateCompleted, RunStateRunning, false},
		{RunStateCompleted, RunStateFailed, false},
		{RunStateCancelled, RunStateRunning, false},
		{RunStateFailed, RunStateCompleted, false},
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
		{"valid: running->completed", Transition{RunStateRunning, RunStateCompleted}, false},
		{"valid: running->cancelled", Transition{RunStateRunning, RunStateCancelled}, false},
		{"valid: running->failed", Transition{RunStateRunning, RunStateFailed}, false},
		{"invalid: completed->running", Transition{RunStateCompleted, RunStateRunning}, true},
		{"invalid: invalid source", Transition{RunState("bad"), RunStateCompleted}, true},
		{"invalid: invalid target", Transition{RunStateRunning, RunState("bad")}, true},
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
		{"string running", "running", RunStateRunning, false},
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
	if len(states) != 4 {
		t.Errorf("AllStates() returned %d states, want 4", len(states))
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
	if len(transitions) != 3 {
		t.Errorf("ValidTransitions() returned %d transitions, want 3", len(transitions))
	}

	// All should be valid
	for _, tr := range transitions {
		if err := tr.Validate(); err != nil {
			t.Errorf("ValidTransitions() returned invalid transition: %v", err)
		}
	}
}
