-- =============================================================================
-- AGENTPG SCHEMA v2.0 - DOWN MIGRATION
-- =============================================================================
-- Reverses all changes from 001_agentpg_migration.up.sql
-- Objects are dropped in reverse order to handle foreign key dependencies.
-- =============================================================================

-- =============================================================================
-- DROP TRIGGERS
-- =============================================================================

-- Cleanup triggers
DROP TRIGGER IF EXISTS trg_cleanup_orphaned_tools ON agentpg_instance_tools;
DROP TRIGGER IF EXISTS trg_cleanup_orphaned_agents ON agentpg_instance_agents;
DROP TRIGGER IF EXISTS trg_cleanup_orphaned_work ON agentpg_instances;

-- Notification triggers
DROP TRIGGER IF EXISTS trg_child_run_complete ON agentpg_runs;
DROP TRIGGER IF EXISTS trg_tools_complete ON agentpg_tool_executions;
DROP TRIGGER IF EXISTS trg_tool_created ON agentpg_tool_executions;
DROP TRIGGER IF EXISTS trg_run_state_change ON agentpg_runs;
DROP TRIGGER IF EXISTS trg_run_created ON agentpg_runs;

-- =============================================================================
-- DROP FUNCTIONS
-- =============================================================================

-- Cleanup functions
DROP FUNCTION IF EXISTS agentpg_cleanup_orphaned_tools();
DROP FUNCTION IF EXISTS agentpg_cleanup_orphaned_agents();
DROP FUNCTION IF EXISTS agentpg_cleanup_orphaned_work();

-- Notification functions
DROP FUNCTION IF EXISTS agentpg_handle_child_run_complete();
DROP FUNCTION IF EXISTS agentpg_notify_tools_complete();
DROP FUNCTION IF EXISTS agentpg_notify_tool_created();
DROP FUNCTION IF EXISTS agentpg_notify_run_state_change();
DROP FUNCTION IF EXISTS agentpg_notify_run_created();

-- Stored procedures
DROP FUNCTION IF EXISTS agentpg_get_stuck_runs(INTERVAL, INTEGER, INTEGER);
DROP FUNCTION IF EXISTS agentpg_get_iterations_for_poll(TEXT, INTERVAL, INTEGER);
DROP FUNCTION IF EXISTS agentpg_claim_tool_executions(TEXT, INTEGER);
DROP FUNCTION IF EXISTS agentpg_claim_runs(TEXT, INTEGER, agentpg_run_mode);
DROP FUNCTION IF EXISTS agentpg_claim_runs(TEXT, INTEGER);

-- =============================================================================
-- DROP FOREIGN KEY CONSTRAINTS (deferred FKs)
-- =============================================================================

-- These FKs were added after table creation to handle circular references
ALTER TABLE agentpg_runs DROP CONSTRAINT IF EXISTS fk_runs_parent_tool_execution;
ALTER TABLE agentpg_runs DROP CONSTRAINT IF EXISTS fk_runs_current_iteration;

-- =============================================================================
-- DROP INDEXES
-- =============================================================================

-- Archive indexes
DROP INDEX IF EXISTS idx_archive_session;
DROP INDEX IF EXISTS idx_archive_compaction;

-- Compaction indexes
DROP INDEX IF EXISTS idx_compaction_session;

-- Instance capability indexes
DROP INDEX IF EXISTS idx_instance_tools_by_tool;
DROP INDEX IF EXISTS idx_instance_agents_by_agent;

-- Instance indexes
DROP INDEX IF EXISTS idx_instances_heartbeat;

-- Tool execution indexes
DROP INDEX IF EXISTS idx_tool_exec_pending_scheduled;
DROP INDEX IF EXISTS idx_tool_exec_child_run;
DROP INDEX IF EXISTS idx_tool_exec_iteration;
DROP INDEX IF EXISTS idx_tool_exec_run;
DROP INDEX IF EXISTS idx_tool_exec_running;
DROP INDEX IF EXISTS idx_tool_exec_pending;

-- Iteration indexes
DROP INDEX IF EXISTS idx_iterations_polling;
DROP INDEX IF EXISTS idx_iterations_batch;
DROP INDEX IF EXISTS idx_iterations_run;

-- Content block indexes
DROP INDEX IF EXISTS idx_content_blocks_tool_use;
DROP INDEX IF EXISTS idx_content_blocks_message;

-- Message indexes
DROP INDEX IF EXISTS idx_messages_preserved;
DROP INDEX IF EXISTS idx_messages_run;
DROP INDEX IF EXISTS idx_messages_session;

-- Run indexes
DROP INDEX IF EXISTS idx_runs_stuck_rescue;
DROP INDEX IF EXISTS idx_runs_claimed_instance;
DROP INDEX IF EXISTS idx_runs_parent;
DROP INDEX IF EXISTS idx_runs_session;
DROP INDEX IF EXISTS idx_runs_active;
DROP INDEX IF EXISTS idx_runs_pending_tools;
DROP INDEX IF EXISTS idx_runs_pending_claim;
DROP INDEX IF EXISTS idx_runs_pending_batch;
DROP INDEX IF EXISTS idx_runs_pending_streaming;

-- Tool indexes
DROP INDEX IF EXISTS idx_tools_agent;

-- Session indexes
DROP INDEX IF EXISTS idx_sessions_parent;
DROP INDEX IF EXISTS idx_sessions_tenant_updated;
DROP INDEX IF EXISTS idx_sessions_tenant_identifier;

-- =============================================================================
-- DROP TABLES
-- =============================================================================
-- Order matters: drop tables with FK references first

-- Compaction tables
DROP TABLE IF EXISTS agentpg_message_archive;
DROP TABLE IF EXISTS agentpg_compaction_events;

-- Leader election (UNLOGGED)
DROP TABLE IF EXISTS agentpg_leader;

-- Instance capability tables (UNLOGGED)
DROP TABLE IF EXISTS agentpg_instance_tools;
DROP TABLE IF EXISTS agentpg_instance_agents;

-- Instances (UNLOGGED)
DROP TABLE IF EXISTS agentpg_instances;

-- Tool executions (references runs, iterations, agents)
DROP TABLE IF EXISTS agentpg_tool_executions;

-- Iterations (references runs, messages)
DROP TABLE IF EXISTS agentpg_iterations;

-- Content blocks (references messages)
DROP TABLE IF EXISTS agentpg_content_blocks;

-- Messages (references sessions, runs)
DROP TABLE IF EXISTS agentpg_messages;

-- Runs (references sessions, agents, self)
DROP TABLE IF EXISTS agentpg_runs;

-- Tools (references agents)
DROP TABLE IF EXISTS agentpg_tools;

-- Agents (no FK dependencies from other core tables)
DROP TABLE IF EXISTS agentpg_agents;

-- Sessions (self-referential only)
DROP TABLE IF EXISTS agentpg_sessions;

-- =============================================================================
-- DROP ENUM TYPES
-- =============================================================================

DROP TYPE IF EXISTS agentpg_message_role;
DROP TYPE IF EXISTS agentpg_content_type;
DROP TYPE IF EXISTS agentpg_tool_execution_state;
DROP TYPE IF EXISTS agentpg_batch_status;
DROP TYPE IF EXISTS agentpg_run_state;
DROP TYPE IF EXISTS agentpg_run_mode;
