// Package maintenance provides background services for AgentPG instances.
//
// This package includes:
//   - Heartbeat service: keeps the instance registered as active
//   - Cleanup service: removes stale instances and orphaned runs (leader only)
package maintenance

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/youssefsiam38/agentpg/storage"
)

// Default heartbeat configuration values
const (
	DefaultHeartbeatInterval = 30 * time.Second
	DefaultInstanceTTL       = 2 * time.Minute
)

// HeartbeatConfig holds configuration for the heartbeat service.
type HeartbeatConfig struct {
	// Interval is how often to send heartbeats.
	// Default: 30 seconds
	Interval time.Duration

	// OnError is called when a heartbeat fails.
	// If nil, errors are silently ignored.
	OnError func(err error)
}

// DefaultHeartbeatConfig returns the default heartbeat configuration.
func DefaultHeartbeatConfig() *HeartbeatConfig {
	return &HeartbeatConfig{
		Interval: DefaultHeartbeatInterval,
	}
}

// Heartbeat sends periodic heartbeats to keep an instance registered as active.
type Heartbeat struct {
	store      storage.Store
	instanceID string
	config     *HeartbeatConfig

	started atomic.Bool
	done    chan struct{}
	cancel  context.CancelFunc
}

// NewHeartbeat creates a new heartbeat service.
func NewHeartbeat(store storage.Store, instanceID string, config *HeartbeatConfig) *Heartbeat {
	if config == nil {
		config = DefaultHeartbeatConfig()
	}

	return &Heartbeat{
		store:      store,
		instanceID: instanceID,
		config:     config,
		done:       make(chan struct{}),
	}
}

// Start begins sending heartbeats.
// It returns immediately and runs the heartbeat loop in a goroutine.
func (h *Heartbeat) Start(ctx context.Context) error {
	if !h.started.CompareAndSwap(false, true) {
		return ErrAlreadyStarted
	}

	ctx, h.cancel = context.WithCancel(ctx)
	go h.run(ctx)

	return nil
}

// Stop stops sending heartbeats.
func (h *Heartbeat) Stop(ctx context.Context) error {
	if !h.started.Load() {
		return ErrNotStarted
	}

	h.cancel()
	<-h.done

	h.started.Store(false)
	return nil
}

// run is the main heartbeat loop.
func (h *Heartbeat) run(ctx context.Context) {
	defer close(h.done)

	// Send initial heartbeat
	h.sendHeartbeat(ctx)

	ticker := time.NewTicker(h.config.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.sendHeartbeat(ctx)
		}
	}
}

// sendHeartbeat sends a single heartbeat.
func (h *Heartbeat) sendHeartbeat(ctx context.Context) {
	err := h.store.UpdateInstanceHeartbeat(ctx, h.instanceID)
	if err != nil && h.config.OnError != nil {
		h.config.OnError(err)
	}
}

// IsRunning returns true if the heartbeat service is running.
func (h *Heartbeat) IsRunning() bool {
	return h.started.Load()
}
