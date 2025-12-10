// Package notifier provides a high-level interface for PostgreSQL LISTEN/NOTIFY.
//
// This package provides:
//   - Automatic listener management with reconnection
//   - Polling fallback for drivers that don't support LISTEN
//   - Typed event handling
//   - Graceful shutdown
//
// For drivers that support Listener (pgx/v5), this uses PostgreSQL's LISTEN/NOTIFY
// for real-time event delivery. For database/sql, events are still sent via NOTIFY
// but must be received through polling.
package notifier

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/youssefsiam38/agentpg/driver"
)

// EventType represents the type of event.
type EventType string

// Event types that can be subscribed to.
const (
	EventRunStateChanged      EventType = "run_state_changed"
	EventRunCreated           EventType = "run_created"
	EventToolPending          EventType = "tool_pending"
	EventRunToolsComplete     EventType = "run_tools_complete"
	EventInstanceRegistered   EventType = "instance_registered"
	EventInstanceDeregistered EventType = "instance_deregistered"
	EventLeaderChanged        EventType = "leader_changed"
)

// Event represents a notification event.
type Event struct {
	// Type is the event type.
	Type EventType

	// Payload is the event payload (e.g., run ID, instance ID).
	Payload string

	// ReceivedAt is when the event was received.
	ReceivedAt time.Time
}

// Handler is called when an event is received.
type Handler func(event *Event)

// Config holds configuration for the notifier.
type Config struct {
	// ReconnectDelay is how long to wait before reconnecting after a disconnect.
	// Default: 5 seconds
	ReconnectDelay time.Duration

	// OnError is called when an error occurs.
	OnError func(err error)

	// OnReconnect is called when the listener reconnects.
	OnReconnect func()
}

// DefaultConfig returns the default configuration.
func DefaultConfig() *Config {
	return &Config{
		ReconnectDelay: 5 * time.Second,
	}
}

// channelToEventType maps PostgreSQL channel names to event types.
var channelToEventType = map[string]EventType{
	driver.ChannelRunStateChanged:      EventRunStateChanged,
	driver.ChannelRunCreated:           EventRunCreated,
	driver.ChannelToolPending:          EventToolPending,
	driver.ChannelRunToolsComplete:     EventRunToolsComplete,
	driver.ChannelInstanceRegistered:   EventInstanceRegistered,
	driver.ChannelInstanceDeregistered: EventInstanceDeregistered,
	driver.ChannelLeaderChanged:        EventLeaderChanged,
}

// eventTypeToChannel maps event types to PostgreSQL channel names.
var eventTypeToChannel = map[EventType]string{
	EventRunStateChanged:      driver.ChannelRunStateChanged,
	EventRunCreated:           driver.ChannelRunCreated,
	EventToolPending:          driver.ChannelToolPending,
	EventRunToolsComplete:     driver.ChannelRunToolsComplete,
	EventInstanceRegistered:   driver.ChannelInstanceRegistered,
	EventInstanceDeregistered: driver.ChannelInstanceDeregistered,
	EventLeaderChanged:        driver.ChannelLeaderChanged,
}

// Subscription represents an active subscription to events.
type Subscription struct {
	eventType EventType
	handler   Handler
	id        int64
}

// Notifier provides event notification capabilities.
type Notifier struct {
	getListener func(ctx context.Context) (driver.Listener, error)
	notifier    driver.Notifier
	config      *Config

	mu            sync.RWMutex
	subscriptions map[EventType][]*Subscription
	nextSubID     int64

	started atomic.Bool
	done    chan struct{}
	cancel  context.CancelFunc
}

// NewNotifier creates a new notifier.
// The getListener function returns a new listener instance for receiving notifications.
// The notifier is used for sending notifications.
// If getListener returns nil, notifications will not be received (send-only mode).
func NewNotifier(
	getListener func(ctx context.Context) (driver.Listener, error),
	notifier driver.Notifier,
	config *Config,
) *Notifier {
	if config == nil {
		config = DefaultConfig()
	}

	return &Notifier{
		getListener:   getListener,
		notifier:      notifier,
		config:        config,
		subscriptions: make(map[EventType][]*Subscription),
		done:          make(chan struct{}),
	}
}

// Start begins listening for notifications.
// If the driver doesn't support listeners (database/sql), this is a no-op.
func (n *Notifier) Start(ctx context.Context) error {
	if !n.started.CompareAndSwap(false, true) {
		return ErrAlreadyStarted
	}

	ctx, n.cancel = context.WithCancel(ctx)
	go n.run(ctx)

	return nil
}

// Stop stops the notifier.
func (n *Notifier) Stop(ctx context.Context) error {
	if !n.started.Load() {
		return ErrNotStarted
	}

	n.cancel()
	<-n.done

	n.started.Store(false)
	return nil
}

// Subscribe registers a handler for the given event type.
// Returns a function to unsubscribe.
func (n *Notifier) Subscribe(eventType EventType, handler Handler) func() {
	n.mu.Lock()
	defer n.mu.Unlock()

	sub := &Subscription{
		eventType: eventType,
		handler:   handler,
		id:        n.nextSubID,
	}
	n.nextSubID++

	n.subscriptions[eventType] = append(n.subscriptions[eventType], sub)

	return func() {
		n.unsubscribe(eventType, sub.id)
	}
}

// unsubscribe removes a subscription.
func (n *Notifier) unsubscribe(eventType EventType, id int64) {
	n.mu.Lock()
	defer n.mu.Unlock()

	subs := n.subscriptions[eventType]
	for i, sub := range subs {
		if sub.id == id {
			n.subscriptions[eventType] = append(subs[:i], subs[i+1:]...)
			break
		}
	}
}

// Notify sends a notification.
func (n *Notifier) Notify(ctx context.Context, eventType EventType, payload string) error {
	if n.notifier == nil {
		return ErrNotifyNotSupported
	}

	channel, ok := eventTypeToChannel[eventType]
	if !ok {
		return ErrUnknownEventType
	}

	return n.notifier.Notify(ctx, channel, payload)
}

// run is the main notification loop.
func (n *Notifier) run(ctx context.Context) {
	defer close(n.done)

	for {
		select {
		case <-ctx.Done():
			return
		default:
			if err := n.listenLoop(ctx); err != nil {
				if ctx.Err() != nil {
					return
				}
				if n.config.OnError != nil {
					n.config.OnError(err)
				}
				// Wait before reconnecting
				select {
				case <-ctx.Done():
					return
				case <-time.After(n.config.ReconnectDelay):
					if n.config.OnReconnect != nil {
						n.config.OnReconnect()
					}
				}
			}
		}
	}
}

// listenLoop creates a listener and processes notifications until an error occurs.
func (n *Notifier) listenLoop(ctx context.Context) error {
	if n.getListener == nil {
		// No listener support, just wait for context cancellation
		<-ctx.Done()
		return ctx.Err()
	}

	listener, err := n.getListener(ctx)
	if err != nil {
		return err
	}
	if listener == nil {
		// Driver doesn't support listeners
		<-ctx.Done()
		return ctx.Err()
	}
	defer func() { _ = listener.Close(ctx) }()

	// Subscribe to all channels
	for channel := range channelToEventType {
		if err := listener.Listen(ctx, channel); err != nil {
			return err
		}
	}

	// Process notifications
	for {
		notification, err := listener.WaitForNotification(ctx)
		if err != nil {
			return err
		}

		eventType, ok := channelToEventType[notification.Channel]
		if !ok {
			continue
		}

		event := &Event{
			Type:       eventType,
			Payload:    notification.Payload,
			ReceivedAt: time.Now(),
		}

		n.dispatch(event)
	}
}

// dispatch sends an event to all subscribed handlers.
func (n *Notifier) dispatch(event *Event) {
	n.mu.RLock()
	subs := make([]*Subscription, len(n.subscriptions[event.Type]))
	copy(subs, n.subscriptions[event.Type])
	n.mu.RUnlock()

	for _, sub := range subs {
		// Call handlers synchronously to maintain ordering
		// Handlers should be quick; long operations should be done asynchronously
		sub.handler(event)
	}
}

// IsRunning returns true if the notifier is running.
func (n *Notifier) IsRunning() bool {
	return n.started.Load()
}
