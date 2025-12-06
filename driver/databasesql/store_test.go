package databasesql

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/youssefsiam38/agentpg/driver"
	"github.com/youssefsiam38/agentpg/storage"
)

func getTestDB(t *testing.T) *sql.DB {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set, skipping integration test")
		return nil
	}

	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}

	if err := db.Ping(); err != nil {
		t.Fatalf("Failed to ping: %v", err)
	}

	return db
}

func cleanTables(ctx context.Context, db *sql.DB) error {
	tables := []string{"agentpg_message_archive", "agentpg_compaction_events", "agentpg_messages", "agentpg_sessions"}
	for _, table := range tables {
		_, err := db.ExecContext(ctx, "TRUNCATE TABLE "+table+" CASCADE")
		if err != nil {
			return err
		}
	}
	return nil
}

func TestIntegration_DatabaseSQL_Store_SessionLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	db := getTestDB(t)
	if db == nil {
		return
	}
	defer db.Close()

	ctx := context.Background()
	if err := cleanTables(ctx, db); err != nil {
		t.Fatalf("Failed to clean tables: %v", err)
	}

	drv := New(db)
	store := drv.GetStore()

	// Create session
	metadata := map[string]any{"key": "value"}
	sessionID, err := store.CreateSession(ctx, "tenant1", "user1", nil, metadata)
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	if sessionID == "" {
		t.Fatal("Expected non-empty session ID")
	}

	// Get session
	session, err := store.GetSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}
	if session.TenantID != "tenant1" {
		t.Errorf("Expected tenant_id 'tenant1', got '%s'", session.TenantID)
	}
	if session.Identifier != "user1" {
		t.Errorf("Expected identifier 'user1', got '%s'", session.Identifier)
	}
	if session.Metadata["key"] != "value" {
		t.Errorf("Expected metadata key 'value', got '%v'", session.Metadata["key"])
	}

	// Get session by tenant and identifier
	session2, err := store.GetSessionByTenantAndIdentifier(ctx, "tenant1", "user1")
	if err != nil {
		t.Fatalf("GetSessionByTenantAndIdentifier failed: %v", err)
	}
	if session2.ID != sessionID {
		t.Errorf("Expected session ID '%s', got '%s'", sessionID, session2.ID)
	}

	// Get sessions by tenant
	sessions, err := store.GetSessionsByTenant(ctx, "tenant1")
	if err != nil {
		t.Fatalf("GetSessionsByTenant failed: %v", err)
	}
	if len(sessions) != 1 {
		t.Errorf("Expected 1 session, got %d", len(sessions))
	}
}

func TestIntegration_DatabaseSQL_Store_MessageOperations(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	db := getTestDB(t)
	if db == nil {
		return
	}
	defer db.Close()

	ctx := context.Background()
	if err := cleanTables(ctx, db); err != nil {
		t.Fatalf("Failed to clean tables: %v", err)
	}

	drv := New(db)
	store := drv.GetStore()

	// Create session
	sessionID, _ := store.CreateSession(ctx, "tenant1", "test", nil, nil)

	// Save message
	msgID := uuid.New().String()
	msg := &storage.Message{
		ID:        msgID,
		SessionID: sessionID,
		Role:      "user",
		Content:   []any{map[string]any{"type": "text", "text": "hello"}},
		Usage: &storage.MessageUsage{
			InputTokens:  5,
			OutputTokens: 5,
		},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if err := store.SaveMessage(ctx, msg); err != nil {
		t.Fatalf("SaveMessage failed: %v", err)
	}

	// Get messages
	messages, err := store.GetMessages(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetMessages failed: %v", err)
	}
	if len(messages) != 1 {
		t.Errorf("Expected 1 message, got %d", len(messages))
	}
	if messages[0].ID != msgID {
		t.Errorf("Expected message ID '%s', got '%s'", msgID, messages[0].ID)
	}

	// Get session token count
	tokenCount, err := store.GetSessionTokenCount(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetSessionTokenCount failed: %v", err)
	}
	if tokenCount != 10 {
		t.Errorf("Expected token count 10, got %d", tokenCount)
	}

	// Delete messages
	if err := store.DeleteMessages(ctx, []string{msgID}); err != nil {
		t.Fatalf("DeleteMessages failed: %v", err)
	}

	messages, _ = store.GetMessages(ctx, sessionID)
	if len(messages) != 0 {
		t.Errorf("Expected 0 messages after delete, got %d", len(messages))
	}
}

func TestIntegration_DatabaseSQL_Store_Transaction(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	db := getTestDB(t)
	if db == nil {
		return
	}
	defer db.Close()

	ctx := context.Background()
	if err := cleanTables(ctx, db); err != nil {
		t.Fatalf("Failed to clean tables: %v", err)
	}

	drv := New(db)
	store := drv.GetStore()

	// Create session outside tx
	sessionID, _ := store.CreateSession(ctx, "tenant1", "test", nil, nil)

	// Test transaction commit using driver
	execTx, err := drv.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin failed: %v", err)
	}

	commitMsgID := uuid.New().String()
	msg := &storage.Message{
		ID:        commitMsgID,
		SessionID: sessionID,
		Role:      "user",
		Content:   []any{},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	// Use driver context injection
	txCtx := driver.WithExecutor(ctx, execTx)
	err = store.SaveMessage(txCtx, msg)
	if err != nil {
		t.Fatalf("SaveMessage in tx failed: %v", err)
	}

	err = execTx.Commit(ctx)
	if err != nil {
		t.Fatalf("Commit failed: %v", err)
	}

	// Message should exist after commit
	messages, _ := store.GetMessages(ctx, sessionID)
	found := false
	for _, m := range messages {
		if m.ID == commitMsgID {
			found = true
			break
		}
	}
	if !found {
		t.Error("Message should exist after commit")
	}

	// Test transaction rollback
	execTx2, err := drv.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin failed: %v", err)
	}

	rollbackMsgID := uuid.New().String()
	msg2 := &storage.Message{
		ID:        rollbackMsgID,
		SessionID: sessionID,
		Role:      "user",
		Content:   []any{},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	// Use driver context injection
	txCtx2 := driver.WithExecutor(ctx, execTx2)
	if err := store.SaveMessage(txCtx2, msg2); err != nil {
		t.Fatalf("SaveMessage in tx failed: %v", err)
	}

	// Rollback
	if err := execTx2.Rollback(ctx); err != nil {
		t.Fatalf("Rollback failed: %v", err)
	}

	// Message should not exist after rollback
	messages, _ = store.GetMessages(ctx, sessionID)
	for _, m := range messages {
		if m.ID == rollbackMsgID {
			t.Error("Message should not exist after rollback")
		}
	}
}

func TestIntegration_DatabaseSQL_Driver_NestedTransactions(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	db := getTestDB(t)
	if db == nil {
		return
	}
	defer db.Close()

	ctx := context.Background()
	if err := cleanTables(ctx, db); err != nil {
		t.Fatalf("Failed to clean tables: %v", err)
	}

	drv := New(db)
	store := drv.GetStore()

	// Create session
	sessionID, _ := store.CreateSession(ctx, "tenant1", "test", nil, nil)

	// Start outer transaction
	outerTx, err := drv.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin outer failed: %v", err)
	}
	defer outerTx.Rollback(ctx)

	// Save message in outer transaction
	outerMsgID := uuid.New().String()
	outerMsg := &storage.Message{
		ID:        outerMsgID,
		SessionID: sessionID,
		Role:      "user",
		Content:   []any{map[string]any{"type": "text", "text": "outer"}},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	outerCtx := driver.WithExecutor(ctx, outerTx)
	err = store.SaveMessage(outerCtx, outerMsg)
	if err != nil {
		t.Fatalf("SaveMessage in outer tx failed: %v", err)
	}

	// Start inner (nested) transaction - this uses savepoints in database/sql driver
	innerTx, err := outerTx.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin inner failed: %v", err)
	}

	// Save message in inner transaction
	innerMsgID := uuid.New().String()
	innerMsg := &storage.Message{
		ID:        innerMsgID,
		SessionID: sessionID,
		Role:      "assistant",
		Content:   []any{map[string]any{"type": "text", "text": "inner"}},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	innerCtx := driver.WithExecutor(ctx, innerTx)
	if err := store.SaveMessage(innerCtx, innerMsg); err != nil {
		t.Fatalf("SaveMessage in inner tx failed: %v", err)
	}

	// Rollback inner transaction (releases savepoint)
	if err := innerTx.Rollback(ctx); err != nil {
		t.Fatalf("Rollback inner failed: %v", err)
	}

	// Commit outer transaction
	if err := outerTx.Commit(ctx); err != nil {
		t.Fatalf("Commit outer failed: %v", err)
	}

	// Verify: outer message should exist, inner message should not
	messages, _ := store.GetMessages(ctx, sessionID)
	outerFound := false
	innerFound := false
	for _, m := range messages {
		if m.ID == outerMsgID {
			outerFound = true
		}
		if m.ID == innerMsgID {
			innerFound = true
		}
	}

	if !outerFound {
		t.Error("Outer message should exist after commit")
	}
	if innerFound {
		t.Error("Inner message should NOT exist after inner rollback (savepoint rollback)")
	}
}

func TestIntegration_DatabaseSQL_Store_CompactionEvent(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	db := getTestDB(t)
	if db == nil {
		return
	}
	defer db.Close()

	ctx := context.Background()
	if err := cleanTables(ctx, db); err != nil {
		t.Fatalf("Failed to clean tables: %v", err)
	}

	drv := New(db)
	store := drv.GetStore()

	// Create session
	sessionID, _ := store.CreateSession(ctx, "tenant1", "test", nil, nil)

	// Save compaction event
	preservedID1 := uuid.New().String()
	preservedID2 := uuid.New().String()
	event := &storage.CompactionEvent{
		SessionID:           sessionID,
		Strategy:            "hybrid",
		OriginalTokens:      100000,
		CompactedTokens:     50000,
		MessagesRemoved:     10,
		SummaryContent:      "Summary of conversation",
		PreservedMessageIDs: []string{preservedID1, preservedID2},
		ModelUsed:           "claude-3-5-haiku",
		DurationMs:          1500,
		CreatedAt:           time.Now(),
	}

	if err := store.SaveCompactionEvent(ctx, event); err != nil {
		t.Fatalf("SaveCompactionEvent failed: %v", err)
	}

	// Get compaction history
	history, err := store.GetCompactionHistory(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetCompactionHistory failed: %v", err)
	}
	if len(history) != 1 {
		t.Errorf("Expected 1 compaction event, got %d", len(history))
	}
	if history[0].Strategy != "hybrid" {
		t.Errorf("Expected strategy 'hybrid', got '%s'", history[0].Strategy)
	}
	if len(history[0].PreservedMessageIDs) != 2 {
		t.Errorf("Expected 2 preserved message IDs, got %d", len(history[0].PreservedMessageIDs))
	}

	// Update compaction count
	if err := store.UpdateSessionCompactionCount(ctx, sessionID); err != nil {
		t.Fatalf("UpdateSessionCompactionCount failed: %v", err)
	}

	session, _ := store.GetSession(ctx, sessionID)
	if session.CompactionCount != 1 {
		t.Errorf("Expected compaction count 1, got %d", session.CompactionCount)
	}
}
