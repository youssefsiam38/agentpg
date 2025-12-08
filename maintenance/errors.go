package maintenance

import "errors"

// Errors returned by the maintenance package.
var (
	// ErrAlreadyStarted is returned when Start() is called on an already started service.
	ErrAlreadyStarted = errors.New("service already started")

	// ErrNotStarted is returned when Stop() is called on a service that hasn't started.
	ErrNotStarted = errors.New("service not started")
)
