// Package databasesql provides a database/sql driver implementation for AgentPG.
package databasesql

import (
	"context"
	"database/sql"

	"github.com/youssefsiam38/agentpg/driver"
)

// Driver implements driver.Driver using database/sql.
type Driver struct {
	db       *sql.DB
	store    *Store
	listener *Listener
	connStr  string
}

// New creates a new database/sql driver using the provided connection.
// The connStr is required for creating listener connections.
func New(db *sql.DB, connStr string) *Driver {
	store := &Store{db: db}
	return &Driver{
		db:      db,
		store:   store,
		connStr: connStr,
	}
}

// Store returns the store interface for database operations.
func (d *Driver) Store() driver.Store[*sql.Tx] {
	return d.store
}

// Listener returns the listener for LISTEN/NOTIFY.
// Uses lib/pq for LISTEN/NOTIFY support.
func (d *Driver) Listener() driver.Listener {
	if d.listener == nil {
		d.listener = NewListener(d.connStr)
	}
	return d.listener
}

// BeginTx starts a new transaction.
func (d *Driver) BeginTx(ctx context.Context) (*sql.Tx, error) {
	return d.db.BeginTx(ctx, nil)
}

// CommitTx commits a transaction.
func (d *Driver) CommitTx(ctx context.Context, tx *sql.Tx) error {
	return tx.Commit()
}

// RollbackTx rolls back a transaction.
func (d *Driver) RollbackTx(ctx context.Context, tx *sql.Tx) error {
	return tx.Rollback()
}

// Close closes the driver and releases resources.
func (d *Driver) Close() error {
	if d.listener != nil {
		if err := d.listener.Close(); err != nil {
			return err
		}
	}
	// Note: We don't close the db as it was provided externally
	return nil
}

// DB returns the underlying database connection.
func (d *Driver) DB() *sql.DB {
	return d.db
}

// Compile-time check
var _ driver.Driver[*sql.Tx] = (*Driver)(nil)
