package leadership

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/youssefsiam38/agentpg/storage"
)

// mockStore implements the storage.Store interface for testing leader election.
type mockStore struct {
	storage.Store
	leader        atomic.Value // string
	leaderExpires atomic.Value // time.Time
	electCalled   atomic.Int32
	reelectCalled atomic.Int32
	resignCalled  atomic.Int32
	electErr      error
	reelectErr    error
	resignErr     error
}

func (m *mockStore) getLeader() string {
	if v := m.leader.Load(); v != nil {
		return v.(string)
	}
	return ""
}

func (m *mockStore) getLeaderExpires() time.Time {
	if v := m.leaderExpires.Load(); v != nil {
		return v.(time.Time)
	}
	return time.Time{}
}

func (m *mockStore) LeaderAttemptElect(ctx context.Context, params *storage.LeaderElectParams) (bool, error) {
	m.electCalled.Add(1)
	if m.electErr != nil {
		return false, m.electErr
	}

	// If no leader or leader expired, we can become leader
	if m.getLeader() == "" || time.Now().After(m.getLeaderExpires()) {
		m.leader.Store(params.LeaderID)
		m.leaderExpires.Store(time.Now().Add(params.TTL))
		return true, nil
	}

	return false, nil
}

func (m *mockStore) LeaderAttemptReelect(ctx context.Context, params *storage.LeaderElectParams) (bool, error) {
	m.reelectCalled.Add(1)
	if m.reelectErr != nil {
		return false, m.reelectErr
	}

	// Can only re-elect if we are currently the leader
	if m.getLeader() == params.LeaderID && time.Now().Before(m.getLeaderExpires()) {
		m.leaderExpires.Store(time.Now().Add(params.TTL))
		return true, nil
	}

	return false, nil
}

func (m *mockStore) LeaderResign(ctx context.Context, leaderID string) error {
	m.resignCalled.Add(1)
	if m.resignErr != nil {
		return m.resignErr
	}

	if m.getLeader() == leaderID {
		m.leader.Store("")
		m.leaderExpires.Store(time.Time{})
	}

	return nil
}

func TestElector_StartStop(t *testing.T) {
	store := &mockStore{}
	elector := NewElector(store, "instance-1", &Config{
		LeaderTTL:       100 * time.Millisecond,
		ElectionPeriod:  50 * time.Millisecond,
		ReelectionDelay: 25 * time.Millisecond,
	}, Callbacks{})

	ctx := context.Background()

	// Start should succeed
	if err := elector.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Second start should fail
	if err := elector.Start(ctx); err != ErrAlreadyStarted {
		t.Fatalf("Start() error = %v, want %v", err, ErrAlreadyStarted)
	}

	// Give time for at least one election attempt
	time.Sleep(100 * time.Millisecond)

	// Stop should succeed
	if err := elector.Stop(ctx); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	// At least one election should have been attempted
	if store.electCalled.Load() == 0 {
		t.Error("Expected at least one election attempt")
	}
}

func TestElector_BecomesLeader(t *testing.T) {
	store := &mockStore{}

	var becameLeaderCount atomic.Int32

	elector := NewElector(store, "instance-1", &Config{
		LeaderTTL:       100 * time.Millisecond,
		ElectionPeriod:  50 * time.Millisecond,
		ReelectionDelay: 25 * time.Millisecond,
	}, Callbacks{
		OnBecameLeader: func(ctx context.Context) {
			becameLeaderCount.Add(1)
		},
	})

	ctx := context.Background()

	if err := elector.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Wait for election
	time.Sleep(100 * time.Millisecond)

	if !elector.IsLeader() {
		t.Error("Expected to be leader")
	}

	if becameLeaderCount.Load() != 1 {
		t.Errorf("OnBecameLeader called %d times, want 1", becameLeaderCount.Load())
	}

	if err := elector.Stop(ctx); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
}

func TestElector_Resign(t *testing.T) {
	store := &mockStore{}

	var lostLeadershipCount atomic.Int32

	elector := NewElector(store, "instance-1", &Config{
		LeaderTTL:       100 * time.Millisecond,
		ElectionPeriod:  50 * time.Millisecond,
		ReelectionDelay: 25 * time.Millisecond,
	}, Callbacks{
		OnLostLeadership: func(ctx context.Context) {
			lostLeadershipCount.Add(1)
		},
	})

	ctx := context.Background()

	if err := elector.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Wait for election
	time.Sleep(100 * time.Millisecond)

	if !elector.IsLeader() {
		t.Error("Expected to be leader before resign")
	}

	// Resign
	if err := elector.Resign(ctx); err != nil {
		t.Fatalf("Resign() error = %v", err)
	}

	if elector.IsLeader() {
		t.Error("Expected not to be leader after resign")
	}

	if lostLeadershipCount.Load() != 1 {
		t.Errorf("OnLostLeadership called %d times, want 1", lostLeadershipCount.Load())
	}

	if store.resignCalled.Load() != 1 {
		t.Errorf("LeaderResign called %d times, want 1", store.resignCalled.Load())
	}

	if err := elector.Stop(ctx); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
}

func TestElector_ReelectionMaintainsLeadership(t *testing.T) {
	store := &mockStore{}

	elector := NewElector(store, "instance-1", &Config{
		LeaderTTL:       100 * time.Millisecond,
		ElectionPeriod:  50 * time.Millisecond,
		ReelectionDelay: 25 * time.Millisecond,
	}, Callbacks{})

	ctx := context.Background()

	if err := elector.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Wait for initial election + some re-elections
	time.Sleep(200 * time.Millisecond)

	if !elector.IsLeader() {
		t.Error("Expected to remain leader")
	}

	// Should have re-elected at least once
	if store.reelectCalled.Load() == 0 {
		t.Error("Expected at least one re-election attempt")
	}

	if err := elector.Stop(ctx); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config.LeaderTTL != DefaultLeaderTTL {
		t.Errorf("LeaderTTL = %v, want %v", config.LeaderTTL, DefaultLeaderTTL)
	}

	if config.ElectionPeriod != DefaultElectionPeriod {
		t.Errorf("ElectionPeriod = %v, want %v", config.ElectionPeriod, DefaultElectionPeriod)
	}

	if config.ReelectionDelay != DefaultReelectionDelay {
		t.Errorf("ReelectionDelay = %v, want %v", config.ReelectionDelay, DefaultReelectionDelay)
	}
}
