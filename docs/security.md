# Security Guide

This guide covers security considerations for deploying AgentPG in production.

## Overview

AgentPG handles sensitive data including:
- API keys and credentials
- User conversations
- Tool inputs and outputs
- Database connections

This guide provides recommendations for securing each component.

---

## API Key Security

### Environment Variables

**Never hardcode API keys:**

```go
// Bad - hardcoded key
client := anthropic.NewClient(
    option.WithAPIKey("sk-ant-api03-..."),  // DON'T DO THIS
)

// Good - from environment
client := anthropic.NewClient()  // Reads ANTHROPIC_API_KEY

// Good - explicit environment variable
client := anthropic.NewClient(
    option.WithAPIKey(os.Getenv("ANTHROPIC_API_KEY")),
)
```

### Key Rotation

Rotate API keys regularly:

1. Generate new key in Anthropic Console
2. Update environment variable
3. Restart application
4. Revoke old key

### Key Permissions

Use the principle of least privilege:
- Create separate keys for development/production
- Use read-only keys where possible
- Monitor key usage in Anthropic Console

---

## Database Security

### Connection Security

```go
// Always use SSL/TLS in production
databaseURL := "postgres://user:pass@host:5432/db?sslmode=require"

// Or with certificate verification
databaseURL := "postgres://user:pass@host:5432/db?sslmode=verify-full&sslrootcert=/path/to/ca.crt"
```

### Connection Pooling

```go
config, _ := pgxpool.ParseConfig(databaseURL)

// Limit connections to prevent resource exhaustion
config.MaxConns = 25
config.MinConns = 5

// Set connection lifetime
config.MaxConnLifetime = time.Hour
config.MaxConnIdleTime = 30 * time.Minute
```

### Credential Management

```go
// Use secret managers in production
import "cloud.google.com/go/secretmanager/apiv1"

func getDatabaseURL(ctx context.Context) (string, error) {
    client, _ := secretmanager.NewClient(ctx)
    result, _ := client.AccessSecretVersion(ctx, &secretmanagerpb.AccessSecretVersionRequest{
        Name: "projects/my-project/secrets/database-url/versions/latest",
    })
    return string(result.Payload.Data), nil
}
```

### SQL Injection Prevention

AgentPG uses parameterized queries internally:

```go
// Internal implementation uses parameterized queries
_, err := pool.Exec(ctx, `
    INSERT INTO messages (id, session_id, content)
    VALUES ($1, $2, $3)
`, id, sessionID, content)  // Parameters, not string interpolation
```

For custom queries, always use parameters:

```go
// Good - parameterized
rows, _ := pool.Query(ctx, "SELECT * FROM sessions WHERE tenant_id = $1", tenantID)

// Bad - string interpolation
query := fmt.Sprintf("SELECT * FROM sessions WHERE tenant_id = '%s'", tenantID)  // DON'T
```

---

## Multi-Tenancy

### Data Isolation

AgentPG uses `tenant_id` for data isolation:

```go
// Sessions are isolated by tenant
agent.NewSession(ctx, "company-a", "user-1", nil)  // Company A only
agent.NewSession(ctx, "company-b", "user-1", nil)  // Company B only (separate)
```

### Tenant ID Validation

Always validate tenant IDs from user input:

```go
func handleRequest(w http.ResponseWriter, r *http.Request) {
    tenantID := r.Header.Get("X-Tenant-ID")

    // Validate tenant ID
    if !isValidTenant(tenantID) {
        http.Error(w, "Invalid tenant", http.StatusForbidden)
        return
    }

    // Verify user belongs to tenant
    userID := getUserFromToken(r)
    if !userBelongsToTenant(userID, tenantID) {
        http.Error(w, "Access denied", http.StatusForbidden)
        return
    }

    agent.LoadSessionByIdentifier(ctx, tenantID, userID)
}
```

### Cross-Tenant Access Prevention

Database queries always include tenant filtering:

```sql
-- All session queries include tenant_id
SELECT * FROM sessions WHERE tenant_id = $1 AND identifier = $2;

-- Messages accessed through sessions (tenant-scoped)
SELECT m.* FROM messages m
JOIN sessions s ON m.session_id = s.id
WHERE s.tenant_id = $1;
```

---

## Tool Security

### Input Validation

All tool inputs are validated against schemas:

```go
// Schema validation happens automatically
schema := tool.ToolSchema{
    Type: "object",
    Properties: map[string]tool.PropertyDef{
        "url": {
            Type:        "string",
            Description: "URL to fetch",
            // Pattern validation could be added
        },
    },
    Required: []string{"url"},
}
```

### Dangerous Operations

For tools that perform dangerous operations:

```go
func (t *FileWriteTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
    var params struct {
        Path    string `json:"path"`
        Content string `json:"content"`
    }
    json.Unmarshal(input, &params)

    // Validate path
    if !isAllowedPath(params.Path) {
        return "", fmt.Errorf("path not allowed: %s", params.Path)
    }

    // Check path traversal
    cleanPath := filepath.Clean(params.Path)
    if strings.Contains(cleanPath, "..") {
        return "", fmt.Errorf("path traversal not allowed")
    }

    // Ensure within allowed directory
    if !strings.HasPrefix(cleanPath, allowedDir) {
        return "", fmt.Errorf("path outside allowed directory")
    }

    return writeFile(cleanPath, params.Content)
}
```

### Command Injection Prevention

```go
// Bad - shell execution with user input
func (t *ShellTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
    var params struct {
        Command string `json:"command"`
    }
    json.Unmarshal(input, &params)

    // DON'T DO THIS
    out, _ := exec.Command("sh", "-c", params.Command).Output()
    return string(out), nil
}

// Good - use allowlist of commands
func (t *SafeShellTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
    var params struct {
        Action string   `json:"action"`
        Args   []string `json:"args"`
    }
    json.Unmarshal(input, &params)

    // Allowlist of safe actions
    allowedActions := map[string]string{
        "list_files": "ls",
        "disk_usage": "du",
        "date":       "date",
    }

    cmd, ok := allowedActions[params.Action]
    if !ok {
        return "", fmt.Errorf("action not allowed: %s", params.Action)
    }

    // Execute with validated command
    out, _ := exec.Command(cmd, params.Args...).Output()
    return string(out), nil
}
```

### HTTP Request Tools

```go
func (t *HTTPTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
    var params struct {
        URL string `json:"url"`
    }
    json.Unmarshal(input, &params)

    // Parse and validate URL
    u, err := url.Parse(params.URL)
    if err != nil {
        return "", fmt.Errorf("invalid URL")
    }

    // Only allow HTTPS
    if u.Scheme != "https" {
        return "", fmt.Errorf("only HTTPS URLs allowed")
    }

    // Block internal IPs (SSRF prevention)
    if isInternalIP(u.Host) {
        return "", fmt.Errorf("internal URLs not allowed")
    }

    // Block sensitive domains
    if isSensitiveDomain(u.Host) {
        return "", fmt.Errorf("domain not allowed")
    }

    return fetchURL(ctx, params.URL)
}
```

---

## Data Protection

### Sensitive Data in Conversations

Use hooks to filter sensitive data:

```go
agent.OnBeforeMessage(func(ctx context.Context, messages []any) error {
    for _, msg := range messages {
        if containsPII(msg) {
            return fmt.Errorf("PII detected - request blocked")
        }
    }
    return nil
})
```

### Encryption at Rest

PostgreSQL supports encryption:

```sql
-- Enable pgcrypto extension
CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- For highly sensitive data, consider column-level encryption
-- Note: This requires application changes
```

Consider using managed databases with encryption at rest (AWS RDS, Google Cloud SQL, etc.).

### Data Retention

Implement data retention policies:

```sql
-- Delete old sessions
DELETE FROM sessions
WHERE updated_at < NOW() - INTERVAL '90 days';

-- Delete old archives
DELETE FROM message_archive
WHERE archived_at < NOW() - INTERVAL '30 days';
```

### Audit Logging

Use hooks for comprehensive audit trails:

```go
agent.OnToolCall(func(ctx context.Context, name string, input json.RawMessage, output string, err error) error {
    auditLog.Record(audit.Entry{
        Timestamp: time.Now(),
        UserID:    ctx.Value("user_id").(string),
        TenantID:  ctx.Value("tenant_id").(string),
        Action:    "tool_call",
        Tool:      name,
        Input:     string(input),
        Success:   err == nil,
    })
    return nil
})
```

---

## Network Security

### TLS Configuration

```go
// For custom HTTP clients used in tools
tlsConfig := &tls.Config{
    MinVersion: tls.VersionTLS12,
    CipherSuites: []uint16{
        tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
        tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
    },
}

client := &http.Client{
    Transport: &http.Transport{
        TLSClientConfig: tlsConfig,
    },
}
```

### Timeouts

```go
// Always set timeouts
client := &http.Client{
    Timeout: 30 * time.Second,
}

// Tool executor timeout
executor.SetDefaultTimeout(30 * time.Second)
```

---

## Deployment Security

### Container Security

```dockerfile
# Use non-root user
FROM golang:1.21-alpine AS builder
# ... build ...

FROM alpine:latest
RUN adduser -D -u 1000 appuser
USER appuser
COPY --from=builder /app/binary /app/binary
CMD ["/app/binary"]
```

### Secrets in Kubernetes

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: agentpg-secrets
type: Opaque
data:
  anthropic-api-key: <base64-encoded>
  database-url: <base64-encoded>
---
apiVersion: apps/v1
kind: Deployment
spec:
  template:
    spec:
      containers:
      - name: agent
        env:
        - name: ANTHROPIC_API_KEY
          valueFrom:
            secretKeyRef:
              name: agentpg-secrets
              key: anthropic-api-key
```

---

## Security Checklist

### Pre-Production

- [ ] API keys stored in environment variables or secret manager
- [ ] Database connections use SSL/TLS
- [ ] Database credentials are not hardcoded
- [ ] Tool inputs are validated
- [ ] Sensitive data filtering is enabled
- [ ] Audit logging is configured
- [ ] Rate limiting is implemented
- [ ] Error messages don't leak sensitive information

### Production

- [ ] All connections encrypted (TLS 1.2+)
- [ ] Database encryption at rest enabled
- [ ] API keys rotated regularly
- [ ] Access logs monitored
- [ ] Anomaly detection in place
- [ ] Incident response plan documented
- [ ] Regular security audits scheduled

---

## Reporting Security Issues

If you discover a security vulnerability in AgentPG:

1. **Do not** open a public GitHub issue
2. Email security concerns to the maintainers
3. Include:
   - Description of the vulnerability
   - Steps to reproduce
   - Potential impact
   - Suggested fix (if any)

---

## See Also

- [Deployment](./deployment.md) - Production deployment guide
- [Configuration](./configuration.md) - Security-related configuration
- [Hooks](./hooks.md) - Security monitoring with hooks
