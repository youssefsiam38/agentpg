// Package leadership provides leader election for distributed AgentPG instances.
//
// Only one instance can be the leader at a time. The leader is responsible for
// running cleanup operations like orphan run detection and stale instance removal.
//
// Leader election uses a TTL-based lease stored in PostgreSQL. The leader must
// renew its lease before it expires, or another instance can take over.
package leadership

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/youssefsiam38/agentpg/storage"
)

// Default configuration values
const (
	DefaultLeaderTTL       = 30 * time.Second
	DefaultElectionPeriod  = 10 * time.Second
	DefaultReelectionDelay = 5 * time.Second
)

// Config holds configuration for the leader election system.
type Config struct {
	// LeaderTTL is how long a leader's lease is valid.
	// Default: 30 seconds
	LeaderTTL time.Duration

	// ElectionPeriod is how often to attempt becoming leader when not leader.
	// Default: 10 seconds
	ElectionPeriod time.Duration

	// ReelectionDelay is how long to wait before attempting re-election after
	// becoming leader. Should be less than LeaderTTL.
	// Default: 5 seconds
	ReelectionDelay time.Duration
}

// DefaultConfig returns the default configuration.
func DefaultConfig() *Config {
	return &Config{
		LeaderTTL:       DefaultLeaderTTL,
		ElectionPeriod:  DefaultElectionPeriod,
		ReelectionDelay: DefaultReelectionDelay,
	}
}

// Callbacks are called when leadership status changes.
type Callbacks struct {
	// OnBecameLeader is called when this instance becomes the leader.
	// It is called with the context that was passed to Start().
	OnBecameLeader func(ctx context.Context)

	// OnLostLeadership is called when this instance loses leadership.
	// This can happen due to:
	//   - Failed to renew lease (network issue, DB issue)
	//   - Explicit resignation via Resign()
	//   - Context cancellation during Stop()
	OnLostLeadership func(ctx context.Context)
}

// Elector manages leader election for an AgentPG instance.
type Elector struct {
	store      storage.Store
	instanceID string
	config     *Config
	callbacks  Callbacks

	// mu protects the following fields
	mu       sync.RWMutex
	isLeader bool

	// started indicates if the elector is running
	started atomic.Bool

	// done is closed when the elector stops
	done chan struct{}

	// cancel cancels the election goroutine
	cancel context.CancelFunc
}

// NewElector creates a new leader elector.
func NewElector(store storage.Store, instanceID string, config *Config, callbacks Callbacks) *Elector {
	if config == nil {
		config = DefaultConfig()
	}

	return &Elector{
		store:      store,
		instanceID: instanceID,
		config:     config,
		callbacks:  callbacks,
		done:       make(chan struct{}),
	}
}

// Start begins the leader election process.
// It returns immediately and runs the election loop in a goroutine.
// Call Stop() to stop the election process.
func (e *Elector) Start(ctx context.Context) error {
	if !e.started.CompareAndSwap(false, true) {
		return ErrAlreadyStarted
	}

	ctx, e.cancel = context.WithCancel(ctx)
	go e.runElectionLoop(ctx)

	return nil
}

// Stop stops the leader election process.
// If this instance is the leader, it will resign before stopping.
func (e *Elector) Stop(ctx context.Context) error {
	if !e.started.Load() {
		return ErrNotStarted
	}

	// Cancel the election loop
	e.cancel()

	// Wait for the election loop to finish
	<-e.done

	// Resign if we were the leader
	e.mu.Lock()
	wasLeader := e.isLeader
	e.isLeader = false
	e.mu.Unlock()

	if wasLeader {
		// Best effort resignation
		resignCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		_ = e.store.LeaderResign(resignCtx, e.instanceID)

		if e.callbacks.OnLostLeadership != nil {
			e.callbacks.OnLostLeadership(ctx)
		}
	}

	e.started.Store(false)
	return nil
}

// IsLeader returns true if this instance is currently the leader.
func (e *Elector) IsLeader() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.isLeader
}

// IsRunning returns true if the elector is running.
func (e *Elector) IsRunning() bool {
	return e.started.Load()
}

// Resign voluntarily gives up leadership.
// This is useful when the instance needs to shut down gracefully.
func (e *Elector) Resign(ctx context.Context) error {
	e.mu.Lock()
	wasLeader := e.isLeader
	e.isLeader = false
	e.mu.Unlock()

	if !wasLeader {
		return nil
	}

	if err := e.store.LeaderResign(ctx, e.instanceID); err != nil {
		return err
	}

	if e.callbacks.OnLostLeadership != nil {
		e.callbacks.OnLostLeadership(ctx)
	}

	return nil
}

// runElectionLoop is the main election loop that runs in a goroutine.
func (e *Elector) runElectionLoop(ctx context.Context) {
	defer close(e.done)

	// Try to become leader immediately
	e.attemptElection(ctx)

	for {
		var delay time.Duration
		if e.IsLeader() {
			delay = e.config.ReelectionDelay
		} else {
			delay = e.config.ElectionPeriod
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
			if e.IsLeader() {
				e.attemptReelection(ctx)
			} else {
				e.attemptElection(ctx)
			}
		}
	}
}

// attemptElection tries to become the leader.
func (e *Elector) attemptElection(ctx context.Context) {
	params := &storage.LeaderElectParams{
		LeaderID: e.instanceID,
		TTL:      e.config.LeaderTTL,
	}

	elected, err := e.store.LeaderAttemptElect(ctx, params)
	if err != nil {
		// Log error but continue - we'll retry on next tick
		return
	}

	if elected {
		e.mu.Lock()
		wasLeader := e.isLeader
		e.isLeader = true
		e.mu.Unlock()

		if !wasLeader && e.callbacks.OnBecameLeader != nil {
			e.callbacks.OnBecameLeader(ctx)
		}
	}
}

// attemptReelection tries to renew the leader lease.
func (e *Elector) attemptReelection(ctx context.Context) {
	params := &storage.LeaderElectParams{
		LeaderID: e.instanceID,
		TTL:      e.config.LeaderTTL,
	}

	reelected, err := e.store.LeaderAttemptReelect(ctx, params)
	if err != nil || !reelected {
		// Lost leadership
		e.mu.Lock()
		e.isLeader = false
		e.mu.Unlock()

		if e.callbacks.OnLostLeadership != nil {
			e.callbacks.OnLostLeadership(ctx)
		}
	}
}
