-- AgentPG Schema
-- Event-driven architecture with normalized content blocks and state machines.
-- All tables use the agentpg_ prefix to avoid collisions with client tables.

-- =============================================================================
-- ENUM TYPES
-- =============================================================================

-- Run state machine states (extended for async flow)
CREATE TYPE agentpg_run_state AS ENUM(
    'pending',              -- Run created, waiting for worker pickup
    'pending_api',          -- Worker claimed, calling Claude API
    'pending_tools',        -- API returned tool_use, waiting for all tools to complete
    'awaiting_continuation', -- pause_turn or max_tokens, needs continuation
    'completed',            -- Terminal: successful completion
    'cancelled',            -- Terminal: user/system cancelled
    'failed'                -- Terminal: error occurred
);

-- Tool execution states
CREATE TYPE agentpg_tool_execution_state AS ENUM(
    'pending',    -- Waiting for worker pickup
    'running',    -- Currently executing
    'completed',  -- Success
    'failed',     -- Error (can retry)
    'skipped'     -- Skipped (run cancelled)
);

-- Content block types (aligned with Claude API)
CREATE TYPE agentpg_content_block_type AS ENUM(
    'text',
    'tool_use',
    'tool_result',
    'image',
    'document',
    'thinking',
    'server_tool_use',
    'web_search_tool_result'
);

-- =============================================================================
-- SESSIONS TABLE
-- =============================================================================
-- Stores conversation sessions with multi-tenant isolation.
-- Each session belongs to a tenant and has a unique identifier within that tenant.
--
-- Note: Session state is NOT stored here - it's derived from the latest run's state.

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
-- Runs are picked up by workers and processed asynchronously.
--
-- State machine flow:
--   pending -> pending_api -> (completed | pending_tools | awaiting_continuation | failed)
--   pending_tools -> pending_api (when all tools complete)
--   awaiting_continuation -> pending_api (on continuation)

CREATE TABLE agentpg_runs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id UUID NOT NULL REFERENCES agentpg_sessions(id) ON DELETE CASCADE,

    -- State machine
    state agentpg_run_state NOT NULL DEFAULT 'pending',

    -- Agent tracking
    agent_name TEXT NOT NULL,

    -- Request/Response
    prompt TEXT NOT NULL,
    response_text TEXT,
    stop_reason TEXT,

    -- Token usage (cumulative across iterations)
    input_tokens INTEGER NOT NULL DEFAULT 0,
    output_tokens INTEGER NOT NULL DEFAULT 0,

    -- Iterations
    iteration_count INTEGER NOT NULL DEFAULT 0,
    tool_iterations INTEGER NOT NULL DEFAULT 0,

    -- Error tracking
    error_message TEXT,
    error_type TEXT,

    -- Worker tracking
    instance_id TEXT,                      -- Instance that created this run
    worker_instance_id TEXT,               -- Instance currently processing this run
    last_api_call_at TIMESTAMPTZ,          -- For stuck run detection
    continuation_required BOOLEAN NOT NULL DEFAULT FALSE,

    -- Metadata
    metadata JSONB NOT NULL DEFAULT '{}',

    -- Timestamps
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finalized_at TIMESTAMPTZ,

    -- Constraint: terminal states require finalized_at
    CONSTRAINT finalized_or_finalized_at_null CHECK (
        (state IN ('completed', 'cancelled', 'failed') AND finalized_at IS NOT NULL)
        OR (state NOT IN ('completed', 'cancelled', 'failed') AND finalized_at IS NULL)
    )
);

CREATE INDEX idx_agentpg_runs_session ON agentpg_runs(session_id, started_at DESC);
CREATE INDEX idx_agentpg_runs_state ON agentpg_runs(state);
CREATE INDEX idx_agentpg_runs_pending ON agentpg_runs(state, started_at)
    WHERE state IN ('pending', 'pending_api', 'pending_tools', 'awaiting_continuation');
CREATE INDEX idx_agentpg_runs_finalized ON agentpg_runs(state, finalized_at)
    WHERE finalized_at IS NOT NULL;
CREATE INDEX idx_agentpg_runs_instance ON agentpg_runs(instance_id)
    WHERE instance_id IS NOT NULL;
CREATE INDEX idx_agentpg_runs_worker ON agentpg_runs(worker_instance_id)
    WHERE worker_instance_id IS NOT NULL;

-- =============================================================================
-- MESSAGES TABLE
-- =============================================================================
-- Stores individual messages within conversation sessions.
-- Content is normalized into agentpg_content_blocks table.

CREATE TABLE agentpg_messages (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id UUID NOT NULL REFERENCES agentpg_sessions(id) ON DELETE CASCADE,
    run_id UUID REFERENCES agentpg_runs(id) ON DELETE SET NULL,
    role TEXT NOT NULL CHECK (role IN ('user', 'assistant', 'system')),

    -- Token usage for this message
    usage JSONB NOT NULL DEFAULT '{}',

    -- Metadata
    metadata JSONB NOT NULL DEFAULT '{}',
    is_preserved BOOLEAN NOT NULL DEFAULT FALSE,
    is_summary BOOLEAN NOT NULL DEFAULT FALSE,

    -- Timestamps
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_agentpg_messages_session ON agentpg_messages(session_id, created_at);
CREATE INDEX idx_agentpg_messages_run ON agentpg_messages(run_id, created_at)
    WHERE run_id IS NOT NULL;
CREATE INDEX idx_agentpg_messages_preserved ON agentpg_messages(session_id, is_preserved)
    WHERE is_preserved = true;
CREATE INDEX idx_agentpg_messages_summary ON agentpg_messages(session_id, is_summary)
    WHERE is_summary = true;

-- =============================================================================
-- CONTENT BLOCKS TABLE
-- =============================================================================
-- Normalized content blocks with proper relations.
-- Each block belongs to a message and has an ordering index.
-- Tool results reference their corresponding tool use blocks.

CREATE TABLE agentpg_content_blocks (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    message_id UUID NOT NULL REFERENCES agentpg_messages(id) ON DELETE CASCADE,

    -- Ordering within message
    block_index INTEGER NOT NULL,

    -- Block type
    type agentpg_content_block_type NOT NULL,

    -- Text content (for text, thinking blocks)
    text TEXT,

    -- Tool use fields (for tool_use, server_tool_use blocks)
    tool_use_id TEXT,                      -- Claude's tool_use_id (e.g., "toolu_01...")
    tool_name TEXT,
    tool_input JSONB,

    -- Tool result fields (for tool_result blocks)
    -- References the tool_use block this result is for
    tool_result_for_id UUID REFERENCES agentpg_content_blocks(id) ON DELETE SET NULL,
    tool_content TEXT,
    is_error BOOLEAN NOT NULL DEFAULT FALSE,

    -- Image/Document source (for image, document blocks)
    -- Structure: {type, media_type, data, url}
    source JSONB,

    -- Web search results (for web_search_tool_result blocks)
    web_search_results JSONB,

    -- Metadata
    metadata JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- Ensure uniqueness of block_index within message
    CONSTRAINT unique_block_index UNIQUE (message_id, block_index)
);

CREATE INDEX idx_agentpg_content_blocks_message ON agentpg_content_blocks(message_id, block_index);
CREATE INDEX idx_agentpg_content_blocks_type ON agentpg_content_blocks(type);
CREATE INDEX idx_agentpg_content_blocks_tool_use ON agentpg_content_blocks(tool_use_id)
    WHERE tool_use_id IS NOT NULL;
CREATE INDEX idx_agentpg_content_blocks_tool_result ON agentpg_content_blocks(tool_result_for_id)
    WHERE tool_result_for_id IS NOT NULL;

-- =============================================================================
-- TOOL EXECUTIONS TABLE
-- =============================================================================
-- Tracks individual tool executions with state machine.
-- Links tool_use content block to tool_result content block.
-- Workers pick up pending executions and process them in parallel.

CREATE TABLE agentpg_tool_executions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id UUID NOT NULL REFERENCES agentpg_runs(id) ON DELETE CASCADE,

    -- State machine
    state agentpg_tool_execution_state NOT NULL DEFAULT 'pending',

    -- Link to content blocks
    tool_use_block_id UUID NOT NULL REFERENCES agentpg_content_blocks(id) ON DELETE CASCADE,
    tool_result_block_id UUID REFERENCES agentpg_content_blocks(id) ON DELETE SET NULL,

    -- Tool details (denormalized for query efficiency)
    tool_name TEXT NOT NULL,
    tool_input JSONB NOT NULL,

    -- Result
    tool_output TEXT,
    error_message TEXT,

    -- Worker tracking
    instance_id TEXT,
    attempt_count INTEGER NOT NULL DEFAULT 0,
    max_attempts INTEGER NOT NULL DEFAULT 3,

    -- Timestamps
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,

    -- Constraint: terminal states require completed_at
    CONSTRAINT execution_state_timestamps CHECK (
        (state IN ('completed', 'failed', 'skipped') AND completed_at IS NOT NULL)
        OR (state IN ('pending', 'running') AND completed_at IS NULL)
    )
);

CREATE INDEX idx_agentpg_tool_executions_run ON agentpg_tool_executions(run_id, created_at);
CREATE INDEX idx_agentpg_tool_executions_pending ON agentpg_tool_executions(state, created_at)
    WHERE state = 'pending';
CREATE INDEX idx_agentpg_tool_executions_running ON agentpg_tool_executions(state, started_at)
    WHERE state = 'running';
CREATE INDEX idx_agentpg_tool_executions_instance ON agentpg_tool_executions(instance_id)
    WHERE instance_id IS NOT NULL;

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

CREATE UNLOGGED TABLE agentpg_leader (
    name TEXT PRIMARY KEY DEFAULT 'default' CHECK (name = 'default'),
    leader_id TEXT NOT NULL,
    elected_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,

    CONSTRAINT leader_id_length CHECK (char_length(leader_id) > 0 AND char_length(leader_id) < 128)
);

-- =============================================================================
-- TRIGGER: Run Created Notification
-- =============================================================================
-- Notify workers when a new run is created in pending state.

CREATE OR REPLACE FUNCTION agentpg_run_created_notify()
RETURNS TRIGGER AS $$
DECLARE
    payload json;
BEGIN
    IF NEW.state = 'pending' THEN
        payload = json_build_object(
            'run_id', NEW.id,
            'session_id', NEW.session_id,
            'agent_name', NEW.agent_name
        );
        PERFORM pg_notify('agentpg_run_created', payload::text);
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_agentpg_run_created_notify
    AFTER INSERT ON agentpg_runs
    FOR EACH ROW
    EXECUTE FUNCTION agentpg_run_created_notify();

-- =============================================================================
-- TRIGGER: Run State Change Notification
-- =============================================================================
-- Notify on any run state change (for polling clients and worker coordination).

CREATE OR REPLACE FUNCTION agentpg_run_state_notify()
RETURNS TRIGGER AS $$
DECLARE
    payload json;
BEGIN
    -- Notify on state change
    IF OLD IS NULL OR OLD.state IS DISTINCT FROM NEW.state THEN
        payload = json_build_object(
            'run_id', NEW.id,
            'session_id', NEW.session_id,
            'state', NEW.state,
            'previous_state', COALESCE(OLD.state::text, 'none'),
            'agent_name', NEW.agent_name,
            'stop_reason', NEW.stop_reason
        );
        PERFORM pg_notify('agentpg_run_state_changed', payload::text);

        -- Also notify on finalization specifically
        IF NEW.state IN ('completed', 'cancelled', 'failed') THEN
            PERFORM pg_notify('agentpg_run_finalized', payload::text);
        END IF;
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_agentpg_run_state_notify
    AFTER INSERT OR UPDATE ON agentpg_runs
    FOR EACH ROW
    EXECUTE FUNCTION agentpg_run_state_notify();

-- =============================================================================
-- TRIGGER: Tool Execution Pending Notification
-- =============================================================================
-- Notify workers when tool executions are pending.

CREATE OR REPLACE FUNCTION agentpg_tool_pending_notify()
RETURNS TRIGGER AS $$
DECLARE
    payload json;
BEGIN
    IF NEW.state = 'pending' AND (OLD IS NULL OR OLD.state IS DISTINCT FROM 'pending') THEN
        payload = json_build_object(
            'execution_id', NEW.id,
            'run_id', NEW.run_id,
            'tool_name', NEW.tool_name
        );
        PERFORM pg_notify('agentpg_tool_pending', payload::text);
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_agentpg_tool_pending_notify
    AFTER INSERT OR UPDATE ON agentpg_tool_executions
    FOR EACH ROW
    EXECUTE FUNCTION agentpg_tool_pending_notify();

-- =============================================================================
-- TRIGGER: All Tools Complete Notification
-- =============================================================================
-- Notify when ALL tool executions for a run are complete.
-- This triggers the next API call with tool results.

CREATE OR REPLACE FUNCTION agentpg_tools_complete_notify()
RETURNS TRIGGER AS $$
DECLARE
    payload json;
    total_count INTEGER;
    terminal_count INTEGER;
BEGIN
    -- Only check when transitioning to a terminal state
    IF NEW.state IN ('completed', 'failed', 'skipped') AND
       (OLD IS NULL OR OLD.state NOT IN ('completed', 'failed', 'skipped')) THEN

        -- Count total and terminal executions for this run
        SELECT COUNT(*), COUNT(*) FILTER (WHERE state IN ('completed', 'failed', 'skipped'))
        INTO total_count, terminal_count
        FROM agentpg_tool_executions
        WHERE run_id = NEW.run_id;

        -- If all executions are terminal, notify
        IF total_count > 0 AND total_count = terminal_count THEN
            payload = json_build_object(
                'run_id', NEW.run_id,
                'total_executions', total_count,
                'completed', (SELECT COUNT(*) FROM agentpg_tool_executions
                              WHERE run_id = NEW.run_id AND state = 'completed'),
                'failed', (SELECT COUNT(*) FROM agentpg_tool_executions
                           WHERE run_id = NEW.run_id AND state = 'failed')
            );
            PERFORM pg_notify('agentpg_run_tools_complete', payload::text);
        END IF;
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_agentpg_tools_complete_notify
    AFTER UPDATE ON agentpg_tool_executions
    FOR EACH ROW
    EXECUTE FUNCTION agentpg_tools_complete_notify();

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
-- TRIGGER: Orphan Run Cleanup on Instance Delete
-- =============================================================================
-- When an instance is deleted, mark its workable runs as failed.

CREATE OR REPLACE FUNCTION agentpg_cleanup_orphan_runs()
RETURNS TRIGGER AS $$
BEGIN
    UPDATE agentpg_runs
    SET state = 'failed',
        finalized_at = NOW(),
        error_message = 'Instance disconnected',
        error_type = 'orphan'
    WHERE worker_instance_id = OLD.id
      AND state IN ('pending', 'pending_api', 'pending_tools', 'awaiting_continuation');
    RETURN OLD;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_agentpg_cleanup_orphan_runs
    BEFORE DELETE ON agentpg_instances
    FOR EACH ROW
    EXECUTE FUNCTION agentpg_cleanup_orphan_runs();

-- =============================================================================
-- TRIGGER: Orphan Tool Execution Cleanup on Instance Delete
-- =============================================================================
-- When an instance is deleted, mark its running tool executions as failed.

CREATE OR REPLACE FUNCTION agentpg_cleanup_orphan_tool_executions()
RETURNS TRIGGER AS $$
BEGIN
    UPDATE agentpg_tool_executions
    SET state = 'failed',
        completed_at = NOW(),
        error_message = 'Instance disconnected'
    WHERE instance_id = OLD.id
      AND state = 'running';
    RETURN OLD;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_agentpg_cleanup_orphan_tool_executions
    BEFORE DELETE ON agentpg_instances
    FOR EACH ROW
    EXECUTE FUNCTION agentpg_cleanup_orphan_tool_executions();
