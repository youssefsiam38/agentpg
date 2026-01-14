package api

import (
	"net/http"

	"github.com/youssefsiam38/agentpg/ui/service"
)

// Config holds API router configuration.
type Config struct {
	// TenantID filters data to a single tenant.
	// If empty, shows all tenants (admin mode).
	TenantID string

	// PageSize for pagination.
	PageSize int

	// Logger for structured logging.
	Logger Logger
}

// Logger interface for structured logging.
type Logger interface {
	Debug(msg string, args ...any)
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
}

// router holds the API router state.
type router[TTx any] struct {
	svc    *service.Service[TTx]
	config *Config
}

// NewRouter creates a new API router.
func NewRouter[TTx any](svc *service.Service[TTx], cfg *Config) http.Handler {
	if cfg == nil {
		cfg = &Config{
			PageSize: 25,
		}
	}

	r := &router[TTx]{
		svc:    svc,
		config: cfg,
	}

	mux := http.NewServeMux()

	// Dashboard
	mux.HandleFunc("GET /dashboard", r.handleDashboard)
	mux.HandleFunc("GET /dashboard/events", r.handleDashboardEvents)

	// Sessions
	mux.HandleFunc("GET /sessions", r.handleListSessions)
	mux.HandleFunc("GET /sessions/{id}", r.handleGetSession)
	mux.HandleFunc("POST /sessions", r.handleCreateSession)

	// Runs
	mux.HandleFunc("GET /runs", r.handleListRuns)
	mux.HandleFunc("GET /runs/{id}", r.handleGetRun)
	mux.HandleFunc("GET /runs/{id}/hierarchy", r.handleGetRunHierarchy)
	mux.HandleFunc("GET /runs/{id}/iterations", r.handleGetRunIterations)
	mux.HandleFunc("GET /runs/{id}/tool-executions", r.handleGetRunToolExecutions)
	mux.HandleFunc("GET /runs/{id}/messages", r.handleGetRunMessages)

	// Iterations
	mux.HandleFunc("GET /iterations", r.handleListIterations)
	mux.HandleFunc("GET /iterations/{id}", r.handleGetIteration)

	// Tool Executions
	mux.HandleFunc("GET /tool-executions", r.handleListToolExecutions)
	mux.HandleFunc("GET /tool-executions/{id}", r.handleGetToolExecution)

	// Messages
	mux.HandleFunc("GET /messages", r.handleListMessages)
	mux.HandleFunc("GET /messages/{id}", r.handleGetMessage)

	// Registry
	mux.HandleFunc("GET /agents", r.handleListAgents)
	mux.HandleFunc("GET /agents/{name}", r.handleGetAgent)
	mux.HandleFunc("GET /tools", r.handleListTools)
	mux.HandleFunc("GET /tools/{name}", r.handleGetTool)

	// Instances
	mux.HandleFunc("GET /instances", r.handleListInstances)
	mux.HandleFunc("GET /instances/{id}", r.handleGetInstance)

	// Compaction
	mux.HandleFunc("GET /compaction-events", r.handleListCompactionEvents)
	mux.HandleFunc("GET /compaction-events/{id}", r.handleGetCompactionEvent)

	// Tenants (admin mode only)
	mux.HandleFunc("GET /tenants", r.handleListTenants)

	return withMiddleware(mux, cfg)
}

// withMiddleware wraps the handler with common middleware.
func withMiddleware(handler http.Handler, cfg *Config) http.Handler {
	// Add JSON content type
	handler = jsonMiddleware(handler)
	// Add error recovery
	handler = recoveryMiddleware(handler, cfg.Logger)
	return handler
}

// jsonMiddleware sets JSON content type for all responses.
func jsonMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		next.ServeHTTP(w, r)
	})
}

// recoveryMiddleware recovers from panics and returns 500.
func recoveryMiddleware(next http.Handler, logger Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				if logger != nil {
					logger.Error("panic recovered", "error", err, "path", r.URL.Path)
				}
				http.Error(w, `{"error":{"code":"internal_error","message":"internal server error"}}`, http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}
