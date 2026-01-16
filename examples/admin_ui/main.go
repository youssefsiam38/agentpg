// Example: admin_ui
//
// This example demonstrates how to embed the AgentPG admin UI into your application.
// It shows:
// - Mounting the UI handler for the web interface
// - Running the UI alongside your own application routes
//
// Run with:
//
//	DATABASE_URL=postgres://user:pass@localhost/agentpg ANTHROPIC_API_KEY=sk-... go run main.go
//
// Then open http://localhost:8080/ui/ in your browser.
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/youssefsiam38/agentpg"
	"github.com/youssefsiam38/agentpg/driver/pgxv5"
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
		APIKey: os.Getenv("ANTHROPIC_API_KEY"),
		Name:   "admin-ui-example",
	})
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}

	// Register a simple assistant agent
	if err := client.RegisterAgent(&agentpg.AgentDefinition{
		Name:         "assistant",
		Description:  "A helpful AI assistant",
		Model:        "claude-sonnet-4-5-20250929",
		SystemPrompt: "You are a helpful AI assistant. Be concise and friendly.",
	}); err != nil {
		log.Fatalf("Failed to register agent: %v", err)
	}

	// Start the client (begins processing runs)
	if err := client.Start(ctx); err != nil {
		log.Fatalf("Failed to start client: %v", err)
	}
	defer client.Stop(context.Background())

	// Create HTTP server with admin UI
	mux := http.NewServeMux()

	// Mount the admin UI frontend at /ui/
	// This provides the web interface with HTMX + Tailwind
	uiHandler := ui.UIHandler(drv.Store(), client, &ui.Config{
		PageSize:        25,
		RefreshInterval: 5 * time.Second,
	})
	mux.Handle("/ui/", http.StripPrefix("/ui", uiHandler))

	// Your own application routes
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprintf(w, `<!DOCTYPE html>
<html>
<head><title>AgentPG Admin UI Example</title></head>
<body style="font-family: sans-serif; max-width: 600px; margin: 50px auto; padding: 20px;">
	<h1>AgentPG Admin UI Example</h1>
	<p>This example demonstrates the embedded admin UI.</p>
	<ul>
		<li><a href="/ui/">Admin UI Dashboard</a> - Full web interface</li>
		<li><a href="/ui/sessions">Sessions</a> - View conversation sessions</li>
		<li><a href="/ui/runs">Runs</a> - Monitor agent runs</li>
		<li><a href="/ui/agents">Agents</a> - View registered agents</li>
		<li><a href="/ui/instances">Instances</a> - Monitor worker instances</li>
		<li><a href="/ui/chat">Chat</a> - Interactive chat interface</li>
	</ul>
</body>
</html>`)
	})

	// Health check endpoint
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "ok")
	})

	// Start HTTP server
	server := &http.Server{
		Addr:    ":8080",
		Handler: mux,
	}

	go func() {
		log.Println("Starting server on http://localhost:8080")
		log.Println("Admin UI available at http://localhost:8080/ui/")
		if err := server.ListenAndServe(); err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	// Wait for shutdown
	<-ctx.Done()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	server.Shutdown(shutdownCtx)
	log.Println("Server stopped")
}
