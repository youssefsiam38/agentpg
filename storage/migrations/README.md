# AgentPG Database Migrations

This directory contains SQL migration files for setting up the AgentPG database schema.

## Migration Files

Migrations are numbered and include both `up` (apply) and `down` (rollback) scripts:

1. **001_create_sessions** - Creates the `sessions` table for conversation sessions
2. **002_create_messages** - Creates the `messages` table for conversation messages
3. **003_create_compaction_events** - Creates the `compaction_events` table for compaction audit trail
4. **004_create_message_archive** - Creates the `message_archive` table for message reversibility

## Applying Migrations

AgentPG provides migration SQL files but **does not include a migration runner**. You should use your preferred migration tool to apply these migrations.

### Option 1: Using golang-migrate

```bash
# Install golang-migrate
go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest

# Apply migrations
migrate -path ./storage/migrations -database "postgresql://user:pass@localhost:5432/dbname?sslmode=disable" up

# Rollback migrations
migrate -path ./storage/migrations -database "postgresql://user:pass@localhost:5432/dbname?sslmode=disable" down
```

### Option 2: Using goose

```bash
# Install goose
go install github.com/pressly/goose/v3/cmd/goose@latest

# Apply migrations
goose -dir ./storage/migrations postgres "user=myuser password=mypass dbname=mydb sslmode=disable" up

# Rollback migrations
goose -dir ./storage/migrations postgres "user=myuser password=mypass dbname=mydb sslmode=disable" down
```

### Option 3: Manual Application

You can also apply the migrations manually using `psql`:

```bash
psql -U myuser -d mydb -f storage/migrations/001_create_sessions.up.sql
psql -U myuser -d mydb -f storage/migrations/002_create_messages.up.sql
psql -U myuser -d mydb -f storage/migrations/003_create_compaction_events.up.sql
psql -U myuser -d mydb -f storage/migrations/004_create_message_archive.up.sql
```

## Schema Overview

### sessions
- Stores conversation sessions
- Tracks total tokens and compaction count
- Contains project metadata

### messages
- Stores all conversation messages
- Content stored as JSONB for flexibility
- Supports preservation flags for compaction

### compaction_events
- Audit trail for all compaction operations
- Tracks token savings and strategy used
- Links to preserved messages

### message_archive
- Stores removed messages for reversibility
- Enables rollback of compaction operations
- Linked to compaction events

## Notes

- All tables use UUID primary keys
- Foreign keys have CASCADE deletes
- Timestamps use `TIMESTAMPTZ` for timezone awareness
- JSONB columns enable flexible schema evolution
- Indexes optimize common query patterns
