package notifier

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/youssefsiam38/agentpg/driver"
)

// mockNotifier implements driver.Notifier for testing.
type mockNotifier struct {
	notifications []struct {
		channel string
		payload string
	}
	mu        sync.Mutex
	notifyErr error
}

func (m *mockNotifier) Notify(ctx context.Context, channel, payload string) error {
	if m.notifyErr != nil {
		return m.notifyErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.notifications = append(m.notifications, struct {
		channel string
		payload string
	}{channel, payload})
	return nil
}

// mockListener implements driver.Listener for testing.
type mockListener struct {
	notifications chan *driver.Notification
	closed        atomic.Bool
	listenErr     error
	closeErr      error
}

func newMockListener() *mockListener {
	return &mockListener{
		notifications: make(chan *driver.Notification, 10),
	}
}

func (m *mockListener) Listen(ctx context.Context, channel string) error {
	return m.listenErr
}

func (m *mockListener) Unlisten(ctx context.Context, channel string) error {
	return nil
}

func (m *mockListener) UnlistenAll(ctx context.Context) error {
	return nil
}

func (m *mockListener) WaitForNotification(ctx context.Context) (*driver.Notification, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case n := <-m.notifications:
		return n, nil
	}
}

func (m *mockListener) Ping(ctx context.Context) error {
	return nil
}

func (m *mockListener) Close(ctx context.Context) error {
	m.closed.Store(true)
	return m.closeErr
}

func (m *mockListener) IsClosed() bool {
	return m.closed.Load()
}

func TestNotifier_StartStop(t *testing.T) {
	n := NewNotifier(nil, nil, nil)

	ctx := context.Background()

	// Start should succeed
	if err := n.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if !n.IsRunning() {
		t.Error("Expected notifier to be running")
	}

	// Second start should fail
	if err := n.Start(ctx); err != ErrAlreadyStarted {
		t.Fatalf("Start() error = %v, want %v", err, ErrAlreadyStarted)
	}

	// Stop should succeed
	if err := n.Stop(ctx); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	if n.IsRunning() {
		t.Error("Expected notifier to not be running")
	}
}

func TestNotifier_StopNotStarted(t *testing.T) {
	n := NewNotifier(nil, nil, nil)

	if err := n.Stop(context.Background()); err != ErrNotStarted {
		t.Fatalf("Stop() error = %v, want %v", err, ErrNotStarted)
	}
}

func TestNotifier_Subscribe(t *testing.T) {
	listener := newMockListener()
	getListener := func(ctx context.Context) (driver.Listener, error) {
		return listener, nil
	}

	n := NewNotifier(getListener, nil, nil)

	var receivedEvents []*Event
	var mu sync.Mutex

	unsubscribe := n.Subscribe(EventRunStateChanged, func(event *Event) {
		mu.Lock()
		receivedEvents = append(receivedEvents, event)
		mu.Unlock()
	})

	ctx := context.Background()
	if err := n.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Give time for listener to start
	time.Sleep(50 * time.Millisecond)

	// Send a notification
	listener.notifications <- &driver.Notification{
		Channel: driver.ChannelRunStateChanged,
		Payload: "run-123",
	}

	// Wait for delivery
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	if len(receivedEvents) != 1 {
		t.Errorf("Received %d events, want 1", len(receivedEvents))
	} else {
		if receivedEvents[0].Type != EventRunStateChanged {
			t.Errorf("Event type = %v, want %v", receivedEvents[0].Type, EventRunStateChanged)
		}
		if receivedEvents[0].Payload != "run-123" {
			t.Errorf("Event payload = %v, want run-123", receivedEvents[0].Payload)
		}
	}
	mu.Unlock()

	// Unsubscribe
	unsubscribe()

	// Send another notification
	listener.notifications <- &driver.Notification{
		Channel: driver.ChannelRunStateChanged,
		Payload: "run-456",
	}

	// Wait for potential delivery
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	if len(receivedEvents) != 1 {
		t.Errorf("Received %d events after unsubscribe, want 1", len(receivedEvents))
	}
	mu.Unlock()

	if err := n.Stop(ctx); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
}

func TestNotifier_Notify(t *testing.T) {
	mock := &mockNotifier{}
	n := NewNotifier(nil, mock, nil)

	ctx := context.Background()

	if err := n.Notify(ctx, EventRunStateChanged, "run-123"); err != nil {
		t.Fatalf("Notify() error = %v", err)
	}

	mock.mu.Lock()
	if len(mock.notifications) != 1 {
		t.Errorf("Sent %d notifications, want 1", len(mock.notifications))
	} else {
		if mock.notifications[0].channel != driver.ChannelRunStateChanged {
			t.Errorf("Channel = %v, want %v", mock.notifications[0].channel, driver.ChannelRunStateChanged)
		}
		if mock.notifications[0].payload != "run-123" {
			t.Errorf("Payload = %v, want run-123", mock.notifications[0].payload)
		}
	}
	mock.mu.Unlock()
}

func TestNotifier_NotifyNotSupported(t *testing.T) {
	n := NewNotifier(nil, nil, nil)

	err := n.Notify(context.Background(), EventRunStateChanged, "run-123")
	if err != ErrNotifyNotSupported {
		t.Errorf("Notify() error = %v, want %v", err, ErrNotifyNotSupported)
	}
}

func TestNotifier_UnknownEventType(t *testing.T) {
	mock := &mockNotifier{}
	n := NewNotifier(nil, mock, nil)

	err := n.Notify(context.Background(), EventType("unknown"), "payload")
	if err != ErrUnknownEventType {
		t.Errorf("Notify() error = %v, want %v", err, ErrUnknownEventType)
	}
}

func TestNotifier_MultipleSubscribers(t *testing.T) {
	listener := newMockListener()
	getListener := func(ctx context.Context) (driver.Listener, error) {
		return listener, nil
	}

	n := NewNotifier(getListener, nil, nil)

	var count1, count2 atomic.Int32

	n.Subscribe(EventRunStateChanged, func(event *Event) {
		count1.Add(1)
	})

	n.Subscribe(EventRunStateChanged, func(event *Event) {
		count2.Add(1)
	})

	ctx := context.Background()
	if err := n.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Give time for listener to start
	time.Sleep(50 * time.Millisecond)

	// Send a notification
	listener.notifications <- &driver.Notification{
		Channel: driver.ChannelRunStateChanged,
		Payload: "run-123",
	}

	// Wait for delivery
	time.Sleep(50 * time.Millisecond)

	if count1.Load() != 1 {
		t.Errorf("Handler 1 called %d times, want 1", count1.Load())
	}

	if count2.Load() != 1 {
		t.Errorf("Handler 2 called %d times, want 1", count2.Load())
	}

	if err := n.Stop(ctx); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config.ReconnectDelay != 5*time.Second {
		t.Errorf("ReconnectDelay = %v, want 5s", config.ReconnectDelay)
	}
}
