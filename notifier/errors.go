package notifier

import "errors"

// Errors returned by the notifier package.
var (
	// ErrAlreadyStarted is returned when Start() is called on an already started notifier.
	ErrAlreadyStarted = errors.New("notifier already started")

	// ErrNotStarted is returned when Stop() is called on a notifier that hasn't started.
	ErrNotStarted = errors.New("notifier not started")

	// ErrNotifyNotSupported is returned when Notify is called but no notifier is available.
	ErrNotifyNotSupported = errors.New("notify not supported")

	// ErrUnknownEventType is returned when an unknown event type is used.
	ErrUnknownEventType = errors.New("unknown event type")
)
