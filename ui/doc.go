// Package ui provides an embedded admin UI for AgentPG.
//
// The package provides two separate HTTP handlers:
//   - APIHandler: REST API endpoints with JSON responses
//   - UIHandler: SSR frontend with HTMX + Tailwind
//
// Both handlers share the same service layer, ensuring consistency
// between API and frontend operations.
//
// # Quick Start
//
//	pool, _ := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
//	drv := pgxv5.New(pool)
//
//	client, _ := agentpg.NewClient(drv, &agentpg.ClientConfig{
//	    APIKey: os.Getenv("ANTHROPIC_API_KEY"),
//	})
//	client.RegisterAgent(&agentpg.AgentDefinition{
//	    Name:  "assistant",
//	    Model: "claude-sonnet-4-5-20250929",
//	})
//	client.Start(ctx)
//
//	mux := http.NewServeMux()
//	mux.Handle("/api/", http.StripPrefix("/api", ui.APIHandler(drv.Store(), nil)))
//	mux.Handle("/ui/", http.StripPrefix("/ui", ui.UIHandler(drv.Store(), client, nil)))
//
//	http.ListenAndServe(":8080", mux)
//
// # Configuration
//
// Both handlers accept an optional Config struct for customization:
//
//	cfg := &ui.Config{
//	    TenantID:        "my-tenant",  // Filter to single tenant (empty = admin mode)
//	    ReadOnly:        false,         // Disable chat if true
//	    AuthMiddleware:  myAuthMiddleware,
//	    RefreshInterval: 5 * time.Second,
//	    PageSize:        25,
//	}
//
// # Framework Integration
//
// The handlers return standard http.Handler, compatible with any Go framework:
//
//	// Standard library
//	http.Handle("/api/", ui.APIHandler(store, cfg))
//
//	// Chi
//	r.Mount("/api", ui.APIHandler(store, cfg))
//
//	// Gin
//	router.Any("/api/*any", gin.WrapH(ui.APIHandler(store, cfg)))
//
//	// Echo
//	e.Any("/api/*", echo.WrapHandler(ui.APIHandler(store, cfg)))
package ui
