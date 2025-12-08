package pgxv5

import (
	"context"
	"fmt"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/youssefsiam38/agentpg/driver"
)

// Listener implements driver.Listener using a dedicated pgxpool connection.
type Listener struct {
	conn   *pgxpool.Conn
	mu     sync.RWMutex
	closed bool
}

// Listen starts listening on the specified channel.
func (l *Listener) Listen(ctx context.Context, channel string) error {
	l.mu.RLock()
	if l.closed {
		l.mu.RUnlock()
		return fmt.Errorf("listener is closed")
	}
	l.mu.RUnlock()

	_, err := l.conn.Exec(ctx, fmt.Sprintf("LISTEN %s", quoteIdent(channel)))
	return err
}

// Unlisten stops listening on the specified channel.
func (l *Listener) Unlisten(ctx context.Context, channel string) error {
	l.mu.RLock()
	if l.closed {
		l.mu.RUnlock()
		return fmt.Errorf("listener is closed")
	}
	l.mu.RUnlock()

	_, err := l.conn.Exec(ctx, fmt.Sprintf("UNLISTEN %s", quoteIdent(channel)))
	return err
}

// UnlistenAll stops listening on all channels.
func (l *Listener) UnlistenAll(ctx context.Context) error {
	l.mu.RLock()
	if l.closed {
		l.mu.RUnlock()
		return fmt.Errorf("listener is closed")
	}
	l.mu.RUnlock()

	_, err := l.conn.Exec(ctx, "UNLISTEN *")
	return err
}

// WaitForNotification waits for a notification on any subscribed channel.
func (l *Listener) WaitForNotification(ctx context.Context) (*driver.Notification, error) {
	l.mu.RLock()
	if l.closed {
		l.mu.RUnlock()
		return nil, fmt.Errorf("listener is closed")
	}
	l.mu.RUnlock()

	notification, err := l.conn.Conn().WaitForNotification(ctx)
	if err != nil {
		return nil, err
	}

	return &driver.Notification{
		Channel: notification.Channel,
		Payload: notification.Payload,
	}, nil
}

// Ping checks if the listener connection is healthy.
func (l *Listener) Ping(ctx context.Context) error {
	l.mu.RLock()
	if l.closed {
		l.mu.RUnlock()
		return fmt.Errorf("listener is closed")
	}
	l.mu.RUnlock()

	return l.conn.Ping(ctx)
}

// Close closes the listener connection.
func (l *Listener) Close(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.closed {
		return nil
	}

	l.closed = true

	// Unlisten all before releasing
	_, _ = l.conn.Exec(ctx, "UNLISTEN *")

	// Release the connection back to the pool
	l.conn.Release()
	return nil
}

// IsClosed returns true if the listener has been closed.
func (l *Listener) IsClosed() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.closed
}

// Notifier implements driver.Notifier using the pgxpool.
type Notifier struct {
	pool *pgxpool.Pool
}

// Notify sends a notification on the specified channel.
func (n *Notifier) Notify(ctx context.Context, channel, payload string) error {
	_, err := n.pool.Exec(ctx, "SELECT pg_notify($1, $2)", channel, payload)
	return err
}

// quoteIdent quotes an identifier for use in SQL.
// This prevents SQL injection in channel names.
func quoteIdent(s string) string {
	return `"` + doubleQuoteEscape(s) + `"`
}

// doubleQuoteEscape escapes double quotes in a string.
func doubleQuoteEscape(s string) string {
	result := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '"' {
			result = append(result, '"', '"')
		} else {
			result = append(result, s[i])
		}
	}
	return string(result)
}

// Ensure interfaces are implemented
var (
	_ driver.Listener = (*Listener)(nil)
	_ driver.Notifier = (*Notifier)(nil)
)
