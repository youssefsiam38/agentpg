package pgxv5

import (
	"context"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/youssefsiam38/agentpg/driver"
)

// Listener implements driver.Listener using pgx/v5.
type Listener struct {
	pool    *pgxpool.Pool
	conn    *pgxpool.Conn
	notifCh chan driver.Notification
	done    chan struct{}
	mu      sync.Mutex
	closed  bool
}

// NewListener creates a new Listener using the provided connection pool.
func NewListener(pool *pgxpool.Pool) *Listener {
	return &Listener{
		pool:    pool,
		notifCh: make(chan driver.Notification, 100),
		done:    make(chan struct{}),
	}
}

// Listen starts listening for notifications on the specified channels.
// This method acquires a dedicated connection from the pool.
func (l *Listener) Listen(ctx context.Context, channels ...string) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.closed {
		return nil
	}

	// Acquire a dedicated connection for listening
	conn, err := l.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	l.conn = conn

	// Subscribe to channels
	for _, channel := range channels {
		_, err := conn.Exec(ctx, "LISTEN "+channel)
		if err != nil {
			conn.Release()
			l.conn = nil
			return err
		}
	}

	// Start notification loop in background
	go l.listenLoop(ctx)

	return nil
}

// listenLoop continuously waits for notifications and sends them to the channel.
func (l *Listener) listenLoop(ctx context.Context) {
	defer func() {
		l.mu.Lock()
		if l.conn != nil {
			l.conn.Release()
			l.conn = nil
		}
		l.mu.Unlock()
	}()

	for {
		select {
		case <-l.done:
			return
		case <-ctx.Done():
			return
		default:
		}

		// Wait for notification with context
		notification, err := l.conn.Conn().WaitForNotification(ctx)
		if err != nil {
			// Check if we're shutting down
			select {
			case <-l.done:
				return
			case <-ctx.Done():
				return
			default:
				// Connection error, try to reconnect
				continue
			}
		}

		// Send notification to channel
		select {
		case l.notifCh <- driver.Notification{
			Channel: notification.Channel,
			Payload: notification.Payload,
		}:
		case <-l.done:
			return
		case <-ctx.Done():
			return
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

	// Release connection if held
	if l.conn != nil {
		l.conn.Release()
		l.conn = nil
	}

	// Close notification channel
	close(l.notifCh)

	return nil
}

// Compile-time check
var _ driver.Listener = (*Listener)(nil)
