// Package testutil provides test utilities for agentpg
package testutil

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestDB wraps a PostgreSQL connection pool for testing
type TestDB struct {
	Pool *pgxpool.Pool
}

// NewTestDB creates a test database connection from DATABASE_URL env var
// Returns nil if DATABASE_URL is not set (for unit tests)
func NewTestDB(t *testing.T) *TestDB {
	t.Helper()

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set, skipping integration test")
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("Failed to connect to database: %v", err)
	}

	// Verify connection
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Fatalf("Failed to ping database: %v", err)
	}

	return &TestDB{Pool: pool}
}

// Close closes the database connection
func (db *TestDB) Close() {
	if db.Pool != nil {
		db.Pool.Close()
	}
}

// CleanTables truncates all tables for test isolation
func (db *TestDB) CleanTables(ctx context.Context) error {
	tables := []string{
		"agentpg_message_archive",
		"agentpg_compaction_events",
		"agentpg_messages",
		"agentpg_sessions",
	}

	for _, table := range tables {
		_, err := db.Pool.Exec(ctx, fmt.Sprintf("TRUNCATE TABLE %s CASCADE", table))
		if err != nil {
			return fmt.Errorf("failed to truncate %s: %w", table, err)
		}
	}

	return nil
}

// SetupTestSession creates a test session and returns its ID
func (db *TestDB) SetupTestSession(ctx context.Context, t *testing.T) string {
	t.Helper()

	var sessionID string
	err := db.Pool.QueryRow(ctx, `
		INSERT INTO agentpg_sessions (id, tenant_id, identifier, metadata, created_at, updated_at)
		VALUES (gen_random_uuid(), 'test-tenant', 'test-session', '{}', NOW(), NOW())
		RETURNING id
	`).Scan(&sessionID)

	if err != nil {
		t.Fatalf("Failed to create test session: %v", err)
	}

	return sessionID
}

// RequireIntegration skips the test if not running integration tests
func RequireIntegration(t *testing.T) {
	t.Helper()
	if os.Getenv("DATABASE_URL") == "" {
		t.Skip("Skipping integration test: DATABASE_URL not set")
	}
}
