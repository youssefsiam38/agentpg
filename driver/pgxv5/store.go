package pgxv5

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/youssefsiam38/agentpg/driver"
)

// Store implements driver.Store using pgx/v5.
type Store struct {
	pool *pgxpool.Pool
}

// executor is an interface that both *pgxpool.Pool and pgx.Tx implement.
type executor interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Session operations

func (s *Store) CreateSession(ctx context.Context, params driver.CreateSessionParams) (*driver.Session, error) {
	return s.createSession(ctx, s.pool, params)
}

func (s *Store) CreateSessionTx(ctx context.Context, tx pgx.Tx, params driver.CreateSessionParams) (*driver.Session, error) {
	return s.createSession(ctx, tx, params)
}

func (s *Store) createSession(ctx context.Context, e executor, params driver.CreateSessionParams) (*driver.Session, error) {
	var session driver.Session
	var depth int
	if params.ParentSessionID != nil {
		// Get parent's depth
		err := e.QueryRow(ctx, "SELECT depth FROM agentpg_sessions WHERE id = $1", params.ParentSessionID).Scan(&depth)
		if err != nil {
			return nil, fmt.Errorf("failed to get parent session depth: %w", err)
		}
		depth++
	}

	metadata, err := json.Marshal(params.Metadata)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal metadata: %w", err)
	}

	err = e.QueryRow(ctx, `
		INSERT INTO agentpg_sessions (tenant_id, identifier, parent_session_id, depth, metadata)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, tenant_id, identifier, parent_session_id, depth, metadata, compaction_count, created_at, updated_at
	`, params.TenantID, params.Identifier, params.ParentSessionID, depth, metadata).Scan(
		&session.ID, &session.TenantID, &session.Identifier, &session.ParentSessionID,
		&session.Depth, &metadata, &session.CompactionCount, &session.CreatedAt, &session.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}

	if err := json.Unmarshal(metadata, &session.Metadata); err != nil {
		session.Metadata = make(map[string]any)
	}

	return &session, nil
}

func (s *Store) GetSession(ctx context.Context, id uuid.UUID) (*driver.Session, error) {
	var session driver.Session
	var metadata []byte
	err := s.pool.QueryRow(ctx, `
		SELECT id, tenant_id, identifier, parent_session_id, depth, metadata, compaction_count, created_at, updated_at
		FROM agentpg_sessions WHERE id = $1
	`, id).Scan(
		&session.ID, &session.TenantID, &session.Identifier, &session.ParentSessionID,
		&session.Depth, &metadata, &session.CompactionCount, &session.CreatedAt, &session.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get session: %w", err)
	}

	if err := json.Unmarshal(metadata, &session.Metadata); err != nil {
		session.Metadata = make(map[string]any)
	}

	return &session, nil
}

func (s *Store) GetSessionByIdentifier(ctx context.Context, tenantID, identifier string) (*driver.Session, error) {
	var session driver.Session
	var metadata []byte
	err := s.pool.QueryRow(ctx, `
		SELECT id, tenant_id, identifier, parent_session_id, depth, metadata, compaction_count, created_at, updated_at
		FROM agentpg_sessions WHERE tenant_id = $1 AND identifier = $2
	`, tenantID, identifier).Scan(
		&session.ID, &session.TenantID, &session.Identifier, &session.ParentSessionID,
		&session.Depth, &metadata, &session.CompactionCount, &session.CreatedAt, &session.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get session by identifier: %w", err)
	}

	if err := json.Unmarshal(metadata, &session.Metadata); err != nil {
		session.Metadata = make(map[string]any)
	}

	return &session, nil
}

func (s *Store) UpdateSession(ctx context.Context, id uuid.UUID, updates map[string]any) error {
	// Build dynamic update query
	sets := []string{"updated_at = NOW()"}
	args := []any{id}
	i := 2

	for k, v := range updates {
		switch k {
		case "metadata":
			data, _ := json.Marshal(v)
			sets = append(sets, fmt.Sprintf("metadata = $%d", i))
			args = append(args, data)
		case "compaction_count":
			sets = append(sets, fmt.Sprintf("compaction_count = $%d", i))
			args = append(args, v)
		default:
			continue
		}
		i++
	}

	query := fmt.Sprintf("UPDATE agentpg_sessions SET %s WHERE id = $1", joinStrings(sets, ", "))
	_, err := s.pool.Exec(ctx, query, args...)
	return err
}

// Agent operations

func (s *Store) UpsertAgent(ctx context.Context, agent *driver.AgentDefinition) error {
	config, _ := json.Marshal(agent.Config)
	_, err := s.pool.Exec(ctx, `
		INSERT INTO agentpg_agents (name, description, model, system_prompt, max_tokens, temperature, top_k, top_p, tool_names, config, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, NOW())
		ON CONFLICT (name) DO UPDATE SET
			description = EXCLUDED.description,
			model = EXCLUDED.model,
			system_prompt = EXCLUDED.system_prompt,
			max_tokens = EXCLUDED.max_tokens,
			temperature = EXCLUDED.temperature,
			top_k = EXCLUDED.top_k,
			top_p = EXCLUDED.top_p,
			tool_names = EXCLUDED.tool_names,
			config = EXCLUDED.config,
			updated_at = NOW()
	`, agent.Name, agent.Description, agent.Model, agent.SystemPrompt,
		agent.MaxTokens, agent.Temperature, agent.TopK, agent.TopP,
		agent.ToolNames, config)
	return err
}

func (s *Store) GetAgent(ctx context.Context, name string) (*driver.AgentDefinition, error) {
	var agent driver.AgentDefinition
	var config []byte
	err := s.pool.QueryRow(ctx, `
		SELECT name, description, model, system_prompt, max_tokens, temperature, top_k, top_p, tool_names, config, created_at, updated_at
		FROM agentpg_agents WHERE name = $1
	`, name).Scan(
		&agent.Name, &agent.Description, &agent.Model, &agent.SystemPrompt,
		&agent.MaxTokens, &agent.Temperature, &agent.TopK, &agent.TopP,
		&agent.ToolNames, &config, &agent.CreatedAt, &agent.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	json.Unmarshal(config, &agent.Config)
	return &agent, nil
}

func (s *Store) DeleteAgent(ctx context.Context, name string) error {
	_, err := s.pool.Exec(ctx, "DELETE FROM agentpg_agents WHERE name = $1", name)
	return err
}

func (s *Store) ListAgents(ctx context.Context) ([]*driver.AgentDefinition, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT name, description, model, system_prompt, max_tokens, temperature, top_k, top_p, tool_names, config, created_at, updated_at
		FROM agentpg_agents ORDER BY name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var agents []*driver.AgentDefinition
	for rows.Next() {
		var agent driver.AgentDefinition
		var config []byte
		if err := rows.Scan(
			&agent.Name, &agent.Description, &agent.Model, &agent.SystemPrompt,
			&agent.MaxTokens, &agent.Temperature, &agent.TopK, &agent.TopP,
			&agent.ToolNames, &config, &agent.CreatedAt, &agent.UpdatedAt,
		); err != nil {
			return nil, err
		}
		json.Unmarshal(config, &agent.Config)
		agents = append(agents, &agent)
	}
	return agents, rows.Err()
}

// Tool operations

func (s *Store) UpsertTool(ctx context.Context, tool *driver.ToolDefinition) error {
	inputSchema, _ := json.Marshal(tool.InputSchema)
	metadata, _ := json.Marshal(tool.Metadata)
	_, err := s.pool.Exec(ctx, `
		INSERT INTO agentpg_tools (name, description, input_schema, is_agent_tool, agent_name, metadata, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, NOW())
		ON CONFLICT (name) DO UPDATE SET
			description = EXCLUDED.description,
			input_schema = EXCLUDED.input_schema,
			is_agent_tool = EXCLUDED.is_agent_tool,
			agent_name = EXCLUDED.agent_name,
			metadata = EXCLUDED.metadata,
			updated_at = NOW()
	`, tool.Name, tool.Description, inputSchema, tool.IsAgentTool, tool.AgentName, metadata)
	return err
}

func (s *Store) GetTool(ctx context.Context, name string) (*driver.ToolDefinition, error) {
	var tool driver.ToolDefinition
	var inputSchema, metadata []byte
	err := s.pool.QueryRow(ctx, `
		SELECT name, description, input_schema, is_agent_tool, agent_name, metadata, created_at, updated_at
		FROM agentpg_tools WHERE name = $1
	`, name).Scan(
		&tool.Name, &tool.Description, &inputSchema, &tool.IsAgentTool,
		&tool.AgentName, &metadata, &tool.CreatedAt, &tool.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	json.Unmarshal(inputSchema, &tool.InputSchema)
	json.Unmarshal(metadata, &tool.Metadata)
	return &tool, nil
}

func (s *Store) DeleteTool(ctx context.Context, name string) error {
	_, err := s.pool.Exec(ctx, "DELETE FROM agentpg_tools WHERE name = $1", name)
	return err
}

func (s *Store) ListTools(ctx context.Context) ([]*driver.ToolDefinition, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT name, description, input_schema, is_agent_tool, agent_name, metadata, created_at, updated_at
		FROM agentpg_tools ORDER BY name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tools []*driver.ToolDefinition
	for rows.Next() {
		var tool driver.ToolDefinition
		var inputSchema, metadata []byte
		if err := rows.Scan(
			&tool.Name, &tool.Description, &inputSchema, &tool.IsAgentTool,
			&tool.AgentName, &metadata, &tool.CreatedAt, &tool.UpdatedAt,
		); err != nil {
			return nil, err
		}
		json.Unmarshal(inputSchema, &tool.InputSchema)
		json.Unmarshal(metadata, &tool.Metadata)
		tools = append(tools, &tool)
	}
	return tools, rows.Err()
}

// Run operations

func (s *Store) CreateRun(ctx context.Context, params driver.CreateRunParams) (*driver.Run, error) {
	return s.createRun(ctx, s.pool, params)
}

func (s *Store) CreateRunTx(ctx context.Context, tx pgx.Tx, params driver.CreateRunParams) (*driver.Run, error) {
	return s.createRun(ctx, tx, params)
}

func (s *Store) createRun(ctx context.Context, e executor, params driver.CreateRunParams) (*driver.Run, error) {
	var run driver.Run
	metadata, _ := json.Marshal(params.Metadata)

	// Default run_mode to "batch" if not specified
	runMode := params.RunMode
	if runMode == "" {
		runMode = "batch"
	}

	err := e.QueryRow(ctx, `
		INSERT INTO agentpg_runs (session_id, agent_name, prompt, run_mode, parent_run_id, parent_tool_execution_id, depth, created_by_instance_id, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id, session_id, agent_name, run_mode, parent_run_id, parent_tool_execution_id, depth, state, previous_state,
			prompt, current_iteration, current_iteration_id, response_text, stop_reason,
			input_tokens, output_tokens, cache_creation_input_tokens, cache_read_input_tokens,
			iteration_count, tool_iterations, error_message, error_type,
			created_by_instance_id, claimed_by_instance_id, claimed_at, metadata, created_at, started_at, finalized_at,
			rescue_attempts, last_rescue_at
	`, params.SessionID, params.AgentName, params.Prompt, runMode, params.ParentRunID,
		params.ParentToolExecutionID, params.Depth, params.CreatedByInstanceID, metadata).Scan(
		&run.ID, &run.SessionID, &run.AgentName, &run.RunMode, &run.ParentRunID, &run.ParentToolExecutionID, &run.Depth,
		&run.State, &run.PreviousState, &run.Prompt, &run.CurrentIteration, &run.CurrentIterationID,
		&run.ResponseText, &run.StopReason, &run.InputTokens, &run.OutputTokens,
		&run.CacheCreationInputTokens, &run.CacheReadInputTokens, &run.IterationCount, &run.ToolIterations,
		&run.ErrorMessage, &run.ErrorType, &run.CreatedByInstanceID, &run.ClaimedByInstanceID, &run.ClaimedAt,
		&metadata, &run.CreatedAt, &run.StartedAt, &run.FinalizedAt,
		&run.RescueAttempts, &run.LastRescueAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create run: %w", err)
	}
	json.Unmarshal(metadata, &run.Metadata)
	return &run, nil
}

func (s *Store) GetRun(ctx context.Context, id uuid.UUID) (*driver.Run, error) {
	var run driver.Run
	var metadata []byte
	err := s.pool.QueryRow(ctx, `
		SELECT id, session_id, agent_name, run_mode, parent_run_id, parent_tool_execution_id, depth, state, previous_state,
			prompt, current_iteration, current_iteration_id, response_text, stop_reason,
			input_tokens, output_tokens, cache_creation_input_tokens, cache_read_input_tokens,
			iteration_count, tool_iterations, error_message, error_type,
			created_by_instance_id, claimed_by_instance_id, claimed_at, metadata, created_at, started_at, finalized_at,
			rescue_attempts, last_rescue_at
		FROM agentpg_runs WHERE id = $1
	`, id).Scan(
		&run.ID, &run.SessionID, &run.AgentName, &run.RunMode, &run.ParentRunID, &run.ParentToolExecutionID, &run.Depth,
		&run.State, &run.PreviousState, &run.Prompt, &run.CurrentIteration, &run.CurrentIterationID,
		&run.ResponseText, &run.StopReason, &run.InputTokens, &run.OutputTokens,
		&run.CacheCreationInputTokens, &run.CacheReadInputTokens, &run.IterationCount, &run.ToolIterations,
		&run.ErrorMessage, &run.ErrorType, &run.CreatedByInstanceID, &run.ClaimedByInstanceID, &run.ClaimedAt,
		&metadata, &run.CreatedAt, &run.StartedAt, &run.FinalizedAt,
		&run.RescueAttempts, &run.LastRescueAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	json.Unmarshal(metadata, &run.Metadata)
	return &run, nil
}

func (s *Store) UpdateRun(ctx context.Context, id uuid.UUID, updates map[string]any) error {
	return s.updateRun(ctx, id, updates)
}

func (s *Store) UpdateRunState(ctx context.Context, id uuid.UUID, state driver.RunState, updates map[string]any) error {
	if updates == nil {
		updates = make(map[string]any)
	}
	updates["state"] = state
	return s.updateRun(ctx, id, updates)
}

func (s *Store) updateRun(ctx context.Context, id uuid.UUID, updates map[string]any) error {
	sets := []string{}
	args := []any{id}
	i := 2

	for k, v := range updates {
		sets = append(sets, fmt.Sprintf("%s = $%d", k, i))
		switch val := v.(type) {
		case map[string]any:
			data, _ := json.Marshal(val)
			args = append(args, data)
		default:
			args = append(args, v)
		}
		i++
	}

	if len(sets) == 0 {
		return nil
	}

	query := fmt.Sprintf("UPDATE agentpg_runs SET %s WHERE id = $1", joinStrings(sets, ", "))
	_, err := s.pool.Exec(ctx, query, args...)
	return err
}

func (s *Store) ClaimRuns(ctx context.Context, instanceID string, maxCount int, runMode string) ([]*driver.Run, error) {
	var rows pgx.Rows
	var err error

	// Call stored procedure with optional run mode filter
	if runMode == "" {
		// Claim any mode
		rows, err = s.pool.Query(ctx, "SELECT * FROM agentpg_claim_runs($1, $2)", instanceID, maxCount)
	} else {
		// Claim specific mode (batch or streaming)
		rows, err = s.pool.Query(ctx, "SELECT * FROM agentpg_claim_runs($1, $2, $3::agentpg_run_mode)", instanceID, maxCount, runMode)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to claim runs: %w", err)
	}
	defer rows.Close()

	return collectRuns(rows)
}

func (s *Store) GetRunsBySession(ctx context.Context, sessionID uuid.UUID, limit int) ([]*driver.Run, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, session_id, agent_name, run_mode, parent_run_id, parent_tool_execution_id, depth, state, previous_state,
			prompt, current_iteration, current_iteration_id, response_text, stop_reason,
			input_tokens, output_tokens, cache_creation_input_tokens, cache_read_input_tokens,
			iteration_count, tool_iterations, error_message, error_type,
			created_by_instance_id, claimed_by_instance_id, claimed_at, metadata, created_at, started_at, finalized_at,
			rescue_attempts, last_rescue_at
		FROM agentpg_runs WHERE session_id = $1 ORDER BY created_at DESC LIMIT $2
	`, sessionID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return collectRuns(rows)
}

func (s *Store) GetStuckPendingToolsRuns(ctx context.Context, limit int) ([]*driver.Run, error) {
	// Find runs that are in pending_tools state but have no pending tool executions
	// This catches runs where all tools completed but the notification was missed
	rows, err := s.pool.Query(ctx, `
		SELECT r.id, r.session_id, r.agent_name, r.run_mode, r.parent_run_id, r.parent_tool_execution_id, r.depth, r.state, r.previous_state,
			r.prompt, r.current_iteration, r.current_iteration_id, r.response_text, r.stop_reason,
			r.input_tokens, r.output_tokens, r.cache_creation_input_tokens, r.cache_read_input_tokens,
			r.iteration_count, r.tool_iterations, r.error_message, r.error_type,
			r.created_by_instance_id, r.claimed_by_instance_id, r.claimed_at, r.metadata, r.created_at, r.started_at, r.finalized_at,
			r.rescue_attempts, r.last_rescue_at
		FROM agentpg_runs r
		WHERE r.state = 'pending_tools'
		  AND r.current_iteration_id IS NOT NULL
		  AND NOT EXISTS (
			SELECT 1 FROM agentpg_tool_executions te
			WHERE te.iteration_id = r.current_iteration_id
			  AND te.state NOT IN ('completed', 'failed', 'skipped')
		  )
		ORDER BY r.created_at ASC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return collectRuns(rows)
}

func (s *Store) ListRuns(ctx context.Context, params driver.ListRunsParams) ([]*driver.Run, int, error) {
	// Build dynamic query with filters
	baseQuery := `
		SELECT r.id, r.session_id, r.agent_name, r.run_mode, r.parent_run_id, r.parent_tool_execution_id, r.depth, r.state, r.previous_state,
			r.prompt, r.current_iteration, r.current_iteration_id, r.response_text, r.stop_reason,
			r.input_tokens, r.output_tokens, r.cache_creation_input_tokens, r.cache_read_input_tokens,
			r.iteration_count, r.tool_iterations, r.error_message, r.error_type,
			r.created_by_instance_id, r.claimed_by_instance_id, r.claimed_at, r.metadata, r.created_at, r.started_at, r.finalized_at,
			r.rescue_attempts, r.last_rescue_at
		FROM agentpg_runs r`

	countQuery := "SELECT COUNT(*) FROM agentpg_runs r"

	var whereClauses []string
	var args []any
	argNum := 1

	// Join with sessions for tenant filtering
	if params.TenantID != "" {
		baseQuery = baseQuery[:len(baseQuery)] + " JOIN agentpg_sessions s ON r.session_id = s.id"
		countQuery = countQuery + " JOIN agentpg_sessions s ON r.session_id = s.id"
		whereClauses = append(whereClauses, fmt.Sprintf("s.tenant_id = $%d", argNum))
		args = append(args, params.TenantID)
		argNum++
	}

	if params.SessionID != nil {
		whereClauses = append(whereClauses, fmt.Sprintf("r.session_id = $%d", argNum))
		args = append(args, *params.SessionID)
		argNum++
	}

	if params.AgentName != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("r.agent_name = $%d", argNum))
		args = append(args, params.AgentName)
		argNum++
	}

	if params.State != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("r.state = $%d", argNum))
		args = append(args, params.State)
		argNum++
	}

	if params.RunMode != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("r.run_mode = $%d", argNum))
		args = append(args, params.RunMode)
		argNum++
	}

	whereClause := ""
	if len(whereClauses) > 0 {
		whereClause = " WHERE " + whereClauses[0]
		for i := 1; i < len(whereClauses); i++ {
			whereClause += " AND " + whereClauses[i]
		}
	}

	// Get total count
	var total int
	err := s.pool.QueryRow(ctx, countQuery+whereClause, args...).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count runs: %w", err)
	}

	// Add ordering and pagination
	limit := params.Limit
	if limit <= 0 {
		limit = 25
	}
	offset := params.Offset
	if offset < 0 {
		offset = 0
	}

	baseQuery += whereClause + fmt.Sprintf(" ORDER BY r.created_at DESC LIMIT $%d OFFSET $%d", argNum, argNum+1)
	args = append(args, limit, offset)

	rows, err := s.pool.Query(ctx, baseQuery, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list runs: %w", err)
	}
	defer rows.Close()

	runs, err := collectRuns(rows)
	if err != nil {
		return nil, 0, err
	}

	return runs, total, nil
}

// Iteration operations

func (s *Store) CreateIteration(ctx context.Context, params driver.CreateIterationParams) (*driver.Iteration, error) {
	var iter driver.Iteration
	err := s.pool.QueryRow(ctx, `
		INSERT INTO agentpg_iterations (run_id, iteration_number, is_streaming, trigger_type)
		VALUES ($1, $2, $3, $4)
		RETURNING id, run_id, iteration_number, is_streaming,
			batch_id, batch_request_id, batch_status,
			batch_submitted_at, batch_completed_at, batch_expires_at, batch_poll_count, batch_last_poll_at,
			streaming_started_at, streaming_completed_at,
			trigger_type, request_message_ids, stop_reason, response_message_id, has_tool_use, tool_execution_count,
			input_tokens, output_tokens, cache_creation_input_tokens, cache_read_input_tokens,
			error_message, error_type, created_at, started_at, completed_at
	`, params.RunID, params.IterationNumber, params.IsStreaming, params.TriggerType).Scan(
		&iter.ID, &iter.RunID, &iter.IterationNumber, &iter.IsStreaming,
		&iter.BatchID, &iter.BatchRequestID, &iter.BatchStatus,
		&iter.BatchSubmittedAt, &iter.BatchCompletedAt, &iter.BatchExpiresAt, &iter.BatchPollCount, &iter.BatchLastPollAt,
		&iter.StreamingStartedAt, &iter.StreamingCompletedAt,
		&iter.TriggerType, &iter.RequestMessageIDs, &iter.StopReason, &iter.ResponseMessageID, &iter.HasToolUse, &iter.ToolExecutionCount,
		&iter.InputTokens, &iter.OutputTokens, &iter.CacheCreationInputTokens, &iter.CacheReadInputTokens,
		&iter.ErrorMessage, &iter.ErrorType, &iter.CreatedAt, &iter.StartedAt, &iter.CompletedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create iteration: %w", err)
	}
	return &iter, nil
}

func (s *Store) GetIteration(ctx context.Context, id uuid.UUID) (*driver.Iteration, error) {
	var iter driver.Iteration
	err := s.pool.QueryRow(ctx, `
		SELECT id, run_id, iteration_number, is_streaming,
			batch_id, batch_request_id, batch_status,
			batch_submitted_at, batch_completed_at, batch_expires_at, batch_poll_count, batch_last_poll_at,
			streaming_started_at, streaming_completed_at,
			trigger_type, request_message_ids, stop_reason, response_message_id, has_tool_use, tool_execution_count,
			input_tokens, output_tokens, cache_creation_input_tokens, cache_read_input_tokens,
			error_message, error_type, created_at, started_at, completed_at
		FROM agentpg_iterations WHERE id = $1
	`, id).Scan(
		&iter.ID, &iter.RunID, &iter.IterationNumber, &iter.IsStreaming,
		&iter.BatchID, &iter.BatchRequestID, &iter.BatchStatus,
		&iter.BatchSubmittedAt, &iter.BatchCompletedAt, &iter.BatchExpiresAt, &iter.BatchPollCount, &iter.BatchLastPollAt,
		&iter.StreamingStartedAt, &iter.StreamingCompletedAt,
		&iter.TriggerType, &iter.RequestMessageIDs, &iter.StopReason, &iter.ResponseMessageID, &iter.HasToolUse, &iter.ToolExecutionCount,
		&iter.InputTokens, &iter.OutputTokens, &iter.CacheCreationInputTokens, &iter.CacheReadInputTokens,
		&iter.ErrorMessage, &iter.ErrorType, &iter.CreatedAt, &iter.StartedAt, &iter.CompletedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &iter, nil
}

func (s *Store) UpdateIteration(ctx context.Context, id uuid.UUID, updates map[string]any) error {
	sets := []string{}
	args := []any{id}
	i := 2

	for k, v := range updates {
		sets = append(sets, fmt.Sprintf("%s = $%d", k, i))
		args = append(args, v)
		i++
	}

	if len(sets) == 0 {
		return nil
	}

	query := fmt.Sprintf("UPDATE agentpg_iterations SET %s WHERE id = $1", joinStrings(sets, ", "))
	_, err := s.pool.Exec(ctx, query, args...)
	return err
}

func (s *Store) GetIterationsForPoll(ctx context.Context, instanceID string, pollInterval time.Duration, maxCount int) ([]*driver.Iteration, error) {
	rows, err := s.pool.Query(ctx, "SELECT * FROM agentpg_get_iterations_for_poll($1, $2, $3)",
		instanceID, pollInterval.String(), maxCount)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return collectIterations(rows)
}

func (s *Store) GetIterationsByRun(ctx context.Context, runID uuid.UUID) ([]*driver.Iteration, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, run_id, iteration_number, is_streaming,
			batch_id, batch_request_id, batch_status,
			batch_submitted_at, batch_completed_at, batch_expires_at, batch_poll_count, batch_last_poll_at,
			streaming_started_at, streaming_completed_at,
			trigger_type, request_message_ids, stop_reason, response_message_id, has_tool_use, tool_execution_count,
			input_tokens, output_tokens, cache_creation_input_tokens, cache_read_input_tokens,
			error_message, error_type, created_at, started_at, completed_at
		FROM agentpg_iterations WHERE run_id = $1 ORDER BY iteration_number
	`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return collectIterations(rows)
}

// Tool execution operations

func (s *Store) CreateToolExecution(ctx context.Context, params driver.CreateToolExecutionParams) (*driver.ToolExecution, error) {
	var exec driver.ToolExecution
	maxAttempts := params.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 2 // Default: 2 attempts (1 retry) for snappy UX
	}

	err := s.pool.QueryRow(ctx, `
		INSERT INTO agentpg_tool_executions (run_id, iteration_id, tool_use_id, tool_name, tool_input, is_agent_tool, agent_name, max_attempts)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, run_id, iteration_id, state, tool_use_id, tool_name, tool_input, is_agent_tool, agent_name, child_run_id,
			tool_output, is_error, error_message, claimed_by_instance_id, claimed_at, attempt_count, max_attempts,
			scheduled_at, snooze_count, last_error, created_at, started_at, completed_at
	`, params.RunID, params.IterationID, params.ToolUseID, params.ToolName, params.ToolInput,
		params.IsAgentTool, params.AgentName, maxAttempts).Scan(
		&exec.ID, &exec.RunID, &exec.IterationID, &exec.State, &exec.ToolUseID, &exec.ToolName, &exec.ToolInput,
		&exec.IsAgentTool, &exec.AgentName, &exec.ChildRunID, &exec.ToolOutput, &exec.IsError, &exec.ErrorMessage,
		&exec.ClaimedByInstanceID, &exec.ClaimedAt, &exec.AttemptCount, &exec.MaxAttempts,
		&exec.ScheduledAt, &exec.SnoozeCount, &exec.LastError, &exec.CreatedAt, &exec.StartedAt, &exec.CompletedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create tool execution: %w", err)
	}
	return &exec, nil
}

func (s *Store) CreateToolExecutions(ctx context.Context, params []driver.CreateToolExecutionParams) ([]*driver.ToolExecution, error) {
	var execs []*driver.ToolExecution
	for _, p := range params {
		exec, err := s.CreateToolExecution(ctx, p)
		if err != nil {
			return nil, err
		}
		execs = append(execs, exec)
	}
	return execs, nil
}

func (s *Store) CreateToolExecutionsAndUpdateRunState(ctx context.Context, params []driver.CreateToolExecutionParams, runID uuid.UUID, state driver.RunState, runUpdates map[string]any) ([]*driver.ToolExecution, error) {
	// Convert params to JSONB format expected by stored procedure
	type toolParam struct {
		RunID       string          `json:"run_id"`
		IterationID string          `json:"iteration_id"`
		ToolUseID   string          `json:"tool_use_id"`
		ToolName    string          `json:"tool_name"`
		ToolInput   json.RawMessage `json:"tool_input"`
		IsAgentTool bool            `json:"is_agent_tool"`
		AgentName   *string         `json:"agent_name"`
		MaxAttempts int             `json:"max_attempts"`
	}

	toolParams := make([]toolParam, len(params))
	for i, p := range params {
		maxAttempts := p.MaxAttempts
		if maxAttempts <= 0 {
			maxAttempts = 2
		}
		toolParams[i] = toolParam{
			RunID:       p.RunID.String(),
			IterationID: p.IterationID.String(),
			ToolUseID:   p.ToolUseID,
			ToolName:    p.ToolName,
			ToolInput:   p.ToolInput,
			IsAgentTool: p.IsAgentTool,
			AgentName:   p.AgentName,
			MaxAttempts: maxAttempts,
		}
	}

	toolParamsJSON, err := json.Marshal(toolParams)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal tool params: %w", err)
	}

	runUpdatesJSON, err := json.Marshal(runUpdates)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal run updates: %w", err)
	}

	rows, err := s.pool.Query(ctx,
		"SELECT * FROM agentpg_create_tool_executions_and_update_run($1, $2, $3::agentpg_run_state, $4)",
		toolParamsJSON, runID, string(state), runUpdatesJSON)
	if err != nil {
		return nil, fmt.Errorf("failed to create tool executions and update run: %w", err)
	}
	defer rows.Close()

	return collectToolExecutions(rows)
}

func (s *Store) GetToolExecution(ctx context.Context, id uuid.UUID) (*driver.ToolExecution, error) {
	var exec driver.ToolExecution
	err := s.pool.QueryRow(ctx, `
		SELECT id, run_id, iteration_id, state, tool_use_id, tool_name, tool_input, is_agent_tool, agent_name, child_run_id,
			tool_output, is_error, error_message, claimed_by_instance_id, claimed_at, attempt_count, max_attempts,
			scheduled_at, snooze_count, last_error, created_at, started_at, completed_at
		FROM agentpg_tool_executions WHERE id = $1
	`, id).Scan(
		&exec.ID, &exec.RunID, &exec.IterationID, &exec.State, &exec.ToolUseID, &exec.ToolName, &exec.ToolInput,
		&exec.IsAgentTool, &exec.AgentName, &exec.ChildRunID, &exec.ToolOutput, &exec.IsError, &exec.ErrorMessage,
		&exec.ClaimedByInstanceID, &exec.ClaimedAt, &exec.AttemptCount, &exec.MaxAttempts,
		&exec.ScheduledAt, &exec.SnoozeCount, &exec.LastError, &exec.CreatedAt, &exec.StartedAt, &exec.CompletedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &exec, nil
}

func (s *Store) UpdateToolExecution(ctx context.Context, id uuid.UUID, updates map[string]any) error {
	sets := []string{}
	args := []any{id}
	i := 2

	for k, v := range updates {
		sets = append(sets, fmt.Sprintf("%s = $%d", k, i))
		args = append(args, v)
		i++
	}

	if len(sets) == 0 {
		return nil
	}

	query := fmt.Sprintf("UPDATE agentpg_tool_executions SET %s WHERE id = $1", joinStrings(sets, ", "))
	_, err := s.pool.Exec(ctx, query, args...)
	return err
}

func (s *Store) ClaimToolExecutions(ctx context.Context, instanceID string, maxCount int) ([]*driver.ToolExecution, error) {
	rows, err := s.pool.Query(ctx, "SELECT * FROM agentpg_claim_tool_executions($1, $2)", instanceID, maxCount)
	if err != nil {
		return nil, fmt.Errorf("failed to claim tool executions: %w", err)
	}
	defer rows.Close()

	return collectToolExecutions(rows)
}

func (s *Store) CompleteToolExecution(ctx context.Context, id uuid.UUID, output string, isError bool, errorMsg string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE agentpg_tool_executions
		SET state = CASE WHEN $3 THEN 'failed'::agentpg_tool_execution_state ELSE 'completed'::agentpg_tool_execution_state END,
			tool_output = $2,
			is_error = $3,
			error_message = NULLIF($4, ''),
			completed_at = NOW()
		WHERE id = $1
	`, id, output, isError, errorMsg)
	return err
}

func (s *Store) GetToolExecutionsByRun(ctx context.Context, runID uuid.UUID) ([]*driver.ToolExecution, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, run_id, iteration_id, state, tool_use_id, tool_name, tool_input, is_agent_tool, agent_name, child_run_id,
			tool_output, is_error, error_message, claimed_by_instance_id, claimed_at, attempt_count, max_attempts,
			scheduled_at, snooze_count, last_error, created_at, started_at, completed_at
		FROM agentpg_tool_executions WHERE run_id = $1 ORDER BY created_at
	`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return collectToolExecutions(rows)
}

func (s *Store) GetToolExecutionsByIteration(ctx context.Context, iterationID uuid.UUID) ([]*driver.ToolExecution, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, run_id, iteration_id, state, tool_use_id, tool_name, tool_input, is_agent_tool, agent_name, child_run_id,
			tool_output, is_error, error_message, claimed_by_instance_id, claimed_at, attempt_count, max_attempts,
			scheduled_at, snooze_count, last_error, created_at, started_at, completed_at
		FROM agentpg_tool_executions WHERE iteration_id = $1 ORDER BY created_at
	`, iterationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return collectToolExecutions(rows)
}

func (s *Store) GetPendingToolExecutionsForRun(ctx context.Context, runID uuid.UUID) ([]*driver.ToolExecution, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, run_id, iteration_id, state, tool_use_id, tool_name, tool_input, is_agent_tool, agent_name, child_run_id,
			tool_output, is_error, error_message, claimed_by_instance_id, claimed_at, attempt_count, max_attempts,
			scheduled_at, snooze_count, last_error, created_at, started_at, completed_at
		FROM agentpg_tool_executions WHERE run_id = $1 AND state NOT IN ('completed', 'failed', 'skipped')
	`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return collectToolExecutions(rows)
}

func (s *Store) ListToolExecutions(ctx context.Context, params driver.ListToolExecutionsParams) ([]*driver.ToolExecution, int, error) {
	// Build dynamic query with filters
	baseQuery := `
		SELECT id, run_id, iteration_id, state, tool_use_id, tool_name, tool_input, is_agent_tool, agent_name, child_run_id,
			tool_output, is_error, error_message, claimed_by_instance_id, claimed_at, attempt_count, max_attempts,
			scheduled_at, snooze_count, last_error, created_at, started_at, completed_at
		FROM agentpg_tool_executions`

	countQuery := "SELECT COUNT(*) FROM agentpg_tool_executions"

	var whereClauses []string
	var args []any
	argNum := 1

	if params.RunID != nil {
		whereClauses = append(whereClauses, fmt.Sprintf("run_id = $%d", argNum))
		args = append(args, *params.RunID)
		argNum++
	}

	if params.IterationID != nil {
		whereClauses = append(whereClauses, fmt.Sprintf("iteration_id = $%d", argNum))
		args = append(args, *params.IterationID)
		argNum++
	}

	if params.ToolName != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("tool_name = $%d", argNum))
		args = append(args, params.ToolName)
		argNum++
	}

	if params.State != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("state = $%d", argNum))
		args = append(args, params.State)
		argNum++
	}

	if params.IsAgentTool != nil {
		whereClauses = append(whereClauses, fmt.Sprintf("is_agent_tool = $%d", argNum))
		args = append(args, *params.IsAgentTool)
		argNum++
	}

	whereClause := ""
	if len(whereClauses) > 0 {
		whereClause = " WHERE " + whereClauses[0]
		for i := 1; i < len(whereClauses); i++ {
			whereClause += " AND " + whereClauses[i]
		}
	}

	// Get total count
	var total int
	err := s.pool.QueryRow(ctx, countQuery+whereClause, args...).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count tool executions: %w", err)
	}

	// Add ordering and pagination
	limit := params.Limit
	if limit <= 0 {
		limit = 25
	}
	offset := params.Offset
	if offset < 0 {
		offset = 0
	}

	baseQuery += whereClause + fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d OFFSET $%d", argNum, argNum+1)
	args = append(args, limit, offset)

	rows, err := s.pool.Query(ctx, baseQuery, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list tool executions: %w", err)
	}
	defer rows.Close()

	executions, err := collectToolExecutions(rows)
	if err != nil {
		return nil, 0, err
	}

	return executions, total, nil
}

// Tool execution retry operations

func (s *Store) RetryToolExecution(ctx context.Context, id uuid.UUID, scheduledAt time.Time, lastError string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE agentpg_tool_executions
		SET state = 'pending'::agentpg_tool_execution_state,
			claimed_by_instance_id = NULL,
			claimed_at = NULL,
			started_at = NULL,
			scheduled_at = $2,
			last_error = $3
		WHERE id = $1
	`, id, scheduledAt, lastError)
	return err
}

func (s *Store) SnoozeToolExecution(ctx context.Context, id uuid.UUID, scheduledAt time.Time) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE agentpg_tool_executions
		SET state = 'pending'::agentpg_tool_execution_state,
			claimed_by_instance_id = NULL,
			claimed_at = NULL,
			started_at = NULL,
			scheduled_at = $2,
			attempt_count = GREATEST(0, attempt_count - 1),
			snooze_count = snooze_count + 1
		WHERE id = $1
	`, id, scheduledAt)
	return err
}

func (s *Store) DiscardToolExecution(ctx context.Context, id uuid.UUID, errorMsg string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE agentpg_tool_executions
		SET state = 'failed'::agentpg_tool_execution_state,
			is_error = TRUE,
			error_message = $2,
			completed_at = NOW()
		WHERE id = $1
	`, id, errorMsg)
	return err
}

func (s *Store) CompleteToolsAndContinueRun(ctx context.Context, sessionID, runID uuid.UUID, contentBlocks []driver.ContentBlock) (*driver.Message, error) {
	// Convert content blocks to JSONB format expected by stored procedure
	type contentBlock struct {
		Type               string `json:"type"`
		ToolResultForUseID string `json:"tool_result_for_use_id"`
		ToolContent        string `json:"tool_content"`
		IsError            bool   `json:"is_error"`
	}

	blocks := make([]contentBlock, len(contentBlocks))
	for i, b := range contentBlocks {
		blocks[i] = contentBlock{
			Type:               b.Type,
			ToolResultForUseID: b.ToolResultForUseID,
			ToolContent:        b.ToolContent,
			IsError:            b.IsError,
		}
	}

	blocksJSON, err := json.Marshal(blocks)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal content blocks: %w", err)
	}

	var msg driver.Message
	var usage, metadata []byte

	err = s.pool.QueryRow(ctx,
		"SELECT * FROM agentpg_complete_tools_and_continue_run($1, $2, $3)",
		sessionID, runID, blocksJSON,
	).Scan(
		&msg.ID, &msg.SessionID, &msg.RunID, &msg.Role,
		&usage, &msg.IsPreserved, &msg.IsSummary, &metadata,
		&msg.CreatedAt, &msg.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to complete tools and continue run: %w", err)
	}

	json.Unmarshal(usage, &msg.Usage)
	json.Unmarshal(metadata, &msg.Metadata)
	msg.Content = contentBlocks

	return &msg, nil
}

// Run rescue operations

func (s *Store) GetStuckRuns(ctx context.Context, timeout time.Duration, maxRescueAttempts, limit int) ([]*driver.Run, error) {
	rows, err := s.pool.Query(ctx, "SELECT * FROM agentpg_get_stuck_runs($1, $2, $3)", timeout, maxRescueAttempts, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to get stuck runs: %w", err)
	}
	defer rows.Close()

	return collectRuns(rows)
}

func (s *Store) RescueRun(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE agentpg_runs
		SET state = 'pending'::agentpg_run_state,
			claimed_by_instance_id = NULL,
			claimed_at = NULL,
			started_at = NULL,
			rescue_attempts = rescue_attempts + 1,
			last_rescue_at = NOW()
		WHERE id = $1
	`, id)
	return err
}

// Message operations

func (s *Store) CreateMessage(ctx context.Context, params driver.CreateMessageParams) (*driver.Message, error) {
	var msg driver.Message
	usage, _ := json.Marshal(params.Usage)
	metadata, _ := json.Marshal(params.Metadata)

	err := s.pool.QueryRow(ctx, `
		INSERT INTO agentpg_messages (session_id, run_id, role, usage, is_preserved, is_summary, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, session_id, run_id, role, usage, is_preserved, is_summary, metadata, created_at, updated_at
	`, params.SessionID, params.RunID, params.Role, usage, params.IsPreserved, params.IsSummary, metadata).Scan(
		&msg.ID, &msg.SessionID, &msg.RunID, &msg.Role, &usage, &msg.IsPreserved, &msg.IsSummary, &metadata, &msg.CreatedAt, &msg.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create message: %w", err)
	}
	json.Unmarshal(usage, &msg.Usage)
	json.Unmarshal(metadata, &msg.Metadata)

	// Create content blocks
	if err := s.CreateContentBlocks(ctx, msg.ID, params.Content); err != nil {
		return nil, err
	}
	msg.Content = params.Content

	return &msg, nil
}

func (s *Store) GetMessage(ctx context.Context, id uuid.UUID) (*driver.Message, error) {
	var msg driver.Message
	var usage, metadata []byte
	err := s.pool.QueryRow(ctx, `
		SELECT id, session_id, run_id, role, usage, is_preserved, is_summary, metadata, created_at, updated_at
		FROM agentpg_messages WHERE id = $1
	`, id).Scan(
		&msg.ID, &msg.SessionID, &msg.RunID, &msg.Role, &usage, &msg.IsPreserved, &msg.IsSummary, &metadata, &msg.CreatedAt, &msg.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	json.Unmarshal(usage, &msg.Usage)
	json.Unmarshal(metadata, &msg.Metadata)

	// Get content blocks
	blocks, err := s.GetContentBlocks(ctx, msg.ID)
	if err != nil {
		return nil, err
	}
	msg.Content = blocks

	return &msg, nil
}

func (s *Store) GetMessages(ctx context.Context, sessionID uuid.UUID, limit int) ([]*driver.Message, error) {
	query := `SELECT id, session_id, run_id, role, usage, is_preserved, is_summary, metadata, created_at, updated_at
		FROM agentpg_messages WHERE session_id = $1 ORDER BY created_at`
	var rows pgx.Rows
	var err error
	if limit > 0 {
		rows, err = s.pool.Query(ctx, query+" LIMIT $2", sessionID, limit)
	} else {
		rows, err = s.pool.Query(ctx, query, sessionID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []*driver.Message
	for rows.Next() {
		var msg driver.Message
		var usage, metadata []byte
		if err := rows.Scan(
			&msg.ID, &msg.SessionID, &msg.RunID, &msg.Role, &usage, &msg.IsPreserved, &msg.IsSummary, &metadata, &msg.CreatedAt, &msg.UpdatedAt,
		); err != nil {
			return nil, err
		}
		json.Unmarshal(usage, &msg.Usage)
		json.Unmarshal(metadata, &msg.Metadata)

		// Get content blocks
		blocks, err := s.GetContentBlocks(ctx, msg.ID)
		if err != nil {
			return nil, err
		}
		msg.Content = blocks

		messages = append(messages, &msg)
	}
	return messages, rows.Err()
}

func (s *Store) GetMessagesByRun(ctx context.Context, runID uuid.UUID) ([]*driver.Message, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, session_id, run_id, role, usage, is_preserved, is_summary, metadata, created_at, updated_at
		FROM agentpg_messages WHERE run_id = $1 ORDER BY created_at
	`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []*driver.Message
	for rows.Next() {
		var msg driver.Message
		var usage, metadata []byte
		if err := rows.Scan(
			&msg.ID, &msg.SessionID, &msg.RunID, &msg.Role, &usage, &msg.IsPreserved, &msg.IsSummary, &metadata, &msg.CreatedAt, &msg.UpdatedAt,
		); err != nil {
			return nil, err
		}
		json.Unmarshal(usage, &msg.Usage)
		json.Unmarshal(metadata, &msg.Metadata)

		// Get content blocks
		blocks, err := s.GetContentBlocks(ctx, msg.ID)
		if err != nil {
			return nil, err
		}
		msg.Content = blocks

		messages = append(messages, &msg)
	}
	return messages, rows.Err()
}

func (s *Store) GetMessagesForRunContext(ctx context.Context, runID uuid.UUID) ([]*driver.Message, error) {
	// Get the run to determine session and depth
	var sessionID uuid.UUID
	var depth int
	err := s.pool.QueryRow(ctx, `
		SELECT session_id, depth FROM agentpg_runs WHERE id = $1
	`, runID).Scan(&sessionID, &depth)
	if err != nil {
		return nil, fmt.Errorf("failed to get run: %w", err)
	}

	if depth > 0 {
		// Nested run: only this run's messages
		return s.GetMessagesByRun(ctx, runID)
	}

	// Root run: get messages from all root-level runs in session
	// This excludes messages from child runs (agent-as-tool invocations)
	rows, err := s.pool.Query(ctx, `
		SELECT m.id, m.session_id, m.run_id, m.role, m.usage,
		       m.is_preserved, m.is_summary, m.metadata, m.created_at, m.updated_at
		FROM agentpg_messages m
		WHERE m.session_id = $1
		  AND (
		      m.run_id IS NULL
		      OR m.run_id IN (
		          SELECT r.id FROM agentpg_runs r
		          WHERE r.session_id = $1 AND r.depth = 0
		      )
		  )
		ORDER BY m.created_at
	`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []*driver.Message
	for rows.Next() {
		var msg driver.Message
		var usage, metadata []byte
		if err := rows.Scan(
			&msg.ID, &msg.SessionID, &msg.RunID, &msg.Role, &usage, &msg.IsPreserved, &msg.IsSummary, &metadata, &msg.CreatedAt, &msg.UpdatedAt,
		); err != nil {
			return nil, err
		}
		json.Unmarshal(usage, &msg.Usage)
		json.Unmarshal(metadata, &msg.Metadata)

		// Get content blocks
		blocks, err := s.GetContentBlocks(ctx, msg.ID)
		if err != nil {
			return nil, err
		}
		msg.Content = blocks

		messages = append(messages, &msg)
	}
	return messages, rows.Err()
}

func (s *Store) UpdateMessage(ctx context.Context, id uuid.UUID, updates map[string]any) error {
	sets := []string{"updated_at = NOW()"}
	args := []any{id}
	i := 2

	for k, v := range updates {
		sets = append(sets, fmt.Sprintf("%s = $%d", k, i))
		switch val := v.(type) {
		case map[string]any:
			data, _ := json.Marshal(val)
			args = append(args, data)
		default:
			args = append(args, v)
		}
		i++
	}

	query := fmt.Sprintf("UPDATE agentpg_messages SET %s WHERE id = $1", joinStrings(sets, ", "))
	_, err := s.pool.Exec(ctx, query, args...)
	return err
}

func (s *Store) DeleteMessage(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx, "DELETE FROM agentpg_messages WHERE id = $1", id)
	return err
}

// Content block operations

func (s *Store) CreateContentBlocks(ctx context.Context, messageID uuid.UUID, blocks []driver.ContentBlock) error {
	for i, block := range blocks {
		metadata, _ := json.Marshal(block.Metadata)
		_, err := s.pool.Exec(ctx, `
			INSERT INTO agentpg_content_blocks (message_id, block_index, type, text, tool_use_id, tool_name, tool_input,
				tool_result_for_use_id, tool_content, is_error, source, search_results, metadata)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		`, messageID, i, block.Type, nullIfEmpty(block.Text), nullIfEmpty(block.ToolUseID), nullIfEmpty(block.ToolName),
			nullIfEmptyBytes(block.ToolInput), nullIfEmpty(block.ToolResultForUseID), nullIfEmpty(block.ToolContent),
			block.IsError, nullIfEmptyBytes(block.Source), nullIfEmptyBytes(block.SearchResults), metadata)
		if err != nil {
			return fmt.Errorf("failed to create content block: %w", err)
		}
	}
	return nil
}

func (s *Store) GetContentBlocks(ctx context.Context, messageID uuid.UUID) ([]driver.ContentBlock, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT type, text, tool_use_id, tool_name, tool_input, tool_result_for_use_id, tool_content, is_error, source, search_results, metadata
		FROM agentpg_content_blocks WHERE message_id = $1 ORDER BY block_index
	`, messageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var blocks []driver.ContentBlock
	for rows.Next() {
		var block driver.ContentBlock
		var text, toolUseID, toolName, toolResultForUseID, toolContent *string
		var toolInput, source, searchResults, metadata []byte
		if err := rows.Scan(
			&block.Type, &text, &toolUseID, &toolName, &toolInput,
			&toolResultForUseID, &toolContent, &block.IsError, &source, &searchResults, &metadata,
		); err != nil {
			return nil, err
		}
		if text != nil {
			block.Text = *text
		}
		if toolUseID != nil {
			block.ToolUseID = *toolUseID
		}
		if toolName != nil {
			block.ToolName = *toolName
		}
		block.ToolInput = toolInput
		if toolResultForUseID != nil {
			block.ToolResultForUseID = *toolResultForUseID
		}
		if toolContent != nil {
			block.ToolContent = *toolContent
		}
		block.Source = source
		block.SearchResults = searchResults
		json.Unmarshal(metadata, &block.Metadata)
		blocks = append(blocks, block)
	}
	return blocks, rows.Err()
}

// Instance operations

func (s *Store) RegisterInstance(ctx context.Context, params driver.RegisterInstanceParams) error {
	metadata, _ := json.Marshal(params.Metadata)
	_, err := s.pool.Exec(ctx, `
		INSERT INTO agentpg_instances (id, name, hostname, pid, version, max_concurrent_runs, max_concurrent_tools, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (id) DO UPDATE SET
			name = EXCLUDED.name,
			hostname = EXCLUDED.hostname,
			pid = EXCLUDED.pid,
			version = EXCLUDED.version,
			max_concurrent_runs = EXCLUDED.max_concurrent_runs,
			max_concurrent_tools = EXCLUDED.max_concurrent_tools,
			metadata = EXCLUDED.metadata,
			last_heartbeat_at = NOW()
	`, params.ID, params.Name, params.Hostname, params.PID, params.Version,
		params.MaxConcurrentRuns, params.MaxConcurrentTools, metadata)
	return err
}

func (s *Store) UnregisterInstance(ctx context.Context, instanceID string) error {
	_, err := s.pool.Exec(ctx, "DELETE FROM agentpg_instances WHERE id = $1", instanceID)
	return err
}

func (s *Store) UpdateHeartbeat(ctx context.Context, instanceID string) error {
	result, err := s.pool.Exec(ctx, "UPDATE agentpg_instances SET last_heartbeat_at = NOW() WHERE id = $1", instanceID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("instance not found: %s", instanceID)
	}
	return nil
}

func (s *Store) GetInstance(ctx context.Context, instanceID string) (*driver.Instance, error) {
	var inst driver.Instance
	var metadata []byte
	err := s.pool.QueryRow(ctx, `
		SELECT id, name, hostname, pid, version, max_concurrent_runs, max_concurrent_tools,
			metadata, created_at, last_heartbeat_at
		FROM agentpg_instances WHERE id = $1
	`, instanceID).Scan(
		&inst.ID, &inst.Name, &inst.Hostname, &inst.PID, &inst.Version,
		&inst.MaxConcurrentRuns, &inst.MaxConcurrentTools,
		&metadata, &inst.CreatedAt, &inst.LastHeartbeatAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	json.Unmarshal(metadata, &inst.Metadata)
	// ActiveRunCount and ActiveToolCount are calculated on-the-fly via GetInstanceActiveCounts
	return &inst, nil
}

func (s *Store) ListInstances(ctx context.Context) ([]*driver.Instance, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, name, hostname, pid, version, max_concurrent_runs, max_concurrent_tools,
			metadata, created_at, last_heartbeat_at
		FROM agentpg_instances ORDER BY created_at
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var instances []*driver.Instance
	for rows.Next() {
		var inst driver.Instance
		var metadata []byte
		if err := rows.Scan(
			&inst.ID, &inst.Name, &inst.Hostname, &inst.PID, &inst.Version,
			&inst.MaxConcurrentRuns, &inst.MaxConcurrentTools,
			&metadata, &inst.CreatedAt, &inst.LastHeartbeatAt,
		); err != nil {
			return nil, err
		}
		json.Unmarshal(metadata, &inst.Metadata)
		// ActiveRunCount and ActiveToolCount are calculated on-the-fly via GetAllInstanceActiveCounts
		instances = append(instances, &inst)
	}
	return instances, rows.Err()
}

func (s *Store) GetStaleInstances(ctx context.Context, ttl time.Duration) ([]string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id FROM agentpg_instances WHERE last_heartbeat_at < NOW() - $1::interval
	`, ttl.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *Store) DeleteStaleInstances(ctx context.Context, ttl time.Duration) (int, error) {
	result, err := s.pool.Exec(ctx, `
		DELETE FROM agentpg_instances WHERE last_heartbeat_at < NOW() - $1::interval
	`, ttl.String())
	if err != nil {
		return 0, err
	}
	return int(result.RowsAffected()), nil
}

// GetInstanceActiveCounts returns the active run and tool counts for an instance.
// Counts are calculated on-the-fly by querying runs and tool_executions tables.
func (s *Store) GetInstanceActiveCounts(ctx context.Context, instanceID string) (activeRuns, activeTools int, err error) {
	// Count active runs claimed by this instance
	err = s.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM agentpg_runs
		WHERE claimed_by_instance_id = $1
		  AND state NOT IN ('completed', 'cancelled', 'failed')
	`, instanceID).Scan(&activeRuns)
	if err != nil {
		return 0, 0, err
	}

	// Count active tool executions claimed by this instance
	err = s.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM agentpg_tool_executions
		WHERE claimed_by_instance_id = $1
		  AND state = 'running'
	`, instanceID).Scan(&activeTools)
	if err != nil {
		return 0, 0, err
	}

	return activeRuns, activeTools, nil
}

// GetAllInstanceActiveCounts returns the active run and tool counts for all instances.
// Returns a map of instance ID to [activeRuns, activeTools].
func (s *Store) GetAllInstanceActiveCounts(ctx context.Context) (map[string][2]int, error) {
	result := make(map[string][2]int)

	// Get active run counts by instance
	rows, err := s.pool.Query(ctx, `
		SELECT claimed_by_instance_id, COUNT(*)
		FROM agentpg_runs
		WHERE claimed_by_instance_id IS NOT NULL
		  AND state NOT IN ('completed', 'cancelled', 'failed')
		GROUP BY claimed_by_instance_id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var instanceID string
		var count int
		if err := rows.Scan(&instanceID, &count); err != nil {
			return nil, err
		}
		counts := result[instanceID]
		counts[0] = count
		result[instanceID] = counts
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Get active tool counts by instance
	rows, err = s.pool.Query(ctx, `
		SELECT claimed_by_instance_id, COUNT(*)
		FROM agentpg_tool_executions
		WHERE claimed_by_instance_id IS NOT NULL
		  AND state = 'running'
		GROUP BY claimed_by_instance_id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var instanceID string
		var count int
		if err := rows.Scan(&instanceID, &count); err != nil {
			return nil, err
		}
		counts := result[instanceID]
		counts[1] = count
		result[instanceID] = counts
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return result, nil
}

// Instance capability operations

func (s *Store) RegisterInstanceAgent(ctx context.Context, instanceID, agentName string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO agentpg_instance_agents (instance_id, agent_name)
		VALUES ($1, $2)
		ON CONFLICT (instance_id, agent_name) DO NOTHING
	`, instanceID, agentName)
	return err
}

func (s *Store) RegisterInstanceTool(ctx context.Context, instanceID, toolName string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO agentpg_instance_tools (instance_id, tool_name)
		VALUES ($1, $2)
		ON CONFLICT (instance_id, tool_name) DO NOTHING
	`, instanceID, toolName)
	return err
}

func (s *Store) UnregisterInstanceAgent(ctx context.Context, instanceID, agentName string) error {
	_, err := s.pool.Exec(ctx, `
		DELETE FROM agentpg_instance_agents WHERE instance_id = $1 AND agent_name = $2
	`, instanceID, agentName)
	return err
}

func (s *Store) UnregisterInstanceTool(ctx context.Context, instanceID, toolName string) error {
	_, err := s.pool.Exec(ctx, `
		DELETE FROM agentpg_instance_tools WHERE instance_id = $1 AND tool_name = $2
	`, instanceID, toolName)
	return err
}

func (s *Store) GetInstanceAgents(ctx context.Context, instanceID string) ([]string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT agent_name FROM agentpg_instance_agents WHERE instance_id = $1
	`, instanceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

func (s *Store) GetInstanceTools(ctx context.Context, instanceID string) ([]string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT tool_name FROM agentpg_instance_tools WHERE instance_id = $1
	`, instanceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

// Leader election

func (s *Store) TryAcquireLeader(ctx context.Context, instanceID string, ttl time.Duration) (bool, error) {
	result, err := s.pool.Exec(ctx, `
		INSERT INTO agentpg_leader (leader_id, elected_at, expires_at)
		VALUES ($1, NOW(), NOW() + $2::interval)
		ON CONFLICT (name) DO UPDATE SET
			leader_id = $1,
			elected_at = NOW(),
			expires_at = NOW() + $2::interval
		WHERE agentpg_leader.expires_at < NOW()
	`, instanceID, ttl.String())
	if err != nil {
		return false, err
	}
	return result.RowsAffected() > 0, nil
}

func (s *Store) RefreshLeader(ctx context.Context, instanceID string, ttl time.Duration) error {
	result, err := s.pool.Exec(ctx, `
		UPDATE agentpg_leader SET expires_at = NOW() + $2::interval WHERE leader_id = $1
	`, instanceID, ttl.String())
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("not the leader")
	}
	return nil
}

func (s *Store) GetLeader(ctx context.Context) (string, error) {
	var leaderID string
	err := s.pool.QueryRow(ctx, `
		SELECT leader_id FROM agentpg_leader WHERE expires_at > NOW()
	`).Scan(&leaderID)
	if err == pgx.ErrNoRows {
		return "", nil
	}
	return leaderID, err
}

func (s *Store) IsLeader(ctx context.Context, instanceID string) (bool, error) {
	leader, err := s.GetLeader(ctx)
	if err != nil {
		return false, err
	}
	return leader == instanceID, nil
}

func (s *Store) ReleaseLeader(ctx context.Context, instanceID string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM agentpg_leader WHERE leader_id = $1`, instanceID)
	return err
}

// Compaction operations

func (s *Store) CreateCompactionEvent(ctx context.Context, params driver.CreateCompactionEventParams) (*driver.CompactionEvent, error) {
	var event driver.CompactionEvent
	preservedIDs, _ := json.Marshal(params.PreservedMessageIDs)

	err := s.pool.QueryRow(ctx, `
		INSERT INTO agentpg_compaction_events (session_id, strategy, original_tokens, compacted_tokens, messages_removed, summary_content, preserved_message_ids, model_used, duration_ms)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id, session_id, strategy, original_tokens, compacted_tokens, messages_removed, summary_content, preserved_message_ids, model_used, duration_ms, created_at
	`, params.SessionID, params.Strategy, params.OriginalTokens, params.CompactedTokens,
		params.MessagesRemoved, params.SummaryContent, preservedIDs, params.ModelUsed, params.DurationMS).Scan(
		&event.ID, &event.SessionID, &event.Strategy, &event.OriginalTokens, &event.CompactedTokens,
		&event.MessagesRemoved, &event.SummaryContent, &preservedIDs, &event.ModelUsed, &event.DurationMS, &event.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create compaction event: %w", err)
	}
	json.Unmarshal(preservedIDs, &event.PreservedMessageIDs)
	return &event, nil
}

func (s *Store) ArchiveMessage(ctx context.Context, compactionEventID, messageID, sessionID uuid.UUID, originalMessage map[string]any) error {
	data, _ := json.Marshal(originalMessage)
	_, err := s.pool.Exec(ctx, `
		INSERT INTO agentpg_message_archive (id, compaction_event_id, session_id, original_message)
		VALUES ($1, $2, $3, $4)
	`, messageID, compactionEventID, sessionID, data)
	return err
}

func (s *Store) GetCompactionEvents(ctx context.Context, sessionID uuid.UUID, limit int) ([]*driver.CompactionEvent, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, session_id, strategy, original_tokens, compacted_tokens, messages_removed, summary_content, preserved_message_ids, model_used, duration_ms, created_at
		FROM agentpg_compaction_events WHERE session_id = $1 ORDER BY created_at DESC LIMIT $2
	`, sessionID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []*driver.CompactionEvent
	for rows.Next() {
		var event driver.CompactionEvent
		var preservedIDs []byte
		if err := rows.Scan(
			&event.ID, &event.SessionID, &event.Strategy, &event.OriginalTokens, &event.CompactedTokens,
			&event.MessagesRemoved, &event.SummaryContent, &preservedIDs, &event.ModelUsed, &event.DurationMS, &event.CreatedAt,
		); err != nil {
			return nil, err
		}
		json.Unmarshal(preservedIDs, &event.PreservedMessageIDs)
		events = append(events, &event)
	}
	return events, rows.Err()
}

// Helper functions

func joinStrings(strs []string, sep string) string {
	if len(strs) == 0 {
		return ""
	}
	result := strs[0]
	for _, s := range strs[1:] {
		result += sep + s
	}
	return result
}

func nullIfEmpty[T comparable](v T) any {
	var zero T
	if v == zero {
		return nil
	}
	return v
}

func nullIfEmptyBytes(v []byte) any {
	if len(v) == 0 {
		return nil
	}
	return v
}

func collectRuns(rows pgx.Rows) ([]*driver.Run, error) {
	var runs []*driver.Run
	for rows.Next() {
		var run driver.Run
		var metadata []byte
		if err := rows.Scan(
			&run.ID, &run.SessionID, &run.AgentName, &run.RunMode, &run.ParentRunID, &run.ParentToolExecutionID, &run.Depth,
			&run.State, &run.PreviousState, &run.Prompt, &run.CurrentIteration, &run.CurrentIterationID,
			&run.ResponseText, &run.StopReason, &run.InputTokens, &run.OutputTokens,
			&run.CacheCreationInputTokens, &run.CacheReadInputTokens, &run.IterationCount, &run.ToolIterations,
			&run.ErrorMessage, &run.ErrorType, &run.CreatedByInstanceID, &run.ClaimedByInstanceID, &run.ClaimedAt,
			&metadata, &run.CreatedAt, &run.StartedAt, &run.FinalizedAt,
			&run.RescueAttempts, &run.LastRescueAt,
		); err != nil {
			return nil, err
		}
		json.Unmarshal(metadata, &run.Metadata)
		runs = append(runs, &run)
	}
	return runs, rows.Err()
}

func collectIterations(rows pgx.Rows) ([]*driver.Iteration, error) {
	var iterations []*driver.Iteration
	for rows.Next() {
		var iter driver.Iteration
		if err := rows.Scan(
			&iter.ID, &iter.RunID, &iter.IterationNumber, &iter.IsStreaming,
			&iter.BatchID, &iter.BatchRequestID, &iter.BatchStatus,
			&iter.BatchSubmittedAt, &iter.BatchCompletedAt, &iter.BatchExpiresAt, &iter.BatchPollCount, &iter.BatchLastPollAt,
			&iter.StreamingStartedAt, &iter.StreamingCompletedAt,
			&iter.TriggerType, &iter.RequestMessageIDs, &iter.StopReason, &iter.ResponseMessageID, &iter.HasToolUse, &iter.ToolExecutionCount,
			&iter.InputTokens, &iter.OutputTokens, &iter.CacheCreationInputTokens, &iter.CacheReadInputTokens,
			&iter.ErrorMessage, &iter.ErrorType, &iter.CreatedAt, &iter.StartedAt, &iter.CompletedAt,
		); err != nil {
			return nil, err
		}
		iterations = append(iterations, &iter)
	}
	return iterations, rows.Err()
}

func collectToolExecutions(rows pgx.Rows) ([]*driver.ToolExecution, error) {
	var executions []*driver.ToolExecution
	for rows.Next() {
		var exec driver.ToolExecution
		if err := rows.Scan(
			&exec.ID, &exec.RunID, &exec.IterationID, &exec.State, &exec.ToolUseID, &exec.ToolName, &exec.ToolInput,
			&exec.IsAgentTool, &exec.AgentName, &exec.ChildRunID, &exec.ToolOutput, &exec.IsError, &exec.ErrorMessage,
			&exec.ClaimedByInstanceID, &exec.ClaimedAt, &exec.AttemptCount, &exec.MaxAttempts,
			&exec.ScheduledAt, &exec.SnoozeCount, &exec.LastError, &exec.CreatedAt, &exec.StartedAt, &exec.CompletedAt,
		); err != nil {
			return nil, err
		}
		executions = append(executions, &exec)
	}
	return executions, rows.Err()
}

// Compile-time check
var _ driver.Store[pgx.Tx] = (*Store)(nil)
