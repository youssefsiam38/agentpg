# Deployment Guide

This guide covers deploying AgentPG applications to production environments.

## Prerequisites

- PostgreSQL 14+ with migrations applied
- Anthropic API key
- Go 1.21+ (for building)
- Container runtime (Docker/Podman) or VM

## Building for Production

### Standard Build

```bash
# Build with optimizations
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-s -w" \
    -o agent-server \
    ./cmd/server

# Verify binary
./agent-server --version
```

### Docker Build

```dockerfile
# Dockerfile
FROM golang:1.21-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w" \
    -o /agent-server \
    ./cmd/server

FROM alpine:3.19
RUN apk --no-cache add ca-certificates tzdata
RUN adduser -D -u 1000 appuser

COPY --from=builder /agent-server /agent-server
USER appuser

EXPOSE 8080
ENTRYPOINT ["/agent-server"]
```

```bash
# Build image
docker build -t agentpg-server:latest .

# Run container
docker run -d \
    -p 8080:8080 \
    -e ANTHROPIC_API_KEY=$ANTHROPIC_API_KEY \
    -e DATABASE_URL=$DATABASE_URL \
    agentpg-server:latest
```

---

## Database Setup

### PostgreSQL Configuration

**Recommended settings for production:**

```ini
# postgresql.conf

# Memory
shared_buffers = 256MB           # 25% of RAM for dedicated DB server
effective_cache_size = 768MB     # 75% of RAM
work_mem = 16MB                  # Per-query memory
maintenance_work_mem = 128MB     # For VACUUM, CREATE INDEX

# Write-ahead log
wal_level = replica              # For backups/replication
max_wal_size = 1GB
min_wal_size = 80MB

# Query planning
random_page_cost = 1.1           # For SSD storage
effective_io_concurrency = 200   # For SSD storage

# Connections
max_connections = 200            # Adjust based on app pool size
```

### Running Migrations

```bash
# Using psql
psql "$DATABASE_URL" \
    -f storage/migrations/001_create_sessions.up.sql \
    -f storage/migrations/002_create_messages.up.sql \
    -f storage/migrations/003_create_compaction_events.up.sql \
    -f storage/migrations/004_create_message_archive.up.sql

# Using golang-migrate
migrate -database "$DATABASE_URL" -path storage/migrations up

# Using make
make migrate
```

### Connection Pooling

For high-traffic applications, use connection pooling:

**PgBouncer:**
```ini
# pgbouncer.ini
[databases]
agentpg = host=db-server port=5432 dbname=agentpg

[pgbouncer]
listen_port = 6432
listen_addr = *
auth_type = md5
auth_file = /etc/pgbouncer/userlist.txt
pool_mode = transaction
max_client_conn = 1000
default_pool_size = 25
```

**Application configuration:**
```go
config, _ := pgxpool.ParseConfig(databaseURL)
config.MaxConns = 25              // Match pool size
config.MinConns = 5
config.MaxConnLifetime = time.Hour
config.MaxConnIdleTime = 30 * time.Minute
config.HealthCheckPeriod = time.Minute
```

---

## Kubernetes Deployment

### ConfigMap and Secret

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: agentpg-secrets
type: Opaque
stringData:
  anthropic-api-key: "sk-ant-api03-..."
  database-url: "postgres://user:pass@db:5432/agentpg?sslmode=require"
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: agentpg-config
data:
  MODEL: "claude-sonnet-4-5-20250929"
  MAX_TOKENS: "4096"
  LOG_LEVEL: "info"
```

### Deployment

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: agentpg-server
spec:
  replicas: 3
  selector:
    matchLabels:
      app: agentpg-server
  template:
    metadata:
      labels:
        app: agentpg-server
    spec:
      containers:
      - name: server
        image: agentpg-server:latest
        ports:
        - containerPort: 8080
        env:
        - name: ANTHROPIC_API_KEY
          valueFrom:
            secretKeyRef:
              name: agentpg-secrets
              key: anthropic-api-key
        - name: DATABASE_URL
          valueFrom:
            secretKeyRef:
              name: agentpg-secrets
              key: database-url
        envFrom:
        - configMapRef:
            name: agentpg-config
        resources:
          requests:
            cpu: 100m
            memory: 128Mi
          limits:
            cpu: 500m
            memory: 512Mi
        livenessProbe:
          httpGet:
            path: /health
            port: 8080
          initialDelaySeconds: 5
          periodSeconds: 10
        readinessProbe:
          httpGet:
            path: /ready
            port: 8080
          initialDelaySeconds: 5
          periodSeconds: 5
```

### Service

```yaml
apiVersion: v1
kind: Service
metadata:
  name: agentpg-server
spec:
  selector:
    app: agentpg-server
  ports:
  - port: 80
    targetPort: 8080
  type: ClusterIP
---
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: agentpg-ingress
  annotations:
    nginx.ingress.kubernetes.io/ssl-redirect: "true"
spec:
  tls:
  - hosts:
    - api.example.com
    secretName: tls-secret
  rules:
  - host: api.example.com
    http:
      paths:
      - path: /
        pathType: Prefix
        backend:
          service:
            name: agentpg-server
            port:
              number: 80
```

### Horizontal Pod Autoscaler

```yaml
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: agentpg-hpa
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: agentpg-server
  minReplicas: 2
  maxReplicas: 10
  metrics:
  - type: Resource
    resource:
      name: cpu
      target:
        type: Utilization
        averageUtilization: 70
```

---

## Docker Compose (Simple Deployment)

```yaml
# docker-compose.yml
version: '3.8'

services:
  db:
    image: postgres:16-alpine
    environment:
      POSTGRES_USER: agentpg
      POSTGRES_PASSWORD: agentpg
      POSTGRES_DB: agentpg
    volumes:
      - postgres_data:/var/lib/postgresql/data
      - ./storage/migrations:/docker-entrypoint-initdb.d
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U agentpg"]
      interval: 5s
      timeout: 5s
      retries: 5

  server:
    build: .
    ports:
      - "8080:8080"
    environment:
      DATABASE_URL: postgres://agentpg:agentpg@db:5432/agentpg?sslmode=disable
      ANTHROPIC_API_KEY: ${ANTHROPIC_API_KEY}
    depends_on:
      db:
        condition: service_healthy
    restart: unless-stopped

volumes:
  postgres_data:
```

---

## Cloud Deployments

### AWS

**RDS PostgreSQL:**
```bash
aws rds create-db-instance \
    --db-instance-identifier agentpg-db \
    --db-instance-class db.t3.medium \
    --engine postgres \
    --engine-version 16.1 \
    --master-username admin \
    --master-user-password $DB_PASSWORD \
    --allocated-storage 100 \
    --storage-encrypted \
    --vpc-security-group-ids sg-xxx
```

**ECS Task Definition:**
```json
{
  "family": "agentpg",
  "networkMode": "awsvpc",
  "containerDefinitions": [
    {
      "name": "server",
      "image": "xxx.dkr.ecr.region.amazonaws.com/agentpg:latest",
      "portMappings": [{"containerPort": 8080}],
      "secrets": [
        {"name": "ANTHROPIC_API_KEY", "valueFrom": "arn:aws:secretsmanager:..."},
        {"name": "DATABASE_URL", "valueFrom": "arn:aws:secretsmanager:..."}
      ],
      "logConfiguration": {
        "logDriver": "awslogs",
        "options": {
          "awslogs-group": "/ecs/agentpg",
          "awslogs-region": "us-east-1",
          "awslogs-stream-prefix": "ecs"
        }
      }
    }
  ],
  "requiresCompatibilities": ["FARGATE"],
  "cpu": "512",
  "memory": "1024"
}
```

### Google Cloud

**Cloud SQL:**
```bash
gcloud sql instances create agentpg-db \
    --database-version=POSTGRES_16 \
    --tier=db-custom-2-7680 \
    --region=us-central1 \
    --storage-size=100GB \
    --storage-auto-increase
```

**Cloud Run:**
```bash
gcloud run deploy agentpg-server \
    --image gcr.io/project/agentpg:latest \
    --platform managed \
    --region us-central1 \
    --set-secrets ANTHROPIC_API_KEY=anthropic-key:latest,DATABASE_URL=db-url:latest \
    --min-instances 1 \
    --max-instances 10
```

---

## Health Checks

Implement health and readiness endpoints:

```go
func main() {
    // ... agent setup ...

    http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusOK)
        w.Write([]byte("OK"))
    })

    http.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
        // Check database connectivity
        if err := pool.Ping(r.Context()); err != nil {
            w.WriteHeader(http.StatusServiceUnavailable)
            w.Write([]byte("Database unavailable"))
            return
        }

        w.WriteHeader(http.StatusOK)
        w.Write([]byte("Ready"))
    })
}
```

---

## Monitoring

### Metrics

Export Prometheus metrics:

```go
import "github.com/prometheus/client_golang/prometheus/promhttp"

http.Handle("/metrics", promhttp.Handler())
```

Key metrics to track:
- Request latency
- Token usage (input/output)
- Tool execution time
- Compaction frequency
- Database connection pool stats
- Error rates

### Logging

Structured logging for production:

```go
import "go.uber.org/zap"

logger, _ := zap.NewProduction()
defer logger.Sync()

logger.Info("Agent request",
    zap.String("session_id", sessionID),
    zap.Int("input_tokens", usage.InputTokens),
    zap.Duration("latency", elapsed),
)
```

### Alerting

Configure alerts for:
- High error rates (> 1%)
- High latency (p99 > 10s)
- Database connection pool exhaustion
- Frequent compaction (potential memory leak)
- API rate limit approaching

---

## Performance Tuning

### Application Settings

```go
agent, _ := agentpg.New(cfg,
    // Optimize for production
    agentpg.WithMaxRetries(3),
    agentpg.WithMaxToolIterations(20),

    // Tune compaction
    agentpg.WithCompactionTrigger(0.85),
    agentpg.WithCompactionTarget(80000),
    agentpg.WithSummarizerModel("claude-3-5-haiku-20241022"), // Fast summarization
)
```

### Database Indexes

Verify index usage:

```sql
-- Check index usage
SELECT
    schemaname,
    tablename,
    indexname,
    idx_scan,
    idx_tup_read,
    idx_tup_fetch
FROM pg_stat_user_indexes
ORDER BY idx_scan DESC;
```

### Connection Pool Tuning

```go
config.MaxConns = 25              // Based on: (core_count * 2) + spindle_count
config.MinConns = 5
config.MaxConnLifetime = time.Hour
config.MaxConnIdleTime = 30 * time.Minute
```

---

## Backup and Recovery

### Database Backups

```bash
# pg_dump backup
pg_dump "$DATABASE_URL" > backup_$(date +%Y%m%d).sql

# Continuous archiving with WAL
archive_command = 'cp %p /backup/wal/%f'
```

### Point-in-Time Recovery

```bash
# Restore to specific point
pg_restore -d agentpg \
    --target-time="2024-01-15 10:30:00" \
    backup.dump
```

---

## Disaster Recovery

### Multi-Region

1. **Primary Region**: Main database and application
2. **Secondary Region**: Read replica and standby application
3. **Failover**: DNS-based failover with health checks

### RPO/RTO Targets

| Tier | RPO | RTO |
|------|-----|-----|
| Standard | 1 hour | 4 hours |
| Business | 15 min | 1 hour |
| Enterprise | 1 min | 15 min |

---

## Checklist

### Pre-Deployment

- [ ] Database migrations applied
- [ ] SSL/TLS configured
- [ ] Secrets stored securely
- [ ] Health checks implemented
- [ ] Logging configured
- [ ] Metrics exposed
- [ ] Backups configured

### Post-Deployment

- [ ] Verify health endpoints
- [ ] Test failover procedures
- [ ] Monitor error rates
- [ ] Verify backups work
- [ ] Document runbooks

---

## See Also

- [Security](./security.md) - Security best practices
- [Configuration](./configuration.md) - All configuration options
- [Architecture](./architecture.md) - System design
