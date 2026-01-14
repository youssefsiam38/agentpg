package service

import "errors"

// Service package errors.
var (
	// ErrNotFound indicates a resource was not found.
	ErrNotFound = errors.New("service: not found")
)
