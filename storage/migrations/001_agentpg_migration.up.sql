-- AgentPG Schema
-- Event-driven architecture with registration system and state machines.
-- All tables use the agentpg_ prefix to avoid collisions with client tables.

-- =============================================================================
-- ENUM TYPES
-- =============================================================================

-- Run state machine states
CREATE TYPE agentpg_run_state AS ENUM(
    'running',     -- Run in progress
    'completed',   -- Run finished successfully
    'cancelled',   -- Run cancelled by user/system
    'failed'       -- Run failed with error
);

-- =============================================================================
-- SESSIONS TABLE
-- =============================================================================
-- Stores conversation sessions with multi-tenant isolation.
-- Each session belongs to a tenant and has a unique identifier within that tenant.
--
-- Columns:
--   id              - UUID primary key, auto-generated
--   tenant_id       - Tenant identifier for multi-tenancy isolation (e.g., "org-123")
--   identifier      - Custom session identifier within tenant (e.g., "user-456")
--   metadata        - Application-specific data as JSONB
--   compaction_count - Number of compaction operations performed
--   created_at      - Session creation timestamp
--   updated_at      - Last update timestamp

CREATE TABLE agentpg_sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id TEXT NOT NULL,
    identifier TEXT NOT NULL,
    parent_session_id UUID REFERENCES agentpg_sessions(id) ON DELETE CASCADE,
    metadata JSONB NOT NULL DEFAULT '{}',
    compaction_count INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_agentpg_sessions_tenant_identifier ON agentpg_sessions(tenant_id, identifier);
CREATE INDEX idx_agentpg_sessions_tenant_updated ON agentpg_sessions(tenant_id, updated_at DESC);

-- =============================================================================
-- RUNS TABLE
-- =============================================================================
-- Tracks each Run() invocation with a state machine.
-- Each run represents a single prompt-response cycle (potentially with tool calls).
--
-- Columns:
--   id              - UUID primary key
--   session_id      - Parent session
--   state           - Current state in the state machine
--   agent_name      - Name of the agent that executed this run
--   prompt          - Original user prompt
--   response_text   - Final response text (NULL while running)
--   stop_reason     - Why the run stopped (end_turn, tool_use, max_tokens, etc.)
--   input_tokens    - Total input tokens consumed
--   output_tokens   - Total output tokens generated
--   tool_iterations - Number of tool call iterations
--   error_message   - Error message if failed
--   error_type      - Error classification if failed
--   instance_id     - Instance that created this run
--   started_at      - When the run started
--   finalized_at    - When the run completed/failed/cancelled (NULL while running)

CREATE TABLE agentpg_runs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id UUID NOT NULL REFERENCES agentpg_sessions(id) ON DELETE CASCADE,

    -- State machine
    state agentpg_run_state NOT NULL DEFAULT 'running',

    -- Agent tracking
    agent_name TEXT NOT NULL,

    -- Request/Response
    prompt TEXT NOT NULL,
    response_text TEXT,
    stop_reason TEXT,

    -- Token usage for this run
    input_tokens INTEGER NOT NULL DEFAULT 0,
    output_tokens INTEGER NOT NULL DEFAULT 0,

    -- Iterations
    tool_iterations INTEGER NOT NULL DEFAULT 0,

    -- Error tracking
    error_message TEXT,
    error_type TEXT,

    -- Instance tracking
    instance_id TEXT,

    -- Metadata
    metadata JSONB NOT NULL DEFAULT '{}',

    -- Timestamps
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finalized_at TIMESTAMPTZ,

    -- Constraint: terminal states require finalized_at
    CONSTRAINT finalized_or_finalized_at_null CHECK (
        (state IN ('completed', 'cancelled', 'failed') AND finalized_at IS NOT NULL)
        OR (state = 'running' AND finalized_at IS NULL)
    )
);

CREATE INDEX idx_agentpg_runs_session ON agentpg_runs(session_id, started_at DESC);
CREATE INDEX idx_agentpg_runs_state ON agentpg_runs(state) WHERE state = 'running';
CREATE INDEX idx_agentpg_runs_finalized ON agentpg_runs(state, finalized_at) WHERE finalized_at IS NOT NULL;
CREATE INDEX idx_agentpg_runs_instance ON agentpg_runs(instance_id) WHERE instance_id IS NOT NULL;

-- =============================================================================
-- MESSAGES TABLE
-- =============================================================================
-- Stores individual messages within conversation sessions.
-- Messages link to both session (conversation history) and run (run-specific queries).
--
-- Columns:
--   id           - UUID primary key
--   session_id   - Foreign key to parent session (NOT NULL, for conversation queries)
--   run_id       - Foreign key to run that created this message (nullable)
--   role         - Message role: 'user', 'assistant', or 'system'
--   content      - Array of content blocks as JSONB
--   usage        - Token usage breakdown
--   metadata     - Application-specific metadata
--   is_preserved - Protected from compaction
--   is_summary   - Synthetic summary message

CREATE TABLE agentpg_messages (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id UUID NOT NULL REFERENCES agentpg_sessions(id) ON DELETE CASCADE,
    run_id UUID REFERENCES agentpg_runs(id) ON DELETE SET NULL,
    role TEXT NOT NULL CHECK (role IN ('user', 'assistant', 'system')),
    content JSONB NOT NULL,
    usage JSONB NOT NULL DEFAULT '{}',
    metadata JSONB NOT NULL DEFAULT '{}',
    is_preserved BOOLEAN NOT NULL DEFAULT FALSE,
    is_summary BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_agentpg_messages_session ON agentpg_messages(session_id, created_at);
CREATE INDEX idx_agentpg_messages_run ON agentpg_messages(run_id, created_at) WHERE run_id IS NOT NULL;
CREATE INDEX idx_agentpg_messages_preserved ON agentpg_messages(session_id, is_preserved) WHERE is_preserved = true;
CREATE INDEX idx_agentpg_messages_summary ON agentpg_messages(session_id, is_summary) WHERE is_summary = true;

-- =============================================================================
-- COMPACTION EVENTS TABLE
-- =============================================================================
-- Audit trail for context compaction operations.

CREATE TABLE agentpg_compaction_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id UUID NOT NULL REFERENCES agentpg_sessions(id) ON DELETE CASCADE,
    strategy TEXT NOT NULL,
    original_tokens INTEGER NOT NULL,
    compacted_tokens INTEGER NOT NULL,
    messages_removed INTEGER NOT NULL,
    summary_content TEXT,
    preserved_message_ids JSONB,
    model_used TEXT,
    duration_ms BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_agentpg_compaction_session ON agentpg_compaction_events(session_id, created_at DESC);
CREATE INDEX idx_agentpg_compaction_strategy ON agentpg_compaction_events(strategy);

-- =============================================================================
-- MESSAGE ARCHIVE TABLE
-- =============================================================================
-- Stores archived messages removed during compaction for reversibility.

CREATE TABLE agentpg_message_archive (
    id UUID PRIMARY KEY,
    compaction_event_id UUID REFERENCES agentpg_compaction_events(id) ON DELETE CASCADE,
    session_id UUID NOT NULL REFERENCES agentpg_sessions(id) ON DELETE CASCADE,
    original_message JSONB NOT NULL,
    archived_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_agentpg_archive_compaction ON agentpg_message_archive(compaction_event_id);
CREATE INDEX idx_agentpg_archive_session ON agentpg_message_archive(session_id, archived_at DESC);

-- =============================================================================
-- INSTANCES TABLE (UNLOGGED for performance)
-- =============================================================================
-- Tracks running instances of agentpg clients with heartbeat.
-- UNLOGGED because instance data is transient and doesn't need WAL durability.
--
-- Columns:
--   id              - Instance UUID (generated on startup)
--   hostname        - Machine hostname
--   pid             - Process ID
--   version         - AgentPG version
--   metadata        - Additional instance metadata
--   created_at      - When instance started
--   last_heartbeat_at - Last heartbeat timestamp

CREATE UNLOGGED TABLE agentpg_instances (
    id TEXT PRIMARY KEY,
    hostname TEXT,
    pid INTEGER,
    version TEXT,
    metadata JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_heartbeat_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT id_length CHECK (char_length(id) > 0 AND char_length(id) < 128)
);

CREATE INDEX idx_agentpg_instances_heartbeat ON agentpg_instances(last_heartbeat_at);

-- =============================================================================
-- AGENTS TABLE
-- =============================================================================
-- Stores registered agent definitions (global, not per-tenant).
-- Each unique agent name has one row.

CREATE TABLE agentpg_agents (
    name TEXT PRIMARY KEY,
    description TEXT,
    model TEXT NOT NULL,
    system_prompt TEXT,
    max_tokens INTEGER,
    temperature REAL,
    config JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT name_length CHECK (char_length(name) > 0 AND char_length(name) < 256)
);

-- =============================================================================
-- TOOLS TABLE
-- =============================================================================
-- Stores registered tool definitions (global).

CREATE TABLE agentpg_tools (
    name TEXT PRIMARY KEY,
    description TEXT NOT NULL,
    input_schema JSONB NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT name_length CHECK (char_length(name) > 0 AND char_length(name) < 128)
);

-- =============================================================================
-- INSTANCE-AGENT BRIDGE TABLE (UNLOGGED)
-- =============================================================================
-- Tracks which agents are registered on which instances.

CREATE UNLOGGED TABLE agentpg_instance_agents (
    instance_id TEXT NOT NULL REFERENCES agentpg_instances(id) ON DELETE CASCADE,
    agent_name TEXT NOT NULL REFERENCES agentpg_agents(name) ON DELETE CASCADE,
    registered_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (instance_id, agent_name)
);

CREATE INDEX idx_agentpg_instance_agents_agent ON agentpg_instance_agents(agent_name);

-- =============================================================================
-- INSTANCE-TOOL BRIDGE TABLE (UNLOGGED)
-- =============================================================================
-- Tracks which tools are registered on which instances.

CREATE UNLOGGED TABLE agentpg_instance_tools (
    instance_id TEXT NOT NULL REFERENCES agentpg_instances(id) ON DELETE CASCADE,
    tool_name TEXT NOT NULL REFERENCES agentpg_tools(name) ON DELETE CASCADE,
    registered_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (instance_id, tool_name)
);

CREATE INDEX idx_agentpg_instance_tools_tool ON agentpg_instance_tools(tool_name);

-- =============================================================================
-- LEADER ELECTION TABLE (UNLOGGED)
-- =============================================================================
-- Single-row table for leader election.
-- Only one instance can be leader at a time for cleanup operations.

CREATE UNLOGGED TABLE agentpg_leader (
    name TEXT PRIMARY KEY DEFAULT 'default' CHECK (name = 'default'),
    leader_id TEXT NOT NULL,
    elected_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,

    CONSTRAINT leader_id_length CHECK (char_length(leader_id) > 0 AND char_length(leader_id) < 128)
);

-- =============================================================================
-- TRIGGER: Orphan Agent Cleanup
-- =============================================================================
-- When the last instance referencing an agent is removed, delete the orphaned agent.

CREATE OR REPLACE FUNCTION agentpg_delete_orphaned_agent()
RETURNS TRIGGER AS $$
BEGIN
    DELETE FROM agentpg_agents
    WHERE name = OLD.agent_name
    AND NOT EXISTS (
        SELECT 1 FROM agentpg_instance_agents
        WHERE agent_name = OLD.agent_name
    );
    RETURN OLD;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_agentpg_delete_orphaned_agent
    AFTER DELETE ON agentpg_instance_agents
    FOR EACH ROW
    EXECUTE FUNCTION agentpg_delete_orphaned_agent();

-- =============================================================================
-- TRIGGER: Orphan Tool Cleanup
-- =============================================================================
-- When the last instance referencing a tool is removed, delete the orphaned tool.

CREATE OR REPLACE FUNCTION agentpg_delete_orphaned_tool()
RETURNS TRIGGER AS $$
BEGIN
    DELETE FROM agentpg_tools
    WHERE name = OLD.tool_name
    AND NOT EXISTS (
        SELECT 1 FROM agentpg_instance_tools
        WHERE tool_name = OLD.tool_name
    );
    RETURN OLD;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_agentpg_delete_orphaned_tool
    AFTER DELETE ON agentpg_instance_tools
    FOR EACH ROW
    EXECUTE FUNCTION agentpg_delete_orphaned_tool();

-- =============================================================================
-- TRIGGER: Run State Change Notification
-- =============================================================================
-- Notify when a run transitions to a terminal state.

CREATE OR REPLACE FUNCTION agentpg_run_notify()
RETURNS TRIGGER AS $$
DECLARE
    payload json;
BEGIN
    -- Notify when transitioning from running to terminal state
    IF NEW.state IN ('completed', 'cancelled', 'failed') AND
       (OLD IS NULL OR OLD.state = 'running') THEN
        payload = json_build_object(
            'run_id', NEW.id,
            'session_id', NEW.session_id,
            'state', NEW.state,
            'agent_name', NEW.agent_name
        );
        PERFORM pg_notify('agentpg_run_finalized', payload::text);
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_agentpg_run_notify
    AFTER INSERT OR UPDATE ON agentpg_runs
    FOR EACH ROW
    EXECUTE FUNCTION agentpg_run_notify();

-- =============================================================================
-- TRIGGER: Orphan Run Cleanup on Instance Stale
-- =============================================================================
-- When an instance is deleted, mark its running runs as failed.

CREATE OR REPLACE FUNCTION agentpg_cleanup_orphan_runs()
RETURNS TRIGGER AS $$
BEGIN
    UPDATE agentpg_runs
    SET state = 'failed',
        finalized_at = NOW(),
        error_message = 'Instance disconnected',
        error_type = 'orphan'
    WHERE instance_id = OLD.id
      AND state = 'running';
    RETURN OLD;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_agentpg_cleanup_orphan_runs
    BEFORE DELETE ON agentpg_instances
    FOR EACH ROW
    EXECUTE FUNCTION agentpg_cleanup_orphan_runs();
