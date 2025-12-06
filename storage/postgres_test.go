package storage

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/youssefsiam38/agentpg/internal/testutil"
)

func TestIntegration_PostgresStore_SessionLifecycle(t *testing.T) {
	testutil.RequireIntegration(t)

	db := testutil.NewTestDB(t)
	if db == nil {
		return
	}
	defer db.Close()

	ctx := context.Background()
	if err := db.CleanTables(ctx); err != nil {
		t.Fatalf("Failed to clean tables: %v", err)
	}

	store := NewPostgresStore(db.Pool)

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

func TestIntegration_PostgresStore_MessageOperations(t *testing.T) {
	testutil.RequireIntegration(t)

	db := testutil.NewTestDB(t)
	if db == nil {
		return
	}
	defer db.Close()

	ctx := context.Background()
	if err := db.CleanTables(ctx); err != nil {
		t.Fatalf("Failed to clean tables: %v", err)
	}

	store := NewPostgresStore(db.Pool)

	// Create session
	sessionID, _ := store.CreateSession(ctx, "tenant1", "test", nil, nil)

	// Save message
	msgID := uuid.New().String()
	msg := &Message{
		ID:        msgID,
		SessionID: sessionID,
		Role:      "user",
		Content:   []any{map[string]any{"type": "text", "text": "hello"}},
		Usage: &MessageUsage{
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

func TestIntegration_PostgresStore_Transaction(t *testing.T) {
	testutil.RequireIntegration(t)

	db := testutil.NewTestDB(t)
	if db == nil {
		return
	}
	defer db.Close()

	ctx := context.Background()
	if err := db.CleanTables(ctx); err != nil {
		t.Fatalf("Failed to clean tables: %v", err)
	}

	store := NewPostgresStore(db.Pool)

	// Create session outside tx
	sessionID, _ := store.CreateSession(ctx, "tenant1", "test", nil, nil)

	// Test transaction commit using pool.Begin() and WithTx()
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin failed: %v", err)
	}

	commitMsgID := uuid.New().String()
	msg := &Message{
		ID:        commitMsgID,
		SessionID: sessionID,
		Role:      "user",
		Content:   []any{},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	// Use WithTx to create context with transaction
	txCtx := WithTx(ctx, tx)
	if err := store.SaveMessage(txCtx, msg); err != nil {
		t.Fatalf("SaveMessage in tx failed: %v", err)
	}

	if err := tx.Commit(ctx); err != nil {
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
	tx2, err := db.Pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin failed: %v", err)
	}

	rollbackMsgID := uuid.New().String()
	msg2 := &Message{
		ID:        rollbackMsgID,
		SessionID: sessionID,
		Role:      "user",
		Content:   []any{},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	// Use WithTx to create context with transaction
	txCtx2 := WithTx(ctx, tx2)
	if err := store.SaveMessage(txCtx2, msg2); err != nil {
		t.Fatalf("SaveMessage in tx failed: %v", err)
	}

	// Rollback
	if err := tx2.Rollback(ctx); err != nil {
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

func TestIntegration_PostgresStore_CompactionEvent(t *testing.T) {
	testutil.RequireIntegration(t)

	db := testutil.NewTestDB(t)
	if db == nil {
		return
	}
	defer db.Close()

	ctx := context.Background()
	if err := db.CleanTables(ctx); err != nil {
		t.Fatalf("Failed to clean tables: %v", err)
	}

	store := NewPostgresStore(db.Pool)

	// Create session
	sessionID, _ := store.CreateSession(ctx, "tenant1", "test", nil, nil)

	// Save compaction event
	preservedID1 := uuid.New().String()
	preservedID2 := uuid.New().String()
	event := &CompactionEvent{
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
