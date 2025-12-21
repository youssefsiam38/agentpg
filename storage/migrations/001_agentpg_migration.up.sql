-- =============================================================================
-- AGENTPG SCHEMA v2.1
-- =============================================================================
-- Event-driven distributed AI agent framework with multi-level nested agents.
-- Supports both Claude Batch API (async) and Streaming API (real-time).
--
-- ARCHITECTURE OVERVIEW:
-- 1. Sessions: Conversation contexts (supports hierarchical nested sessions)
-- 2. Runs: Individual agent invocations with parent-child relationships
-- 3. Iterations: Each API call within a run (batch or streaming)
-- 4. Messages: Conversation history within sessions
-- 5. Content Blocks: Normalized message content (text, tool_use, tool_result, etc.)
-- 6. Tool Executions: Pending tool work (includes agent-as-tool calls)
-- 7. Instances: Distributed worker registration and capability tracking
-- 8. Agents/Tools: Schema definitions for agents and tools
--
-- KEY DESIGN PRINCIPLES:
-- - All state changes through PostgreSQL (single source of truth)
-- - LISTEN/NOTIFY for real-time events (with polling fallback)
-- - SELECT FOR UPDATE SKIP LOCKED for race-safe claiming
-- - Parent-child relationships enable multi-level agent hierarchies
-- - UNLOGGED tables for ephemeral instance data (performance)
-- - Dual API support: Claude Batch API (24h window) and Streaming API (real-time)
-- - Iteration tracking for both batch and streaming modes
-- =============================================================================

-- =============================================================================
-- ENUM TYPES
-- =============================================================================

-- -----------------------------------------------------------------------------
-- Run Mode (Batch vs Streaming API)
-- -----------------------------------------------------------------------------
-- Determines which Claude API is used for processing.
-- - batch: Uses Claude Batch API (24h processing window, cost-effective)
-- - streaming: Uses Claude Streaming API (real-time, lower latency)
-- -----------------------------------------------------------------------------
CREATE TYPE agentpg_run_mode AS ENUM(
    'batch',                -- Uses Claude Batch API (async, 24h window)
    'streaming'             -- Uses Claude Streaming API (real-time)
);

-- -----------------------------------------------------------------------------
-- Run State Machine
-- -----------------------------------------------------------------------------
-- Represents the lifecycle of a single agent run.
-- Supports both Batch API and Streaming API modes.
--
-- BATCH MODE STATE TRANSITIONS:
--   pending ──────────────────┐
--       │ (worker claims)     │
--       v                     │
--   batch_submitting ─────────┤
--       │ (batch created)     │
--       v                     │
--   batch_pending ────────────┤
--       │ (polling)           │
--       v                     │
--   batch_processing ─────────┤
--       │ (batch complete)    │
--       ├──> pending_tools    │ (has tool_use blocks)
--       ├──> completed        │ (stop_reason=end_turn)
--       ├──> awaiting_input   │ (stop_reason=max_tokens, needs continuation)
--       └──> failed           │ (error)
--
-- STREAMING MODE STATE TRANSITIONS:
--   pending ──────────────────┐
--       │ (worker claims)     │
--       v                     │
--   streaming ────────────────┤
--       │ (stream complete)   │
--       ├──> pending_tools    │ (has tool_use blocks)
--       ├──> completed        │ (stop_reason=end_turn)
--       ├──> awaiting_input   │ (stop_reason=max_tokens)
--       └──> failed           │ (error)
--
--   pending_tools ────────────┤
--       │ (all tools done)    │
--       └──> pending          │ (continue with tool_results)
--
-- TERMINAL STATES: completed, cancelled, failed
-- -----------------------------------------------------------------------------
CREATE TYPE agentpg_run_state AS ENUM(
    'pending',              -- Waiting for worker to claim
    'batch_submitting',     -- [Batch] Worker is preparing and submitting batch request
    'batch_pending',        -- [Batch] Batch submitted, waiting for Claude to start processing
    'batch_processing',     -- [Batch] Claude is processing the batch
    'streaming',            -- [Streaming] Worker is processing via streaming API
    'pending_tools',        -- Response has tool_use, waiting for tool executions
    'awaiting_input',       -- Paused (max_tokens reached, needs user/system continuation)
    'completed',            -- Terminal: successful completion (stop_reason=end_turn)
    'cancelled',            -- Terminal: user/system cancelled
    'failed'                -- Terminal: unrecoverable error
);

-- -----------------------------------------------------------------------------
-- Batch Status (mirrors Claude API)
-- -----------------------------------------------------------------------------
-- Tracks the processing status of a Claude Batch API request.
-- Used in the iterations table to track each API call.
-- -----------------------------------------------------------------------------
CREATE TYPE agentpg_batch_status AS ENUM(
    'in_progress',          -- Batch is being processed by Claude
    'canceling',            -- Cancellation requested
    'ended'                 -- Batch processing complete (check result_type for success/error)
);

-- -----------------------------------------------------------------------------
-- Tool Execution State Machine
-- -----------------------------------------------------------------------------
-- Represents lifecycle of a single tool execution.
--
-- STATE TRANSITIONS:
--   pending ──────────────────┐
--       │ (worker claims)     │
--       v                     │
--   running ──────────────────┤
--       ├──> completed        │ (success)
--       ├──> failed           │ (error, may retry)
--       └──> skipped          │ (run cancelled)
--
-- For AGENT TOOLS (is_agent_tool=true):
--   - Creates a child run (child_run_id)
--   - Execution completes when child run completes
--   - Result is the child run's final response
-- -----------------------------------------------------------------------------
CREATE TYPE agentpg_tool_execution_state AS ENUM(
    'pending',              -- Waiting for worker to claim
    'running',              -- Tool is executing (or child agent run in progress)
    'completed',            -- Success (result populated)
    'failed',               -- Error (may retry if attempt_count < max_attempts)
    'skipped'               -- Skipped (parent run was cancelled)
);

-- -----------------------------------------------------------------------------
-- Content Block Types (aligned with Claude API)
-- -----------------------------------------------------------------------------
-- All content types supported by the Claude API.
-- Used in agentpg_content_blocks to identify block type.
-- -----------------------------------------------------------------------------
CREATE TYPE agentpg_content_type AS ENUM(
    'text',                 -- Text content
    'tool_use',             -- Tool invocation from Claude
    'tool_result',          -- Result of tool execution
    'image',                -- Image content (base64 or URL)
    'document',             -- Document content (PDF, etc.)
    'thinking',             -- Extended thinking content
    'server_tool_use',      -- Server-side tool use
    'web_search_result'     -- Web search results
);

-- -----------------------------------------------------------------------------
-- Message Role
-- -----------------------------------------------------------------------------
-- The role of a message in the conversation.
-- -----------------------------------------------------------------------------
CREATE TYPE agentpg_message_role AS ENUM(
    'user',                 -- User input
    'assistant',            -- Claude response
    'system'                -- System messages (compaction summaries, etc.)
);

-- =============================================================================
-- SESSIONS TABLE
-- =============================================================================
-- Conversation context with multi-tenant isolation and hierarchical support.
--
-- ARCHITECTURE ROLE:
-- - Each session is a conversation thread
-- - Sessions can be nested (parent_session_id) for agent-as-tool scenarios
-- - Multi-tenant via tenant_id (never query across tenants)
--
-- HIERARCHICAL SESSIONS:
-- When Agent A calls Agent B as a tool:
-- - Agent B gets its own session (child of A's session)
-- - This maintains conversation isolation
-- - parent_session_id links them for context/debugging
--
-- COMPACTION:
-- - compaction_count tracks how many times context was compacted
-- - Messages marked is_summary=true are compaction results
-- =============================================================================
CREATE TABLE agentpg_sessions (
    -- Primary key
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- Multi-tenant isolation
    -- All queries MUST filter by tenant_id
    tenant_id TEXT NOT NULL,

    -- User-provided identifier (unique within tenant)
    -- Used to look up sessions by a human-readable name
    identifier TEXT NOT NULL,

    -- Hierarchical session support for nested agents
    -- NULL for root sessions, set when agent-as-tool creates child session
    parent_session_id UUID REFERENCES agentpg_sessions(id) ON DELETE CASCADE,

    -- Depth in session hierarchy (0 = root, 1 = first child, etc.)
    -- Used for query optimization and preventing infinite nesting
    depth INTEGER NOT NULL DEFAULT 0,

    -- Arbitrary metadata (JSON)
    -- Stores user-defined data like description, tags, etc.
    metadata JSONB NOT NULL DEFAULT '{}',

    -- Context compaction tracking
    -- Incremented each time the session's context is compacted
    compaction_count INTEGER NOT NULL DEFAULT 0,

    -- Timestamps
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

COMMENT ON TABLE agentpg_sessions IS
'Conversation sessions with multi-tenant isolation and hierarchical support for nested agents.';

COMMENT ON COLUMN agentpg_sessions.tenant_id IS
'Tenant identifier for multi-tenant isolation. All queries must filter by this.';

COMMENT ON COLUMN agentpg_sessions.parent_session_id IS
'Links child sessions to parent when agent-as-tool creates nested conversation context.';

COMMENT ON COLUMN agentpg_sessions.depth IS
'Hierarchy depth (0=root). Used for optimization and preventing infinite nesting.';

-- Indexes
CREATE INDEX idx_sessions_tenant_identifier ON agentpg_sessions(tenant_id, identifier);
CREATE INDEX idx_sessions_tenant_updated ON agentpg_sessions(tenant_id, updated_at DESC);
CREATE INDEX idx_sessions_parent ON agentpg_sessions(parent_session_id) WHERE parent_session_id IS NOT NULL;

-- =============================================================================
-- AGENTS TABLE
-- =============================================================================
-- Agent definitions (schema registry).
--
-- ARCHITECTURE ROLE:
-- - Defines agent capabilities (model, system prompt, tools, config)
-- - Referenced by runs to know how to process them
-- - Registered per-instance via agentpg_instance_agents
--
-- AGENT-AS-TOOL:
-- - When agent A uses agent B as tool, a tool entry is created in agentpg_tools
-- - The tool's agent_name references this table
-- =============================================================================
CREATE TABLE agentpg_agents (
    -- Agent name (primary key, must be unique)
    name TEXT PRIMARY KEY,

    -- Human-readable description
    description TEXT,

    -- Claude model to use (e.g., "claude-sonnet-4-5-20250929")
    model TEXT NOT NULL,

    -- System prompt for this agent
    system_prompt TEXT,

    -- Model parameters
    max_tokens INTEGER,
    temperature REAL,
    top_k INTEGER,
    top_p REAL,

    -- Tool names this agent can use (references agentpg_tools.name)
    -- Includes both regular tools and agent-as-tool names
    tool_names TEXT[] NOT NULL DEFAULT '{}',

    -- Additional configuration (JSON)
    -- Examples: auto_compaction, compaction_trigger, extended_context
    config JSONB NOT NULL DEFAULT '{}',

    -- Timestamps
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT agent_name_valid CHECK (char_length(name) > 0 AND char_length(name) < 256)
);

COMMENT ON TABLE agentpg_agents IS
'Agent definitions including model config, system prompt, and available tools.';

COMMENT ON COLUMN agentpg_agents.tool_names IS
'Array of tool names this agent can invoke. Includes both regular tools and agent-as-tool names.';

COMMENT ON COLUMN agentpg_agents.config IS
'Additional configuration like auto_compaction, compaction_trigger, extended_context, etc.';

-- =============================================================================
-- TOOLS TABLE
-- =============================================================================
-- Tool definitions (schema registry).
--
-- ARCHITECTURE ROLE:
-- - Defines tool interface (name, description, input_schema)
-- - is_agent_tool=true means this tool invokes another agent
-- - agent_name is set for agent-as-tool entries
--
-- AGENT-AS-TOOL PATTERN:
-- When AgentB.AsToolFor(AgentA) is called:
-- 1. A tool entry is created with is_agent_tool=true, agent_name='AgentB'
-- 2. AgentA's tool_names includes this tool
-- 3. When AgentA calls this tool, system creates a child run for AgentB
-- =============================================================================
CREATE TABLE agentpg_tools (
    -- Tool name (primary key, must be unique)
    name TEXT PRIMARY KEY,

    -- Human-readable description (shown to Claude)
    description TEXT NOT NULL,

    -- JSON Schema for tool input
    input_schema JSONB NOT NULL,

    -- Agent-as-tool support
    -- When true, calling this tool creates a child run for the specified agent
    is_agent_tool BOOLEAN NOT NULL DEFAULT FALSE,

    -- For agent-as-tool: the name of the agent to invoke
    agent_name TEXT REFERENCES agentpg_agents(name) ON DELETE CASCADE,

    -- Additional metadata
    metadata JSONB NOT NULL DEFAULT '{}',

    -- Timestamps
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT tool_name_valid CHECK (char_length(name) > 0 AND char_length(name) < 128),
    CONSTRAINT agent_tool_consistency CHECK (
        (is_agent_tool = TRUE AND agent_name IS NOT NULL) OR
        (is_agent_tool = FALSE AND agent_name IS NULL)
    )
);

COMMENT ON TABLE agentpg_tools IS
'Tool definitions including input schema. Agent-as-tool entries have is_agent_tool=true.';

COMMENT ON COLUMN agentpg_tools.is_agent_tool IS
'TRUE if this tool invokes another agent. When called, creates a child run.';

COMMENT ON COLUMN agentpg_tools.agent_name IS
'For agent-as-tool entries, the name of the agent to invoke.';

-- Index for agent-as-tool lookups
CREATE INDEX idx_tools_agent ON agentpg_tools(agent_name) WHERE is_agent_tool = TRUE;

-- =============================================================================
-- RUNS TABLE
-- =============================================================================
-- Central table for agent run execution with hierarchical support.
--
-- ARCHITECTURE ROLE:
-- - Each row is a single Run()/RunFast() invocation
-- - Implements state machine for async processing (Batch or Streaming API)
-- - Supports parent-child relationships for nested agent calls
--
-- RUN MODES:
-- - batch: Uses Claude Batch API (24h processing window, cost-effective)
-- - streaming: Uses Claude Streaming API (real-time, low latency)
--
-- NESTED AGENT RUNS:
-- When Agent A calls Agent B as a tool:
-- 1. A tool_use block appears in A's response
-- 2. Tool execution is created for the agent-as-tool
-- 3. Child run is created with parent_run_id = A's run
-- 4. Child run executes independently (may be on different worker)
-- 5. When child completes, tool execution gets result (via trigger)
-- 6. Parent run continues with tool_result
--
-- MULTI-ITERATION SUPPORT:
-- A single run can have multiple iterations (each = one API call):
-- prompt → api1 → tools → api2 → tools → api3 → end_turn
-- Iterations are tracked in agentpg_iterations table.
-- =============================================================================
CREATE TABLE agentpg_runs (
    -- Primary key
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- Session this run belongs to
    session_id UUID NOT NULL REFERENCES agentpg_sessions(id) ON DELETE CASCADE,

    -- Agent executing this run
    agent_name TEXT NOT NULL REFERENCES agentpg_agents(name),

    -- ==========================================================================
    -- RUN MODE (Batch vs Streaming API)
    -- ==========================================================================

    -- Which Claude API to use for this run
    -- 'batch': Claude Batch API (24h processing, cost-effective)
    -- 'streaming': Claude Streaming API (real-time, low latency)
    run_mode agentpg_run_mode NOT NULL DEFAULT 'batch',

    -- ==========================================================================
    -- HIERARCHICAL RUN SUPPORT
    -- ==========================================================================

    -- Parent run (NULL for root runs, set for agent-as-tool invocations)
    parent_run_id UUID REFERENCES agentpg_runs(id) ON DELETE CASCADE,

    -- Tool execution that spawned this run (for agent-as-tool)
    -- Set when this run was created to execute an agent-as-tool
    parent_tool_execution_id UUID, -- FK added after tool_executions table

    -- Depth in run hierarchy (0 = root, 1 = first child, etc.)
    -- PM → Lead → Worker would be depths 0 → 1 → 2
    depth INTEGER NOT NULL DEFAULT 0,

    -- ==========================================================================
    -- STATE MACHINE
    -- ==========================================================================

    -- Current state in the run lifecycle
    state agentpg_run_state NOT NULL DEFAULT 'pending',

    -- Previous state (for debugging/auditing)
    previous_state agentpg_run_state,

    -- ==========================================================================
    -- REQUEST
    -- ==========================================================================

    -- User prompt that initiated this run
    prompt TEXT NOT NULL,

    -- ==========================================================================
    -- CURRENT ITERATION TRACKING
    -- ==========================================================================
    -- The current/latest iteration for this run.
    -- Detailed tracking is in agentpg_iterations table (supports both batch and streaming).
    -- A run can have many iterations: prompt→api1→tools→api2→tools→api3→end_turn

    -- Current iteration number (updated as run progresses)
    current_iteration INTEGER NOT NULL DEFAULT 0,

    -- Reference to current iteration record
    current_iteration_id UUID, -- FK added after iterations table created

    -- ==========================================================================
    -- FINAL RESPONSE (populated when run completes)
    -- ==========================================================================

    -- Final text response (from the last iteration)
    response_text TEXT,

    -- Final stop reason (from the last iteration)
    stop_reason TEXT,

    -- ==========================================================================
    -- TOKEN USAGE (cumulative across all iterations)
    -- ==========================================================================

    input_tokens INTEGER NOT NULL DEFAULT 0,
    output_tokens INTEGER NOT NULL DEFAULT 0,
    cache_creation_input_tokens INTEGER NOT NULL DEFAULT 0,
    cache_read_input_tokens INTEGER NOT NULL DEFAULT 0,

    -- ==========================================================================
    -- ITERATION TRACKING
    -- ==========================================================================

    -- Total iterations (each batch request is one iteration)
    iteration_count INTEGER NOT NULL DEFAULT 0,

    -- Iterations that involved tool use
    tool_iterations INTEGER NOT NULL DEFAULT 0,

    -- ==========================================================================
    -- ERROR TRACKING
    -- ==========================================================================

    -- Error details when state = 'failed'
    error_message TEXT,
    error_type TEXT,                        -- e.g., 'batch_error', 'tool_error', 'timeout'

    -- ==========================================================================
    -- WORKER/CLAIMING
    -- ==========================================================================

    -- Instance that created this run (for debugging)
    created_by_instance_id TEXT,

    -- Instance currently processing this run
    -- Used for:
    -- 1. Routing work back to the same instance during multi-iteration
    -- 2. Detecting stuck runs when instance dies
    claimed_by_instance_id TEXT,

    -- When run was claimed (for stuck run detection)
    claimed_at TIMESTAMPTZ,

    -- ==========================================================================
    -- METADATA & TIMESTAMPS
    -- ==========================================================================

    -- User-provided metadata
    metadata JSONB NOT NULL DEFAULT '{}',

    -- Timestamps
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at TIMESTAMPTZ,                 -- When first claimed by a worker
    finalized_at TIMESTAMPTZ,               -- When reached terminal state

    -- ==========================================================================
    -- RESCUE TRACKING
    -- ==========================================================================

    -- Number of times this run has been rescued from a stuck state
    rescue_attempts INTEGER NOT NULL DEFAULT 0,

    -- Timestamp of the last rescue attempt
    last_rescue_at TIMESTAMPTZ,

    -- ==========================================================================
    -- CONSTRAINTS
    -- ==========================================================================

    -- Terminal states must have finalized_at set
    CONSTRAINT run_finalized_consistency CHECK (
        (state IN ('completed', 'cancelled', 'failed') AND finalized_at IS NOT NULL)
        OR (state NOT IN ('completed', 'cancelled', 'failed') AND finalized_at IS NULL)
    ),

    -- Depth must be consistent with parent_run_id
    CONSTRAINT run_depth_consistency CHECK (
        (parent_run_id IS NULL AND depth = 0)
        OR (parent_run_id IS NOT NULL AND depth > 0)
    )
);

COMMENT ON TABLE agentpg_runs IS
'Agent run executions with hierarchical support for nested agent-as-tool calls. Supports both Batch and Streaming API modes.';

COMMENT ON COLUMN agentpg_runs.run_mode IS
'Which Claude API to use: batch (24h async, cost-effective) or streaming (real-time, low latency).';

COMMENT ON COLUMN agentpg_runs.parent_run_id IS
'For agent-as-tool invocations, links to the parent run that called this agent.';

COMMENT ON COLUMN agentpg_runs.parent_tool_execution_id IS
'For agent-as-tool invocations, links to the tool execution that spawned this run.';

COMMENT ON COLUMN agentpg_runs.depth IS
'Hierarchy depth (0=root). PM→Lead→Worker would be depths 0→1→2.';

COMMENT ON COLUMN agentpg_runs.current_iteration IS
'Current iteration number. Incremented each time a new API call (batch or streaming) is made.';

COMMENT ON COLUMN agentpg_runs.claimed_by_instance_id IS
'Instance processing this run. Used for work routing and stuck run detection.';

-- Indexes for worker queries
-- General pending runs index (both batch and streaming)
CREATE INDEX idx_runs_pending_claim ON agentpg_runs(state, created_at)
    WHERE state = 'pending' AND claimed_by_instance_id IS NULL;

-- Separate indexes for batch vs streaming pending runs (for mode-specific claiming)
CREATE INDEX idx_runs_pending_batch ON agentpg_runs(state, run_mode, created_at)
    WHERE state = 'pending' AND run_mode = 'batch' AND claimed_by_instance_id IS NULL;

CREATE INDEX idx_runs_pending_streaming ON agentpg_runs(state, run_mode, created_at)
    WHERE state = 'pending' AND run_mode = 'streaming' AND claimed_by_instance_id IS NULL;

CREATE INDEX idx_runs_pending_tools ON agentpg_runs(state)
    WHERE state = 'pending_tools';

-- Active runs index (includes both batch states and streaming state)
CREATE INDEX idx_runs_active ON agentpg_runs(state)
    WHERE state IN ('batch_submitting', 'batch_pending', 'batch_processing', 'streaming');

CREATE INDEX idx_runs_session ON agentpg_runs(session_id, created_at DESC);

CREATE INDEX idx_runs_parent ON agentpg_runs(parent_run_id)
    WHERE parent_run_id IS NOT NULL;

CREATE INDEX idx_runs_claimed_instance ON agentpg_runs(claimed_by_instance_id)
    WHERE claimed_by_instance_id IS NOT NULL;

-- Index for stuck runs rescue (efficient rescue queries)
CREATE INDEX idx_runs_stuck_rescue ON agentpg_runs(claimed_at, rescue_attempts)
    WHERE state IN ('batch_submitting', 'batch_pending', 'batch_processing', 'streaming', 'pending_tools');

-- =============================================================================
-- MESSAGES TABLE
-- =============================================================================
-- Conversation messages within sessions.
--
-- ARCHITECTURE ROLE:
-- - Stores the conversation history
-- - Messages belong to sessions and optionally to specific runs
-- - Content is normalized into agentpg_content_blocks
--
-- COMPACTION:
-- - is_preserved: Never remove during compaction (e.g., important context)
-- - is_summary: This message is a compaction summary replacing removed messages
-- =============================================================================
CREATE TABLE agentpg_messages (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- Session this message belongs to
    session_id UUID NOT NULL REFERENCES agentpg_sessions(id) ON DELETE CASCADE,

    -- Run that created this message (NULL for user messages before any run)
    run_id UUID REFERENCES agentpg_runs(id) ON DELETE SET NULL,

    -- Message role
    role agentpg_message_role NOT NULL,

    -- Token usage for this message (from Claude response)
    -- Structure: {input_tokens, output_tokens, cache_creation_input_tokens, cache_read_input_tokens}
    usage JSONB NOT NULL DEFAULT '{}',

    -- Compaction flags
    is_preserved BOOLEAN NOT NULL DEFAULT FALSE,
    is_summary BOOLEAN NOT NULL DEFAULT FALSE,

    -- User-provided metadata
    metadata JSONB NOT NULL DEFAULT '{}',

    -- Timestamps
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

COMMENT ON TABLE agentpg_messages IS
'Conversation messages. Content stored in agentpg_content_blocks.';

COMMENT ON COLUMN agentpg_messages.is_preserved IS
'If TRUE, this message is never removed during compaction.';

COMMENT ON COLUMN agentpg_messages.is_summary IS
'If TRUE, this message is a compaction summary replacing removed messages.';

-- Indexes
CREATE INDEX idx_messages_session ON agentpg_messages(session_id, created_at);
CREATE INDEX idx_messages_run ON agentpg_messages(run_id) WHERE run_id IS NOT NULL;
CREATE INDEX idx_messages_preserved ON agentpg_messages(session_id) WHERE is_preserved = TRUE;

-- =============================================================================
-- CONTENT BLOCKS TABLE
-- =============================================================================
-- Normalized content blocks within messages.
--
-- ARCHITECTURE ROLE:
-- - Each message can have multiple content blocks
-- - Supports all Claude content types (text, tool_use, tool_result, etc.)
-- - tool_use blocks link to tool_executions via tool_use_id
-- - tool_result blocks reference their corresponding tool_use
-- =============================================================================
CREATE TABLE agentpg_content_blocks (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- Parent message
    message_id UUID NOT NULL REFERENCES agentpg_messages(id) ON DELETE CASCADE,

    -- Order within message (0-indexed)
    block_index INTEGER NOT NULL,

    -- Content type
    type agentpg_content_type NOT NULL,

    -- ==========================================================================
    -- TYPE-SPECIFIC FIELDS
    -- ==========================================================================

    -- Text content (for text, thinking types)
    text TEXT,

    -- Tool use fields (for tool_use, server_tool_use types)
    tool_use_id TEXT,                       -- Claude's tool_use_id (toolu_...)
    tool_name TEXT,
    tool_input JSONB,

    -- Tool result fields (for tool_result type)
    tool_result_for_use_id TEXT,            -- References the tool_use_id this result is for
    tool_content TEXT,
    is_error BOOLEAN NOT NULL DEFAULT FALSE,

    -- Media source (for image, document types)
    -- Structure: {type, media_type, data, url}
    source JSONB,

    -- Web search results (for web_search_result type)
    search_results JSONB,

    -- User-provided metadata
    metadata JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- Unique block index within message
    CONSTRAINT content_block_unique_index UNIQUE (message_id, block_index)
);

COMMENT ON TABLE agentpg_content_blocks IS
'Normalized content blocks within messages. Supports all Claude content types.';

COMMENT ON COLUMN agentpg_content_blocks.tool_use_id IS
'Claude-generated ID (toolu_...) for tool_use blocks. Used to match tool_results.';

COMMENT ON COLUMN agentpg_content_blocks.tool_result_for_use_id IS
'References the tool_use_id that this result corresponds to.';

-- Indexes
CREATE INDEX idx_content_blocks_message ON agentpg_content_blocks(message_id, block_index);
CREATE INDEX idx_content_blocks_tool_use ON agentpg_content_blocks(tool_use_id)
    WHERE tool_use_id IS NOT NULL;

-- =============================================================================
-- ITERATIONS TABLE
-- =============================================================================
-- Tracks each Claude API call (batch or streaming) within a run.
--
-- ARCHITECTURE ROLE:
-- - A single Run can have MULTIPLE iterations
-- - Each iteration = one API call + response (batch OR streaming)
-- - Enables tracking: prompt → api1 → tools → api2 → tools → api3 → end_turn
--
-- MULTI-ITERATION FLOW:
-- 1. Run created with user prompt
-- 2. Iteration 1: Call API with prompt, get response with tool_use
-- 3. Execute tools, store tool_results
-- 4. Iteration 2: Call API with tool_results, get response with more tool_use
-- 5. Execute tools, store tool_results
-- 6. Iteration 3: Call API with tool_results, get response with end_turn
-- 7. Run completed
--
-- BATCH MODE:
-- - batch_id, batch_request_id, batch_status, batch_* fields are populated
-- - Requires polling for status
--
-- STREAMING MODE:
-- - is_streaming = TRUE
-- - batch_* fields are NULL (not used)
-- - streaming_started_at, streaming_completed_at track timing
-- - Response is accumulated in real-time
--
-- Each iteration stores its own API tracking, request/response, and token usage.
-- =============================================================================
CREATE TABLE agentpg_iterations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- Parent run
    run_id UUID NOT NULL REFERENCES agentpg_runs(id) ON DELETE CASCADE,

    -- Iteration number within run (1-indexed)
    iteration_number INTEGER NOT NULL,

    -- ==========================================================================
    -- API MODE
    -- ==========================================================================

    -- TRUE if this iteration used streaming API instead of batch API
    is_streaming BOOLEAN NOT NULL DEFAULT FALSE,

    -- ==========================================================================
    -- BATCH API TRACKING (only populated when is_streaming = FALSE)
    -- ==========================================================================

    -- Claude Batch API identifiers for THIS iteration
    batch_id TEXT,                          -- Claude's batch ID (msgbatch_...)
    batch_request_id TEXT,                  -- Our correlation ID (custom_id in Batch API)
    batch_status agentpg_batch_status,      -- Current batch status

    -- Batch timing
    batch_submitted_at TIMESTAMPTZ,         -- When batch was submitted to Claude
    batch_completed_at TIMESTAMPTZ,         -- When batch completed
    batch_expires_at TIMESTAMPTZ,           -- 24h from submit (Claude's limit)

    -- Polling tracking
    batch_poll_count INTEGER NOT NULL DEFAULT 0,
    batch_last_poll_at TIMESTAMPTZ,

    -- ==========================================================================
    -- STREAMING API TRACKING (only populated when is_streaming = TRUE)
    -- ==========================================================================

    -- Streaming timing (batch_* fields are used for batch mode)
    streaming_started_at TIMESTAMPTZ,       -- When streaming API call started
    streaming_completed_at TIMESTAMPTZ,     -- When streaming finished

    -- ==========================================================================
    -- REQUEST CONTEXT
    -- ==========================================================================

    -- What triggered this iteration
    -- 'user_prompt' = first iteration
    -- 'tool_results' = after tools complete
    -- 'continuation' = after max_tokens
    trigger_type TEXT NOT NULL,

    -- Message IDs included in this batch request (for debugging/audit)
    request_message_ids JSONB,

    -- ==========================================================================
    -- RESPONSE
    -- ==========================================================================

    -- Stop reason from this iteration
    stop_reason TEXT,                       -- 'end_turn', 'tool_use', 'max_tokens', etc.

    -- Response message ID (the assistant message created from this batch)
    response_message_id UUID REFERENCES agentpg_messages(id) ON DELETE SET NULL,

    -- Did this iteration produce tool_use blocks?
    has_tool_use BOOLEAN NOT NULL DEFAULT FALSE,

    -- Number of tool executions created from this iteration
    tool_execution_count INTEGER NOT NULL DEFAULT 0,

    -- ==========================================================================
    -- TOKEN USAGE (for this iteration only)
    -- ==========================================================================

    input_tokens INTEGER NOT NULL DEFAULT 0,
    output_tokens INTEGER NOT NULL DEFAULT 0,
    cache_creation_input_tokens INTEGER NOT NULL DEFAULT 0,
    cache_read_input_tokens INTEGER NOT NULL DEFAULT 0,

    -- ==========================================================================
    -- ERROR TRACKING
    -- ==========================================================================

    error_message TEXT,
    error_type TEXT,

    -- ==========================================================================
    -- TIMESTAMPS
    -- ==========================================================================

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at TIMESTAMPTZ,                 -- When API call initiated (batch submit or stream start)
    completed_at TIMESTAMPTZ,               -- When response fully processed

    -- Unique iteration number per run
    CONSTRAINT unique_iteration_per_run UNIQUE (run_id, iteration_number)
);

COMMENT ON TABLE agentpg_iterations IS
'Tracks each Claude API call within a run (batch or streaming). A run can have many iterations (prompt→tools→prompt→tools→done).';

COMMENT ON COLUMN agentpg_iterations.iteration_number IS
'1-indexed iteration number. Iteration 1 is the initial prompt, subsequent iterations are tool result continuations.';

COMMENT ON COLUMN agentpg_iterations.is_streaming IS
'TRUE if this iteration used streaming API instead of batch API. Determines which tracking fields are populated.';

COMMENT ON COLUMN agentpg_iterations.trigger_type IS
'What caused this iteration: user_prompt (first), tool_results (after tools), continuation (after max_tokens).';

COMMENT ON COLUMN agentpg_iterations.has_tool_use IS
'TRUE if Claude returned tool_use blocks in this iteration. Determines if we need another iteration after tools complete.';

COMMENT ON COLUMN agentpg_iterations.batch_request_id IS
'[Batch only] Our correlation ID within the batch (custom_id in Batch API). Used to match results.';

-- Indexes
CREATE INDEX idx_iterations_run ON agentpg_iterations(run_id, iteration_number);
CREATE INDEX idx_iterations_batch ON agentpg_iterations(batch_id) WHERE batch_id IS NOT NULL;
CREATE INDEX idx_iterations_polling ON agentpg_iterations(batch_status, batch_last_poll_at NULLS FIRST)
    WHERE batch_status = 'in_progress';

-- Add FK from runs to iterations for current_iteration_id
ALTER TABLE agentpg_runs
    ADD CONSTRAINT fk_runs_current_iteration
    FOREIGN KEY (current_iteration_id)
    REFERENCES agentpg_iterations(id) ON DELETE SET NULL;

-- =============================================================================
-- TOOL EXECUTIONS TABLE
-- =============================================================================
-- Pending and completed tool executions.
--
-- ARCHITECTURE ROLE:
-- - Created when Claude returns tool_use blocks
-- - Workers claim and execute tools
-- - For agent-as-tool, creates child run and waits for completion
--
-- AGENT-AS-TOOL FLOW:
-- 1. Parent run gets tool_use for agent tool
-- 2. Tool execution created with is_agent_tool=true
-- 3. Worker claims, creates child run with parent_tool_execution_id set
-- 4. Tool execution state = 'running' until child run completes
-- 5. When child completes, trigger updates tool_output = child's response
-- =============================================================================
CREATE TABLE agentpg_tool_executions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- Parent run this execution belongs to
    run_id UUID NOT NULL REFERENCES agentpg_runs(id) ON DELETE CASCADE,

    -- Which iteration created this tool execution
    iteration_id UUID NOT NULL REFERENCES agentpg_iterations(id) ON DELETE CASCADE,

    -- State machine
    state agentpg_tool_execution_state NOT NULL DEFAULT 'pending',

    -- ==========================================================================
    -- TOOL IDENTIFICATION
    -- ==========================================================================

    -- Claude's tool_use_id (for matching with content blocks and tool_result)
    tool_use_id TEXT NOT NULL,

    -- Tool being executed
    tool_name TEXT NOT NULL,

    -- Input from Claude (parsed JSON)
    tool_input JSONB NOT NULL,

    -- ==========================================================================
    -- AGENT-AS-TOOL SUPPORT
    -- ==========================================================================

    -- Is this executing an agent-as-tool?
    is_agent_tool BOOLEAN NOT NULL DEFAULT FALSE,

    -- For agent-as-tool: the agent being invoked
    agent_name TEXT REFERENCES agentpg_agents(name),

    -- For agent-as-tool: the child run created to execute the agent
    child_run_id UUID REFERENCES agentpg_runs(id) ON DELETE SET NULL,

    -- ==========================================================================
    -- RESULT
    -- ==========================================================================

    -- Tool output (string result or agent response)
    tool_output TEXT,

    -- Error information
    is_error BOOLEAN NOT NULL DEFAULT FALSE,
    error_message TEXT,

    -- ==========================================================================
    -- WORKER/CLAIMING
    -- ==========================================================================

    -- Instance that claimed this execution
    claimed_by_instance_id TEXT,

    -- When execution was claimed
    claimed_at TIMESTAMPTZ,

    -- ==========================================================================
    -- RETRY LOGIC
    -- ==========================================================================

    -- Number of execution attempts
    attempt_count INTEGER NOT NULL DEFAULT 0,

    -- Maximum attempts before giving up (2 = 1 retry for snappy experience)
    max_attempts INTEGER NOT NULL DEFAULT 2,

    -- When this execution is scheduled to run (for retry delays and snoozing)
    scheduled_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- Number of times this execution has been snoozed (does not count as attempts)
    snooze_count INTEGER NOT NULL DEFAULT 0,

    -- The error message from the last failed attempt
    last_error TEXT,

    -- ==========================================================================
    -- TIMESTAMPS
    -- ==========================================================================

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at TIMESTAMPTZ,                 -- When execution started
    completed_at TIMESTAMPTZ,               -- When execution completed

    -- ==========================================================================
    -- CONSTRAINTS
    -- ==========================================================================

    -- Terminal states must have completed_at
    CONSTRAINT tool_exec_state_consistency CHECK (
        (state IN ('completed', 'failed', 'skipped') AND completed_at IS NOT NULL)
        OR (state IN ('pending', 'running') AND completed_at IS NULL)
    ),

    -- Agent tool must have agent_name, non-agent tool must not
    CONSTRAINT tool_exec_agent_consistency CHECK (
        (is_agent_tool = TRUE AND agent_name IS NOT NULL)
        OR (is_agent_tool = FALSE AND agent_name IS NULL)
    )
);

COMMENT ON TABLE agentpg_tool_executions IS
'Tool execution tracking. For agent-as-tool, creates child runs.';

COMMENT ON COLUMN agentpg_tool_executions.is_agent_tool IS
'TRUE if this executes another agent. Creates child_run_id when claimed.';

COMMENT ON COLUMN agentpg_tool_executions.child_run_id IS
'For agent-as-tool: the child run created to execute the agent.';

COMMENT ON COLUMN agentpg_tool_executions.iteration_id IS
'The iteration that created this tool execution. Used to group tools by batch response.';

-- Add FK from runs to tool_executions (deferred because of circular reference)
ALTER TABLE agentpg_runs
    ADD CONSTRAINT fk_runs_parent_tool_execution
    FOREIGN KEY (parent_tool_execution_id)
    REFERENCES agentpg_tool_executions(id) ON DELETE SET NULL;

-- Indexes for worker claiming with SKIP LOCKED
CREATE INDEX idx_tool_exec_pending ON agentpg_tool_executions(state, created_at)
    WHERE state = 'pending' AND claimed_by_instance_id IS NULL;

-- Index for scheduled tool executions (efficient polling for due retries)
CREATE INDEX idx_tool_exec_pending_scheduled ON agentpg_tool_executions(scheduled_at, created_at)
    WHERE state = 'pending' AND claimed_by_instance_id IS NULL;

CREATE INDEX idx_tool_exec_running ON agentpg_tool_executions(state, started_at)
    WHERE state = 'running';

CREATE INDEX idx_tool_exec_run ON agentpg_tool_executions(run_id);
CREATE INDEX idx_tool_exec_iteration ON agentpg_tool_executions(iteration_id);

CREATE INDEX idx_tool_exec_child_run ON agentpg_tool_executions(child_run_id)
    WHERE child_run_id IS NOT NULL;

-- =============================================================================
-- INSTANCES TABLE (UNLOGGED)
-- =============================================================================
-- Running service instances with capacity tracking.
--
-- ARCHITECTURE ROLE:
-- - Tracks which workers are alive (via heartbeat)
-- - Capacity management for distributed load balancing
-- - Capabilities tracked via agentpg_instance_agents and agentpg_instance_tools
--
-- UNLOGGED: Instance data is ephemeral, doesn't need WAL durability.
-- On crash, instances are re-registered on startup.
-- =============================================================================
CREATE UNLOGGED TABLE agentpg_instances (
    -- Instance ID (auto-generated UUID or user-provided)
    id TEXT PRIMARY KEY,

    -- Service name (for grouping/identification)
    -- Multiple instances can share the same name (e.g., same deployment)
    name TEXT NOT NULL,

    -- Host information
    hostname TEXT,
    pid INTEGER,
    version TEXT,

    -- ==========================================================================
    -- CAPACITY MANAGEMENT
    -- ==========================================================================

    -- Maximum concurrent runs this instance can process
    max_concurrent_runs INTEGER NOT NULL DEFAULT 10,

    -- Maximum concurrent tool executions
    max_concurrent_tools INTEGER NOT NULL DEFAULT 50,

    -- NOTE: Active run/tool counts are calculated on-the-fly by querying
    -- agentpg_runs and agentpg_tool_executions tables rather than stored here.
    -- This avoids consistency issues with triggers and ensures accurate counts.

    -- ==========================================================================
    -- METADATA & TIMESTAMPS
    -- ==========================================================================

    -- User-provided metadata (e.g., environment, region)
    metadata JSONB NOT NULL DEFAULT '{}',

    -- Timestamps
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_heartbeat_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- Constraints
    CONSTRAINT instance_id_valid CHECK (char_length(id) > 0 AND char_length(id) < 128),
    CONSTRAINT instance_capacity_valid CHECK (max_concurrent_runs > 0 AND max_concurrent_tools > 0)
);

COMMENT ON TABLE agentpg_instances IS
'Active worker instances with capacity tracking. UNLOGGED for performance.';

COMMENT ON COLUMN agentpg_instances.name IS
'Service name for grouping. Multiple instances can share the same name.';

COMMENT ON COLUMN agentpg_instances.max_concurrent_runs IS
'Maximum runs this instance will process concurrently.';

-- Index for heartbeat cleanup (finding stale instances)
CREATE INDEX idx_instances_heartbeat ON agentpg_instances(last_heartbeat_at);

-- =============================================================================
-- INSTANCE CAPABILITY TABLES (UNLOGGED)
-- =============================================================================
-- Track which agents/tools each instance can handle.
--
-- ARCHITECTURE ROLE:
-- - Enables "route to capable instance" pattern
-- - Instance only sees work it can handle via claiming functions
-- - Allows specialized workers (e.g., one instance for code tools)
-- =============================================================================

-- Instance-Agent capabilities
CREATE UNLOGGED TABLE agentpg_instance_agents (
    instance_id TEXT NOT NULL REFERENCES agentpg_instances(id) ON DELETE CASCADE,
    agent_name TEXT NOT NULL REFERENCES agentpg_agents(name) ON DELETE CASCADE,
    registered_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (instance_id, agent_name)
);

COMMENT ON TABLE agentpg_instance_agents IS
'Which agents each instance can process. Enables specialized workers.';

CREATE INDEX idx_instance_agents_by_agent ON agentpg_instance_agents(agent_name);

-- Instance-Tool capabilities
CREATE UNLOGGED TABLE agentpg_instance_tools (
    instance_id TEXT NOT NULL REFERENCES agentpg_instances(id) ON DELETE CASCADE,
    tool_name TEXT NOT NULL REFERENCES agentpg_tools(name) ON DELETE CASCADE,
    registered_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (instance_id, tool_name)
);

COMMENT ON TABLE agentpg_instance_tools IS
'Which tools each instance can execute. Enables specialized workers.';

CREATE INDEX idx_instance_tools_by_tool ON agentpg_instance_tools(tool_name);

-- =============================================================================
-- LEADER ELECTION TABLE (UNLOGGED)
-- =============================================================================
-- Single-row table for distributed leader election.
--
-- ARCHITECTURE ROLE:
-- - One instance is leader for maintenance tasks
-- - TTL-based lease (leader must refresh before expires_at)
-- - Leader handles: stale instance cleanup, stuck run recovery
--
-- ELECTION PROCESS:
-- 1. Non-leader attempts INSERT ON CONFLICT DO NOTHING
-- 2. If row exists and expires_at < NOW(), try UPDATE WHERE expires_at < NOW()
-- 3. Leader refreshes by UPDATE WHERE leader_id = self
-- =============================================================================
CREATE UNLOGGED TABLE agentpg_leader (
    -- Always 'default' (single row)
    name TEXT PRIMARY KEY DEFAULT 'default' CHECK (name = 'default'),

    -- Current leader instance
    leader_id TEXT NOT NULL,

    -- Election timing
    elected_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,

    CONSTRAINT leader_id_valid CHECK (char_length(leader_id) > 0 AND char_length(leader_id) < 128)
);

COMMENT ON TABLE agentpg_leader IS
'Single-row leader election table. Leader handles maintenance tasks.';

COMMENT ON COLUMN agentpg_leader.expires_at IS
'Leader must refresh before this time or lose leadership.';

-- =============================================================================
-- COMPACTION TABLES
-- =============================================================================

-- Compaction audit trail
CREATE TABLE agentpg_compaction_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id UUID NOT NULL REFERENCES agentpg_sessions(id) ON DELETE CASCADE,

    -- Compaction strategy used
    strategy TEXT NOT NULL,                 -- 'summarization', 'hybrid', etc.

    -- Token counts
    original_tokens INTEGER NOT NULL,       -- Before compaction
    compacted_tokens INTEGER NOT NULL,      -- After compaction

    -- What was removed
    messages_removed INTEGER NOT NULL,

    -- Summary content (if summarization strategy)
    summary_content TEXT,

    -- Preserved message IDs (JSON array)
    preserved_message_ids JSONB,

    -- Model used for summarization
    model_used TEXT,

    -- Performance tracking
    duration_ms BIGINT,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

COMMENT ON TABLE agentpg_compaction_events IS
'Audit trail for context compaction operations.';

CREATE INDEX idx_compaction_session ON agentpg_compaction_events(session_id, created_at DESC);

-- Message archive for reversibility
CREATE TABLE agentpg_message_archive (
    -- Original message ID
    id UUID PRIMARY KEY,

    -- Compaction event that archived this message
    compaction_event_id UUID REFERENCES agentpg_compaction_events(id) ON DELETE CASCADE,

    -- Session for reference
    session_id UUID NOT NULL REFERENCES agentpg_sessions(id) ON DELETE CASCADE,

    -- Full original message JSON
    original_message JSONB NOT NULL,

    archived_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

COMMENT ON TABLE agentpg_message_archive IS
'Archived messages from compaction. Enables undo/audit.';

CREATE INDEX idx_archive_compaction ON agentpg_message_archive(compaction_event_id);
CREATE INDEX idx_archive_session ON agentpg_message_archive(session_id, archived_at DESC);

-- =============================================================================
-- STORED PROCEDURES
-- =============================================================================

-- -----------------------------------------------------------------------------
-- Claim pending runs (race-safe with SKIP LOCKED)
-- -----------------------------------------------------------------------------
-- Returns runs that the instance can process (has agent capability).
-- Uses SELECT FOR UPDATE SKIP LOCKED for race safety across workers.
--
-- p_run_mode: Optional filter for run mode ('batch', 'streaming', or NULL for any)
--
-- The initial state transition depends on run mode:
-- - batch: pending -> batch_submitting
-- - streaming: pending -> streaming
--
-- USAGE:
--   SELECT * FROM agentpg_claim_runs('instance-123', 5);          -- claim any mode
--   SELECT * FROM agentpg_claim_runs('instance-123', 5, 'batch'); -- batch only
--   SELECT * FROM agentpg_claim_runs('instance-123', 5, 'streaming'); -- streaming only
-- -----------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION agentpg_claim_runs(
    p_instance_id TEXT,
    p_max_count INTEGER DEFAULT 1,
    p_run_mode agentpg_run_mode DEFAULT NULL
) RETURNS SETOF agentpg_runs AS $$
BEGIN
    RETURN QUERY
    WITH claimable AS (
        SELECT r.id
        FROM agentpg_runs r
        WHERE r.state = 'pending'
          AND r.claimed_by_instance_id IS NULL
          -- Filter by run mode if specified
          AND (p_run_mode IS NULL OR r.run_mode = p_run_mode)
          -- Only claim if instance has capability for this agent
          AND EXISTS (
              SELECT 1 FROM agentpg_instance_agents ia
              WHERE ia.instance_id = p_instance_id
                AND ia.agent_name = r.agent_name
          )
        ORDER BY r.created_at ASC
        LIMIT p_max_count
        FOR UPDATE OF r SKIP LOCKED
    ),
    claimed AS (
        UPDATE agentpg_runs r
        SET claimed_by_instance_id = p_instance_id,
            claimed_at = NOW(),
            -- Transition to appropriate state based on run mode
            state = CASE
                WHEN r.run_mode = 'batch' THEN 'batch_submitting'::agentpg_run_state
                WHEN r.run_mode = 'streaming' THEN 'streaming'::agentpg_run_state
            END,
            previous_state = 'pending',
            started_at = COALESCE(started_at, NOW())
        FROM claimable c
        WHERE r.id = c.id
        RETURNING r.*
    )
    SELECT * FROM claimed;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION agentpg_claim_runs IS
'Race-safe run claiming with optional run mode filter. Transitions to batch_submitting or streaming based on run mode.';

-- -----------------------------------------------------------------------------
-- Claim pending tool executions (race-safe with SKIP LOCKED)
-- -----------------------------------------------------------------------------
-- Returns tool executions that the instance can process.
-- For agent-tools, checks instance_agents; for regular tools, checks instance_tools.
--
-- USAGE:
--   SELECT * FROM agentpg_claim_tool_executions('instance-123', 10);
-- -----------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION agentpg_claim_tool_executions(
    p_instance_id TEXT,
    p_max_count INTEGER DEFAULT 10
) RETURNS SETOF agentpg_tool_executions AS $$
BEGIN
    RETURN QUERY
    WITH claimable AS (
        SELECT te.id
        FROM agentpg_tool_executions te
        WHERE te.state = 'pending'
          AND te.claimed_by_instance_id IS NULL
          -- Only claim if scheduled time has passed (for retry delays and snoozing)
          AND te.scheduled_at <= NOW()
          -- Only claim if instance has capability for this tool
          AND (
              -- Regular tools: check instance_tools
              (te.is_agent_tool = FALSE AND EXISTS (
                  SELECT 1 FROM agentpg_instance_tools it
                  WHERE it.instance_id = p_instance_id
                    AND it.tool_name = te.tool_name
              ))
              OR
              -- Agent tools: check instance_agents for the target agent
              (te.is_agent_tool = TRUE AND EXISTS (
                  SELECT 1 FROM agentpg_instance_agents ia
                  WHERE ia.instance_id = p_instance_id
                    AND ia.agent_name = te.agent_name
              ))
          )
        ORDER BY te.scheduled_at ASC, te.created_at ASC
        LIMIT p_max_count
        FOR UPDATE OF te SKIP LOCKED
    )
    UPDATE agentpg_tool_executions te
    SET claimed_by_instance_id = p_instance_id,
        claimed_at = NOW(),
        state = 'running',
        started_at = NOW(),
        attempt_count = attempt_count + 1
    FROM claimable c
    WHERE te.id = c.id
    RETURNING te.*;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION agentpg_claim_tool_executions IS
'Race-safe tool claiming. Routes agent-tools to capable instances. Respects scheduled_at for retry delays.';

-- -----------------------------------------------------------------------------
-- Get iterations needing batch polling
-- -----------------------------------------------------------------------------
-- Returns iterations owned by instance that need batch status polling.
--
-- USAGE:
--   SELECT * FROM agentpg_get_iterations_for_poll('instance-123', '30 seconds', 10);
-- -----------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION agentpg_get_iterations_for_poll(
    p_instance_id TEXT,
    p_poll_interval INTERVAL DEFAULT '30 seconds',
    p_max_count INTEGER DEFAULT 10
) RETURNS SETOF agentpg_iterations AS $$
BEGIN
    RETURN QUERY
    SELECT i.*
    FROM agentpg_iterations i
    JOIN agentpg_runs r ON r.id = i.run_id
    WHERE i.batch_status = 'in_progress'
      AND r.claimed_by_instance_id = p_instance_id
      AND (i.batch_last_poll_at IS NULL
           OR i.batch_last_poll_at < NOW() - p_poll_interval)
    ORDER BY i.batch_last_poll_at NULLS FIRST
    LIMIT p_max_count;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION agentpg_get_iterations_for_poll IS
'Returns iterations owned by instance that need batch status polling.';

-- -----------------------------------------------------------------------------
-- Get stuck runs for rescue
-- -----------------------------------------------------------------------------
-- Returns runs that are stuck in non-terminal states and eligible for rescue.
-- A run is considered stuck if:
-- - It is in a non-terminal state (batch_submitting, batch_pending, batch_processing, streaming, pending_tools)
-- - It was claimed more than p_timeout ago
-- - It has fewer than p_max_rescue_attempts rescue attempts
-- - It does NOT have any tool executions pending (e.g., scheduled for retry with backoff)
--
-- USAGE:
--   SELECT * FROM agentpg_get_stuck_runs('5 minutes', 3, 100);
-- -----------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION agentpg_get_stuck_runs(
    p_timeout INTERVAL DEFAULT '5 minutes',
    p_max_rescue_attempts INTEGER DEFAULT 3,
    p_limit INTEGER DEFAULT 100
) RETURNS SETOF agentpg_runs AS $$
BEGIN
    RETURN QUERY
    SELECT r.*
    FROM agentpg_runs r
    WHERE r.state IN ('batch_submitting', 'batch_pending', 'batch_processing', 'streaming', 'pending_tools')
      AND r.claimed_at IS NOT NULL
      AND r.claimed_at < NOW() - p_timeout
      AND r.rescue_attempts < p_max_rescue_attempts
      -- Don't rescue runs that have pending tool executions (e.g., scheduled for retry)
      AND NOT EXISTS (
          SELECT 1 FROM agentpg_tool_executions te
          WHERE te.run_id = r.id
            AND te.state IN ('pending', 'running')
      )
    ORDER BY r.claimed_at ASC
    LIMIT p_limit
    FOR UPDATE OF r SKIP LOCKED;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION agentpg_get_stuck_runs IS
'Returns runs that are stuck in non-terminal states and eligible for rescue. Excludes runs with pending/running tool executions.';

-- =============================================================================
-- NOTIFICATION TRIGGERS
-- =============================================================================

-- -----------------------------------------------------------------------------
-- Run created notification
-- -----------------------------------------------------------------------------
-- Notify workers when a new run is created in pending state.
-- Workers listen on 'agentpg_run_created' channel.
-- Includes run_mode so workers can filter by batch/streaming.
-- -----------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION agentpg_notify_run_created()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.state = 'pending' THEN
        PERFORM pg_notify('agentpg_run_created', json_build_object(
            'run_id', NEW.id,
            'session_id', NEW.session_id,
            'agent_name', NEW.agent_name,
            'run_mode', NEW.run_mode,
            'parent_run_id', NEW.parent_run_id,
            'depth', NEW.depth
        )::text);
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_run_created
    AFTER INSERT ON agentpg_runs
    FOR EACH ROW
    EXECUTE FUNCTION agentpg_notify_run_created();

-- -----------------------------------------------------------------------------
-- Run state change notification
-- -----------------------------------------------------------------------------
-- Notify on any run state change.
-- Workers listen on 'agentpg_run_state' and 'agentpg_run_finalized' channels.
-- -----------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION agentpg_notify_run_state_change()
RETURNS TRIGGER AS $$
BEGIN
    IF OLD.state IS DISTINCT FROM NEW.state THEN
        PERFORM pg_notify('agentpg_run_state', json_build_object(
            'run_id', NEW.id,
            'session_id', NEW.session_id,
            'agent_name', NEW.agent_name,
            'state', NEW.state,
            'previous_state', OLD.state,
            'parent_run_id', NEW.parent_run_id
        )::text);

        -- Additional notification for finalized runs
        IF NEW.state IN ('completed', 'cancelled', 'failed') THEN
            PERFORM pg_notify('agentpg_run_finalized', json_build_object(
                'run_id', NEW.id,
                'session_id', NEW.session_id,
                'state', NEW.state,
                'parent_run_id', NEW.parent_run_id,
                'parent_tool_execution_id', NEW.parent_tool_execution_id
            )::text);
        END IF;
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_run_state_change
    AFTER UPDATE ON agentpg_runs
    FOR EACH ROW
    EXECUTE FUNCTION agentpg_notify_run_state_change();

-- -----------------------------------------------------------------------------
-- Tool execution created notification
-- -----------------------------------------------------------------------------
-- Notify workers when a new tool execution is pending.
-- Workers listen on 'agentpg_tool_pending' channel.
-- -----------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION agentpg_notify_tool_created()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.state = 'pending' THEN
        PERFORM pg_notify('agentpg_tool_pending', json_build_object(
            'execution_id', NEW.id,
            'run_id', NEW.run_id,
            'tool_name', NEW.tool_name,
            'is_agent_tool', NEW.is_agent_tool,
            'agent_name', NEW.agent_name
        )::text);
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_tool_created
    AFTER INSERT ON agentpg_tool_executions
    FOR EACH ROW
    EXECUTE FUNCTION agentpg_notify_tool_created();

-- -----------------------------------------------------------------------------
-- All tools complete notification
-- -----------------------------------------------------------------------------
-- Notify when ALL tool executions for a run's current iteration are complete.
-- This triggers the next iteration (batch API call with tool_results).
-- Workers listen on 'agentpg_tools_complete' channel.
-- -----------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION agentpg_notify_tools_complete()
RETURNS TRIGGER AS $$
DECLARE
    v_pending_count INTEGER;
    v_run_id UUID;
BEGIN
    -- Only check on terminal state transitions
    IF NEW.state IN ('completed', 'failed', 'skipped')
       AND OLD.state NOT IN ('completed', 'failed', 'skipped') THEN

        v_run_id := NEW.run_id;

        -- Count remaining non-terminal executions for this run
        SELECT COUNT(*) INTO v_pending_count
        FROM agentpg_tool_executions
        WHERE run_id = v_run_id
          AND state NOT IN ('completed', 'failed', 'skipped');

        -- If all done, notify
        IF v_pending_count = 0 THEN
            PERFORM pg_notify('agentpg_tools_complete', json_build_object(
                'run_id', v_run_id
            )::text);
        END IF;
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_tools_complete
    AFTER UPDATE ON agentpg_tool_executions
    FOR EACH ROW
    EXECUTE FUNCTION agentpg_notify_tools_complete();

-- -----------------------------------------------------------------------------
-- Child run completed -> Update parent tool execution
-- -----------------------------------------------------------------------------
-- When a child run completes (agent-as-tool), automatically update
-- the parent's tool execution with the result.
--
-- This is crucial for multi-level nested agents:
-- - When Backend Developer completes, Lead's tool execution gets the result
-- - When Lead completes, PM's tool execution gets the result
-- -----------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION agentpg_handle_child_run_complete()
RETURNS TRIGGER AS $$
BEGIN
    -- When a child run completes, update its parent tool execution
    IF NEW.state IN ('completed', 'cancelled', 'failed')
       AND NEW.parent_tool_execution_id IS NOT NULL
       AND (OLD.state IS NULL OR OLD.state NOT IN ('completed', 'cancelled', 'failed')) THEN

        UPDATE agentpg_tool_executions
        SET state = CASE
                WHEN NEW.state = 'completed' THEN 'completed'::agentpg_tool_execution_state
                ELSE 'failed'::agentpg_tool_execution_state
            END,
            tool_output = NEW.response_text,
            is_error = (NEW.state != 'completed'),
            error_message = NEW.error_message,
            completed_at = NOW()
        WHERE id = NEW.parent_tool_execution_id
          AND state = 'running';
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_child_run_complete
    AFTER UPDATE ON agentpg_runs
    FOR EACH ROW
    EXECUTE FUNCTION agentpg_handle_child_run_complete();

-- =============================================================================
-- CLEANUP TRIGGERS
-- =============================================================================

-- -----------------------------------------------------------------------------
-- Orphan cleanup when instance deleted
-- -----------------------------------------------------------------------------
-- When an instance is deleted (disconnected), mark its work as failed.
-- This allows other instances to potentially retry the work.
-- -----------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION agentpg_cleanup_orphaned_work()
RETURNS TRIGGER AS $$
BEGIN
    -- Mark claimed runs as failed
    UPDATE agentpg_runs
    SET state = 'failed',
        previous_state = state,
        finalized_at = NOW(),
        error_message = 'Instance disconnected: ' || OLD.id,
        error_type = 'instance_disconnected'
    WHERE claimed_by_instance_id = OLD.id
      AND state NOT IN ('completed', 'cancelled', 'failed');

    -- Mark claimed tool executions as failed (may be retried)
    UPDATE agentpg_tool_executions
    SET state = 'failed',
        completed_at = NOW(),
        error_message = 'Instance disconnected: ' || OLD.id
    WHERE claimed_by_instance_id = OLD.id
      AND state = 'running';

    RETURN OLD;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_cleanup_orphaned_work
    BEFORE DELETE ON agentpg_instances
    FOR EACH ROW
    EXECUTE FUNCTION agentpg_cleanup_orphaned_work();

-- -----------------------------------------------------------------------------
-- Orphan agent cleanup
-- -----------------------------------------------------------------------------
-- When the last instance referencing an agent is removed, delete the orphan.
-- This keeps the agents table clean of unused definitions.
-- -----------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION agentpg_cleanup_orphaned_agents()
RETURNS TRIGGER AS $$
BEGIN
    DELETE FROM agentpg_agents
    WHERE name = OLD.agent_name
      AND NOT EXISTS (
          SELECT 1 FROM agentpg_instance_agents
          WHERE agent_name = OLD.agent_name
      )
      AND NOT EXISTS (
          SELECT 1 FROM agentpg_runs
          WHERE agent_name = OLD.agent_name
      );
    RETURN OLD;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_cleanup_orphaned_agents
    AFTER DELETE ON agentpg_instance_agents
    FOR EACH ROW
    EXECUTE FUNCTION agentpg_cleanup_orphaned_agents();

-- -----------------------------------------------------------------------------
-- Orphan tool cleanup
-- -----------------------------------------------------------------------------
-- When the last instance referencing a tool is removed, delete the orphan.
-- This keeps the tools table clean of unused definitions.
-- -----------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION agentpg_cleanup_orphaned_tools()
RETURNS TRIGGER AS $$
BEGIN
    DELETE FROM agentpg_tools
    WHERE name = OLD.tool_name
      AND NOT EXISTS (
          SELECT 1 FROM agentpg_instance_tools
          WHERE tool_name = OLD.tool_name
      )
      AND NOT EXISTS (
          SELECT 1 FROM agentpg_tool_executions
          WHERE tool_name = OLD.tool_name
      );
    RETURN OLD;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_cleanup_orphaned_tools
    AFTER DELETE ON agentpg_instance_tools
    FOR EACH ROW
    EXECUTE FUNCTION agentpg_cleanup_orphaned_tools();

-- -----------------------------------------------------------------------------
-- Validate agent is active before creating run
-- -----------------------------------------------------------------------------
-- Prevents creating runs for agents that have no registered instances.
-- This enforces atomicity at the database level, ensuring users cannot
-- create sessions/runs with agents that no active worker can process.
-- -----------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION agentpg_validate_run_agent()
RETURNS TRIGGER AS $$
BEGIN
    -- Check if agent is registered on at least one active instance
    IF NOT EXISTS (
        SELECT 1 FROM agentpg_instance_agents
        WHERE agent_name = NEW.agent_name
    ) THEN
        RAISE EXCEPTION 'Agent "%" is not active (no instances registered)', NEW.agent_name
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_validate_run_agent
    BEFORE INSERT ON agentpg_runs
    FOR EACH ROW
    EXECUTE FUNCTION agentpg_validate_run_agent();

COMMENT ON FUNCTION agentpg_validate_run_agent IS
'Prevents creating runs for inactive agents (no registered instances).';

-- =============================================================================
-- ATOMIC OPERATIONS
-- =============================================================================

-- -----------------------------------------------------------------------------
-- Create tool executions and update run state atomically
-- -----------------------------------------------------------------------------
-- This procedure ensures that tool execution creation and run state update
-- happen in a single transaction, preventing partial state on crash.
--
-- USAGE:
--   SELECT * FROM agentpg_create_tool_executions_and_update_run(
--       '[{"run_id": "...", "iteration_id": "...", "tool_use_id": "...", ...}]'::jsonb,
--       'run-uuid',
--       'pending_tools',
--       '{"tool_iterations": 1, "input_tokens": 100}'::jsonb
--   );
-- -----------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION agentpg_create_tool_executions_and_update_run(
    p_tool_params JSONB,
    p_run_id UUID,
    p_new_state agentpg_run_state,
    p_run_updates JSONB
) RETURNS SETOF agentpg_tool_executions AS $$
DECLARE
    v_param JSONB;
    v_exec agentpg_tool_executions;
    v_max_attempts INTEGER;
BEGIN
    -- Create all tool executions
    FOR v_param IN SELECT * FROM jsonb_array_elements(p_tool_params)
    LOOP
        v_max_attempts := COALESCE((v_param->>'max_attempts')::INTEGER, 2);

        INSERT INTO agentpg_tool_executions (
            run_id, iteration_id, tool_use_id, tool_name, tool_input,
            is_agent_tool, agent_name, max_attempts
        ) VALUES (
            (v_param->>'run_id')::UUID,
            (v_param->>'iteration_id')::UUID,
            v_param->>'tool_use_id',
            v_param->>'tool_name',
            v_param->'tool_input',
            COALESCE((v_param->>'is_agent_tool')::BOOLEAN, FALSE),
            v_param->>'agent_name',
            v_max_attempts
        )
        RETURNING * INTO v_exec;

        RETURN NEXT v_exec;
    END LOOP;

    -- Update run state atomically
    UPDATE agentpg_runs
    SET state = p_new_state,
        previous_state = state,
        tool_iterations = COALESCE((p_run_updates->>'tool_iterations')::INTEGER, tool_iterations),
        input_tokens = COALESCE((p_run_updates->>'input_tokens')::INTEGER, input_tokens),
        output_tokens = COALESCE((p_run_updates->>'output_tokens')::INTEGER, output_tokens),
        cache_creation_input_tokens = COALESCE((p_run_updates->>'cache_creation_input_tokens')::INTEGER, cache_creation_input_tokens),
        cache_read_input_tokens = COALESCE((p_run_updates->>'cache_read_input_tokens')::INTEGER, cache_read_input_tokens),
        iteration_count = COALESCE((p_run_updates->>'iteration_count')::INTEGER, iteration_count),
        response_text = CASE WHEN p_run_updates ? 'response_text' THEN p_run_updates->>'response_text' ELSE response_text END,
        stop_reason = CASE WHEN p_run_updates ? 'stop_reason' THEN p_run_updates->>'stop_reason' ELSE stop_reason END,
        finalized_at = CASE
            WHEN p_new_state IN ('completed', 'cancelled', 'failed') THEN
                COALESCE((p_run_updates->>'finalized_at')::TIMESTAMPTZ, NOW())
            ELSE finalized_at
        END
    WHERE id = p_run_id;

    RETURN;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION agentpg_create_tool_executions_and_update_run IS
'Atomically creates tool executions and updates run state. Prevents partial state on crash.';

-- -----------------------------------------------------------------------------
-- Complete tools and continue run atomically
-- -----------------------------------------------------------------------------
-- This procedure ensures that creating the tool_result message and updating
-- the run state happen in a single transaction.
--
-- RACE-SAFETY: Uses SELECT FOR UPDATE to lock the run and verifies the run is
-- in 'pending_tools' state before proceeding. If run is not in the expected
-- state (e.g., already processed by another instance), returns NULL.
--
-- USAGE:
--   SELECT * FROM agentpg_complete_tools_and_continue_run(
--       'session-uuid',
--       'run-uuid',
--       '[{"type": "tool_result", "tool_result_for_use_id": "...", "tool_content": "...", "is_error": false}]'::jsonb
--   );
-- -----------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION agentpg_complete_tools_and_continue_run(
    p_session_id UUID,
    p_run_id UUID,
    p_content_blocks JSONB
) RETURNS agentpg_messages AS $$
DECLARE
    v_run agentpg_runs;
    v_message agentpg_messages;
    v_block JSONB;
    v_block_index INTEGER := 0;
BEGIN
    -- Lock the run and verify it's in pending_tools state
    -- This prevents race conditions when multiple instances receive the notification
    SELECT * INTO v_run
    FROM agentpg_runs
    WHERE id = p_run_id
    FOR UPDATE;

    -- If run is not in pending_tools state, another instance already processed it
    IF v_run.state != 'pending_tools' THEN
        RETURN NULL;
    END IF;

    -- Create the tool results message
    INSERT INTO agentpg_messages (session_id, run_id, role)
    VALUES (p_session_id, p_run_id, 'user')
    RETURNING * INTO v_message;

    -- Create content blocks for each tool result
    FOR v_block IN SELECT * FROM jsonb_array_elements(p_content_blocks)
    LOOP
        INSERT INTO agentpg_content_blocks (
            message_id, block_index, type,
            tool_result_for_use_id, tool_content, is_error
        ) VALUES (
            v_message.id,
            v_block_index,
            (v_block->>'type')::agentpg_content_type,
            v_block->>'tool_result_for_use_id',
            v_block->>'tool_content',
            COALESCE((v_block->>'is_error')::BOOLEAN, FALSE)
        );
        v_block_index := v_block_index + 1;
    END LOOP;

    -- Update run state back to pending for next iteration
    UPDATE agentpg_runs
    SET state = 'pending'::agentpg_run_state,
        previous_state = state,
        claimed_by_instance_id = NULL,
        claimed_at = NULL
    WHERE id = p_run_id;

    RETURN v_message;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION agentpg_complete_tools_and_continue_run IS
'Atomically creates tool_result message and transitions run to pending for next iteration. Race-safe: returns NULL if run is not in pending_tools state.';
