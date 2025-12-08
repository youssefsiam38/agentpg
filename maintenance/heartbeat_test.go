package maintenance

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/youssefsiam38/agentpg/storage"
)

// mockStore implements storage.Store methods needed for testing.
type mockStore struct {
	storage.Store
	heartbeatCount atomic.Int32
	heartbeatErr   error
}

func (m *mockStore) UpdateInstanceHeartbeat(ctx context.Context, instanceID string) error {
	m.heartbeatCount.Add(1)
	return m.heartbeatErr
}

func TestHeartbeat_StartStop(t *testing.T) {
	store := &mockStore{}
	hb := NewHeartbeat(store, "instance-1", &HeartbeatConfig{
		Interval: 50 * time.Millisecond,
	})

	ctx := context.Background()

	// Start should succeed
	if err := hb.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if !hb.IsRunning() {
		t.Error("Expected heartbeat to be running")
	}

	// Second start should fail
	if err := hb.Start(ctx); err != ErrAlreadyStarted {
		t.Fatalf("Start() error = %v, want %v", err, ErrAlreadyStarted)
	}

	// Wait for some heartbeats
	time.Sleep(150 * time.Millisecond)

	// Stop should succeed
	if err := hb.Stop(ctx); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	if hb.IsRunning() {
		t.Error("Expected heartbeat to not be running")
	}

	// Should have sent at least 2 heartbeats (initial + 2 ticks)
	if count := store.heartbeatCount.Load(); count < 2 {
		t.Errorf("Heartbeat count = %d, want >= 2", count)
	}
}

func TestHeartbeat_StopNotStarted(t *testing.T) {
	store := &mockStore{}
	hb := NewHeartbeat(store, "instance-1", nil)

	if err := hb.Stop(context.Background()); err != ErrNotStarted {
		t.Fatalf("Stop() error = %v, want %v", err, ErrNotStarted)
	}
}

func TestHeartbeat_ErrorCallback(t *testing.T) {
	testErr := ErrNotStarted // using any error
	store := &mockStore{heartbeatErr: testErr}

	var errorCount atomic.Int32

	hb := NewHeartbeat(store, "instance-1", &HeartbeatConfig{
		Interval: 50 * time.Millisecond,
		OnError: func(err error) {
			errorCount.Add(1)
		},
	})

	ctx := context.Background()

	if err := hb.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	if err := hb.Stop(ctx); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	if count := errorCount.Load(); count == 0 {
		t.Error("Expected OnError to be called at least once")
	}
}

func TestDefaultHeartbeatConfig(t *testing.T) {
	config := DefaultHeartbeatConfig()

	if config.Interval != DefaultHeartbeatInterval {
		t.Errorf("Interval = %v, want %v", config.Interval, DefaultHeartbeatInterval)
	}
}
