package maintenance

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/youssefsiam38/agentpg/runstate"
	"github.com/youssefsiam38/agentpg/storage"
)

// Default cleanup configuration values
const (
	DefaultCleanupInterval = 1 * time.Minute
	DefaultStuckRunTimeout = 1 * time.Hour
)

// CleanupConfig holds configuration for the cleanup service.
type CleanupConfig struct {
	// Interval is how often to run cleanup operations.
	// Default: 1 minute
	Interval time.Duration

	// StuckRunTimeout is how long a run can be in "running" state before
	// it's considered stuck and will be marked as failed.
	// Default: 1 hour
	StuckRunTimeout time.Duration

	// OnStaleInstanceCleanup is called when stale instances are cleaned up.
	// The count is the number of instances that were cleaned up.
	OnStaleInstanceCleanup func(count int)

	// OnStuckRunCleanup is called when stuck runs are cleaned up.
	// The count is the number of runs that were marked as failed.
	OnStuckRunCleanup func(count int)

	// OnError is called when a cleanup operation fails.
	OnError func(err error)
}

// DefaultCleanupConfig returns the default cleanup configuration.
func DefaultCleanupConfig() *CleanupConfig {
	return &CleanupConfig{
		Interval:        DefaultCleanupInterval,
		StuckRunTimeout: DefaultStuckRunTimeout,
	}
}

// CleanupResult holds the results of a cleanup operation.
type CleanupResult struct {
	// StaleInstancesCleaned is the number of stale instances cleaned up.
	StaleInstancesCleaned int

	// StuckRunsCleaned is the number of stuck runs marked as failed.
	StuckRunsCleaned int

	// ExpiredLeadersCleaned is the number of expired leader entries cleaned.
	ExpiredLeadersCleaned int

	// Errors contains any errors that occurred during cleanup.
	Errors []error
}

// Cleanup performs cleanup operations for stale instances and stuck runs.
// This should only be run by the leader instance.
type Cleanup struct {
	store  storage.Store
	config *CleanupConfig

	started atomic.Bool
	done    chan struct{}
	cancel  context.CancelFunc
}

// NewCleanup creates a new cleanup service.
func NewCleanup(store storage.Store, config *CleanupConfig) *Cleanup {
	if config == nil {
		config = DefaultCleanupConfig()
	}

	return &Cleanup{
		store:  store,
		config: config,
		done:   make(chan struct{}),
	}
}

// Start begins the cleanup loop.
// It returns immediately and runs cleanup operations in a goroutine.
// This should only be called when this instance is the leader.
func (c *Cleanup) Start(ctx context.Context) error {
	if !c.started.CompareAndSwap(false, true) {
		return ErrAlreadyStarted
	}

	ctx, c.cancel = context.WithCancel(ctx)
	go c.run(ctx)

	return nil
}

// Stop stops the cleanup loop.
func (c *Cleanup) Stop(ctx context.Context) error {
	if !c.started.Load() {
		return ErrNotStarted
	}

	c.cancel()
	<-c.done

	c.started.Store(false)
	return nil
}

// run is the main cleanup loop.
func (c *Cleanup) run(ctx context.Context) {
	defer close(c.done)

	// Run cleanup immediately on start
	c.runCleanup(ctx)

	ticker := time.NewTicker(c.config.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.runCleanup(ctx)
		}
	}
}

// runCleanup performs all cleanup operations.
func (c *Cleanup) runCleanup(ctx context.Context) {
	result := c.RunOnce(ctx)

	if c.config.OnStaleInstanceCleanup != nil && result.StaleInstancesCleaned > 0 {
		c.config.OnStaleInstanceCleanup(result.StaleInstancesCleaned)
	}

	if c.config.OnStuckRunCleanup != nil && result.StuckRunsCleaned > 0 {
		c.config.OnStuckRunCleanup(result.StuckRunsCleaned)
	}

	if c.config.OnError != nil {
		for _, err := range result.Errors {
			c.config.OnError(err)
		}
	}
}

// RunOnce performs cleanup operations once and returns the result.
// This can be called manually for testing or one-off cleanup.
func (c *Cleanup) RunOnce(ctx context.Context) *CleanupResult {
	result := &CleanupResult{}

	// Clean up stale instances
	staleCount, err := c.cleanupStaleInstances(ctx)
	if err != nil {
		result.Errors = append(result.Errors, err)
	} else {
		result.StaleInstancesCleaned = staleCount
	}

	// Clean up stuck runs
	stuckCount, err := c.cleanupStuckRuns(ctx)
	if err != nil {
		result.Errors = append(result.Errors, err)
	} else {
		result.StuckRunsCleaned = stuckCount
	}

	// Clean up expired leader entries
	leaderCount, err := c.store.LeaderDeleteExpired(ctx)
	if err != nil {
		result.Errors = append(result.Errors, err)
	} else {
		result.ExpiredLeadersCleaned = leaderCount
	}

	return result
}

// cleanupStaleInstances finds and removes instances that haven't heartbeated.
func (c *Cleanup) cleanupStaleInstances(ctx context.Context) (int, error) {
	horizon := time.Now().Add(-DefaultInstanceTTL)

	staleIDs, err := c.store.GetStaleInstances(ctx, horizon)
	if err != nil {
		return 0, err
	}

	count := 0
	for _, id := range staleIDs {
		if err := c.store.DeregisterInstance(ctx, id); err != nil {
			// Continue with other instances even if one fails
			continue
		}
		count++
	}

	return count, nil
}

// cleanupStuckRuns finds runs that have been running too long and marks them as failed.
func (c *Cleanup) cleanupStuckRuns(ctx context.Context) (int, error) {
	horizon := time.Now().Add(-c.config.StuckRunTimeout)

	stuckRuns, err := c.store.GetStuckRuns(ctx, horizon)
	if err != nil {
		return 0, err
	}

	count := 0
	for _, run := range stuckRuns {
		errMsg := "run timed out (marked as failed by cleanup)"
		errType := "timeout"

		err := c.store.UpdateRunState(ctx, run.ID, &storage.UpdateRunStateParams{
			State:        runstate.RunStateFailed,
			ErrorMessage: &errMsg,
			ErrorType:    &errType,
		})
		if err != nil {
			// Continue with other runs even if one fails
			continue
		}
		count++
	}

	return count, nil
}

// IsRunning returns true if the cleanup service is running.
func (c *Cleanup) IsRunning() bool {
	return c.started.Load()
}
