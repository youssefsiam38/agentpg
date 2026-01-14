package databasesql

import (
	"context"
	"sync"
	"time"

	"github.com/lib/pq"
	"github.com/youssefsiam38/agentpg/driver"
)

// Listener implements driver.Listener using lib/pq.
type Listener struct {
	connStr  string
	listener *pq.Listener
	notifCh  chan driver.Notification
	done     chan struct{}
	mu       sync.Mutex
	closed   bool
	channels []string
}

// NewListener creates a new Listener using the provided connection string.
func NewListener(connStr string) *Listener {
	return &Listener{
		connStr: connStr,
		notifCh: make(chan driver.Notification, 100),
		done:    make(chan struct{}),
	}
}

// Listen starts listening for notifications on the specified channels.
func (l *Listener) Listen(ctx context.Context, channels ...string) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.closed {
		return nil
	}

	// Create pq listener with reconnection callback
	l.listener = pq.NewListener(
		l.connStr,
		10*time.Second, // minReconnectInterval
		time.Minute,    // maxReconnectInterval
		func(ev pq.ListenerEventType, err error) {
			// Reconnection callback - could log errors here
			if err != nil {
				// Connection error, listener will attempt to reconnect
				return
			}
		},
	)

	// Subscribe to channels
	for _, channel := range channels {
		if err := l.listener.Listen(channel); err != nil {
			_ = l.listener.Close()
			l.listener = nil
			return err
		}
	}
	l.channels = channels

	// Start notification loop in background
	go l.listenLoop()

	return nil
}

// listenLoop continuously waits for notifications and sends them to the channel.
func (l *Listener) listenLoop() {
	for {
		select {
		case <-l.done:
			return
		case notification := <-l.listener.Notify:
			if notification == nil {
				// Connection lost, listener will attempt to reconnect
				// Check if we're shutting down
				select {
				case <-l.done:
					return
				default:
					continue
				}
			}

			// Send notification to channel
			select {
			case l.notifCh <- driver.Notification{
				Channel: notification.Channel,
				Payload: notification.Extra,
			}:
			case <-l.done:
				return
			}
		}
	}
}

// Notifications returns a channel for receiving notifications.
func (l *Listener) Notifications() <-chan driver.Notification {
	return l.notifCh
}

// Close stops listening and releases resources.
func (l *Listener) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.closed {
		return nil
	}
	l.closed = true

	// Signal shutdown
	close(l.done)

	// Close pq listener
	if l.listener != nil {
		_ = l.listener.Close()
		l.listener = nil
	}

	// Close notification channel
	close(l.notifCh)

	return nil
}

// Compile-time check
var _ driver.Listener = (*Listener)(nil)
