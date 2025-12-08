package leadership

import "errors"

// Errors returned by the leadership package.
var (
	// ErrAlreadyStarted is returned when Start() is called on an already started elector.
	ErrAlreadyStarted = errors.New("elector already started")

	// ErrNotStarted is returned when Stop() is called on an elector that hasn't started.
	ErrNotStarted = errors.New("elector not started")
)
