package databasesql

import (
	"context"
	"database/sql"

	"github.com/youssefsiam38/agentpg/driver"
)

// Notifier implements driver.Notifier using database/sql.
type Notifier struct {
	db *sql.DB
}

// Notify sends a notification on the specified channel.
func (n *Notifier) Notify(ctx context.Context, channel, payload string) error {
	_, err := n.db.ExecContext(ctx, "SELECT pg_notify($1, $2)", channel, payload)
	return err
}

// Ensure interface is implemented
var _ driver.Notifier = (*Notifier)(nil)
