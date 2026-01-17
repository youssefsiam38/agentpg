// Package ui provides an embedded web UI for AgentPG.
//
// The package provides an HTTP handler for the SSR frontend:
//   - UIHandler: SSR frontend with HTMX + Tailwind
//
// # Quick Start
//
//	pool, _ := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
//	drv := pgxv5.New(pool)
//
//	client, _ := agentpg.NewClient(drv, &agentpg.ClientConfig{
//	    APIKey: os.Getenv("ANTHROPIC_API_KEY"),
//	})
//	client.Start(ctx)
//
//	// Create agent in database (after Start)
//	client.CreateAgent(ctx, &agentpg.AgentDefinition{
//	    Name:  "assistant",
//	    Model: "claude-sonnet-4-5-20250929",
//	})
//
//	mux := http.NewServeMux()
//	mux.Handle("/ui/", http.StripPrefix("/ui", ui.UIHandler(drv.Store(), client, nil)))
//
//	http.ListenAndServe(":8080", mux)
//
// # Configuration
//
// The handler accepts an optional Config struct for customization:
//
//	cfg := &ui.Config{
//	    MetadataFilter: map[string]any{"tenant_id": "my-tenant"},  // Filter sessions by metadata
//	    ReadOnly:       false,                                      // Disable chat if true
//	    RefreshInterval: 5 * time.Second,
//	    PageSize:        25,
//	}
//
// # Framework Integration
//
// The handler returns standard http.Handler, compatible with any Go framework:
//
//	// Standard library
//	http.Handle("/ui/", http.StripPrefix("/ui", ui.UIHandler(store, client, cfg)))
//
//	// Chi
//	r.Mount("/ui", ui.UIHandler(store, client, cfg))
//
//	// Gin
//	router.Any("/ui/*any", gin.WrapH(ui.UIHandler(store, client, cfg)))
//
//	// Echo
//	e.Any("/ui/*", echo.WrapHandler(ui.UIHandler(store, client, cfg)))
//
// # Adding Middleware
//
// Wrap handlers externally using standard Go patterns:
//
//	// Single middleware
//	http.Handle("/ui/", http.StripPrefix("/ui", authMiddleware(ui.UIHandler(store, client, cfg))))
//
//	// Multiple middlewares chained
//	handler := authMiddleware(loggingMiddleware(rateLimitMiddleware(ui.UIHandler(store, client, cfg))))
//	http.Handle("/ui/", http.StripPrefix("/ui", handler))
//
//	// Using justinas/alice
//	chain := alice.New(authMiddleware, loggingMiddleware)
//	http.Handle("/ui/", http.StripPrefix("/ui", chain.Then(ui.UIHandler(store, client, cfg))))
package ui
