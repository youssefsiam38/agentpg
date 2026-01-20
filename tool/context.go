package tool

import (
	"context"

	"github.com/google/uuid"
)

// Context keys for run information passed to tools
type contextKey string

const (
	runIDKey      contextKey = "agentpg_run_id"
	sessionIDKey  contextKey = "agentpg_session_id"
	variablesKey  contextKey = "agentpg_variables"
)

// RunContext contains run-level information available to tools during execution.
// Tools can access this via GetRunContext() or the convenience GetVariable() helper.
type RunContext struct {
	// RunID is the unique identifier of the current run
	RunID uuid.UUID

	// SessionID is the unique identifier of the session
	SessionID uuid.UUID

	// Variables contains per-run variables passed via the Run() call.
	// These are useful for passing context like storyID, tenantID, userID, etc.
	Variables map[string]any
}

// WithRunContext attaches run context to the given context.
// This is called internally by the tool worker before executing a tool.
func WithRunContext(ctx context.Context, rc RunContext) context.Context {
	ctx = context.WithValue(ctx, runIDKey, rc.RunID)
	ctx = context.WithValue(ctx, sessionIDKey, rc.SessionID)
	ctx = context.WithValue(ctx, variablesKey, rc.Variables)
	return ctx
}

// GetRunContext extracts the full run context from the context.
// Returns false if the context was not enriched with run information.
func GetRunContext(ctx context.Context) (RunContext, bool) {
	runID, ok1 := ctx.Value(runIDKey).(uuid.UUID)
	sessionID, ok2 := ctx.Value(sessionIDKey).(uuid.UUID)
	vars, _ := ctx.Value(variablesKey).(map[string]any)

	if !ok1 || !ok2 {
		return RunContext{}, false
	}
	return RunContext{RunID: runID, SessionID: sessionID, Variables: vars}, true
}

// GetRunID extracts the run ID from the context.
// Returns uuid.Nil and false if not available.
func GetRunID(ctx context.Context) (uuid.UUID, bool) {
	runID, ok := ctx.Value(runIDKey).(uuid.UUID)
	return runID, ok
}

// GetSessionID extracts the session ID from the context.
// Returns uuid.Nil and false if not available.
func GetSessionID(ctx context.Context) (uuid.UUID, bool) {
	sessionID, ok := ctx.Value(sessionIDKey).(uuid.UUID)
	return sessionID, ok
}

// GetVariables extracts all variables from the context.
// Returns nil if no variables were set.
func GetVariables(ctx context.Context) map[string]any {
	vars, _ := ctx.Value(variablesKey).(map[string]any)
	return vars
}

// GetVariable extracts a single variable from the context by key.
// The type parameter T specifies the expected type of the variable.
// Returns the zero value and false if the variable is not found or has wrong type.
//
// Example:
//
//	storyID, ok := tool.GetVariable[string](ctx, "story_id")
//	if !ok {
//	    return "", errors.New("story_id not provided")
//	}
func GetVariable[T any](ctx context.Context, key string) (T, bool) {
	vars, _ := ctx.Value(variablesKey).(map[string]any)
	if vars == nil {
		var zero T
		return zero, false
	}
	val, ok := vars[key]
	if !ok {
		var zero T
		return zero, false
	}
	typed, ok := val.(T)
	if !ok {
		var zero T
		return zero, false
	}
	return typed, ok
}

// MustGetVariable extracts a variable from the context or panics if not found.
// Use this only when the variable is guaranteed to be present.
//
// Example:
//
//	storyID := tool.MustGetVariable[string](ctx, "story_id")
func MustGetVariable[T any](ctx context.Context, key string) T {
	val, ok := GetVariable[T](ctx, key)
	if !ok {
		panic("agentpg: missing or invalid required variable: " + key)
	}
	return val
}

// GetVariableOr extracts a variable from the context or returns the default value.
//
// Example:
//
//	maxRetries := tool.GetVariableOr[int](ctx, "max_retries", 3)
func GetVariableOr[T any](ctx context.Context, key string, defaultValue T) T {
	val, ok := GetVariable[T](ctx, key)
	if !ok {
		return defaultValue
	}
	return val
}
