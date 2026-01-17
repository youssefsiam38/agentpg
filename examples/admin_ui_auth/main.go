// Example: admin_ui_auth
//
// This example demonstrates authentication middleware for the AgentPG admin UI:
// - In-memory user authentication with bcrypt password hashing
// - Session-based auth using secure cookies
// - Login/logout flow with redirect handling
// - Protected admin UI endpoints
// - Public health check endpoint
//
// Default credentials:
//
//	admin / admin123
//	demo  / demo123
//
// Run with:
//
//	DATABASE_URL=postgres://user:pass@localhost/agentpg ANTHROPIC_API_KEY=sk-... go run main.go
//
// Then open http://localhost:8080/ and login.
//
// Security notes for production:
// - Use HTTPS (TLS)
// - Set Secure: true on cookies
// - Use a database for user/session storage
// - Add rate limiting for login attempts
// - Consider CSRF protection
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/youssefsiam38/agentpg"
	"github.com/youssefsiam38/agentpg/driver/pgxv5"
	"github.com/youssefsiam38/agentpg/tool"
	"github.com/youssefsiam38/agentpg/ui"
	"golang.org/x/crypto/bcrypt"
)

// Session represents an authenticated user session.
type Session struct {
	Username  string
	CreatedAt time.Time
}

// In-memory stores for users and sessions.
// In production, use a database instead.
var (
	// users stores username -> bcrypt password hash.
	// Default credentials: admin/admin123, demo/demo123
	users = map[string]string{
		"admin": "$2a$10$tgo2gAgGgTbTzydKAQQFeeuVZ.UOHwiNuB56S2FkYV44CIRv7IXZu", // admin123
		"demo":  "$2a$10$k9.gPoO62SZCJ1S8mf2DuuFkku61lqni77lBYtO4iemvcGkdRhKc6", // demo123
	}

	// sessions stores session token -> Session.
	sessions sync.Map
)

// generateSessionToken creates a cryptographically secure random token.
func generateSessionToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

// createSession creates a new session for the user and returns the token.
func createSession(username string) string {
	token := generateSessionToken()
	sessions.Store(token, &Session{
		Username:  username,
		CreatedAt: time.Now(),
	})
	return token
}

// getSession retrieves a session by token, or nil if not found.
func getSession(token string) *Session {
	if v, ok := sessions.Load(token); ok {
		return v.(*Session)
	}
	return nil
}

// deleteSession removes a session by token.
func deleteSession(token string) {
	sessions.Delete(token)
}

// authMiddleware checks for a valid session cookie.
// If not authenticated:
// - API requests get 401 Unauthorized
// - UI requests are redirected to /login
func authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("agentpg_session")
		if err != nil || getSession(cookie.Value) == nil {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// handleLogin handles GET (show form) and POST (process login).
func handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		errorMsg := ""
		if r.URL.Query().Get("error") == "invalid" {
			errorMsg = "Invalid username or password"
		}
		nextURL := r.URL.Query().Get("next")
		renderLoginPage(w, errorMsg, nextURL)
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse form
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	username := r.FormValue("username")
	password := r.FormValue("password")
	nextURL := r.FormValue("next")

	// Validate credentials
	hash, ok := users[username]
	if !ok || bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		redirectURL := "/login?error=invalid"
		if nextURL != "" {
			redirectURL += "&next=" + url.QueryEscape(nextURL)
		}
		http.Redirect(w, r, redirectURL, http.StatusFound)
		return
	}

	// Create session and set cookie
	token := createSession(username)
	http.SetCookie(w, &http.Cookie{
		Name:     "agentpg_session",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		MaxAge:   86400, // 24 hours
		SameSite: http.SameSiteLaxMode,
		// Secure: true, // Enable in production with HTTPS
	})

	// Redirect to next URL or default to dashboard
	if nextURL == "" {
		nextURL = "/ui/dashboard"
	}
	http.Redirect(w, r, nextURL, http.StatusFound)
}

// handleLogout clears the session and redirects to login.
func handleLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie("agentpg_session"); err == nil {
		deleteSession(cookie.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name:   "agentpg_session",
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})
	http.Redirect(w, r, "/login", http.StatusFound)
}

// handleHome redirects based on authentication state.
func handleHome(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	// Check if authenticated
	cookie, err := r.Cookie("agentpg_session")
	if err == nil && getSession(cookie.Value) != nil {
		http.Redirect(w, r, "/ui/dashboard", http.StatusFound)
		return
	}
	http.Redirect(w, r, "/login", http.StatusFound)
}

// handleHealth provides a public health check endpoint.
func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, "ok")
}

// renderLoginPage renders the login form HTML.
func renderLoginPage(w http.ResponseWriter, errorMsg, nextURL string) {
	errorHTML := ""
	if errorMsg != "" {
		errorHTML = fmt.Sprintf(`<div class="mb-4 p-3 bg-red-100 border border-red-400 text-red-700 rounded">%s</div>`, errorMsg)
	}

	html := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
    <title>Login - AgentPG Admin</title>
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <script src="https://cdn.tailwindcss.com"></script>
</head>
<body class="bg-gray-100 min-h-screen flex items-center justify-center">
    <div class="max-w-md w-full mx-4">
        <div class="bg-white rounded-lg shadow-lg p-8">
            <div class="text-center mb-8">
                <h1 class="text-2xl font-bold text-gray-900">AgentPG Admin</h1>
                <p class="text-gray-600 mt-2">Sign in to access the admin dashboard</p>
            </div>

            %s

            <form method="POST" action="/login" class="space-y-6">
                <input type="hidden" name="next" value="%s">

                <div>
                    <label for="username" class="block text-sm font-medium text-gray-700">Username</label>
                    <input type="text" id="username" name="username" required autofocus
                        class="mt-1 block w-full px-3 py-2 border border-gray-300 rounded-md shadow-sm focus:outline-none focus:ring-indigo-500 focus:border-indigo-500">
                </div>

                <div>
                    <label for="password" class="block text-sm font-medium text-gray-700">Password</label>
                    <input type="password" id="password" name="password" required
                        class="mt-1 block w-full px-3 py-2 border border-gray-300 rounded-md shadow-sm focus:outline-none focus:ring-indigo-500 focus:border-indigo-500">
                </div>

                <button type="submit"
                    class="w-full flex justify-center py-2 px-4 border border-transparent rounded-md shadow-sm text-sm font-medium text-white bg-indigo-600 hover:bg-indigo-700 focus:outline-none focus:ring-2 focus:ring-offset-2 focus:ring-indigo-500">
                    Sign in
                </button>
            </form>

            <div class="mt-6 pt-6 border-t border-gray-200">
                <p class="text-sm text-gray-500 text-center">
                    Default credentials:<br>
                    <code class="bg-gray-100 px-2 py-1 rounded">admin / admin123</code> or
                    <code class="bg-gray-100 px-2 py-1 rounded">demo / demo123</code>
                </p>
            </div>
        </div>
    </div>
</body>
</html>`, errorHTML, nextURL)

	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, html)
}

// Tool implementations

// CalculatorTool performs simple math calculations.
type CalculatorTool struct{}

func (t *CalculatorTool) Name() string        { return "calculator" }
func (t *CalculatorTool) Description() string { return "Perform simple arithmetic calculations" }

func (t *CalculatorTool) InputSchema() tool.ToolSchema {
	return tool.ToolSchema{
		Type: "object",
		Properties: map[string]tool.PropertyDef{
			"a":  {Type: "number", Description: "First number"},
			"b":  {Type: "number", Description: "Second number"},
			"op": {Type: "string", Description: "Operation: add, subtract, multiply, divide", Enum: []string{"add", "subtract", "multiply", "divide"}},
		},
		Required: []string{"a", "b", "op"},
	}
}

func (t *CalculatorTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var params struct {
		A  float64 `json:"a"`
		B  float64 `json:"b"`
		Op string  `json:"op"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}

	var result float64
	switch params.Op {
	case "add":
		result = params.A + params.B
	case "subtract":
		result = params.A - params.B
	case "multiply":
		result = params.A * params.B
	case "divide":
		if params.B == 0 {
			return "", fmt.Errorf("division by zero")
		}
		result = params.A / params.B
	default:
		return "", fmt.Errorf("unknown operation: %s", params.Op)
	}

	return fmt.Sprintf("Result: %g %s %g = %g", params.A, params.Op, params.B, result), nil
}

// logRequests logs HTTP requests.
func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start))
	})
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle shutdown gracefully
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Println("Shutting down...")
		cancel()
	}()

	// Connect to PostgreSQL
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://agentpg:agentpg@localhost:5432/agentpg?sslmode=disable"
	}

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer pool.Close()

	// Create driver and client
	drv := pgxv5.New(pool)
	client, err := agentpg.NewClient(drv, &agentpg.ClientConfig{
		APIKey:            os.Getenv("ANTHROPIC_API_KEY"),
		Name:              "admin-ui-auth-example",
		MaxConcurrentRuns: 5,
	})
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}

	// Register tool
	client.RegisterTool(&CalculatorTool{})

	// Start the client
	if err := client.Start(ctx); err != nil {
		log.Fatalf("Failed to start client: %v", err)
	}

	// Create agent in the database (after client.Start)
	_, err = client.GetOrCreateAgent(ctx, &agentpg.AgentDefinition{
		Name:         "assistant",
		Description:  "A helpful AI assistant with calculator capabilities",
		Model:        "claude-sonnet-4-5-20250929",
		SystemPrompt: "You are a helpful AI assistant. You can perform calculations using the calculator tool. Be concise and friendly.",
		Tools:        []string{"calculator"},
	})
	if err != nil {
		log.Fatalf("Failed to create agent: %v", err)
	}
	defer client.Stop(context.Background())

	// Create HTTP server
	mux := http.NewServeMux()

	// Public routes (no auth required)
	mux.HandleFunc("/login", handleLogin)
	mux.HandleFunc("/logout", handleLogout)
	mux.HandleFunc("/health", handleHealth)
	mux.HandleFunc("/", handleHome)

	// Protected UI - wrap handler with authMiddleware externally
	uiConfig := &ui.Config{
		BasePath:        "/ui",
		PageSize:        25,
		RefreshInterval: 5 * time.Second,
	}
	mux.Handle("/ui/", http.StripPrefix("/ui", authMiddleware(ui.UIHandler(drv.Store(), client, uiConfig))))

	// Start server
	server := &http.Server{
		Addr:    ":8080",
		Handler: logRequests(mux),
	}

	go func() {
		log.Println("===========================================")
		log.Println("AgentPG Admin UI with Authentication")
		log.Println("===========================================")
		log.Println("")
		log.Println("Server starting on http://localhost:8080")
		log.Println("")
		log.Println("Default credentials:")
		log.Println("  admin / admin123")
		log.Println("  demo  / demo123")
		log.Println("")
		log.Println("Endpoints:")
		log.Println("  /          - Redirects based on auth state")
		log.Println("  /login     - Login page")
		log.Println("  /logout    - Logout and redirect to login")
		log.Println("  /ui/       - Admin UI (requires auth)")
		log.Println("  /health    - Health check (public)")
		log.Println("")
		log.Println("===========================================")

		if err := server.ListenAndServe(); err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	<-ctx.Done()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	server.Shutdown(shutdownCtx)
	log.Println("Server stopped")
}
