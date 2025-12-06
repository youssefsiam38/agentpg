-- AgentPG Initial Schema
-- This migration creates all tables with the agentpg_ prefix to avoid collisions with client tables.

-- =============================================================================
-- SESSIONS TABLE
-- =============================================================================
-- Stores conversation sessions with multi-tenant isolation.
-- Each session belongs to a tenant and has a unique identifier within that tenant.
--
-- Columns:
--   id              - UUID primary key, auto-generated
--   tenant_id       - Tenant identifier for multi-tenancy isolation (e.g., "org-123", "team-abc")
--   identifier      - Custom session identifier within tenant (e.g., "user-456", "conversation-789")
--   metadata        - Application-specific data as JSONB (preferences, tags, UI state)
--   compaction_count - Number of compaction operations performed on this session
--   created_at      - Session creation timestamp
--   updated_at      - Last update timestamp
--
-- Usage patterns:
--   - Primary lookup: (tenant_id, identifier)
--   - List by tenant: tenant_id with updated_at ordering
--   - Find nested sessions: parent_session_id

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

-- Index for primary lookup pattern: find session by tenant and identifier
CREATE INDEX idx_agentpg_sessions_tenant_identifier ON agentpg_sessions(tenant_id, identifier);

-- Index for listing sessions by tenant ordered by recency
CREATE INDEX idx_agentpg_sessions_tenant_updated ON agentpg_sessions(tenant_id, updated_at DESC);


-- =============================================================================
-- MESSAGES TABLE
-- =============================================================================
-- Stores individual messages within conversation sessions.
-- Messages follow the Anthropic API format with role and content blocks.
--
-- Columns:
--   id           - UUID primary key, auto-generated
--   session_id   - Foreign key to parent session (CASCADE delete)
--   role         - Message role: 'user', 'assistant', or 'system'
--   content      - Array of content blocks as JSONB: [{"type": "text", "text": "..."}, ...]
--   usage        - Token usage breakdown: {"input_tokens": N, "output_tokens": N, ...}
--   metadata     - Application-specific metadata as JSONB
--   is_preserved - If true, message is protected from compaction (never removed)
--   is_summary   - If true, message is a synthetic summary (not original user/assistant)
--   created_at   - Message creation timestamp
--   updated_at   - Last update timestamp
--
-- Usage patterns:
--   - Get conversation history: session_id ordered by created_at
--   - Find preserved messages: session_id with is_preserved = true
--   - Find summary messages: session_id with is_summary = true

CREATE TABLE agentpg_messages (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id UUID NOT NULL REFERENCES agentpg_sessions(id) ON DELETE CASCADE,
    role TEXT NOT NULL CHECK (role IN ('user', 'assistant', 'system')),
    content JSONB NOT NULL,
    usage JSONB NOT NULL DEFAULT '{}',
    metadata JSONB NOT NULL DEFAULT '{}',
    is_preserved BOOLEAN NOT NULL DEFAULT FALSE,
    is_summary BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Index for retrieving conversation history in order
CREATE INDEX idx_agentpg_messages_session ON agentpg_messages(session_id, created_at);

-- Partial index for finding preserved messages (saves storage by only indexing preserved=true)
CREATE INDEX idx_agentpg_messages_preserved ON agentpg_messages(session_id, is_preserved) WHERE is_preserved = true;

-- Partial index for finding summary messages (saves storage by only indexing is_summary=true)
CREATE INDEX idx_agentpg_messages_summary ON agentpg_messages(session_id, is_summary) WHERE is_summary = true;


-- =============================================================================
-- COMPACTION EVENTS TABLE
-- =============================================================================
-- Audit trail for context compaction operations.
-- Stores metadata about each compaction for analytics and potential rollback.
--
-- Columns:
--   id                    - UUID primary key, auto-generated
--   session_id            - Foreign key to session being compacted (CASCADE delete)
--   strategy              - Strategy used: "summarization", "hybrid", "prune", "rolling_window", etc.
--   original_tokens       - Total tokens before compaction
--   compacted_tokens      - Total tokens after compaction
--   messages_removed      - Count of messages removed/archived
--   summary_content       - Generated summary text (NULL for prune-only strategies)
--   preserved_message_ids - Array of message IDs that were protected: ["uuid1", "uuid2", ...]
--   model_used            - Model that generated summary (e.g., "claude-sonnet-4-5-20250929")
--   duration_ms           - Operation duration in milliseconds
--   created_at            - Compaction completion timestamp
--
-- Usage patterns:
--   - Get compaction history: session_id ordered by created_at DESC
--   - Analytics by strategy: group by strategy

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

-- Index for retrieving compaction history for a session
CREATE INDEX idx_agentpg_compaction_session ON agentpg_compaction_events(session_id, created_at DESC);

-- Index for analytics queries by strategy
CREATE INDEX idx_agentpg_compaction_strategy ON agentpg_compaction_events(strategy);


-- =============================================================================
-- MESSAGE ARCHIVE TABLE
-- =============================================================================
-- Stores archived messages removed during compaction for reversibility/rollback.
-- Messages can be restored if a compaction needs to be undone.
--
-- Columns:
--   id                  - UUID primary key (copied from original message, NOT auto-generated)
--   compaction_event_id - Foreign key to compaction event that archived this (CASCADE delete)
--   session_id          - Foreign key to session for quick lookups (CASCADE delete)
--   original_message    - Complete message object as JSONB (includes all fields for restoration)
--   archived_at         - Archival timestamp
--
-- Usage patterns:
--   - Restore from compaction: compaction_event_id
--   - Browse session archive: session_id ordered by archived_at DESC

CREATE TABLE agentpg_message_archive (
    id UUID PRIMARY KEY,
    compaction_event_id UUID REFERENCES agentpg_compaction_events(id) ON DELETE CASCADE,
    session_id UUID NOT NULL REFERENCES agentpg_sessions(id) ON DELETE CASCADE,
    original_message JSONB NOT NULL,
    archived_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Index for retrieving all messages from a specific compaction event
CREATE INDEX idx_agentpg_archive_compaction ON agentpg_message_archive(compaction_event_id);

-- Index for browsing session archive history
CREATE INDEX idx_agentpg_archive_session ON agentpg_message_archive(session_id, archived_at DESC);
