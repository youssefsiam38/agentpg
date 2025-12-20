package frontend

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/youssefsiam38/agentpg/ui/service"
)

// parseInt parses an integer from a query parameter with a default.
func parseInt(r *http.Request, key string, defaultVal int) int {
	val := r.URL.Query().Get(key)
	if val == "" {
		return defaultVal
	}
	i, err := strconv.Atoi(val)
	if err != nil {
		return defaultVal
	}
	return i
}

// parseUUID parses a UUID from a string.
func parseUUID(s string) (uuid.UUID, error) {
	return uuid.Parse(s)
}

// Main page handlers

func (rt *router[TTx]) handleRedirectToDashboard(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/dashboard", http.StatusTemporaryRedirect)
}

func (rt *router[TTx]) handleDashboard(w http.ResponseWriter, r *http.Request) {
	stats, err := rt.svc.GetDashboardStats(r.Context(), rt.config.TenantID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	data := map[string]any{
		"Title": "Dashboard",
		"Stats": stats,
	}

	if err := rt.renderer.render(w, r, "dashboard.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (rt *router[TTx]) handleSessions(w http.ResponseWriter, r *http.Request) {
	params := service.SessionListParams{
		TenantID: rt.config.TenantID,
		Limit:    parseInt(r, "limit", rt.config.PageSize),
		Offset:   parseInt(r, "offset", 0),
		OrderBy:  r.URL.Query().Get("order_by"),
		OrderDir: r.URL.Query().Get("order_dir"),
	}
	if tenantID := r.URL.Query().Get("tenant_id"); tenantID != "" {
		params.TenantID = tenantID
	}

	list, err := rt.svc.ListSessions(r.Context(), params)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Get tenants for filter dropdown
	tenants, _ := rt.svc.ListTenants(r.Context())

	data := map[string]any{
		"Title":         "Sessions",
		"Sessions":      list.Sessions,
		"TotalCount":    list.TotalCount,
		"HasMore":       list.HasMore,
		"Limit":         params.Limit,
		"Offset":        params.Offset,
		"CurrentTenant": params.TenantID,
		"Tenants":       tenants,
		"CurrentPage":   params.Offset/params.Limit + 1,
		"TotalPages":    (list.TotalCount + params.Limit - 1) / params.Limit,
	}

	if err := rt.renderer.render(w, r, "sessions/list.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (rt *router[TTx]) handleSessionDetail(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(r.PathValue("id"))
	if err != nil {
		http.Error(w, "Invalid session ID", http.StatusBadRequest)
		return
	}

	detail, err := rt.svc.GetSessionDetail(r.Context(), id)
	if err != nil {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}

	data := map[string]any{
		"Title":   "Session: " + detail.Session.Identifier,
		"Session": detail,
	}

	if err := rt.renderer.render(w, r, "sessions/detail.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (rt *router[TTx]) handleRuns(w http.ResponseWriter, r *http.Request) {
	params := service.RunListParams{
		TenantID:  rt.config.TenantID,
		AgentName: r.URL.Query().Get("agent_name"),
		State:     r.URL.Query().Get("state"),
		RunMode:   r.URL.Query().Get("run_mode"),
		Limit:     parseInt(r, "limit", rt.config.PageSize),
		Offset:    parseInt(r, "offset", 0),
		OrderBy:   r.URL.Query().Get("order_by"),
		OrderDir:  r.URL.Query().Get("order_dir"),
	}
	if tenantID := r.URL.Query().Get("tenant_id"); tenantID != "" {
		params.TenantID = tenantID
	}
	if sessionID := r.URL.Query().Get("session_id"); sessionID != "" {
		if id, err := parseUUID(sessionID); err == nil {
			params.SessionID = &id
		}
	}

	list, err := rt.svc.ListRuns(r.Context(), params)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Get agents for filter dropdown
	agents, _ := rt.svc.ListAgents(r.Context())

	data := map[string]any{
		"Title":        "Runs",
		"Runs":         list.Runs,
		"TotalCount":   list.TotalCount,
		"HasMore":      list.HasMore,
		"Limit":        params.Limit,
		"Offset":       params.Offset,
		"CurrentAgent": params.AgentName,
		"CurrentState": params.State,
		"CurrentMode":  params.RunMode,
		"Agents":       agents,
		"States":       []string{"pending", "batch_submitting", "batch_pending", "batch_processing", "streaming", "pending_tools", "completed", "failed", "cancelled"},
		"CurrentPage":  params.Offset/params.Limit + 1,
		"TotalPages":   (list.TotalCount + params.Limit - 1) / params.Limit,
	}

	if err := rt.renderer.render(w, r, "runs/list.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (rt *router[TTx]) handleRunDetail(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(r.PathValue("id"))
	if err != nil {
		http.Error(w, "Invalid run ID", http.StatusBadRequest)
		return
	}

	detail, err := rt.svc.GetRunDetail(r.Context(), id)
	if err != nil {
		http.Error(w, "Run not found", http.StatusNotFound)
		return
	}

	// Get hierarchy if this is a root run
	var hierarchy *service.RunHierarchy
	if detail.Run.ParentRunID == nil {
		hierarchy, _ = rt.svc.GetRunHierarchy(r.Context(), id)
	}

	data := map[string]any{
		"Title":     "Run: " + id.String()[:8],
		"Run":       detail,
		"Hierarchy": hierarchy,
	}

	if err := rt.renderer.render(w, r, "runs/detail.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (rt *router[TTx]) handleConversation(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(r.PathValue("id"))
	if err != nil {
		http.Error(w, "Invalid run ID", http.StatusBadRequest)
		return
	}

	// Get run details first
	runDetail, err := rt.svc.GetRunDetail(r.Context(), id)
	if err != nil {
		http.Error(w, "Run not found", http.StatusNotFound)
		return
	}

	// Get conversation
	conversation, err := rt.svc.GetConversation(r.Context(), runDetail.Run.SessionID, 100)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	data := map[string]any{
		"Title":        "Conversation",
		"Run":          runDetail,
		"Conversation": conversation,
	}

	if err := rt.renderer.render(w, r, "messages/conversation.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (rt *router[TTx]) handleToolExecutions(w http.ResponseWriter, r *http.Request) {
	params := service.ToolExecutionListParams{
		ToolName: r.URL.Query().Get("tool_name"),
		State:    r.URL.Query().Get("state"),
		Limit:    parseInt(r, "limit", rt.config.PageSize),
		Offset:   parseInt(r, "offset", 0),
	}
	if runID := r.URL.Query().Get("run_id"); runID != "" {
		if id, err := parseUUID(runID); err == nil {
			params.RunID = &id
		}
	}
	if isAgentTool := r.URL.Query().Get("is_agent_tool"); isAgentTool != "" {
		val := isAgentTool == "true"
		params.IsAgentTool = &val
	}

	list, err := rt.svc.ListToolExecutions(r.Context(), params)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Get tools for filter dropdown
	tools, _ := rt.svc.ListTools(r.Context())

	data := map[string]any{
		"Title":        "Tool Executions",
		"Executions":   list,
		"CurrentTool":  params.ToolName,
		"CurrentState": params.State,
		"Tools":        tools,
		"States":       []string{"pending", "running", "completed", "failed", "skipped"},
	}

	if err := rt.renderer.render(w, r, "tools/list.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (rt *router[TTx]) handleToolExecutionDetail(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(r.PathValue("id"))
	if err != nil {
		http.Error(w, "Invalid tool execution ID", http.StatusBadRequest)
		return
	}

	detail, err := rt.svc.GetToolExecutionDetail(r.Context(), id)
	if err != nil {
		http.Error(w, "Tool execution not found", http.StatusNotFound)
		return
	}

	// Flatten the data structure for easier template access
	data := map[string]any{
		"Title":     "Tool Execution: " + detail.Execution.ToolName,
		"Execution": detail.Execution, // The actual ToolExecution
		"Run":       detail.Run,
		"ChildRun":  detail.ChildRun,
	}

	if err := rt.renderer.render(w, r, "tools/detail.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (rt *router[TTx]) handleAgents(w http.ResponseWriter, r *http.Request) {
	agents, err := rt.svc.ListAgents(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	tools, err := rt.svc.ListTools(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	data := map[string]any{
		"Title":  "Agents & Tools",
		"Agents": agents,
		"Tools":  tools,
	}

	if err := rt.renderer.render(w, r, "agents/list.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (rt *router[TTx]) handleInstances(w http.ResponseWriter, r *http.Request) {
	instances, err := rt.svc.ListInstances(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Get leader info (ignore error - leader may not exist)
	leader, _ := rt.svc.GetLeaderInfo(r.Context())

	data := map[string]any{
		"Title":     "Instances",
		"Instances": instances,
		"Leader":    leader,
	}

	if err := rt.renderer.render(w, r, "instances/list.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (rt *router[TTx]) handleCompaction(w http.ResponseWriter, r *http.Request) {
	var sessionID *uuid.UUID
	if sessionIDStr := r.URL.Query().Get("session_id"); sessionIDStr != "" {
		if id, err := parseUUID(sessionIDStr); err == nil {
			sessionID = &id
		}
	}

	params := service.CompactionEventListParams{
		SessionID: sessionID,
		Limit:     parseInt(r, "limit", rt.config.PageSize),
		Offset:    parseInt(r, "offset", 0),
	}

	events, err := rt.svc.ListCompactionEvents(r.Context(), params)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	data := map[string]any{
		"Title":  "Compaction Events",
		"Events": events,
	}

	if err := rt.renderer.render(w, r, "compaction/list.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// Chat handlers

func (rt *router[TTx]) handleChat(w http.ResponseWriter, r *http.Request) {
	// Get recent sessions for sidebar
	var sessionList []*service.SessionSummary
	sessions, err := rt.svc.ListSessions(r.Context(), service.SessionListParams{
		TenantID: rt.config.TenantID,
		Limit:    10,
		OrderBy:  "updated_at",
		OrderDir: "desc",
	})
	if err == nil && sessions != nil {
		sessionList = sessions.Sessions
	}

	agents, _ := rt.svc.ListAgents(r.Context())

	data := map[string]any{
		"Title":    "Chat",
		"Sessions": sessionList,
		"Agents":   agents,
	}

	if err := rt.renderer.render(w, r, "chat/interface.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (rt *router[TTx]) handleChatNew(w http.ResponseWriter, r *http.Request) {
	agents, _ := rt.svc.ListAgents(r.Context())
	tenants, _ := rt.svc.ListTenants(r.Context())

	data := map[string]any{
		"Title":   "New Chat",
		"Agents":  agents,
		"Tenants": tenants,
	}

	if err := rt.renderer.render(w, r, "chat/new.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (rt *router[TTx]) handleChatSend(w http.ResponseWriter, r *http.Request) {
	if rt.config.ReadOnly || rt.client == nil {
		http.Error(w, "Chat is disabled", http.StatusForbidden)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	agentName := r.FormValue("agent_name")
	message := r.FormValue("message")
	isNewSession := r.FormValue("new_session") == "true"

	if agentName == "" || message == "" {
		http.Error(w, "Missing required fields", http.StatusBadRequest)
		return
	}

	var sessionID uuid.UUID
	var err error

	if isNewSession {
		// Create a new session
		tenantID := r.FormValue("tenant_id")
		if tenantID == "" {
			tenantID = "default"
		}
		identifier := r.FormValue("session_id") // session_id field is used as identifier for new sessions
		if identifier == "" {
			identifier = fmt.Sprintf("chat-%d", time.Now().UnixNano())
		}

		sessionID, err = rt.client.NewSession(r.Context(), tenantID, identifier, nil, nil)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to create session: %v", err), http.StatusInternalServerError)
			return
		}
	} else {
		sessionIDStr := r.FormValue("session_id")
		if sessionIDStr == "" {
			http.Error(w, "Missing session_id", http.StatusBadRequest)
			return
		}
		sessionID, err = parseUUID(sessionIDStr)
		if err != nil {
			http.Error(w, "Invalid session ID", http.StatusBadRequest)
			return
		}
	}

	// Use streaming API for lower latency
	runID, err := rt.client.RunFast(r.Context(), sessionID, agentName, message)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// For new sessions, redirect to the chat session page with pending run
	if isNewSession {
		http.Redirect(w, r, fmt.Sprintf("%s/chat/session/%s?pending_run=%s", rt.config.BasePath, sessionID, runID), http.StatusSeeOther)
		return
	}

	// Return a fragment that will poll for completion
	data := map[string]any{
		"BasePath":  rt.config.BasePath,
		"RunID":     runID,
		"SessionID": sessionID,
		"Message":   message,
		"State":     "pending",
	}

	if err := rt.renderer.renderFragment(w, "chat/pending.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (rt *router[TTx]) handleChatPoll(w http.ResponseWriter, r *http.Request) {
	runIDStr := r.PathValue("runId")
	runID, err := parseUUID(runIDStr)
	if err != nil {
		http.Error(w, "Invalid run ID", http.StatusBadRequest)
		return
	}

	run, err := rt.svc.Store().GetRun(r.Context(), runID)
	if err != nil {
		http.Error(w, "Run not found", http.StatusNotFound)
		return
	}

	// Get tool executions for this run
	toolExecs, _ := rt.svc.GetRunToolExecutions(r.Context(), runID)

	data := map[string]any{
		"BasePath":       rt.config.BasePath,
		"Run":            run,
		"ToolExecutions": toolExecs,
		"IsComplete":     run.State == "completed" || run.State == "failed" || run.State == "cancelled",
	}

	if err := rt.renderer.renderFragment(w, "chat/response.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (rt *router[TTx]) handleChatSession(w http.ResponseWriter, r *http.Request) {
	sessionIDStr := r.PathValue("sessionId")
	sessionID, err := parseUUID(sessionIDStr)
	if err != nil {
		http.Error(w, "Invalid session ID", http.StatusBadRequest)
		return
	}

	// Get session details
	session, err := rt.svc.GetSessionDetail(r.Context(), sessionID)
	if err != nil {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}

	// Get conversation
	conversation, _ := rt.svc.GetConversation(r.Context(), sessionID, 100)

	// Get agents for the send form
	agents, _ := rt.svc.ListAgents(r.Context())

	// Get recent sessions for sidebar
	sessions, _ := rt.svc.ListSessions(r.Context(), service.SessionListParams{
		TenantID: rt.config.TenantID,
		Limit:    10,
		OrderBy:  "updated_at",
		OrderDir: "desc",
	})

	// Check for pending run (from redirect after new session creation)
	var pendingRunID *uuid.UUID
	if pendingRunStr := r.URL.Query().Get("pending_run"); pendingRunStr != "" {
		if runID, err := parseUUID(pendingRunStr); err == nil {
			// Only show pending if run is actually still in a non-terminal state
			if run, err := rt.svc.GetRun(r.Context(), runID); err == nil && run != nil {
				state := string(run.State)
				if state != "completed" && state != "failed" && state != "cancelled" {
					pendingRunID = &runID
				}
			}
		}
	}

	data := map[string]any{
		"Title":        "Chat: " + session.Session.Identifier,
		"Session":      session,
		"Conversation": conversation,
		"Agents":       agents,
		"Sessions":     sessions.Sessions,
		"PendingRunID": pendingRunID,
	}

	if err := rt.renderer.render(w, r, "chat/interface.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// Fragment handlers for HTMX

func (rt *router[TTx]) handleFragmentDashboardStats(w http.ResponseWriter, r *http.Request) {
	stats, err := rt.svc.GetDashboardStats(r.Context(), rt.config.TenantID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := rt.renderer.renderFragment(w, "fragments/dashboard-stats.html", stats); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (rt *router[TTx]) handleFragmentRunList(w http.ResponseWriter, r *http.Request) {
	params := service.RunListParams{
		TenantID:  rt.config.TenantID,
		AgentName: r.URL.Query().Get("agent_name"),
		State:     r.URL.Query().Get("state"),
		Limit:     parseInt(r, "limit", 10),
		Offset:    parseInt(r, "offset", 0),
	}

	list, err := rt.svc.ListRuns(r.Context(), params)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	data := map[string]any{
		"BasePath": rt.config.BasePath,
		"Runs":     list.Runs,
		"HasMore":  list.HasMore,
	}

	if err := rt.renderer.renderFragment(w, "fragments/run-list.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (rt *router[TTx]) handleFragmentSessionList(w http.ResponseWriter, r *http.Request) {
	params := service.SessionListParams{
		TenantID: rt.config.TenantID,
		Limit:    parseInt(r, "limit", 10),
		Offset:   parseInt(r, "offset", 0),
	}
	if tenantID := r.URL.Query().Get("tenant_id"); tenantID != "" {
		params.TenantID = tenantID
	}

	list, err := rt.svc.ListSessions(r.Context(), params)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	data := map[string]any{
		"BasePath": rt.config.BasePath,
		"Sessions": list.Sessions,
		"HasMore":  list.HasMore,
	}

	if err := rt.renderer.renderFragment(w, "fragments/session-list.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
