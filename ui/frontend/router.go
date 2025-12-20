package frontend

import (
	"embed"
	"html/template"
	"io/fs"
	"net/http"
	"time"

	"github.com/youssefsiam38/agentpg"
	"github.com/youssefsiam38/agentpg/ui/service"
)

//go:embed templates/*
var templatesFS embed.FS

//go:embed static/*
var staticFS embed.FS

// Config holds frontend router configuration.
type Config struct {
	// BasePath is the URL prefix where the UI is mounted.
	// All navigation links will be prefixed with this path.
	BasePath string

	// TenantID filters data to a single tenant.
	// If empty, shows all tenants (admin mode).
	TenantID string

	// ReadOnly disables write operations (chat, session creation).
	ReadOnly bool

	// PageSize for pagination.
	PageSize int

	// RefreshInterval for auto-refresh.
	RefreshInterval time.Duration

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

// router holds the frontend router state.
type router[TTx any] struct {
	svc      *service.Service[TTx]
	client   *agentpg.Client[TTx]
	config   *Config
	tmpl     *template.Template
	renderer *renderer
}

// NewRouter creates a new frontend router.
func NewRouter[TTx any](svc *service.Service[TTx], client *agentpg.Client[TTx], cfg *Config) http.Handler {
	if cfg == nil {
		cfg = &Config{
			PageSize:        25,
			RefreshInterval: 5 * time.Second,
		}
	}

	// Parse base templates (layout, nav, shared fragments)
	// Page-specific templates are parsed dynamically by the renderer
	// to avoid conflicts between "content" blocks in different pages.
	baseTmpl := template.Must(template.New("").
		Funcs(templateFuncs()).
		ParseFS(templatesFS,
			"templates/base.html",
			"templates/dashboard.html",
			"templates/fragments/dashboard-stats.html",
			"templates/chat/message-bubble.html",
		))

	r := &router[TTx]{
		svc:      svc,
		client:   client,
		config:   cfg,
		tmpl:     baseTmpl,
		renderer: newRenderer(baseTmpl, templatesFS, cfg),
	}

	mux := http.NewServeMux()

	// Static assets
	staticSub, _ := fs.Sub(staticFS, "static")
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticSub))))

	// Main pages
	mux.HandleFunc("GET /", r.handleRedirectToDashboard)
	mux.HandleFunc("GET /dashboard", r.handleDashboard)
	mux.HandleFunc("GET /sessions", r.handleSessions)
	mux.HandleFunc("GET /sessions/{id}", r.handleSessionDetail)
	mux.HandleFunc("GET /runs", r.handleRuns)
	mux.HandleFunc("GET /runs/{id}", r.handleRunDetail)
	mux.HandleFunc("GET /runs/{id}/conversation", r.handleConversation)
	mux.HandleFunc("GET /tool-executions", r.handleToolExecutions)
	mux.HandleFunc("GET /tool-executions/{id}", r.handleToolExecutionDetail)
	mux.HandleFunc("GET /agents", r.handleAgents)
	mux.HandleFunc("GET /instances", r.handleInstances)
	mux.HandleFunc("GET /compaction", r.handleCompaction)
	mux.HandleFunc("GET /messages/session/{sessionId}", r.handleSessionConversation)

	// Chat interface
	mux.HandleFunc("GET /chat", r.handleChat)
	mux.HandleFunc("GET /chat/new", r.handleChatNew)
	mux.HandleFunc("POST /chat/send", r.handleChatSend)
	mux.HandleFunc("GET /chat/poll/{runId}", r.handleChatPoll)
	mux.HandleFunc("GET /chat/session/{sessionId}", r.handleChatSession)
	mux.HandleFunc("GET /chat/session/{sessionId}/messages", r.handleChatMessages)

	// HTMX fragments
	mux.HandleFunc("GET /fragments/dashboard-stats", r.handleFragmentDashboardStats)
	mux.HandleFunc("GET /fragments/run-list", r.handleFragmentRunList)
	mux.HandleFunc("GET /fragments/session-list", r.handleFragmentSessionList)

	return withFrontendMiddleware(mux, cfg)
}

// withFrontendMiddleware wraps the handler with frontend-specific middleware.
func withFrontendMiddleware(handler http.Handler, cfg *Config) http.Handler {
	handler = frontendRecoveryMiddleware(handler, cfg.Logger)
	return handler
}

// frontendRecoveryMiddleware recovers from panics.
func frontendRecoveryMiddleware(next http.Handler, logger Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				if logger != nil {
					logger.Error("panic recovered", "error", err, "path", r.URL.Path)
				}
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// templateFuncs returns custom template functions.
func templateFuncs() template.FuncMap {
	return template.FuncMap{
		"formatDuration": formatDuration,
		"formatTime":     formatTime,
		"formatTimeAgo":  formatTimeAgo,
		"formatTokens":   formatTokens,
		"truncate":       truncate,
		"stateColor":     stateColor,
		"stateBgColor":   stateBgColor,
		"json":           jsonEncode,
		"safeHTML":       safeHTML,
		"markdown":       markdown,
		"add":            add,
		"sub":            sub,
		"mul":            mul,
		"mulFloat":       mulFloat,
		"div":            div,
		"div64":          div64,
		"seq":            seq,
		"contains":       contains,
		"default":        defaultVal,
		"dict":           dictFunc,
	}
}

// dictFunc creates a map from key-value pairs for use in templates.
// Usage: {{template "foo" (dict "key1" val1 "key2" val2)}}
func dictFunc(values ...any) map[string]any {
	if len(values)%2 != 0 {
		return nil
	}
	dict := make(map[string]any, len(values)/2)
	for i := 0; i < len(values); i += 2 {
		key, ok := values[i].(string)
		if !ok {
			continue
		}
		dict[key] = values[i+1]
	}
	return dict
}
