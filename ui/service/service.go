package service

import (
	"github.com/youssefsiam38/agentpg/driver"
)

// Service provides admin UI operations.
// The TTx type parameter represents the native transaction type
// from the driver (e.g., pgx.Tx or *sql.Tx).
type Service[TTx any] struct {
	store driver.Store[TTx]
}

// New creates a new Service with the given store.
func New[TTx any](store driver.Store[TTx]) *Service[TTx] {
	return &Service[TTx]{
		store: store,
	}
}

// Store returns the underlying store.
// This is useful for advanced operations not covered by the service.
func (s *Service[TTx]) Store() driver.Store[TTx] {
	return s.store
}
