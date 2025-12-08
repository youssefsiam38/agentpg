package pgxv5

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/youssefsiam38/agentpg/driver"
	"github.com/youssefsiam38/agentpg/internal/testutil"
	"github.com/youssefsiam38/agentpg/runstate"
	"github.com/youssefsiam38/agentpg/storage"
)

func TestIntegration_Store_SessionLifecycle(t *testing.T) {
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

	drv := New(db.Pool)
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

func TestIntegration_Store_MessageOperations(t *testing.T) {
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

	drv := New(db.Pool)
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

func TestIntegration_Store_Transaction(t *testing.T) {
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

	drv := New(db.Pool)
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
	if err := store.SaveMessage(txCtx, msg); err != nil {
		t.Fatalf("SaveMessage in tx failed: %v", err)
	}

	if err := execTx.Commit(ctx); err != nil {
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

func TestIntegration_Store_CompactionEvent(t *testing.T) {
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

	drv := New(db.Pool)
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

func TestIntegration_Driver_NestedTransactions(t *testing.T) {
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

	drv := New(db.Pool)
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
	if err := store.SaveMessage(outerCtx, outerMsg); err != nil {
		t.Fatalf("SaveMessage in outer tx failed: %v", err)
	}

	// Start inner (nested) transaction
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

	// Rollback inner transaction
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
		t.Error("Inner message should NOT exist after inner rollback")
	}
}

// =============================================================================
// Distributed Integration Tests
// =============================================================================

func TestIntegration_Store_RunOperations(t *testing.T) {
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

	drv := New(db.Pool)
	store := drv.GetStore()

	// Create session first
	sessionID, err := store.CreateSession(ctx, "tenant1", "test-run", nil, nil)
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	// Create a run
	runParams := &storage.CreateRunParams{
		SessionID:  sessionID,
		AgentName:  "test-agent",
		Prompt:     "Hello, world!",
		InstanceID: "instance-1",
	}
	runID, err := store.CreateRun(ctx, runParams)
	if err != nil {
		t.Fatalf("CreateRun failed: %v", err)
	}
	if runID == "" {
		t.Fatal("Expected non-empty run ID")
	}

	// Get the run
	run, err := store.GetRun(ctx, runID)
	if err != nil {
		t.Fatalf("GetRun failed: %v", err)
	}
	if run.SessionID != sessionID {
		t.Errorf("Expected session ID '%s', got '%s'", sessionID, run.SessionID)
	}
	if run.State != runstate.RunStateRunning {
		t.Errorf("Expected state 'running', got '%s'", run.State)
	}
	if run.Prompt != "Hello, world!" {
		t.Errorf("Expected prompt 'Hello, world!', got '%s'", run.Prompt)
	}

	// Update run state to completed
	responseText := "Hello back!"
	updateParams := &storage.UpdateRunStateParams{
		State:        runstate.RunStateCompleted,
		ResponseText: &responseText,
		InputTokens:  10,
		OutputTokens: 20,
	}
	if err := store.UpdateRunState(ctx, runID, updateParams); err != nil {
		t.Fatalf("UpdateRunState failed: %v", err)
	}

	// Verify the update
	run, err = store.GetRun(ctx, runID)
	if err != nil {
		t.Fatalf("GetRun after update failed: %v", err)
	}
	if run.State != runstate.RunStateCompleted {
		t.Errorf("Expected state 'completed', got '%s'", run.State)
	}
	if run.FinalizedAt == nil {
		t.Error("Expected FinalizedAt to be set")
	}

	// Get session runs
	runs, err := store.GetSessionRuns(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetSessionRuns failed: %v", err)
	}
	if len(runs) != 1 {
		t.Errorf("Expected 1 run, got %d", len(runs))
	}

	// Test GetStuckRuns (should return nothing since run is completed)
	stuckRuns, err := store.GetStuckRuns(ctx, time.Now().Add(-1*time.Hour))
	if err != nil {
		t.Fatalf("GetStuckRuns failed: %v", err)
	}
	if len(stuckRuns) != 0 {
		t.Errorf("Expected 0 stuck runs, got %d", len(stuckRuns))
	}
}

func TestIntegration_Store_RunWithMessages(t *testing.T) {
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

	drv := New(db.Pool)
	store := drv.GetStore()

	// Create session
	sessionID, _ := store.CreateSession(ctx, "tenant1", "test", nil, nil)

	// Create run
	runParams := &storage.CreateRunParams{
		SessionID:  sessionID,
		AgentName:  "test-agent",
		Prompt:     "Test prompt",
		InstanceID: "instance-1",
	}
	runID, _ := store.CreateRun(ctx, runParams)

	// Save messages with run_id
	msgID := uuid.New().String()
	msg := &storage.Message{
		ID:        msgID,
		SessionID: sessionID,
		RunID:     &runID,
		Role:      "user",
		Content:   []any{map[string]any{"type": "text", "text": "hello"}},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := store.SaveMessage(ctx, msg); err != nil {
		t.Fatalf("SaveMessage failed: %v", err)
	}

	// Get run messages
	messages, err := store.GetRunMessages(ctx, runID)
	if err != nil {
		t.Fatalf("GetRunMessages failed: %v", err)
	}
	if len(messages) != 1 {
		t.Errorf("Expected 1 message, got %d", len(messages))
	}
	if messages[0].RunID == nil || *messages[0].RunID != runID {
		t.Errorf("Expected run ID '%s', got '%v'", runID, messages[0].RunID)
	}

	// Verify GetMessages also returns run_id
	allMessages, err := store.GetMessages(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetMessages failed: %v", err)
	}
	if len(allMessages) != 1 {
		t.Errorf("Expected 1 message, got %d", len(allMessages))
	}
	if allMessages[0].RunID == nil || *allMessages[0].RunID != runID {
		t.Errorf("Expected run ID '%s' in GetMessages result, got '%v'", runID, allMessages[0].RunID)
	}
}

func TestIntegration_Store_InstanceOperations(t *testing.T) {
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

	drv := New(db.Pool)
	store := drv.GetStore()

	// Register instance
	instanceID := uuid.New().String()
	params := &storage.RegisterInstanceParams{
		ID:       instanceID,
		Hostname: "test-host",
		PID:      12345,
		Version:  "1.0.0",
		Metadata: map[string]any{"env": "test"},
	}
	if err := store.RegisterInstance(ctx, params); err != nil {
		t.Fatalf("RegisterInstance failed: %v", err)
	}

	// Get instance
	instance, err := store.GetInstance(ctx, instanceID)
	if err != nil {
		t.Fatalf("GetInstance failed: %v", err)
	}
	if instance.Hostname == nil || *instance.Hostname != "test-host" {
		t.Errorf("Expected hostname 'test-host', got '%v'", instance.Hostname)
	}
	if instance.PID == nil || *instance.PID != 12345 {
		t.Errorf("Expected PID 12345, got %v", instance.PID)
	}

	// Update heartbeat
	if err := store.UpdateInstanceHeartbeat(ctx, instanceID); err != nil {
		t.Fatalf("UpdateInstanceHeartbeat failed: %v", err)
	}

	// Get active instances (horizon = 2 minutes ago)
	active, err := store.GetActiveInstances(ctx, time.Now().Add(-2*time.Minute))
	if err != nil {
		t.Fatalf("GetActiveInstances failed: %v", err)
	}
	if len(active) != 1 {
		t.Errorf("Expected 1 active instance, got %d", len(active))
	}

	// Test stale detection (should return nothing since we just heartbeated)
	stale, err := store.GetStaleInstances(ctx, time.Now().Add(-1*time.Minute))
	if err != nil {
		t.Fatalf("GetStaleInstances failed: %v", err)
	}
	if len(stale) != 0 {
		t.Errorf("Expected 0 stale instances, got %d", len(stale))
	}

	// Deregister instance
	if err := store.DeregisterInstance(ctx, instanceID); err != nil {
		t.Fatalf("DeregisterInstance failed: %v", err)
	}

	// Verify deregistration
	_, err = store.GetInstance(ctx, instanceID)
	if err == nil {
		t.Error("Expected error getting deregistered instance")
	}
}

func TestIntegration_Store_LeaderOperations(t *testing.T) {
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

	drv := New(db.Pool)
	store := drv.GetStore()

	instance1 := "instance-1"
	instance2 := "instance-2"
	ttl := 30 * time.Second

	// First instance attempts election - should succeed
	elected, err := store.LeaderAttemptElect(ctx, &storage.LeaderElectParams{
		LeaderID: instance1,
		TTL:      ttl,
	})
	if err != nil {
		t.Fatalf("LeaderAttemptElect failed: %v", err)
	}
	if !elected {
		t.Error("First instance should become leader")
	}

	// Get current leader
	leader, err := store.LeaderGetCurrent(ctx)
	if err != nil {
		t.Fatalf("LeaderGetCurrent failed: %v", err)
	}
	if leader == nil {
		t.Fatal("Expected leader to exist")
	}
	if leader.LeaderID != instance1 {
		t.Errorf("Expected leader '%s', got '%s'", instance1, leader.LeaderID)
	}

	// Second instance attempts election - should fail
	elected, err = store.LeaderAttemptElect(ctx, &storage.LeaderElectParams{
		LeaderID: instance2,
		TTL:      ttl,
	})
	if err != nil {
		t.Fatalf("LeaderAttemptElect failed: %v", err)
	}
	if elected {
		t.Error("Second instance should NOT become leader while first holds lease")
	}

	// First instance re-elects - should succeed
	reelected, err := store.LeaderAttemptReelect(ctx, &storage.LeaderElectParams{
		LeaderID: instance1,
		TTL:      ttl,
	})
	if err != nil {
		t.Fatalf("LeaderAttemptReelect failed: %v", err)
	}
	if !reelected {
		t.Error("First instance should succeed in re-election")
	}

	// Second instance tries to re-elect (not the leader) - should fail
	reelected, err = store.LeaderAttemptReelect(ctx, &storage.LeaderElectParams{
		LeaderID: instance2,
		TTL:      ttl,
	})
	if err != nil {
		t.Fatalf("LeaderAttemptReelect failed: %v", err)
	}
	if reelected {
		t.Error("Second instance should NOT succeed in re-election")
	}

	// First instance resigns
	if err := store.LeaderResign(ctx, instance1); err != nil {
		t.Fatalf("LeaderResign failed: %v", err)
	}

	// Verify no leader
	leader, err = store.LeaderGetCurrent(ctx)
	if err != nil {
		t.Fatalf("LeaderGetCurrent after resign failed: %v", err)
	}
	if leader != nil {
		t.Error("Expected no leader after resignation")
	}

	// Second instance can now become leader
	elected, err = store.LeaderAttemptElect(ctx, &storage.LeaderElectParams{
		LeaderID: instance2,
		TTL:      ttl,
	})
	if err != nil {
		t.Fatalf("LeaderAttemptElect failed: %v", err)
	}
	if !elected {
		t.Error("Second instance should become leader after first resigned")
	}
}

func TestIntegration_Store_AgentToolRegistration(t *testing.T) {
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

	drv := New(db.Pool)
	store := drv.GetStore()

	// Register instance first
	instanceID := uuid.New().String()
	instanceParams := &storage.RegisterInstanceParams{
		ID:       instanceID,
		Hostname: "test-host",
		PID:      12345,
		Version:  "1.0.0",
	}
	if err := store.RegisterInstance(ctx, instanceParams); err != nil {
		t.Fatalf("RegisterInstance failed: %v", err)
	}

	// Register agent
	maxTokens := 4096
	agentParams := &storage.RegisterAgentParams{
		Name:         "test-agent",
		Description:  "A test agent",
		Model:        "claude-sonnet-4-5-20250929",
		SystemPrompt: "You are a test agent",
		MaxTokens:    &maxTokens,
	}
	if err := store.RegisterAgent(ctx, agentParams); err != nil {
		t.Fatalf("RegisterAgent failed: %v", err)
	}

	// Get agent
	agent, err := store.GetAgent(ctx, "test-agent")
	if err != nil {
		t.Fatalf("GetAgent failed: %v", err)
	}
	if agent.Description == nil || *agent.Description != "A test agent" {
		t.Errorf("Expected description 'A test agent', got '%v'", agent.Description)
	}

	// Link instance to agent
	if err := store.RegisterInstanceAgent(ctx, instanceID, "test-agent"); err != nil {
		t.Fatalf("RegisterInstanceAgent failed: %v", err)
	}

	// Register tool
	toolParams := &storage.RegisterToolParams{
		Name:        "test-tool",
		Description: "A test tool",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
	}
	if err := store.RegisterTool(ctx, toolParams); err != nil {
		t.Fatalf("RegisterTool failed: %v", err)
	}

	// Get tool
	tool, err := store.GetTool(ctx, "test-tool")
	if err != nil {
		t.Fatalf("GetTool failed: %v", err)
	}
	if tool.Description != "A test tool" {
		t.Errorf("Expected description 'A test tool', got '%s'", tool.Description)
	}

	// Link instance to tool
	if err := store.RegisterInstanceTool(ctx, instanceID, "test-tool"); err != nil {
		t.Fatalf("RegisterInstanceTool failed: %v", err)
	}

	// Get available agents (with active instances, horizon = 2 minutes ago)
	agents, err := store.GetAvailableAgents(ctx, time.Now().Add(-2*time.Minute))
	if err != nil {
		t.Fatalf("GetAvailableAgents failed: %v", err)
	}
	if len(agents) != 1 {
		t.Errorf("Expected 1 available agent, got %d", len(agents))
	}

	// Get available tools (with active instances, horizon = 2 minutes ago)
	tools, err := store.GetAvailableTools(ctx, time.Now().Add(-2*time.Minute))
	if err != nil {
		t.Fatalf("GetAvailableTools failed: %v", err)
	}
	if len(tools) != 1 {
		t.Errorf("Expected 1 available tool, got %d", len(tools))
	}
}
