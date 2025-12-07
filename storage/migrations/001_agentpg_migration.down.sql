-- AgentPG Schema Rollback
-- Drops all tables in reverse dependency order.

DROP TABLE IF EXISTS agentpg_message_archive;
DROP TABLE IF EXISTS agentpg_compaction_events;
DROP TABLE IF EXISTS agentpg_messages;
DROP TABLE IF EXISTS agentpg_sessions;
