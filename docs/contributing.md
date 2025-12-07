# Contributing Guide

This guide covers development setup and contribution guidelines for AgentPG maintainers.

## Development Setup

### Prerequisites

- Go 1.21+
- PostgreSQL 14+
- Docker and Docker Compose
- Make

### Clone and Setup

```bash
# Clone repository
git clone https://github.com/youssefsiam38/agentpg.git
cd agentpg

# Install dependencies
go mod download

# Start development database
docker-compose up -d

# Run migrations
make migrate

# Verify setup
make test
```

### Environment Variables

Create a `.env` file (not committed):

```bash
# .env
DATABASE_URL=postgres://agentpg:agentpg@localhost:5432/agentpg?sslmode=disable
ANTHROPIC_API_KEY=sk-ant-api03-...
```

---

## Project Structure

```
agentpg/
├── agent.go           # Core agent orchestration
├── config.go          # Configuration types and defaults
├── options.go         # Functional options
├── session.go         # Session management
├── message.go         # Message types and helpers
├── errors.go          # Error types
├── doc.go             # Package documentation
│
├── compaction/        # Context compaction
│   ├── manager.go     # Compaction orchestration
│   ├── strategy.go    # Strategy interface
│   ├── summarization.go
│   ├── hybrid.go
│   ├── partitioner.go # Message partitioning
│   └── tokens.go      # Token counting
│
├── storage/           # Persistence layer
│   ├── store.go       # Store interface
│   └── migrations/    # SQL migrations
│
├── driver/            # Database drivers
│   ├── driver.go      # Driver interface
│   ├── executor.go    # Executor interface
│   ├── context.go     # Context utilities
│   ├── pgxv5/         # pgx/v5 driver (recommended)
│   │   ├── driver.go
│   │   └── store.go
│   └── databasesql/   # database/sql driver
│       ├── driver.go
│       └── store.go
│
├── tool/              # Tool system
│   ├── tool.go        # Tool interface
│   ├── registry.go    # Tool registry
│   ├── executor.go    # Execution engine
│   ├── validator.go   # Input validation
│   └── builtin/       # Built-in tools
│
├── streaming/         # Streaming support
│   ├── accumulator.go # Message accumulation
│   └── event.go       # Stream events
│
├── hooks/             # Hook system
│   ├── hooks.go       # Hook registry
│   └── logging.go     # Logging hooks
│
├── types/             # Shared types
│   └── message.go
│
├── internal/          # Internal packages
│   ├── anthropic/     # Anthropic SDK helpers
│   └── testutil/      # Test utilities
│
├── examples/          # Example applications
│   ├── basic/
│   └── streaming/
│
└── docs/              # Documentation
```

---

## Migration Naming Convention

**IMPORTANT**: All migration files MUST follow this exact naming pattern. This is non-negotiable.

### File Naming Pattern

```
{NNN}_agentpg_migration.{up|down}.sql
```

Where `{NNN}` is a three-digit sequence number (001, 002, 003, etc.).

### Examples

```
storage/migrations/
├── 001_agentpg_migration.up.sql
├── 001_agentpg_migration.down.sql
├── 002_agentpg_migration.up.sql
├── 002_agentpg_migration.down.sql
├── 003_agentpg_migration.up.sql
├── 003_agentpg_migration.down.sql
└── README.md
```

### Rules

- The name is **always** `agentpg_migration` - never add suffixes or change it
- Only the sequence number changes between migrations
- Each migration must have both `.up.sql` and `.down.sql` files

### DO NOT

- `001_initial_schema.up.sql` - wrong name
- `002_agentpg_migration_add_indexes.up.sql` - no suffixes allowed
- `002_add_tables.up.sql` - wrong name

---

## Development Workflow

### Make Commands

```bash
# Run all tests
make test

# Run unit tests only
make test-unit

# Run integration tests (requires DATABASE_URL)
make test-integration

# Run linter
make lint

# Build binary
make build

# Start development database
make docker-up

# Stop development database
make docker-down

# Run migrations
make migrate

# Format code
make fmt

# Generate documentation
make docs
```

### Running Tests

```bash
# All tests
go test -v -race ./...

# Unit tests only
go test -v -race -short ./...

# Integration tests
DATABASE_URL="..." go test -v -race -run Integration ./...

# Specific package
go test -v ./tool/...

# With coverage
go test -v -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

### Local Development

```bash
# Start database
docker-compose up -d

# Run example
ANTHROPIC_API_KEY=... go run ./examples/basic/

# Watch for changes (using air)
air
```

---

## Code Style

### Go Guidelines

Follow standard Go conventions:
- Use `gofmt` for formatting
- Use `golangci-lint` for linting
- Write idiomatic Go code
- Document exported functions

### Naming Conventions

```go
// Good: Clear, descriptive names
func (a *Agent) LoadSession(ctx context.Context, sessionID string) error

// Good: Consistent prefixes
func NewAgent(cfg Config) (*Agent, error)
func NewMessage(sessionID string, role Role) *Message

// Good: Interface naming
type Store interface { ... }
type Tool interface { ... }
```

### Error Handling

```go
// Good: Wrap errors with context
if err != nil {
    return fmt.Errorf("failed to load session %s: %w", sessionID, err)
}

// Good: Use custom error types for API
return NewAgentErrorWithSession("LoadSession", sessionID, err)

// Good: Check specific errors
if errors.Is(err, ErrSessionNotFound) {
    // Handle not found case
}
```

### Documentation

```go
// ExecuteParallel executes multiple tool calls concurrently.
//
// The function spawns a goroutine for each call and waits for all to complete.
// Results are returned in the same order as the input calls.
// If the context is cancelled, in-flight calls will receive the cancellation.
//
// Example:
//
//     calls := []ToolCallRequest{{...}, {...}}
//     results := executor.ExecuteParallel(ctx, calls)
//     for _, r := range results {
//         if r.Error != nil {
//             log.Printf("Tool %s failed: %v", r.ToolName, r.Error)
//         }
//     }
func (e *Executor) ExecuteParallel(ctx context.Context, calls []ToolCallRequest) []*ExecuteResult
```

---

## Testing Guidelines

### Unit Tests

```go
func TestValidator_ValidateInput(t *testing.T) {
    validator := NewValidator()

    tests := []struct {
        name    string
        schema  ToolSchema
        input   string
        wantErr bool
    }{
        {
            name: "valid string",
            schema: ToolSchema{
                Type: "object",
                Properties: map[string]PropertyDef{
                    "name": {Type: "string"},
                },
                Required: []string{"name"},
            },
            input:   `{"name": "test"}`,
            wantErr: false,
        },
        // More test cases...
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            err := validator.ValidateInput(tt.schema, json.RawMessage(tt.input))
            if (err != nil) != tt.wantErr {
                t.Errorf("ValidateInput() error = %v, wantErr %v", err, tt.wantErr)
            }
        })
    }
}
```

### Integration Tests

```go
func TestIntegration_Store_SessionLifecycle(t *testing.T) {
    testutil.RequireIntegration(t)

    db := testutil.NewTestDB(t)
    if db == nil {
        return
    }
    defer db.Close()

    ctx := context.Background()
    if err := db.CleanTables(ctx); err != nil {
        t.Fatalf("Failed to clean tables: %v", err)
    }

    drv := pgxv5.New(db.Pool)
    store := drv.GetStore()

    // Test session creation
    sessionID, err := store.CreateSession(ctx, "tenant1", "user1", nil, nil)
    if err != nil {
        t.Fatalf("CreateSession failed: %v", err)
    }
    // More assertions...
}
```

### Test Utilities

```go
// internal/testutil/testutil.go

// RequireIntegration skips if not running integration tests
func RequireIntegration(t *testing.T) {
    t.Helper()
    if os.Getenv("DATABASE_URL") == "" {
        t.Skip("Skipping integration test: DATABASE_URL not set")
    }
}

// NewTestDB creates a test database connection
func NewTestDB(t *testing.T) *TestDB {
    t.Helper()
    // ...
}
```

---

## Pull Request Process

### Before Submitting

1. **Test your changes:**
   ```bash
   make test
   make lint
   ```

2. **Update documentation** if needed

3. **Add tests** for new functionality

4. **Run integration tests** if touching database code

### PR Requirements

- [ ] Tests pass (`make test`)
- [ ] Linter passes (`make lint`)
- [ ] Documentation updated
- [ ] Commit messages follow conventions
- [ ] PR description explains the change

### Commit Messages

Follow conventional commits:

```
feat: add support for extended context
fix: resolve race condition in ExecuteParallel
docs: update deployment guide
test: add integration tests for transactions
refactor: simplify compaction partitioner
chore: update dependencies
```

### Review Process

1. CI checks must pass
2. At least one maintainer approval
3. Address all review comments
4. Squash commits before merge

---

## Architecture Decisions

### Adding New Features

1. **Discuss first** - Open an issue to discuss major changes
2. **Write a design doc** for significant features
3. **Consider backward compatibility**
4. **Add configuration options** for customizable behavior

### Code Organization

- Keep packages focused and cohesive
- Use interfaces for extensibility
- Avoid circular dependencies
- Place internal code in `internal/`

### Dependencies

- Minimize external dependencies
- Prefer standard library where possible
- Vendor critical dependencies
- Keep go.mod clean

---

## Release Process

### Versioning

Follow semantic versioning:
- **Major**: Breaking changes
- **Minor**: New features, backward compatible
- **Patch**: Bug fixes

### Release Checklist

1. [ ] Update version in code
2. [ ] Update CHANGELOG.md
3. [ ] Run full test suite
4. [ ] Tag release: `git tag v1.2.3`
5. [ ] Push tag: `git push origin v1.2.3`
6. [ ] Create GitHub release
7. [ ] Verify Go package index

### Changelog

```markdown
## [1.2.0] - 2024-01-15

### Added
- Support for extended context (1M tokens)
- New compaction strategy: hybrid

### Fixed
- Race condition in parallel tool execution

### Changed
- Default compaction trigger changed to 85%
```

---

## Getting Help

### For Contributors

- Open an issue for questions
- Join discussions on GitHub
- Read existing code and tests

### Resources

- [Go Code Review Comments](https://github.com/golang/go/wiki/CodeReviewComments)
- [Effective Go](https://golang.org/doc/effective_go)
- [Go Proverbs](https://go-proverbs.github.io/)

---

## Code of Conduct

- Be respectful and inclusive
- Focus on constructive feedback
- Help others learn and grow
- Report issues professionally

---

## See Also

- [Architecture](./architecture.md) - System design
- [API Reference](./api-reference.md) - API documentation
- [Testing](./getting-started.md#testing) - Testing guide
