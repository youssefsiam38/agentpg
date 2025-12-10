package maintenance

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/youssefsiam38/agentpg/runstate"
	"github.com/youssefsiam38/agentpg/storage"
)

// cleanupMockStore implements storage.Store methods needed for cleanup testing.
type cleanupMockStore struct {
	storage.Store
	staleInstances []string
	stuckRuns      []*storage.Run

	deregisteredInstances []string
	updatedRuns           []string

	getStaleErr        error
	deregisterErr      error
	getStuckRunsErr    error
	updateRunStateErr  error
	deleteExpiredCount int
	deleteExpiredErr   error
}

func (m *cleanupMockStore) GetStaleInstances(ctx context.Context, horizon time.Time) ([]string, error) {
	if m.getStaleErr != nil {
		return nil, m.getStaleErr
	}
	return m.staleInstances, nil
}

func (m *cleanupMockStore) DeregisterInstance(ctx context.Context, instanceID string) error {
	if m.deregisterErr != nil {
		return m.deregisterErr
	}
	m.deregisteredInstances = append(m.deregisteredInstances, instanceID)
	return nil
}

func (m *cleanupMockStore) GetStuckRuns(ctx context.Context, horizon time.Time) ([]*storage.Run, error) {
	if m.getStuckRunsErr != nil {
		return nil, m.getStuckRunsErr
	}
	return m.stuckRuns, nil
}

func (m *cleanupMockStore) UpdateRunState(ctx context.Context, runID string, params *storage.UpdateRunStateParams) error {
	if m.updateRunStateErr != nil {
		return m.updateRunStateErr
	}
	m.updatedRuns = append(m.updatedRuns, runID)
	return nil
}

func (m *cleanupMockStore) LeaderDeleteExpired(ctx context.Context) (int, error) {
	if m.deleteExpiredErr != nil {
		return 0, m.deleteExpiredErr
	}
	return m.deleteExpiredCount, nil
}

func TestCleanup_StartStop(t *testing.T) {
	store := &cleanupMockStore{}
	cleanup := NewCleanup(store, &CleanupConfig{
		Interval:        50 * time.Millisecond,
		StuckRunTimeout: time.Hour,
	})

	ctx := context.Background()

	// Start should succeed
	if err := cleanup.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if !cleanup.IsRunning() {
		t.Error("Expected cleanup to be running")
	}

	// Second start should fail
	if err := cleanup.Start(ctx); err != ErrAlreadyStarted {
		t.Fatalf("Start() error = %v, want %v", err, ErrAlreadyStarted)
	}

	// Stop should succeed
	if err := cleanup.Stop(ctx); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	if cleanup.IsRunning() {
		t.Error("Expected cleanup to not be running")
	}
}

func TestCleanup_StopNotStarted(t *testing.T) {
	store := &cleanupMockStore{}
	cleanup := NewCleanup(store, nil)

	if err := cleanup.Stop(context.Background()); err != ErrNotStarted {
		t.Fatalf("Stop() error = %v, want %v", err, ErrNotStarted)
	}
}

func TestCleanup_RunOnce_StaleInstances(t *testing.T) {
	store := &cleanupMockStore{
		staleInstances: []string{"instance-1", "instance-2", "instance-3"},
	}

	cleanup := NewCleanup(store, DefaultCleanupConfig())

	result := cleanup.RunOnce(context.Background())

	if result.StaleInstancesCleaned != 3 {
		t.Errorf("StaleInstancesCleaned = %d, want 3", result.StaleInstancesCleaned)
	}

	if len(store.deregisteredInstances) != 3 {
		t.Errorf("DeregisteredInstances = %d, want 3", len(store.deregisteredInstances))
	}
}

func TestCleanup_RunOnce_StuckRuns(t *testing.T) {
	store := &cleanupMockStore{
		stuckRuns: []*storage.Run{
			{ID: "run-1", State: runstate.RunStatePendingAPI},
			{ID: "run-2", State: runstate.RunStatePendingAPI},
		},
	}

	cleanup := NewCleanup(store, DefaultCleanupConfig())

	result := cleanup.RunOnce(context.Background())

	if result.StuckRunsCleaned != 2 {
		t.Errorf("StuckRunsCleaned = %d, want 2", result.StuckRunsCleaned)
	}

	if len(store.updatedRuns) != 2 {
		t.Errorf("UpdatedRuns = %d, want 2", len(store.updatedRuns))
	}
}

func TestCleanup_RunOnce_ExpiredLeaders(t *testing.T) {
	store := &cleanupMockStore{
		deleteExpiredCount: 5,
	}

	cleanup := NewCleanup(store, DefaultCleanupConfig())

	result := cleanup.RunOnce(context.Background())

	if result.ExpiredLeadersCleaned != 5 {
		t.Errorf("ExpiredLeadersCleaned = %d, want 5", result.ExpiredLeadersCleaned)
	}
}

func TestCleanup_Callbacks(t *testing.T) {
	store := &cleanupMockStore{
		staleInstances: []string{"instance-1"},
		stuckRuns: []*storage.Run{
			{ID: "run-1", State: runstate.RunStatePendingAPI},
		},
	}

	var staleCount, stuckCount atomic.Int32

	cleanup := NewCleanup(store, &CleanupConfig{
		Interval:        50 * time.Millisecond,
		StuckRunTimeout: time.Hour,
		OnStaleInstanceCleanup: func(count int) {
			staleCount.Store(int32(count))
		},
		OnStuckRunCleanup: func(count int) {
			stuckCount.Store(int32(count))
		},
	})

	ctx := context.Background()

	if err := cleanup.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Wait for at least one cleanup cycle
	time.Sleep(100 * time.Millisecond)

	if err := cleanup.Stop(ctx); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	if staleCount.Load() != 1 {
		t.Errorf("OnStaleInstanceCleanup count = %d, want 1", staleCount.Load())
	}

	if stuckCount.Load() != 1 {
		t.Errorf("OnStuckRunCleanup count = %d, want 1", stuckCount.Load())
	}
}

func TestDefaultCleanupConfig(t *testing.T) {
	config := DefaultCleanupConfig()

	if config.Interval != DefaultCleanupInterval {
		t.Errorf("Interval = %v, want %v", config.Interval, DefaultCleanupInterval)
	}

	if config.StuckRunTimeout != DefaultStuckRunTimeout {
		t.Errorf("StuckRunTimeout = %v, want %v", config.StuckRunTimeout, DefaultStuckRunTimeout)
	}
}
