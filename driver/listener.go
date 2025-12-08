package driver

import "context"

// Notification represents a PostgreSQL NOTIFY notification.
type Notification struct {
	// Channel is the notification channel name.
	Channel string

	// Payload is the notification payload (may be empty).
	Payload string
}

// Listener provides PostgreSQL LISTEN/NOTIFY functionality.
// This interface is only implemented by drivers that support dedicated
// listener connections (pgx/v5). The database/sql driver cannot support
// this because it uses a connection pool that doesn't maintain a dedicated
// connection for listening.
//
// For drivers that don't support Listener, use polling as a fallback:
//
//	if driver.SupportsListener() {
//	    listener := driver.GetListener()
//	    // use LISTEN/NOTIFY
//	} else {
//	    // fall back to polling
//	}
type Listener interface {
	// Listen starts listening on the specified channel.
	// Multiple channels can be listened to simultaneously.
	// Returns an error if the listener is not connected.
	Listen(ctx context.Context, channel string) error

	// Unlisten stops listening on the specified channel.
	// Returns an error if the listener is not connected.
	Unlisten(ctx context.Context, channel string) error

	// UnlistenAll stops listening on all channels.
	// Returns an error if the listener is not connected.
	UnlistenAll(ctx context.Context) error

	// WaitForNotification waits for a notification on any subscribed channel.
	// The context can be used to cancel the wait.
	// Returns a Notification on success, or an error if:
	//   - The context is cancelled
	//   - The connection is lost
	//   - The listener is closed
	WaitForNotification(ctx context.Context) (*Notification, error)

	// Ping checks if the listener connection is healthy.
	Ping(ctx context.Context) error

	// Close closes the listener connection.
	// After closing, the listener cannot be used.
	Close(ctx context.Context) error

	// IsClosed returns true if the listener has been closed.
	IsClosed() bool
}

// Notifier provides the ability to send NOTIFY notifications.
// Both pgx/v5 and database/sql drivers support this since NOTIFY
// is just a regular SQL command that works through any connection.
type Notifier interface {
	// Notify sends a notification on the specified channel with an optional payload.
	// The notification is sent immediately (not queued for transaction commit).
	Notify(ctx context.Context, channel, payload string) error
}

// Notification channel names used by AgentPG.
const (
	// ChannelRunStateChanged is notified when a run's state changes.
	// Payload contains the run ID.
	ChannelRunStateChanged = "agentpg_run_state_changed"

	// ChannelInstanceRegistered is notified when a new instance registers.
	// Payload contains the instance ID.
	ChannelInstanceRegistered = "agentpg_instance_registered"

	// ChannelInstanceDeregistered is notified when an instance deregisters.
	// Payload contains the instance ID.
	ChannelInstanceDeregistered = "agentpg_instance_deregistered"

	// ChannelLeaderChanged is notified when leadership changes.
	// Payload contains the new leader ID (or empty if no leader).
	ChannelLeaderChanged = "agentpg_leader_changed"
)
