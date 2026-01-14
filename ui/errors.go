package ui

import "errors"

// UI package errors.
var (
	// ErrInvalidConfig indicates invalid configuration.
	ErrInvalidConfig = errors.New("ui: invalid configuration")

	// ErrNotFound indicates a resource was not found.
	ErrNotFound = errors.New("ui: not found")

	// ErrBadRequest indicates an invalid request.
	ErrBadRequest = errors.New("ui: bad request")

	// ErrReadOnly indicates the UI is in read-only mode.
	ErrReadOnly = errors.New("ui: read-only mode")

	// ErrClientRequired indicates a client is required for the operation.
	ErrClientRequired = errors.New("ui: client required for chat operations")
)
