package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/google/uuid"
	"github.com/youssefsiam38/agentpg/ui/service"
)

// Response wraps all API responses.
type Response struct {
	Data  any       `json:"data,omitempty"`
	Error *APIError `json:"error,omitempty"`
	Meta  *Meta     `json:"meta,omitempty"`
}

// APIError represents an API error.
type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`
}

// Meta contains pagination metadata.
type Meta struct {
	TotalCount int  `json:"total_count,omitempty"`
	HasMore    bool `json:"has_more,omitempty"`
	Limit      int  `json:"limit,omitempty"`
	Offset     int  `json:"offset,omitempty"`
}

// writeJSON writes a JSON response.
func writeJSON(w http.ResponseWriter, status int, data any) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(Response{Data: data})
}

// writeJSONWithMeta writes a JSON response with metadata.
func writeJSONWithMeta(w http.ResponseWriter, status int, data any, meta *Meta) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(Response{Data: data, Meta: meta})
}

// writeError writes a JSON error response.
func writeError(w http.ResponseWriter, status int, code, message string) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(Response{
		Error: &APIError{Code: code, Message: message},
	})
}

// parseUUID parses a UUID from a path parameter.
func parseUUID(s string) (uuid.UUID, error) {
	return uuid.Parse(s)
}

// parseInt parses an integer from a query parameter with a default.
// It applies bounds validation to prevent resource exhaustion.
func parseInt(r *http.Request, key string, defaultVal int) int {
	val := r.URL.Query().Get(key)
	if val == "" {
		return defaultVal
	}
	i, err := strconv.Atoi(val)
	if err != nil {
		return defaultVal
	}
	return service.ValidateLimit(i)
}

// parseOffset parses an offset from a query parameter with a default.
func parseOffset(r *http.Request, key string, defaultVal int) int {
	val := r.URL.Query().Get(key)
	if val == "" {
		return defaultVal
	}
	i, err := strconv.Atoi(val)
	if err != nil {
		return defaultVal
	}
	return service.ValidateOffset(i)
}

// Dashboard handlers

func (rt *router[TTx]) handleDashboard(w http.ResponseWriter, r *http.Request) {
	stats, err := rt.svc.GetDashboardStats(r.Context(), rt.config.TenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

func (rt *router[TTx]) handleDashboardEvents(w http.ResponseWriter, r *http.Request) {
	// SSE endpoint - will be implemented in events.go
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "sse_not_supported", "SSE not supported")
		return
	}

	// Send initial stats
	stats, err := rt.svc.GetDashboardStats(r.Context(), rt.config.TenantID)
	if err == nil {
		data, _ := json.Marshal(stats)
		_, _ = w.Write([]byte("event: stats\ndata: "))
		_, _ = w.Write(data)
		_, _ = w.Write([]byte("\n\n"))
		flusher.Flush()
	}

	// Keep connection open until client disconnects
	<-r.Context().Done()
}

// Session handlers

func (rt *router[TTx]) handleListSessions(w http.ResponseWriter, r *http.Request) {
	params := service.SessionListParams{
		TenantID: rt.config.TenantID,
		Limit:    parseInt(r, "limit", rt.config.PageSize),
		Offset:   parseOffset(r, "offset", 0),
		OrderBy:  service.ValidateOrderBy(r.URL.Query().Get("order_by"), service.AllowedSessionOrderBy),
		OrderDir: service.ValidateOrderDir(r.URL.Query().Get("order_dir")),
	}
	// Only allow tenant_id override if config.TenantID is empty (admin mode)
	if rt.config.TenantID == "" {
		if tenantID := r.URL.Query().Get("tenant_id"); tenantID != "" {
			params.TenantID = tenantID
		}
	}

	list, err := rt.svc.ListSessions(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	writeJSONWithMeta(w, http.StatusOK, list.Sessions, &Meta{
		TotalCount: list.TotalCount,
		HasMore:    list.HasMore,
		Limit:      params.Limit,
		Offset:     params.Offset,
	})
}

func (rt *router[TTx]) handleGetSession(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "invalid session ID")
		return
	}

	detail, err := rt.svc.GetSessionDetail(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "session not found")
		return
	}

	writeJSON(w, http.StatusOK, detail)
}

func (rt *router[TTx]) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	var req service.CreateSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "invalid request body")
		return
	}

	session, err := rt.svc.CreateSession(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, session)
}

// Run handlers

func (rt *router[TTx]) handleListRuns(w http.ResponseWriter, r *http.Request) {
	params := service.RunListParams{
		TenantID:  rt.config.TenantID,
		AgentName: r.URL.Query().Get("agent_name"),
		State:     r.URL.Query().Get("state"),
		RunMode:   r.URL.Query().Get("run_mode"),
		Limit:     parseInt(r, "limit", rt.config.PageSize),
		Offset:    parseOffset(r, "offset", 0),
		OrderBy:   service.ValidateOrderBy(r.URL.Query().Get("order_by"), service.AllowedRunOrderBy),
		OrderDir:  service.ValidateOrderDir(r.URL.Query().Get("order_dir")),
	}
	// Only allow tenant_id override if config.TenantID is empty (admin mode)
	if rt.config.TenantID == "" {
		if tenantID := r.URL.Query().Get("tenant_id"); tenantID != "" {
			params.TenantID = tenantID
		}
	}
	if sessionID := r.URL.Query().Get("session_id"); sessionID != "" {
		if id, err := parseUUID(sessionID); err == nil {
			params.SessionID = &id
		}
	}

	list, err := rt.svc.ListRuns(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	writeJSONWithMeta(w, http.StatusOK, list.Runs, &Meta{
		TotalCount: list.TotalCount,
		HasMore:    list.HasMore,
		Limit:      params.Limit,
		Offset:     params.Offset,
	})
}

func (rt *router[TTx]) handleGetRun(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "invalid run ID")
		return
	}

	detail, err := rt.svc.GetRunDetail(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "run not found")
		return
	}

	writeJSON(w, http.StatusOK, detail)
}

func (rt *router[TTx]) handleGetRunHierarchy(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "invalid run ID")
		return
	}

	hierarchy, err := rt.svc.GetRunHierarchy(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "run not found")
		return
	}

	writeJSON(w, http.StatusOK, hierarchy)
}

func (rt *router[TTx]) handleGetRunIterations(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "invalid run ID")
		return
	}

	iterations, err := rt.svc.GetRunIterations(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "run not found")
		return
	}

	writeJSON(w, http.StatusOK, iterations)
}

func (rt *router[TTx]) handleGetRunToolExecutions(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "invalid run ID")
		return
	}

	executions, err := rt.svc.GetRunToolExecutions(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "run not found")
		return
	}

	writeJSON(w, http.StatusOK, executions)
}

func (rt *router[TTx]) handleGetRunMessages(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "invalid run ID")
		return
	}

	messages, err := rt.svc.GetRunMessages(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "run not found")
		return
	}

	writeJSON(w, http.StatusOK, messages)
}

// Iteration handlers

func (rt *router[TTx]) handleListIterations(w http.ResponseWriter, r *http.Request) {
	runIDStr := r.URL.Query().Get("run_id")
	if runIDStr == "" {
		writeError(w, http.StatusBadRequest, "missing_param", "run_id is required")
		return
	}

	runID, err := parseUUID(runIDStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "invalid run ID")
		return
	}

	iterations, err := rt.svc.GetRunIterations(r.Context(), runID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, iterations)
}

func (rt *router[TTx]) handleGetIteration(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "invalid iteration ID")
		return
	}

	detail, err := rt.svc.GetIterationDetail(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "iteration not found")
		return
	}

	writeJSON(w, http.StatusOK, detail)
}

// Tool Execution handlers

func (rt *router[TTx]) handleListToolExecutions(w http.ResponseWriter, r *http.Request) {
	params := service.ToolExecutionListParams{
		ToolName: r.URL.Query().Get("tool_name"),
		State:    r.URL.Query().Get("state"),
		Limit:    parseInt(r, "limit", rt.config.PageSize),
		Offset:   parseOffset(r, "offset", 0),
	}
	if runID := r.URL.Query().Get("run_id"); runID != "" {
		if id, err := parseUUID(runID); err == nil {
			params.RunID = &id
		}
	}
	if iterationID := r.URL.Query().Get("iteration_id"); iterationID != "" {
		if id, err := parseUUID(iterationID); err == nil {
			params.IterationID = &id
		}
	}
	if isAgentTool := r.URL.Query().Get("is_agent_tool"); isAgentTool != "" {
		val := isAgentTool == "true"
		params.IsAgentTool = &val
	}

	list, err := rt.svc.ListToolExecutions(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, list)
}

func (rt *router[TTx]) handleGetToolExecution(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "invalid tool execution ID")
		return
	}

	detail, err := rt.svc.GetToolExecutionDetail(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "tool execution not found")
		return
	}

	writeJSON(w, http.StatusOK, detail)
}

// Message handlers

func (rt *router[TTx]) handleListMessages(w http.ResponseWriter, r *http.Request) {
	sessionIDStr := r.URL.Query().Get("session_id")
	if sessionIDStr == "" {
		writeError(w, http.StatusBadRequest, "missing_param", "session_id is required")
		return
	}

	sessionID, err := parseUUID(sessionIDStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "invalid session ID")
		return
	}

	limit := parseInt(r, "limit", 100)
	conversation, err := rt.svc.GetConversation(r.Context(), sessionID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, conversation)
}

func (rt *router[TTx]) handleGetMessage(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "invalid message ID")
		return
	}

	message, err := rt.svc.GetMessage(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "message not found")
		return
	}

	writeJSON(w, http.StatusOK, message)
}

// Agent handlers

func (rt *router[TTx]) handleListAgents(w http.ResponseWriter, r *http.Request) {
	agents, err := rt.svc.ListAgents(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, agents)
}

func (rt *router[TTx]) handleGetAgent(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "invalid_name", "agent name is required")
		return
	}

	agent, err := rt.svc.GetAgentWithStats(r.Context(), name)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "agent not found")
		return
	}

	writeJSON(w, http.StatusOK, agent)
}

// Tool handlers

func (rt *router[TTx]) handleListTools(w http.ResponseWriter, r *http.Request) {
	tools, err := rt.svc.ListTools(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, tools)
}

func (rt *router[TTx]) handleGetTool(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "invalid_name", "tool name is required")
		return
	}

	tool, err := rt.svc.GetToolWithStats(r.Context(), name)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "tool not found")
		return
	}

	writeJSON(w, http.StatusOK, tool)
}

// Instance handlers

func (rt *router[TTx]) handleListInstances(w http.ResponseWriter, r *http.Request) {
	instances, err := rt.svc.ListInstances(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, instances)
}

func (rt *router[TTx]) handleGetInstance(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "invalid_id", "instance ID is required")
		return
	}

	instance, err := rt.svc.GetInstanceDetail(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "instance not found")
		return
	}

	writeJSON(w, http.StatusOK, instance)
}

// Compaction handlers

func (rt *router[TTx]) handleListCompactionEvents(w http.ResponseWriter, r *http.Request) {
	var sessionID *uuid.UUID
	if sessionIDStr := r.URL.Query().Get("session_id"); sessionIDStr != "" {
		if id, err := parseUUID(sessionIDStr); err == nil {
			sessionID = &id
		}
	}

	params := service.CompactionEventListParams{
		SessionID: sessionID,
		Limit:     parseInt(r, "limit", rt.config.PageSize),
		Offset:    parseOffset(r, "offset", 0),
	}

	events, err := rt.svc.ListCompactionEvents(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, events)
}

func (rt *router[TTx]) handleGetCompactionEvent(w http.ResponseWriter, r *http.Request) {
	eventID, err := parseUUID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "invalid compaction event ID")
		return
	}

	// Session ID is required to look up compaction events
	sessionIDStr := r.URL.Query().Get("session_id")
	if sessionIDStr == "" {
		writeError(w, http.StatusBadRequest, "missing_param", "session_id query parameter is required")
		return
	}

	sessionID, err := parseUUID(sessionIDStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "invalid session ID")
		return
	}

	event, err := rt.svc.GetCompactionEventDetail(r.Context(), sessionID, eventID)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "compaction event not found")
		return
	}

	writeJSON(w, http.StatusOK, event)
}

// Tenant handlers

func (rt *router[TTx]) handleListTenants(w http.ResponseWriter, r *http.Request) {
	tenants, err := rt.svc.ListTenants(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, tenants)
}
