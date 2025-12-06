# Storage Guide

This guide covers AgentPG's storage layer, including the PostgreSQL schema, migrations, and data model.

## Overview

AgentPG uses PostgreSQL for persistent storage of:
- **Sessions** - Conversation contexts with metadata
- **Messages** - Individual messages with content blocks
- **Compaction Events** - Audit trail for context management
- **Message Archive** - Archived messages for potential rollback

## Database Schema

### Entity Relationship

```
┌─────────────────┐
│    sessions     │
├─────────────────┤
│ id (PK)         │
│ tenant_id       │───┐
│ identifier      │   │
│ metadata        │   │
│ compaction_count│   │
│ created_at      │   │
│ updated_at      │   │
└─────────────────┘   │
         │            │
         │ 1:N        │
         ▼            │
┌─────────────────┐   │    ┌─────────────────────┐
│    messages     │   │    │  compaction_events  │
├─────────────────┤   │    ├─────────────────────┤
│ id (PK)         │   │    │ id (PK)             │
│ session_id (FK) │◄──┼────│ session_id (FK)     │
│ role            │   │    │ strategy            │
│ content (JSONB) │   │    │ original_tokens     │
│ token_count     │   │    │ compacted_tokens    │
│ metadata        │   │    │ messages_removed    │
│ is_preserved    │   │    │ summary_content     │
│ is_summary      │   │    │ preserved_msg_ids   │
│ created_at      │   │    │ model_used          │
│ updated_at      │   │    │ duration_ms         │
└─────────────────┘   │    │ created_at          │
                      │    └─────────────────────┘
                      │              │
                      │              │ 1:N
                      │              ▼
                      │    ┌─────────────────────┐
                      │    │   message_archive   │
                      │    ├─────────────────────┤
                      │    │ id (PK)             │
                      └────│ session_id (FK)     │
                           │ compaction_event_id │
                           │ original_message    │
                           │ archived_at         │
                           └─────────────────────┘
```

### Sessions Table

Stores conversation sessions with multi-tenancy support.

```sql
CREATE TABLE sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id TEXT NOT NULL,
    identifier TEXT NOT NULL,
    metadata JSONB DEFAULT '{}',
    compaction_count INTEGER DEFAULT 0,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- Indexes
CREATE INDEX idx_sessions_tenant_identifier ON sessions(tenant_id, identifier);
CREATE INDEX idx_sessions_tenant_updated ON sessions(tenant_id, updated_at DESC);
```

**Columns:**

| Column | Type | Description |
|--------|------|-------------|
| `id` | UUID | Primary key (auto-generated) |
| `tenant_id` | TEXT | Tenant identifier for multi-tenancy |
| `identifier` | TEXT | Custom session identifier (e.g., user ID) |
| `metadata` | JSONB | Application-specific metadata |
| `compaction_count` | INTEGER | Number of compaction operations performed |
| `created_at` | TIMESTAMPTZ | Creation timestamp |
| `updated_at` | TIMESTAMPTZ | Last update timestamp |

### Messages Table

Stores individual messages with Anthropic-compatible content blocks.

```sql
CREATE TABLE messages (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    role TEXT NOT NULL CHECK (role IN ('user', 'assistant', 'system')),
    content JSONB NOT NULL,
    token_count INTEGER NOT NULL DEFAULT 0,
    metadata JSONB DEFAULT '{}',
    is_preserved BOOLEAN DEFAULT FALSE,
    is_summary BOOLEAN DEFAULT FALSE,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- Indexes
CREATE INDEX idx_messages_session ON messages(session_id, created_at);
CREATE INDEX idx_messages_preserved ON messages(session_id, is_preserved) WHERE is_preserved = true;
CREATE INDEX idx_messages_summary ON messages(session_id, is_summary) WHERE is_summary = true;
```

**Columns:**

| Column | Type | Description |
|--------|------|-------------|
| `id` | UUID | Primary key |
| `session_id` | UUID | Foreign key to sessions |
| `role` | TEXT | Message role: 'user', 'assistant', 'system' |
| `content` | JSONB | Array of content blocks |
| `token_count` | INTEGER | Estimated token count |
| `metadata` | JSONB | Message metadata |
| `is_preserved` | BOOLEAN | Protected from compaction |
| `is_summary` | BOOLEAN | Is a compaction summary |
| `created_at` | TIMESTAMPTZ | Creation timestamp |
| `updated_at` | TIMESTAMPTZ | Last update timestamp |

**Content Format:**
```json
[
  {"type": "text", "text": "Hello, how can I help?"},
  {"type": "tool_use", "id": "tool_123", "name": "search", "input": {"query": "weather"}}
]
```

### Compaction Events Table

Audit trail for context compaction operations.

```sql
CREATE TABLE compaction_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    strategy TEXT NOT NULL,
    original_tokens INTEGER NOT NULL,
    compacted_tokens INTEGER NOT NULL,
    messages_removed INTEGER NOT NULL,
    summary_content TEXT,
    preserved_message_ids JSONB,
    model_used TEXT,
    duration_ms BIGINT,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

-- Indexes
CREATE INDEX idx_compaction_session ON compaction_events(session_id, created_at DESC);
CREATE INDEX idx_compaction_strategy ON compaction_events(strategy);
```

### Message Archive Table

Stores archived messages for potential rollback.

```sql
CREATE TABLE message_archive (
    id UUID PRIMARY KEY,  -- Original message ID
    compaction_event_id UUID REFERENCES compaction_events(id) ON DELETE CASCADE,
    session_id UUID REFERENCES sessions(id) ON DELETE CASCADE,
    original_message JSONB NOT NULL,
    archived_at TIMESTAMPTZ DEFAULT NOW()
);

-- Indexes
CREATE INDEX idx_archive_compaction ON message_archive(compaction_event_id);
CREATE INDEX idx_archive_session ON message_archive(session_id, archived_at DESC);
```

---

## Migrations

### Running Migrations

```bash
# Using psql
psql "$DATABASE_URL" \
  -f storage/migrations/001_create_sessions.up.sql \
  -f storage/migrations/002_create_messages.up.sql \
  -f storage/migrations/003_create_compaction_events.up.sql \
  -f storage/migrations/004_create_message_archive.up.sql

# Using make (if available)
make migrate
```

### Migration Files

```
storage/migrations/
├── 001_create_sessions.up.sql
├── 001_create_sessions.down.sql
├── 002_create_messages.up.sql
├── 002_create_messages.down.sql
├── 003_create_compaction_events.up.sql
├── 003_create_compaction_events.down.sql
├── 004_create_message_archive.up.sql
└── 004_create_message_archive.down.sql
```

### Rollback Migrations

```bash
psql "$DATABASE_URL" \
  -f storage/migrations/004_create_message_archive.down.sql \
  -f storage/migrations/003_create_compaction_events.down.sql \
  -f storage/migrations/002_create_messages.down.sql \
  -f storage/migrations/001_create_sessions.down.sql
```

---

## Store Interface

### Basic Operations

```go
type Store interface {
    // Session operations
    CreateSession(ctx context.Context, tenantID, identifier string, metadata map[string]any) (string, error)
    GetSession(ctx context.Context, id string) (*Session, error)
    GetSessionByTenantAndIdentifier(ctx context.Context, tenantID, identifier string) (*Session, error)
    GetSessionsByTenant(ctx context.Context, tenantID string) ([]*Session, error)
    UpdateSessionCompactionCount(ctx context.Context, sessionID string) error

    // Message operations
    SaveMessage(ctx context.Context, msg *Message) error
    GetMessages(ctx context.Context, sessionID string) ([]*Message, error)
    DeleteMessages(ctx context.Context, ids []string) error
    GetSessionTokenCount(ctx context.Context, sessionID string) (int, error)

    // Compaction operations
    SaveCompactionEvent(ctx context.Context, event *CompactionEvent) error
    GetCompactionHistory(ctx context.Context, sessionID string) ([]*CompactionEvent, error)
    ArchiveMessages(ctx context.Context, eventID, sessionID string, messages []*Message) error

    // Lifecycle
    Close() error
}
```

### Transaction Support

```go
type TxStore interface {
    Store
    BeginTx(ctx context.Context) (Tx, error)
}

type Tx interface {
    Store
    Commit(ctx context.Context) error
    Rollback(ctx context.Context) error
}
```

**Usage:**
```go
tx, err := store.BeginTx(ctx)
if err != nil {
    return err
}
defer tx.Rollback(ctx)  // No-op if committed

// Perform operations
tx.SaveMessage(ctx, msg1)
tx.SaveMessage(ctx, msg2)
tx.DeleteMessages(ctx, oldIDs)

return tx.Commit(ctx)
```

---

## Data Types

### Session

```go
type Session struct {
    ID              string
    TenantID        string
    Identifier      string
    Metadata        map[string]any
    CompactionCount int
    CreatedAt       time.Time
    UpdatedAt       time.Time
}
```

### Message

```go
type Message struct {
    ID          string
    SessionID   string
    Role        string         // "user", "assistant", "system"
    Content     []any          // Content blocks
    TokenCount  int
    Metadata    map[string]any
    IsPreserved bool
    IsSummary   bool
    CreatedAt   time.Time
    UpdatedAt   time.Time
}
```

### CompactionEvent

```go
type CompactionEvent struct {
    ID                  string
    SessionID           string
    Strategy            string
    OriginalTokens      int
    CompactedTokens     int
    MessagesRemoved     int
    SummaryContent      string
    PreservedMessageIDs []string
    ModelUsed           string
    DurationMs          int64
    CreatedAt           time.Time
}
```

---

## Query Patterns

### Get Session Messages

```sql
SELECT * FROM messages
WHERE session_id = $1
ORDER BY created_at ASC;
```

### Get Token Count

```sql
SELECT COALESCE(SUM(token_count), 0)
FROM messages
WHERE session_id = $1;
```

### Find Sessions by Tenant

```sql
SELECT * FROM sessions
WHERE tenant_id = $1
ORDER BY updated_at DESC;
```

### Get Compaction History

```sql
SELECT * FROM compaction_events
WHERE session_id = $1
ORDER BY created_at DESC;
```

---

## Connection Pool Configuration

```go
config, _ := pgxpool.ParseConfig(databaseURL)

// Recommended production settings
config.MaxConns = 25              // Max concurrent connections
config.MinConns = 5               // Keep-alive connections
config.MaxConnLifetime = time.Hour
config.MaxConnIdleTime = 30 * time.Minute
config.HealthCheckPeriod = time.Minute

pool, err := pgxpool.NewWithConfig(ctx, config)
```

### Connection Sizing

| Application Type | MaxConns | MinConns |
|-----------------|----------|----------|
| Development | 5 | 1 |
| Small production | 10-25 | 3-5 |
| High-traffic | 50-100 | 10-20 |

---

## Multi-Tenancy

AgentPG uses `tenant_id` for data isolation:

```go
// Different tenants are completely isolated
agent.NewSession(ctx, "company-a", "user-1", nil)  // Tenant A
agent.NewSession(ctx, "company-b", "user-1", nil)  // Tenant B (separate)

// Query by tenant
sessions, _ := store.GetSessionsByTenant(ctx, "company-a")
```

For single-tenant applications, use a constant:

```go
const TenantID = "default"
agent.NewSession(ctx, TenantID, "user-123", nil)
```

---

## Data Retention

### Automatic Cleanup via CASCADE

When a session is deleted, all related data is automatically cleaned up:
- Messages (CASCADE)
- Compaction events (CASCADE)
- Message archives (CASCADE)

```sql
DELETE FROM sessions WHERE id = $1;
-- All related data is automatically deleted
```

### Manual Cleanup

```sql
-- Delete old sessions (older than 30 days, not updated)
DELETE FROM sessions
WHERE updated_at < NOW() - INTERVAL '30 days';

-- Delete only archived messages (keep compaction events for audit)
DELETE FROM message_archive
WHERE archived_at < NOW() - INTERVAL '90 days';
```

---

## Performance Tips

### Index Usage

The schema includes indexes for common queries:

```sql
-- Session lookup (uses idx_sessions_tenant_identifier)
SELECT * FROM sessions WHERE tenant_id = $1 AND identifier = $2;

-- Message history (uses idx_messages_session)
SELECT * FROM messages WHERE session_id = $1 ORDER BY created_at;

-- Preserved messages (uses partial idx_messages_preserved)
SELECT * FROM messages WHERE session_id = $1 AND is_preserved = true;
```

### JSONB Queries

```sql
-- Query metadata
SELECT * FROM sessions
WHERE metadata->>'plan' = 'premium';

-- Query content blocks
SELECT * FROM messages
WHERE content @> '[{"type": "tool_use"}]';
```

### Vacuuming

For high-churn tables, consider regular vacuuming:

```sql
-- Analyze and vacuum
ANALYZE messages;
VACUUM (VERBOSE) messages;
```

---

## See Also

- [Architecture](./architecture.md) - Storage in the system context
- [Compaction](./compaction.md) - How compaction uses storage
- [Deployment](./deployment.md) - Production database setup
