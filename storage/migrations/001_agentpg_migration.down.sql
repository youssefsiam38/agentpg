-- AgentPG Schema Rollback
-- Drops all objects in reverse dependency order.

-- Drop triggers first
DROP TRIGGER IF EXISTS trg_agentpg_cleanup_orphan_tool_executions ON agentpg_instances;
DROP TRIGGER IF EXISTS trg_agentpg_cleanup_orphan_runs ON agentpg_instances;
DROP TRIGGER IF EXISTS trg_agentpg_delete_orphaned_tool ON agentpg_instance_tools;
DROP TRIGGER IF EXISTS trg_agentpg_delete_orphaned_agent ON agentpg_instance_agents;
DROP TRIGGER IF EXISTS trg_agentpg_tools_complete_notify ON agentpg_tool_executions;
DROP TRIGGER IF EXISTS trg_agentpg_tool_pending_notify ON agentpg_tool_executions;
DROP TRIGGER IF EXISTS trg_agentpg_run_state_notify ON agentpg_runs;
DROP TRIGGER IF EXISTS trg_agentpg_run_created_notify ON agentpg_runs;

-- Drop functions
DROP FUNCTION IF EXISTS agentpg_cleanup_orphan_tool_executions();
DROP FUNCTION IF EXISTS agentpg_cleanup_orphan_runs();
DROP FUNCTION IF EXISTS agentpg_delete_orphaned_tool();
DROP FUNCTION IF EXISTS agentpg_delete_orphaned_agent();
DROP FUNCTION IF EXISTS agentpg_tools_complete_notify();
DROP FUNCTION IF EXISTS agentpg_tool_pending_notify();
DROP FUNCTION IF EXISTS agentpg_run_state_notify();
DROP FUNCTION IF EXISTS agentpg_run_created_notify();

-- Drop tables in reverse dependency order
DROP TABLE IF EXISTS agentpg_leader;
DROP TABLE IF EXISTS agentpg_instance_tools;
DROP TABLE IF EXISTS agentpg_instance_agents;
DROP TABLE IF EXISTS agentpg_tools;
DROP TABLE IF EXISTS agentpg_agents;
DROP TABLE IF EXISTS agentpg_instances;
DROP TABLE IF EXISTS agentpg_message_archive;
DROP TABLE IF EXISTS agentpg_compaction_events;
DROP TABLE IF EXISTS agentpg_tool_executions;
DROP TABLE IF EXISTS agentpg_content_blocks;
DROP TABLE IF EXISTS agentpg_messages;
DROP TABLE IF EXISTS agentpg_runs;
DROP TABLE IF EXISTS agentpg_sessions;

-- Drop enum types
DROP TYPE IF EXISTS agentpg_content_block_type;
DROP TYPE IF EXISTS agentpg_tool_execution_state;
DROP TYPE IF EXISTS agentpg_run_state;
