# Plan: Database-Driven Agents with Metadata Scoping

## Overview

Transform AgentPG from memory-based agent registration to fully database-driven agents, enabling multi-tenant SaaS use cases where users can create unlimited agents using existing tools.

**Key Changes**:
1. Agents stored in database with `metadata` JSONB for flexible scoping (same pattern as sessions)
2. Instances claim runs based on **tool availability**, not agent registration
3. Agent lookup uses name + metadata matching

## Architecture Change

```
CURRENT (Instance-Agent Based):
Instance registers → RegisterInstanceAgent(instanceID, agentName)
Run created → agentpg_claim_runs checks agentpg_instance_agents table
Instance claims run → Only if instance has agent registered

NEW (Tools-Based with Metadata Scoping):
Agent exists in agentpg_agents with tools[] array and metadata JSONB
Instance registers → RegisterInstanceTool(instanceID, toolName) for each tool
Run created → agentpg_claim_runs checks if instance has ALL tools from agent.tools
Agent lookup → Matches by name + metadata filter (e.g., tenant_id in metadata)
Instance claims run → Only if instance has all required tools
```

---

## Phase 1: Database Schema Changes

**File**: `storage/migrations/001_agentpg_migration.up.sql`

### 1.1 Modify `agentpg_agents` table

Add UUID primary key and metadata JSONB for flexible scoping:

```sql
CREATE TABLE agentpg_agents (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}',   -- Flexible scoping (tenant_id, user_id, etc.)
    description TEXT NOT NULL DEFAULT '',
    model TEXT NOT NULL,
    system_prompt TEXT NOT NULL DEFAULT '',
    tool_names TEXT[] NOT NULL DEFAULT '{}',
    agent_names TEXT[] NOT NULL DEFAULT '{}',
    max_tokens INTEGER,
    temperature DOUBLE PRECISION,
    top_k INTEGER,
    top_p DOUBLE PRECISION,
    config JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- GIN index for metadata filtering (same pattern as sessions)
CREATE INDEX agentpg_idx_agents_metadata ON agentpg_agents USING GIN (metadata);
CREATE INDEX agentpg_idx_agents_name ON agentpg_agents(name);
CREATE INDEX agentpg_idx_agents_updated ON agentpg_agents(updated_at DESC);

COMMENT ON TABLE agentpg_agents IS 'Agent definitions with metadata-based scoping for multi-tenant support.';
COMMENT ON COLUMN agentpg_agents.metadata IS 'Flexible JSONB storage for scoping (tenant_id, user_id, environment, etc.). Use @> operator for filtering.';
```

### 1.2 Remove `agentpg_instance_agents` table

Delete entirely - agents are no longer coupled to instances.

```sql
DROP TABLE IF EXISTS agentpg_instance_agents;
```

### 1.3 Update `agentpg_claim_runs` function

Change from checking `agentpg_instance_agents` to checking tool availability:

```sql
CREATE OR REPLACE FUNCTION agentpg_claim_runs(
    p_instance_id TEXT,
    p_max_count INTEGER
)
RETURNS SETOF agentpg_runs AS $$
BEGIN
    RETURN QUERY
    WITH claimable AS (
        SELECT r.id
        FROM agentpg_runs r
        JOIN agentpg_sessions s ON s.id = r.session_id
        JOIN agentpg_agents a ON a.name = r.agent_name
            AND (a.metadata = '{}' OR s.metadata @> a.metadata)  -- Match agent by metadata
        WHERE r.state = 'pending'
          AND r.run_mode = 'batch'
          AND r.claimed_by_instance_id IS NULL
          -- Check instance has ALL required tools for this agent
          AND NOT EXISTS (
              SELECT 1 FROM unnest(a.tool_names) AS required_tool
              WHERE NOT EXISTS (
                  SELECT 1 FROM agentpg_instance_tools it
                  WHERE it.instance_id = p_instance_id
                    AND it.tool_name = required_tool
              )
          )
          -- Also check agent-as-tool requirements (recursive)
          AND NOT EXISTS (
              SELECT 1 FROM unnest(a.agent_names) AS required_agent
              JOIN agentpg_agents child_agent ON child_agent.name = required_agent
                  AND (child_agent.metadata = '{}' OR s.metadata @> child_agent.metadata)
              WHERE NOT EXISTS (
                  SELECT 1 FROM unnest(child_agent.tool_names) AS child_tool
                  WHERE NOT EXISTS (
                      SELECT 1 FROM agentpg_instance_tools it
                      WHERE it.instance_id = p_instance_id
                        AND it.tool_name = child_tool
                  )
              )
          )
        ORDER BY r.created_at ASC
        LIMIT p_max_count
        FOR UPDATE OF r SKIP LOCKED
    )
    UPDATE agentpg_runs r
    SET claimed_by_instance_id = p_instance_id,
        claimed_at = NOW(),
        state = 'batch_submitting'
    FROM claimable c
    WHERE r.id = c.id
    RETURNING r.*;
END;
$$ LANGUAGE plpgsql;
```

### 1.4 Update `agentpg_claim_streaming_runs` function

Same pattern - check tools instead of instance_agents:

```sql
CREATE OR REPLACE FUNCTION agentpg_claim_streaming_runs(
    p_instance_id TEXT,
    p_max_count INTEGER
)
RETURNS SETOF agentpg_runs AS $$
BEGIN
    RETURN QUERY
    WITH claimable AS (
        SELECT r.id
        FROM agentpg_runs r
        JOIN agentpg_sessions s ON s.id = r.session_id
        JOIN agentpg_agents a ON a.name = r.agent_name
            AND (a.metadata = '{}' OR s.metadata @> a.metadata)
        WHERE r.state = 'pending'
          AND r.run_mode = 'streaming'
          AND r.claimed_by_instance_id IS NULL
          -- Check instance has ALL required tools
          AND NOT EXISTS (
              SELECT 1 FROM unnest(a.tool_names) AS required_tool
              WHERE NOT EXISTS (
                  SELECT 1 FROM agentpg_instance_tools it
                  WHERE it.instance_id = p_instance_id
                    AND it.tool_name = required_tool
              )
          )
        ORDER BY r.created_at ASC
        LIMIT p_max_count
        FOR UPDATE OF r SKIP LOCKED
    )
    UPDATE agentpg_runs r
    SET claimed_by_instance_id = p_instance_id,
        claimed_at = NOW(),
        state = 'streaming'
    FROM claimable c
    WHERE r.id = c.id
    RETURNING r.*;
END;
$$ LANGUAGE plpgsql;
```

### 1.5 Update `agentpg_claim_tool_executions` function

For agent-as-tool executions, check if instance has all tools for the target agent:

```sql
CREATE OR REPLACE FUNCTION agentpg_claim_tool_executions(
    p_instance_id TEXT,
    p_tool_names TEXT[],
    p_max_count INTEGER
)
RETURNS SETOF agentpg_tool_executions AS $$
BEGIN
    RETURN QUERY
    WITH claimable AS (
        SELECT te.id
        FROM agentpg_tool_executions te
        JOIN agentpg_runs r ON r.id = te.run_id
        JOIN agentpg_sessions s ON s.id = r.session_id
        WHERE te.state = 'pending'
          AND te.claimed_by_instance_id IS NULL
          AND te.scheduled_at <= NOW()
          AND (
              -- Regular tool: check instance has the tool
              (NOT te.is_agent_tool AND te.tool_name = ANY(p_tool_names))
              OR
              -- Agent-as-tool: check instance has all tools for child agent
              (te.is_agent_tool AND EXISTS (
                  SELECT 1 FROM agentpg_agents child_agent
                  WHERE child_agent.name = te.agent_name
                    AND (child_agent.metadata = '{}' OR s.metadata @> child_agent.metadata)
                    AND NOT EXISTS (
                        SELECT 1 FROM unnest(child_agent.tool_names) AS required_tool
                        WHERE NOT EXISTS (
                            SELECT 1 FROM agentpg_instance_tools it
                            WHERE it.instance_id = p_instance_id
                              AND it.tool_name = required_tool
                        )
                    )
              ))
          )
        ORDER BY te.created_at ASC
        LIMIT p_max_count
        FOR UPDATE OF te SKIP LOCKED
    )
    UPDATE agentpg_tool_executions te
    SET claimed_by_instance_id = p_instance_id,
        claimed_at = NOW(),
        state = 'running'
    FROM claimable c
    WHERE te.id = c.id
    RETURNING te.*;
END;
$$ LANGUAGE plpgsql;
```

### 1.6 Update/Remove triggers

**Remove**:
- `agentpg_trg_validate_run_agent` and `agentpg_validate_run_agent` function (validated at app level now)
- `agentpg_trg_cleanup_orphaned_agents` and `agentpg_cleanup_orphaned_agents` function (agents persist in DB)

**Add**: New validation trigger that checks agent exists in database with matching metadata:

```sql
CREATE OR REPLACE FUNCTION agentpg_validate_run_agent()
RETURNS TRIGGER AS $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM agentpg_agents a
        JOIN agentpg_sessions s ON s.id = NEW.session_id
        WHERE a.name = NEW.agent_name
          AND (a.metadata = '{}' OR s.metadata @> a.metadata)
    ) THEN
        RAISE EXCEPTION 'Agent "%" not found or not accessible for this session', NEW.agent_name;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER agentpg_trg_validate_run_agent
    BEFORE INSERT ON agentpg_runs
    FOR EACH ROW
    EXECUTE FUNCTION agentpg_validate_run_agent();
```

---

## Phase 2: Driver Interface Changes

**File**: `driver/driver.go`

### 2.1 Remove instance-agent methods

```go
// REMOVE these from Store interface:
RegisterInstanceAgent(ctx context.Context, instanceID, agentName string) error
UnregisterInstanceAgent(ctx context.Context, instanceID, agentName string) error
GetInstanceAgents(ctx context.Context, instanceID string) ([]string, error)
```

### 2.2 Update AgentDefinition type

```go
type AgentDefinition struct {
    ID           uuid.UUID      `json:"id"`
    Name         string         `json:"name"`
    Metadata     map[string]any `json:"metadata,omitempty"` // Scoping fields (tenant_id, user_id, etc.)
    Description  string         `json:"description"`
    Model        string         `json:"model"`
    SystemPrompt string         `json:"system_prompt"`
    ToolNames    []string       `json:"tool_names"`
    AgentNames   []string       `json:"agent_names"`
    MaxTokens    *int           `json:"max_tokens,omitempty"`
    Temperature  *float64       `json:"temperature,omitempty"`
    TopK         *int           `json:"top_k,omitempty"`
    TopP         *float64       `json:"top_p,omitempty"`
    Config       map[string]any `json:"config,omitempty"`
    CreatedAt    time.Time      `json:"created_at"`
    UpdatedAt    time.Time      `json:"updated_at"`
}
```

### 2.3 Update agent methods for metadata scoping

```go
// Store interface updates:

// CreateAgent creates a new agent in the database.
CreateAgent(ctx context.Context, agent *AgentDefinition) (*AgentDefinition, error)

// UpdateAgent updates an existing agent.
UpdateAgent(ctx context.Context, agent *AgentDefinition) error

// GetAgent retrieves an agent by ID.
GetAgent(ctx context.Context, id uuid.UUID) (*AgentDefinition, error)

// GetAgentByName retrieves an agent by name with metadata filtering.
// metadataFilter uses JSONB containment (@>) for matching.
GetAgentByName(ctx context.Context, name string, metadataFilter map[string]any) (*AgentDefinition, error)

// DeleteAgent removes an agent by ID.
DeleteAgent(ctx context.Context, id uuid.UUID) error

// ListAgents returns agents matching the metadata filter.
ListAgents(ctx context.Context, params ListAgentsParams) ([]*AgentDefinition, int, error)

type ListAgentsParams struct {
    MetadataFilter map[string]any // Filter by metadata key-value pairs (uses @> operator)
    Limit          int
    Offset         int
}
```

### 2.4 Implement in both drivers

**Files**:
- `driver/pgxv5/store.go`
- `driver/databasesql/store.go`

Example implementation for `GetAgentByName`:

```go
func (s *Store) GetAgentByName(ctx context.Context, name string, metadataFilter map[string]any) (*driver.AgentDefinition, error) {
    query := `
        SELECT id, name, metadata, description, model, system_prompt,
               tool_names, agent_names, max_tokens, temperature, top_k, top_p,
               config, created_at, updated_at
        FROM agentpg_agents
        WHERE name = $1
    `
    args := []any{name}

    if len(metadataFilter) > 0 {
        filterJSON, _ := json.Marshal(metadataFilter)
        query += " AND (metadata = '{}' OR metadata @> $2)"
        args = append(args, filterJSON)
    }

    query += " LIMIT 1"

    var agent driver.AgentDefinition
    // ... scan fields
    return &agent, nil
}
```

---

## Phase 3: Client Changes

**File**: `client.go`

### 3.1 Remove in-memory agent storage

```go
// REMOVE from Client struct:
agents map[string]*AgentDefinition

// REMOVE from NewClient:
agents: make(map[string]*AgentDefinition),
```

### 3.2 Add new agent management methods

```go
// CreateAgent creates a new agent in the database.
// The agent's metadata determines its scope (e.g., tenant_id, user_id).
func (c *Client[TTx]) CreateAgent(ctx context.Context, def *AgentDefinition) (*AgentDefinition, error) {
    if def.Name == "" || def.Model == "" {
        return nil, ErrInvalidConfig
    }

    // Validate tool references exist
    for _, toolName := range def.Tools {
        if _, ok := c.tools[toolName]; !ok {
            return nil, fmt.Errorf("%w: %s", ErrToolNotFound, toolName)
        }
    }

    return c.store.CreateAgent(ctx, &driver.AgentDefinition{
        Name:         def.Name,
        Metadata:     def.Metadata,
        Description:  def.Description,
        Model:        def.Model,
        SystemPrompt: def.SystemPrompt,
        ToolNames:    def.Tools,
        AgentNames:   def.Agents,
        MaxTokens:    def.MaxTokens,
        Temperature:  def.Temperature,
        TopK:         def.TopK,
        TopP:         def.TopP,
        Config:       def.Config,
    })
}

// UpdateAgent updates an existing agent.
func (c *Client[TTx]) UpdateAgent(ctx context.Context, def *AgentDefinition) error {
    // ... validation and update
}

// DeleteAgent removes an agent by ID.
func (c *Client[TTx]) DeleteAgent(ctx context.Context, id uuid.UUID) error {
    return c.store.DeleteAgent(ctx, id)
}

// GetAgent retrieves an agent by ID.
func (c *Client[TTx]) GetAgent(ctx context.Context, id uuid.UUID) (*AgentDefinition, error) {
    return c.store.GetAgent(ctx, id)
}

// ListAgents returns agents matching the metadata filter.
func (c *Client[TTx]) ListAgents(ctx context.Context, metadataFilter map[string]any, limit, offset int) ([]*AgentDefinition, int, error) {
    return c.store.ListAgents(ctx, driver.ListAgentsParams{
        MetadataFilter: metadataFilter,
        Limit:          limit,
        Offset:         offset,
    })
}
```

### 3.3 Keep RegisterAgent for backward compatibility (optional)

```go
// RegisterAgent is a convenience method that creates an agent with empty metadata (global scope).
// For scoped agents, use CreateAgent with metadata.
func (c *Client[TTx]) RegisterAgent(def *AgentDefinition) error {
    _, err := c.CreateAgent(context.Background(), def)
    return err
}
```

### 3.4 Update syncRegistrations

Remove agent registration - only sync tools:

```go
func (c *Client[TTx]) syncRegistrations(ctx context.Context) error {
    // REMOVE: Agent syncing and RegisterInstanceAgent calls

    // KEEP: Tool syncing
    for _, t := range c.tools {
        toolDef := &driver.ToolDefinition{
            Name:        t.Name(),
            Description: t.Description(),
            InputSchema: t.InputSchema(),
        }
        if err := c.store.UpsertTool(ctx, toolDef); err != nil {
            return err
        }
        if err := c.store.RegisterInstanceTool(ctx, c.instanceID, t.Name()); err != nil {
            return err
        }
    }
    return nil
}
```

### 3.5 Update Run methods

Get agent from database using session metadata:

```go
func (c *Client[TTx]) Run(ctx context.Context, sessionID uuid.UUID, agentName, prompt string) (uuid.UUID, error) {
    // Get session to retrieve metadata for agent lookup
    session, err := c.store.GetSession(ctx, sessionID)
    if err != nil {
        return uuid.Nil, err
    }

    // Get agent from database using name and session metadata
    agent, err := c.store.GetAgentByName(ctx, agentName, session.Metadata)
    if err != nil {
        return uuid.Nil, fmt.Errorf("%w: %s", ErrAgentNotFound, agentName)
    }

    // Create run (trigger validates agent exists)
    return c.store.CreateRun(ctx, &driver.Run{
        SessionID: sessionID,
        AgentName: agentName,
        // ... rest of fields
    })
}
```

### 3.6 Update validateReferences

Remove agent validation (agents are now in DB, validated by trigger):

```go
func (c *Client[TTx]) validateReferences() error {
    // REMOVE: Agent existence checks against in-memory map
    // KEEP: Tool existence checks against c.tools map

    for _, t := range c.tools {
        // Validate tool schema
        schema := t.InputSchema()
        if schema.Type != "object" {
            return fmt.Errorf("%w: %s schema type must be 'object'", ErrInvalidToolSchema, t.Name())
        }
    }
    return nil
}
```

---

## Phase 4: Worker Changes

**Files**:
- `worker/run_worker.go`
- `worker/streaming_worker.go`
- `worker/tool_worker.go`

### 4.1 Update run workers to get agent from DB

```go
func (w *RunWorker) processRun(ctx context.Context, run *driver.Run) error {
    // Get session for metadata
    session, err := w.store.GetSession(ctx, run.SessionID)
    if err != nil {
        return fmt.Errorf("session not found: %w", err)
    }

    // Get agent from database using session metadata
    agent, err := w.store.GetAgentByName(ctx, run.AgentName, session.Metadata)
    if err != nil {
        return fmt.Errorf("agent not found: %w", err)
    }

    // Use agent definition for processing
    // ...
}
```

### 4.2 Update tool worker for agent-as-tool

When executing agent-as-tool, get child agent from database:

```go
func (w *ToolWorker) executeAgentTool(ctx context.Context, exec *driver.ToolExecution) error {
    // Get parent run
    parentRun, err := w.store.GetRun(ctx, exec.RunID)
    if err != nil {
        return err
    }

    // Get session for metadata
    session, err := w.store.GetSession(ctx, parentRun.SessionID)
    if err != nil {
        return err
    }

    // Get child agent from database using session metadata
    childAgent, err := w.store.GetAgentByName(ctx, *exec.AgentName, session.Metadata)
    if err != nil {
        return fmt.Errorf("child agent not found: %w", err)
    }

    // Create child run
    // ...
}
```

---

## Phase 5: UI Service Layer Updates

**Files**:
- `ui/service/types.go`
- `ui/service/agents.go`

### 5.1 Update AgentWithStats

```go
type AgentWithStats struct {
    Agent *driver.AgentDefinition `json:"agent"`

    // Stats
    TotalRuns       int `json:"total_runs"`
    ActiveRuns      int `json:"active_runs"`
    CompletedRuns   int `json:"completed_runs"`
    FailedRuns      int `json:"failed_runs"`
    AvgTokensPerRun int `json:"avg_tokens_per_run"`

    // REMOVE: RegisteredOn []string (instances no longer register agents)

    // ADD: Capability info based on tool availability
    CanRun           bool     `json:"can_run"`           // Any instance has all required tools
    CapableInstances []string `json:"capable_instances"` // Instance IDs that can run this agent
    MissingTools     []string `json:"missing_tools"`     // Tools not available on any instance
}
```

### 5.2 Add agent service methods

```go
// CreateAgent creates a new agent.
func (s *Service[TTx]) CreateAgent(ctx context.Context, req CreateAgentRequest) (*driver.AgentDefinition, error) {
    return s.store.CreateAgent(ctx, &driver.AgentDefinition{
        Name:         req.Name,
        Metadata:     req.Metadata,
        Description:  req.Description,
        Model:        req.Model,
        SystemPrompt: req.SystemPrompt,
        ToolNames:    req.Tools,
        AgentNames:   req.Agents,
        MaxTokens:    req.MaxTokens,
        Temperature:  req.Temperature,
        TopK:         req.TopK,
        TopP:         req.TopP,
        Config:       req.Config,
    })
}

// UpdateAgent updates an existing agent.
func (s *Service[TTx]) UpdateAgent(ctx context.Context, id uuid.UUID, req UpdateAgentRequest) error {
    // ...
}

// DeleteAgent removes an agent.
func (s *Service[TTx]) DeleteAgent(ctx context.Context, id uuid.UUID) error {
    return s.store.DeleteAgent(ctx, id)
}

// ListAgentsWithStats returns agents with capability information.
func (s *Service[TTx]) ListAgentsWithStats(ctx context.Context, params AgentListParams) (*AgentList, error) {
    agents, total, err := s.store.ListAgents(ctx, driver.ListAgentsParams{
        MetadataFilter: params.MetadataFilter,
        Limit:          params.Limit,
        Offset:         params.Offset,
    })
    if err != nil {
        return nil, err
    }

    // Get all instances and their tools
    instances, _ := s.store.ListInstances(ctx)
    instanceTools := make(map[string][]string)
    for _, inst := range instances {
        tools, _ := s.store.GetInstanceTools(ctx, inst.ID)
        instanceTools[inst.ID] = tools
    }

    // Calculate capability for each agent
    results := make([]*AgentWithStats, 0, len(agents))
    for _, agent := range agents {
        stats := &AgentWithStats{Agent: agent}

        // Find capable instances
        for instID, tools := range instanceTools {
            if hasAllTools(tools, agent.ToolNames) {
                stats.CapableInstances = append(stats.CapableInstances, instID)
            }
        }
        stats.CanRun = len(stats.CapableInstances) > 0

        // Find missing tools
        allTools := getAllRegisteredTools(instanceTools)
        for _, required := range agent.ToolNames {
            if !contains(allTools, required) {
                stats.MissingTools = append(stats.MissingTools, required)
            }
        }

        results = append(results, stats)
    }

    return &AgentList{
        Agents:     results,
        TotalCount: total,
        HasMore:    params.Offset+len(results) < total,
    }, nil
}
```

### 5.3 Update InstanceDetail

```go
type InstanceDetail struct {
    Instance           *driver.Instance `json:"instance"`
    Status             string           `json:"status"`
    TimeSinceHeartbeat *time.Duration   `json:"time_since_heartbeat,omitempty"`
    ToolNames          []string         `json:"tool_names"`
    IsLeader           bool             `json:"is_leader"`

    // REMOVE: AgentNames []string (instances no longer register agents)

    // ADD: Computed capability
    CapableAgents []string `json:"capable_agents"` // Agent names this instance can run
}
```

---

## Phase 6: UI Frontend Changes

**Files**:
- `ui/frontend/handlers.go`
- `ui/frontend/templates/agents.html`
- `ui/frontend/templates/agent-form.html` (new)

### 6.1 Add agent CRUD handlers

```go
// handleAgentCreate handles POST /agents
func (rt *router[TTx]) handleAgentCreate(w http.ResponseWriter, r *http.Request) {
    // Parse form
    name := r.FormValue("name")
    model := r.FormValue("model")
    systemPrompt := r.FormValue("system_prompt")
    description := r.FormValue("description")

    // Parse tools (multi-select)
    tools := r.Form["tools"]

    // Build metadata from form fields
    metadata := make(map[string]any)
    for k, v := range rt.config.MetadataFilter {
        metadata[k] = v
    }
    // Parse metadata_key_N / metadata_value_N pairs
    for i := 1; i <= 10; i++ {
        key := r.FormValue(fmt.Sprintf("metadata_key_%d", i))
        value := r.FormValue(fmt.Sprintf("metadata_value_%d", i))
        if key != "" && value != "" {
            metadata[key] = value
        }
    }

    agent, err := rt.svc.CreateAgent(r.Context(), service.CreateAgentRequest{
        Name:         name,
        Metadata:     metadata,
        Model:        model,
        SystemPrompt: systemPrompt,
        Description:  description,
        Tools:        tools,
    })
    if err != nil {
        // Handle error
        return
    }

    // Redirect or return fragment
    http.Redirect(w, r, rt.config.BasePath+"/agents/"+agent.ID.String(), http.StatusSeeOther)
}

// handleAgentEdit handles GET /agents/{id}/edit
func (rt *router[TTx]) handleAgentEdit(w http.ResponseWriter, r *http.Request) {
    // ...
}

// handleAgentUpdate handles PUT /agents/{id}
func (rt *router[TTx]) handleAgentUpdate(w http.ResponseWriter, r *http.Request) {
    // ...
}

// handleAgentDelete handles DELETE /agents/{id}
func (rt *router[TTx]) handleAgentDelete(w http.ResponseWriter, r *http.Request) {
    // ...
}
```

### 6.2 Add routes

```go
// In NewRouter:
mux.HandleFunc("GET /agents/new", rt.handleAgentNew)
mux.HandleFunc("POST /agents", rt.handleAgentCreate)
mux.HandleFunc("GET /agents/{id}/edit", rt.handleAgentEdit)
mux.HandleFunc("PUT /agents/{id}", rt.handleAgentUpdate)
mux.HandleFunc("DELETE /agents/{id}", rt.handleAgentDelete)
```

### 6.3 Create agent form template

**File**: `ui/frontend/templates/agent-form.html`

```html
{{define "content"}}
<div class="max-w-2xl mx-auto">
    <h1 class="text-2xl font-bold text-white mb-6">
        {{if .Agent}}Edit Agent{{else}}Create Agent{{end}}
    </h1>

    <form method="POST" action="{{.FormAction}}" class="space-y-6">
        {{if .Agent}}
        <input type="hidden" name="_method" value="PUT">
        {{end}}

        <div>
            <label class="block text-sm font-medium text-gray-300">Name</label>
            <input type="text" name="name" value="{{.Agent.Name}}" required
                   class="mt-1 block w-full rounded-md bg-gray-800 border-gray-700 text-white"
                   placeholder="my-agent">
        </div>

        <div>
            <label class="block text-sm font-medium text-gray-300">Model</label>
            <select name="model" required class="mt-1 block w-full rounded-md bg-gray-800 border-gray-700 text-white">
                <option value="claude-sonnet-4-5-20250929" {{if eq .Agent.Model "claude-sonnet-4-5-20250929"}}selected{{end}}>Claude Sonnet 4.5</option>
                <option value="claude-opus-4-5-20251101" {{if eq .Agent.Model "claude-opus-4-5-20251101"}}selected{{end}}>Claude Opus 4.5</option>
                <option value="claude-3-5-haiku-20241022" {{if eq .Agent.Model "claude-3-5-haiku-20241022"}}selected{{end}}>Claude 3.5 Haiku</option>
            </select>
        </div>

        <div>
            <label class="block text-sm font-medium text-gray-300">System Prompt</label>
            <textarea name="system_prompt" rows="6"
                      class="mt-1 block w-full rounded-md bg-gray-800 border-gray-700 text-white"
                      placeholder="You are a helpful assistant...">{{.Agent.SystemPrompt}}</textarea>
        </div>

        <div>
            <label class="block text-sm font-medium text-gray-300">Description</label>
            <input type="text" name="description" value="{{.Agent.Description}}"
                   class="mt-1 block w-full rounded-md bg-gray-800 border-gray-700 text-white"
                   placeholder="Optional description">
        </div>

        <div>
            <label class="block text-sm font-medium text-gray-300">Tools</label>
            <select name="tools" multiple size="6"
                    class="mt-1 block w-full rounded-md bg-gray-800 border-gray-700 text-white">
                {{range .AvailableTools}}
                <option value="{{.Name}}" {{if contains $.Agent.ToolNames .Name}}selected{{end}}>
                    {{.Name}} - {{.Description}}
                </option>
                {{end}}
            </select>
            <p class="mt-1 text-xs text-gray-500">Hold Ctrl/Cmd to select multiple tools</p>
        </div>

        <!-- Metadata fields -->
        <div>
            <label class="block text-sm font-medium text-gray-300 mb-2">Metadata (Scoping)</label>
            <div class="space-y-2" id="metadata-fields">
                {{range $key, $value := .Agent.Metadata}}
                <div class="flex gap-2">
                    <input type="text" name="metadata_key_{{$.MetadataIndex}}" value="{{$key}}"
                           class="flex-1 rounded-md bg-gray-800 border-gray-700 text-white text-sm"
                           placeholder="Key">
                    <input type="text" name="metadata_value_{{$.MetadataIndex}}" value="{{$value}}"
                           class="flex-1 rounded-md bg-gray-800 border-gray-700 text-white text-sm"
                           placeholder="Value">
                </div>
                {{end}}
                <div class="flex gap-2">
                    <input type="text" name="metadata_key_1"
                           class="flex-1 rounded-md bg-gray-800 border-gray-700 text-white text-sm"
                           placeholder="Key (e.g., tenant_id)">
                    <input type="text" name="metadata_value_1"
                           class="flex-1 rounded-md bg-gray-800 border-gray-700 text-white text-sm"
                           placeholder="Value">
                </div>
            </div>
            <p class="mt-1 text-xs text-gray-500">
                Agents are scoped by metadata. Sessions with matching metadata can use this agent.
            </p>
        </div>

        <div class="flex gap-3">
            <button type="submit" class="px-4 py-2 bg-cyan-600 text-white rounded-md hover:bg-cyan-700">
                {{if .Agent}}Update Agent{{else}}Create Agent{{end}}
            </button>
            <a href="{{.BasePath}}/agents" class="px-4 py-2 bg-gray-700 text-white rounded-md hover:bg-gray-600">
                Cancel
            </a>
        </div>
    </form>
</div>
{{end}}
```

### 6.4 Update agent list view

Show capability status:

```html
<div class="agent-card bg-gray-800 rounded-lg p-4">
    <div class="flex justify-between items-start">
        <div>
            <h3 class="text-lg font-medium text-white">{{.Agent.Name}}</h3>
            <p class="text-sm text-gray-400">{{.Agent.Model}}</p>
        </div>
        <div class="flex gap-2">
            {{if .CanRun}}
            <span class="px-2 py-1 text-xs rounded bg-green-500/20 text-green-400">
                Can run on {{len .CapableInstances}} instance(s)
            </span>
            {{else}}
            <span class="px-2 py-1 text-xs rounded bg-red-500/20 text-red-400">
                Missing tools: {{range .MissingTools}}{{.}} {{end}}
            </span>
            {{end}}
        </div>
    </div>

    <div class="mt-3 flex flex-wrap gap-1">
        {{range .Agent.ToolNames}}
        <span class="px-2 py-0.5 text-xs rounded bg-gray-700 text-gray-300">{{.}}</span>
        {{end}}
    </div>

    {{if .Agent.Metadata}}
    <div class="mt-2 text-xs text-gray-500">
        Scoped: {{range $k, $v := .Agent.Metadata}}{{$k}}={{$v}} {{end}}
    </div>
    {{end}}

    <div class="mt-4 flex gap-2">
        <a href="{{$.BasePath}}/agents/{{.Agent.ID}}/edit"
           class="text-sm text-cyan-400 hover:text-cyan-300">Edit</a>
        <button hx-delete="{{$.BasePath}}/agents/{{.Agent.ID}}"
                hx-confirm="Delete agent '{{.Agent.Name}}'?"
                hx-target="closest .agent-card"
                hx-swap="outerHTML"
                class="text-sm text-red-400 hover:text-red-300">Delete</button>
    </div>
</div>
```

---

## Phase 7: Types Updates

**File**: `types.go`

### 7.1 Update AgentDefinition

```go
// AgentDefinition represents an agent configuration.
type AgentDefinition struct {
    // ID is the unique identifier (set by database on creation).
    ID uuid.UUID `json:"id,omitempty"`

    // Name is the agent's name (required).
    Name string `json:"name"`

    // Metadata contains scoping fields (tenant_id, user_id, etc.).
    // Agents are matched to sessions based on metadata containment.
    Metadata map[string]any `json:"metadata,omitempty"`

    // Description is shown when agent is used as tool.
    Description string `json:"description,omitempty"`

    // Model is the Claude model ID (required).
    Model string `json:"model"`

    // SystemPrompt defines the agent's behavior.
    SystemPrompt string `json:"system_prompt,omitempty"`

    // Tools is the list of tool names this agent can use.
    Tools []string `json:"tools,omitempty"`

    // Agents is the list of agent names this agent can delegate to.
    Agents []string `json:"agents,omitempty"`

    // MaxTokens limits response length.
    MaxTokens *int `json:"max_tokens,omitempty"`

    // Temperature controls randomness (0.0 to 1.0).
    Temperature *float64 `json:"temperature,omitempty"`

    // TopK limits token selection.
    TopK *int `json:"top_k,omitempty"`

    // TopP (nucleus sampling) limits cumulative probability.
    TopP *float64 `json:"top_p,omitempty"`

    // Config holds additional settings as JSON.
    Config map[string]any `json:"config,omitempty"`

    // CreatedAt is when the agent was created.
    CreatedAt time.Time `json:"created_at,omitempty"`

    // UpdatedAt is when the agent was last modified.
    UpdatedAt time.Time `json:"updated_at,omitempty"`
}
```

---

## Files to Modify (Summary)

| File | Changes |
|------|---------|
| `storage/migrations/001_agentpg_migration.up.sql` | Add metadata to agents, remove instance_agents, update claiming functions |
| `driver/driver.go` | Remove instance-agent methods, add metadata-scoped agent methods |
| `driver/pgxv5/store.go` | Implement updated interface |
| `driver/databasesql/store.go` | Implement updated interface |
| `client.go` | Remove in-memory agents, add CreateAgent/UpdateAgent/DeleteAgent |
| `types.go` | Update AgentDefinition with ID and Metadata |
| `worker/run_worker.go` | Get agent from DB using session metadata |
| `worker/streaming_worker.go` | Get agent from DB using session metadata |
| `worker/tool_worker.go` | Get agent from DB for agent-as-tool |
| `ui/service/types.go` | Update AgentWithStats, add CreateAgentRequest |
| `ui/service/agents.go` | Add CRUD methods |
| `ui/frontend/handlers.go` | Add agent form handlers |
| `ui/frontend/router.go` | Register new routes |
| `ui/frontend/templates/agents.html` | Update list view |
| `ui/frontend/templates/agent-form.html` | New create/edit form |
| `CLAUDE.md` | Update documentation |

---

## Verification

### 1. Database Migration
```bash
# Drop and recreate schema
psql $DATABASE_URL -f storage/migrations/001_agentpg_migration.down.sql
psql $DATABASE_URL -f storage/migrations/001_agentpg_migration.up.sql
```

### 2. Unit Tests
```bash
go test ./... -v
```

### 3. Integration Test
```go
// Test agent creation with metadata scoping
agent, err := client.CreateAgent(ctx, &agentpg.AgentDefinition{
    Name:     "my-agent",
    Metadata: map[string]any{"tenant_id": "tenant-1"},
    Model:    "claude-sonnet-4-5-20250929",
    Tools:    []string{"calculator"},
})

// Test that session with matching metadata can use the agent
sessionID, _ := client.NewSession(ctx, nil, map[string]any{"tenant_id": "tenant-1"})
runID, _ := client.Run(ctx, sessionID, "my-agent", "Calculate 2+2")
response, _ := client.WaitForRun(ctx, runID)

// Test that session with different metadata cannot use the agent
sessionID2, _ := client.NewSession(ctx, nil, map[string]any{"tenant_id": "tenant-2"})
_, err := client.Run(ctx, sessionID2, "my-agent", "Calculate 2+2")
// Should return ErrAgentNotFound
```

### 4. UI Verification
- Start server, navigate to `/ui/agents`
- Click "New Agent" button
- Fill form with name, model, system prompt, tools
- Add metadata fields (e.g., tenant_id)
- Submit and verify agent appears in list
- Edit agent, verify changes persist
- Delete agent, verify removed
- Create session with matching metadata
- Create run with agent, verify it completes
- Verify agent shows capability status (can run / missing tools)

### 5. Multi-Instance Test
```bash
# Terminal 1: Instance with calculator tool
go run cmd/worker/main.go --tools=calculator

# Terminal 2: Instance with weather tool
go run cmd/worker/main.go --tools=weather

# Create agent with calculator tool - should only run on terminal 1
# Create agent with weather tool - should only run on terminal 2
# UI should show which instances can run each agent
```

---

## Migration Notes

- This is a **breaking change** for the agent registration API
- Existing `RegisterAgent()` calls need to be updated to `CreateAgent(ctx, def)`
- Agents are now persisted to database, not registered per-instance
- Instance capability is determined by tool registration, not agent registration
- Multi-tenant scenarios use metadata filtering (same pattern as sessions)
