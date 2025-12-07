# Storage Guide

This guide covers AgentPG's storage layer, including the driver pattern, PostgreSQL schema, migrations, and data model.

## Overview

AgentPG uses PostgreSQL for persistent storage of:
- **Sessions** - Conversation contexts with metadata
- **Messages** - Individual messages with content blocks
- **Compaction Events** - Audit trail for context management
- **Message Archive** - Archived messages for potential rollback

## Driver Pattern

AgentPG uses a driver-based architecture (inspired by [River](https://github.com/riverqueue/river)) to support multiple database backends. This provides type-safe transaction handling and flexibility in database connectivity.

### Available Drivers

| Driver | Import | Description |
|--------|--------|-------------|
| pgxv5 | `github.com/youssefsiam38/agentpg/driver/pgxv5` | For pgx/v5 users (recommended) |
| databasesql | `github.com/youssefsiam38/agentpg/driver/databasesql` | For database/sql users |

### Using pgxv5 Driver

```go
import (
    "github.com/jackc/pgx/v5/pgxpool"
    "github.com/youssefsiam38/agentpg"
    "github.com/youssefsiam38/agentpg/driver/pgxv5"
)

pool, _ := pgxpool.New(ctx, dbURL)
drv := pgxv5.New(pool)

agent, _ := agentpg.New(drv, agentpg.Config{
    Client:       &client,
    Model:        "claude-sonnet-4-5-20250929",
    SystemPrompt: "You are helpful",
})

// Manual transaction with native pgx.Tx
tx, _ := pool.Begin(ctx)
response, _ := agent.RunTx(ctx, tx, "Hello")
tx.Commit(ctx)
```

### Using database/sql Driver

```go
import (
    "database/sql"
    _ "github.com/lib/pq"
    "github.com/youssefsiam38/agentpg"
    "github.com/youssefsiam38/agentpg/driver/databasesql"
)

db, _ := sql.Open("postgres", dbURL)
drv := databasesql.New(db)

agent, _ := agentpg.New(drv, agentpg.Config{
    Client:       &client,
    Model:        "claude-sonnet-4-5-20250929",
    SystemPrompt: "You are helpful",
})

// Manual transaction with native *sql.Tx
tx, _ := db.BeginTx(ctx, nil)
response, _ := agent.RunTx(ctx, tx, "Hello")
tx.Commit()
```

### Type Inference

The agent type is automatically inferred from the driver - no explicit generic parameter needed:

```go
// Type is inferred as Agent[pgx.Tx]
agent, _ := agentpg.New(pgxv5.New(pool), config)

// Type is inferred as Agent[*sql.Tx]
agent, _ := agentpg.New(databasesql.New(db), config)
```

---

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
psql "$DATABASE_URL" -f storage/migrations/001_agentpg_migration.up.sql

# Using make (if available)
make migrate
```

### Migration Files

```
storage/migrations/
├── 001_agentpg_migration.up.sql
├── 001_agentpg_migration.down.sql
└── README.md
```

### Rollback Migrations

```bash
psql "$DATABASE_URL" -f storage/migrations/001_agentpg_migration.down.sql
```

---

## Store Interface

Each driver provides a Store implementation. The Store interface defines database operations:

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

### Driver Interface

The Driver interface wraps Store with transaction support:

```go
type Driver[TTx any] interface {
    GetExecutor() Executor
    UnwrapExecutor(tx TTx) ExecutorTx
    UnwrapTx(execTx ExecutorTx) TTx
    Begin(ctx context.Context) (ExecutorTx, error)
    PoolIsSet() bool
    GetStore() storage.Store
}
```

### Transaction Usage

Transactions are handled through the agent's `RunTx` method with native transaction types:

```go
// With pgxv5 driver
tx, _ := pool.Begin(ctx)
defer tx.Rollback(ctx)

response, _ := agent.RunTx(ctx, tx, "Hello")
// Your own database operations using the same tx
_, _ = tx.Exec(ctx, "UPDATE users SET last_active = NOW() WHERE id = $1", userID)

tx.Commit(ctx)

// With database/sql driver
tx, _ := db.BeginTx(ctx, nil)
defer tx.Rollback()

response, _ := agent.RunTx(ctx, tx, "Hello")
// Your own database operations using the same tx
_, _ = tx.ExecContext(ctx, "UPDATE users SET last_active = NOW() WHERE id = $1", userID)

tx.Commit()
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
