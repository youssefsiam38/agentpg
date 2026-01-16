// Example: admin_ui_full
//
// This comprehensive example demonstrates the full capabilities of the AgentPG admin UI:
// - Multiple agents with different capabilities
// - Custom tools for agents
// - Agent-as-tool pattern (nested agents)
// - Chat interface with real-time responses
// - Multi-tenant admin mode
// - Separate read-only monitoring endpoint
//
// Run with:
//
//	DATABASE_URL=postgres://user:pass@localhost/agentpg ANTHROPIC_API_KEY=sk-... go run main.go
//
// Then open:
// - http://localhost:8080/         - Application home
// - http://localhost:8080/ui/      - Admin UI (full access)
// - http://localhost:8080/monitor/ - Read-only monitoring
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/youssefsiam38/agentpg"
	"github.com/youssefsiam38/agentpg/driver/pgxv5"
	"github.com/youssefsiam38/agentpg/tool"
	"github.com/youssefsiam38/agentpg/ui"
)

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
		Name:              "admin-ui-full-example-2",
		MaxConcurrentRuns: 5,
	})
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}

	// Register custom tools
	registerTools(client)

	// Register agents
	registerAgents(client)

	// Start the client
	if err := client.Start(ctx); err != nil {
		log.Fatalf("Failed to start client: %v", err)
	}
	defer client.Stop(context.Background())

	// Create HTTP server
	mux := http.NewServeMux()

	// Full admin UI with chat capabilities
	fullUIConfig := &ui.Config{
		BasePath:        "/ui",
		PageSize:        25,
		RefreshInterval: 5 * time.Second,
		Logger:          slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug})),
		// TenantID:        "default", // Set to specific tenant ID for single-tenant mode
		// TenantID: "", // Empty = admin mode (shows all tenants)
	}

	// Mount full UI at /ui/ (with chat enabled)
	mux.Handle("/ui/", http.StripPrefix("/ui", ui.UIHandler(drv.Store(), client, fullUIConfig)))

	// Mount read-only monitoring UI at /monitor/
	monitorConfig := &ui.Config{
		BasePath:        "/monitor",
		ReadOnly:        true, // Disables chat and write operations
		PageSize:        50,
		RefreshInterval: 10 * time.Second,
	}
	mux.Handle("/monitor/", http.StripPrefix("/monitor", ui.UIHandler(drv.Store(), nil, monitorConfig)))

	// Home page
	mux.HandleFunc("/", handleHome)

	// Demo: Create a sample session and run
	mux.HandleFunc("POST /demo/create-session", func(w http.ResponseWriter, r *http.Request) {
		handleCreateDemoSession(w, r, client)
	})

	// Health check
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "ok")
	})

	// Start server
	server := &http.Server{
		Addr:    ":8090",
		Handler: logRequests(mux),
	}

	go func() {
		log.Println("===========================================")
		log.Println("AgentPG Admin UI Full Example")
		log.Println("===========================================")
		log.Println("")
		log.Println("Server starting on http://localhost:8080")
		log.Println("")
		log.Println("Endpoints:")
		log.Println("  /         - Home page with links")
		log.Println("  /ui/      - Full admin UI with chat")
		log.Println("  /monitor/ - Read-only monitoring UI")
		log.Println("")
		log.Println("Registered Agents:")
		log.Println("  - assistant: General-purpose assistant")
		log.Println("  - researcher: Research specialist with search")
		log.Println("  - analyst: Data analyst with calculator")
		log.Println("  - coordinator: Orchestrates other agents")
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

// registerTools registers custom tools with the client.
func registerTools(client *agentpg.Client[pgx.Tx]) {
	// Calculator tool
	client.RegisterTool(&CalculatorTool{})

	// Web search tool (simulated)
	client.RegisterTool(&WebSearchTool{})

	// Weather tool (simulated)
	client.RegisterTool(&WeatherTool{})

	// Database query tool (simulated)
	client.RegisterTool(&DatabaseQueryTool{})
}

// registerAgents registers all agents with the client.
func registerAgents(client *agentpg.Client[pgx.Tx]) {
	// Basic assistant with calculator tool
	client.RegisterAgent(&agentpg.AgentDefinition{
		Name:         "assistant",
		Description:  "A helpful general-purpose AI assistant",
		Model:        "claude-sonnet-4-5-20250929",
		SystemPrompt: "You are a helpful AI assistant. Be concise, accurate, and friendly. If you don't know something, say so. Use available tools when appropriate.",
		Tools:        []string{"calculator"},
	})

	// Research specialist with web search
	client.RegisterAgent(&agentpg.AgentDefinition{
		Name:        "researcher",
		Description: "Research specialist that can search the web for information",
		Model:       "claude-sonnet-4-5-20250929",
		SystemPrompt: `You are a research specialist. Your job is to find and synthesize information.
When asked about a topic:
1. Search for relevant information
2. Analyze and verify the findings
3. Present a clear, well-organized summary

Always cite your sources when providing information.`,
		Tools: []string{"web_search"},
	})

	// Data analyst with calculator
	client.RegisterAgent(&agentpg.AgentDefinition{
		Name:        "analyst",
		Description: "Data analyst that can perform calculations and analyze data",
		Model:       "claude-sonnet-4-5-20250929",
		SystemPrompt: `You are a data analyst. Your job is to analyze data and perform calculations.
When given numbers or data:
1. Understand what analysis is needed
2. Perform the necessary calculations
3. Present findings with clear explanations

Always show your work when doing calculations.`,
		Tools: []string{"calculator", "database_query"},
	})

	// Weather assistant
	client.RegisterAgent(&agentpg.AgentDefinition{
		Name:        "weather_assistant",
		Description: "Weather specialist that provides weather information",
		Model:       "claude-3-5-haiku-20241022", // Use faster model for simple tasks
		SystemPrompt: `You are a weather assistant. Provide helpful weather information and forecasts.
Be conversational and helpful. Include relevant advice based on weather conditions.`,
		Tools: []string{"get_weather"},
	})

	// Coordinator that can delegate to other agents
	client.RegisterAgent(&agentpg.AgentDefinition{
		Name:        "coordinator",
		Description: "Orchestrator that delegates tasks to specialized agents",
		Model:       "claude-sonnet-4-5-20250929",
		SystemPrompt: `You are a coordinator agent. Your job is to understand user requests and delegate to the appropriate specialist:

- For research questions: delegate to the researcher
- For data analysis and calculations: delegate to the analyst
- For weather information: delegate to the weather_assistant

Analyze the request, delegate to the right specialist, and synthesize their responses into a coherent answer.`,
		Agents: []string{"researcher", "analyst", "weather_assistant"},
	})
}

// Tool implementations

type CalculatorTool struct{}

func (t *CalculatorTool) Name() string        { return "calculator" }
func (t *CalculatorTool) Description() string { return "Perform mathematical calculations" }

func (t *CalculatorTool) InputSchema() tool.ToolSchema {
	return tool.ToolSchema{
		Type: "object",
		Properties: map[string]tool.PropertyDef{
			"expression": {
				Type:        "string",
				Description: "Mathematical expression to evaluate (e.g., '2 + 2', '10 * 5', 'sqrt(16)')",
			},
		},
		Required: []string{"expression"},
	}
}

func (t *CalculatorTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var params struct {
		Expression string `json:"expression"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}

	// Evaluate the expression
	result, err := evalExpression(params.Expression)
	if err != nil {
		return "", fmt.Errorf("failed to evaluate expression: %w", err)
	}
	return fmt.Sprintf("Result: %s = %g", params.Expression, result), nil
}

type WebSearchTool struct{}

func (t *WebSearchTool) Name() string        { return "web_search" }
func (t *WebSearchTool) Description() string { return "Search the web for information" }

func (t *WebSearchTool) InputSchema() tool.ToolSchema {
	return tool.ToolSchema{
		Type: "object",
		Properties: map[string]tool.PropertyDef{
			"query": {
				Type:        "string",
				Description: "Search query",
			},
		},
		Required: []string{"query"},
	}
}

func (t *WebSearchTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var params struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}

	// Simulate web search results
	results := fmt.Sprintf(`Search results for "%s":

1. Wikipedia - %s
   A comprehensive overview of the topic with detailed information...

2. News Article - Latest developments in %s
   Recent updates and news related to the search query...

3. Research Paper - Analysis of %s
   Academic research providing in-depth analysis...`,
		params.Query, params.Query, params.Query, params.Query)

	return results, nil
}

type WeatherTool struct{}

func (t *WeatherTool) Name() string        { return "get_weather" }
func (t *WeatherTool) Description() string { return "Get current weather for a location" }

func (t *WeatherTool) InputSchema() tool.ToolSchema {
	return tool.ToolSchema{
		Type: "object",
		Properties: map[string]tool.PropertyDef{
			"location": {
				Type:        "string",
				Description: "City name or location",
			},
		},
		Required: []string{"location"},
	}
}

func (t *WeatherTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var params struct {
		Location string `json:"location"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}

	// Simulate weather data
	temps := []int{68, 72, 75, 80, 65, 70, 77}
	conditions := []string{"Sunny", "Partly Cloudy", "Cloudy", "Light Rain", "Clear"}
	temp := temps[rand.Intn(len(temps))]
	condition := conditions[rand.Intn(len(conditions))]

	weather := fmt.Sprintf(`Weather for %s:
Temperature: %d°F
Condition: %s
Humidity: %d%%
Wind: %d mph

Forecast: Similar conditions expected for the next 24 hours.`,
		params.Location, temp, condition, 40+rand.Intn(40), 5+rand.Intn(15))

	return weather, nil
}

type DatabaseQueryTool struct{}

func (t *DatabaseQueryTool) Name() string        { return "database_query" }
func (t *DatabaseQueryTool) Description() string { return "Query a database for information" }

func (t *DatabaseQueryTool) InputSchema() tool.ToolSchema {
	return tool.ToolSchema{
		Type: "object",
		Properties: map[string]tool.PropertyDef{
			"query": {
				Type:        "string",
				Description: "SQL-like query description",
			},
		},
		Required: []string{"query"},
	}
}

func (t *DatabaseQueryTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var params struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}

	// Simulate database results
	result := fmt.Sprintf(`Query: %s

Results:
+----------+--------+--------+
| ID       | Name   | Value  |
+----------+--------+--------+
| 1        | Item A | 100.00 |
| 2        | Item B | 250.50 |
| 3        | Item C | 75.25  |
+----------+--------+--------+

Total: 3 rows returned`,
		params.Query)

	return result, nil
}

// HTTP Handlers

func handleHome(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	html := `<!DOCTYPE html>
<html>
<head>
    <title>AgentPG Admin UI - Full Example</title>
    <script src="https://cdn.tailwindcss.com"></script>
</head>
<body class="bg-gray-100 min-h-screen">
    <div class="max-w-4xl mx-auto py-12 px-4">
        <div class="bg-white rounded-lg shadow-lg p-8">
            <h1 class="text-3xl font-bold text-gray-900 mb-4">AgentPG Admin UI</h1>
            <p class="text-gray-600 mb-8">Full-featured example demonstrating the embedded admin UI with multiple agents and tools.</p>

            <div class="grid md:grid-cols-2 gap-6 mb-8">
                <a href="/ui/" class="block p-6 bg-indigo-50 rounded-lg hover:bg-indigo-100 transition">
                    <h2 class="text-xl font-semibold text-indigo-900 mb-2">Admin Dashboard</h2>
                    <p class="text-indigo-700">Full admin UI with chat, session management, and monitoring.</p>
                </a>

                <a href="/monitor/" class="block p-6 bg-green-50 rounded-lg hover:bg-green-100 transition">
                    <h2 class="text-xl font-semibold text-green-900 mb-2">Read-Only Monitor</h2>
                    <p class="text-green-700">Monitoring-only view without chat or write operations.</p>
                </a>
            </div>

            <h3 class="text-lg font-semibold text-gray-900 mb-4">Quick Links</h3>
            <div class="grid md:grid-cols-3 gap-4 mb-8">
                <a href="/ui/chat" class="p-4 bg-purple-50 rounded hover:bg-purple-100 transition text-center">
                    <span class="text-purple-900 font-medium">Chat Interface</span>
                </a>
                <a href="/ui/runs" class="p-4 bg-blue-50 rounded hover:bg-blue-100 transition text-center">
                    <span class="text-blue-900 font-medium">Run Monitor</span>
                </a>
                <a href="/ui/agents" class="p-4 bg-yellow-50 rounded hover:bg-yellow-100 transition text-center">
                    <span class="text-yellow-900 font-medium">Agent Registry</span>
                </a>
                <a href="/ui/instances" class="p-4 bg-red-50 rounded hover:bg-red-100 transition text-center">
                    <span class="text-red-900 font-medium">Instances</span>
                </a>
                <a href="/ui/sessions" class="p-4 bg-gray-50 rounded hover:bg-gray-100 transition text-center">
                    <span class="text-gray-900 font-medium">Sessions</span>
                </a>
                <a href="/ui/tool-executions" class="p-4 bg-gray-50 rounded hover:bg-gray-100 transition text-center">
                    <span class="text-gray-900 font-medium">Tool Executions</span>
                </a>
            </div>

            <h3 class="text-lg font-semibold text-gray-900 mb-4">Registered Agents</h3>
            <div class="space-y-3">
                <div class="p-4 bg-gray-50 rounded">
                    <span class="font-medium">assistant</span>
                    <span class="text-gray-500 ml-2">- General-purpose AI assistant</span>
                </div>
                <div class="p-4 bg-gray-50 rounded">
                    <span class="font-medium">researcher</span>
                    <span class="text-gray-500 ml-2">- Research specialist with web search</span>
                    <span class="ml-2 px-2 py-0.5 bg-blue-100 text-blue-800 text-xs rounded">web_search</span>
                </div>
                <div class="p-4 bg-gray-50 rounded">
                    <span class="font-medium">analyst</span>
                    <span class="text-gray-500 ml-2">- Data analyst with calculator</span>
                    <span class="ml-2 px-2 py-0.5 bg-blue-100 text-blue-800 text-xs rounded">calculator</span>
                    <span class="ml-1 px-2 py-0.5 bg-blue-100 text-blue-800 text-xs rounded">database_query</span>
                </div>
                <div class="p-4 bg-gray-50 rounded">
                    <span class="font-medium">weather_assistant</span>
                    <span class="text-gray-500 ml-2">- Weather information specialist</span>
                    <span class="ml-2 px-2 py-0.5 bg-blue-100 text-blue-800 text-xs rounded">get_weather</span>
                </div>
                <div class="p-4 bg-gray-50 rounded">
                    <span class="font-medium">coordinator</span>
                    <span class="text-gray-500 ml-2">- Orchestrates other agents</span>
                    <span class="ml-2 px-2 py-0.5 bg-purple-100 text-purple-800 text-xs rounded">researcher</span>
                    <span class="ml-1 px-2 py-0.5 bg-purple-100 text-purple-800 text-xs rounded">analyst</span>
                    <span class="ml-1 px-2 py-0.5 bg-purple-100 text-purple-800 text-xs rounded">weather_assistant</span>
                </div>
            </div>

            <div class="mt-8 pt-6 border-t border-gray-200">
                <h3 class="text-lg font-semibold text-gray-900 mb-4">Create Demo Session</h3>
                <p class="text-gray-600 mb-4">Create a sample session with a test run to see the UI in action.</p>
                <form method="POST" action="/demo/create-session" class="flex gap-4">
                    <button type="submit" class="px-4 py-2 bg-indigo-600 text-white rounded hover:bg-indigo-700 transition">
                        Create Demo Session
                    </button>
                </form>
            </div>
        </div>
    </div>
</body>
</html>`
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, html)
}

func handleCreateDemoSession(w http.ResponseWriter, r *http.Request, client *agentpg.Client[pgx.Tx]) {
	ctx := r.Context()

	// Create a demo session
	tenantID := fmt.Sprintf("demo-tenant-%d", time.Now().Unix()%1000)
	sessionID, err := client.NewSession(ctx, tenantID, "demo-session", nil, map[string]any{
		"created_by": "admin_ui_full_example",
		"demo":       true,
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to create session: %v", err), http.StatusInternalServerError)
		return
	}

	// Start a run with the assistant agent
	runID, err := client.RunFast(ctx, sessionID, "assistant", "Hello! Please introduce yourself and explain what you can help with.")
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to create run: %v", err), http.StatusInternalServerError)
		return
	}

	// Redirect to the session in the UI
	http.Redirect(w, r, fmt.Sprintf("/ui/sessions/%s?run=%s", sessionID, runID), http.StatusSeeOther)
}

// logRequests logs HTTP requests
func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start))
	})
}

// evalExpression evaluates a simple mathematical expression
func evalExpression(expr string) (float64, error) {
	// Remove spaces
	expr = strings.ReplaceAll(expr, " ", "")
	return parseAddSub(expr, 0)
}

func parseAddSub(expr string, pos int) (float64, error) {
	result, pos, err := parseMulDiv(expr, pos)
	if err != nil {
		return 0, err
	}
	for pos < len(expr) {
		op := expr[pos]
		if op != '+' && op != '-' {
			break
		}
		pos++
		right, newPos, err := parseMulDiv(expr, pos)
		if err != nil {
			return 0, err
		}
		pos = newPos
		if op == '+' {
			result += right
		} else {
			result -= right
		}
	}
	return result, nil
}

func parseMulDiv(expr string, pos int) (float64, int, error) {
	result, pos, err := parseNumber(expr, pos)
	if err != nil {
		return 0, pos, err
	}
	for pos < len(expr) {
		op := expr[pos]
		if op != '*' && op != '/' {
			break
		}
		pos++
		right, newPos, err := parseNumber(expr, pos)
		if err != nil {
			return 0, pos, err
		}
		pos = newPos
		if op == '*' {
			result *= right
		} else {
			if right == 0 {
				return 0, pos, fmt.Errorf("division by zero")
			}
			result /= right
		}
	}
	return result, pos, nil
}

func parseNumber(expr string, pos int) (float64, int, error) {
	if pos >= len(expr) {
		return 0, pos, fmt.Errorf("unexpected end of expression")
	}

	// Handle parentheses
	if expr[pos] == '(' {
		pos++
		result, err := evalExpression(expr[pos:])
		if err != nil {
			return 0, pos, err
		}
		// Find closing parenthesis
		depth := 1
		for i := pos; i < len(expr); i++ {
			if expr[i] == '(' {
				depth++
			} else if expr[i] == ')' {
				depth--
				if depth == 0 {
					return result, i + 1, nil
				}
			}
		}
		return 0, pos, fmt.Errorf("unmatched parenthesis")
	}

	// Handle negative numbers
	negative := false
	if expr[pos] == '-' {
		negative = true
		pos++
	}

	start := pos
	for pos < len(expr) && (expr[pos] >= '0' && expr[pos] <= '9' || expr[pos] == '.') {
		pos++
	}
	if start == pos {
		return 0, pos, fmt.Errorf("expected number at position %d", pos)
	}

	num, err := strconv.ParseFloat(expr[start:pos], 64)
	if err != nil {
		return 0, pos, err
	}
	if negative {
		num = -num
	}
	return num, pos, nil
}
