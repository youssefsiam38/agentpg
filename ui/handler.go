package ui

import (
	"net/http"

	"github.com/youssefsiam38/agentpg"
	"github.com/youssefsiam38/agentpg/driver"
	"github.com/youssefsiam38/agentpg/ui/api"
	"github.com/youssefsiam38/agentpg/ui/frontend"
	"github.com/youssefsiam38/agentpg/ui/service"
)

// APIHandler returns an http.Handler for the REST API.
// This handler provides JSON endpoints for programmatic access.
//
// Usage:
//
//	http.Handle("/api/", http.StripPrefix("/api", ui.APIHandler(store, cfg)))
//	r.Mount("/api", ui.APIHandler(store, cfg))
func APIHandler[TTx any](store driver.Store[TTx], cfg *Config) http.Handler {
	if cfg == nil {
		cfg = DefaultConfig()
	} else {
		cfg.applyDefaults()
	}

	// Validate configuration (panic on invalid config as this is a programmer error)
	if err := cfg.validate(); err != nil {
		panic("ui: invalid configuration: " + err.Error())
	}

	svc := service.New(store)
	handler := api.NewRouter(svc, &api.Config{
		TenantID: cfg.TenantID,
		PageSize: cfg.PageSize,
		Logger:   cfg.Logger,
	})

	if cfg.AuthMiddleware != nil {
		handler = cfg.AuthMiddleware(handler)
	}

	return handler
}

// UIHandler returns an http.Handler for the SSR frontend.
// This handler provides an interactive admin interface using HTMX + Tailwind.
//
// The client parameter is required for chat functionality. If nil, chat
// features will be disabled (equivalent to ReadOnly mode for chat).
//
// Usage:
//
//	http.Handle("/ui/", http.StripPrefix("/ui", ui.UIHandler(store, client, cfg)))
//	r.Mount("/ui", ui.UIHandler(store, client, cfg))
func UIHandler[TTx any](store driver.Store[TTx], client *agentpg.Client[TTx], cfg *Config) http.Handler {
	if cfg == nil {
		cfg = DefaultConfig()
	} else {
		cfg.applyDefaults()
	}

	// Validate configuration (panic on invalid config as this is a programmer error)
	if err := cfg.validate(); err != nil {
		panic("ui: invalid configuration: " + err.Error())
	}

	svc := service.New(store)
	handler := frontend.NewRouter(svc, client, &frontend.Config{
		BasePath:        cfg.BasePath,
		TenantID:        cfg.TenantID,
		ReadOnly:        cfg.ReadOnly,
		PageSize:        cfg.PageSize,
		RefreshInterval: cfg.RefreshInterval,
		Logger:          cfg.Logger,
	})

	if cfg.AuthMiddleware != nil {
		handler = cfg.AuthMiddleware(handler)
	}

	return handler
}

// Handler returns a combined handler serving both API and frontend.
// This is a convenience function for simple setups where both are
// mounted under the same base path.
//
// Usage:
//
//	http.Handle("/admin/", http.StripPrefix("/admin", ui.Handler(store, client, cfg)))
//
// This mounts:
//   - /admin/api/* - REST API endpoints
//   - /admin/* - Frontend pages
func Handler[TTx any](store driver.Store[TTx], client *agentpg.Client[TTx], cfg *Config) http.Handler {
	if cfg == nil {
		cfg = DefaultConfig()
	} else {
		cfg.applyDefaults()
	}

	// Validate configuration (panic on invalid config as this is a programmer error)
	if err := cfg.validate(); err != nil {
		panic("ui: invalid configuration: " + err.Error())
	}

	mux := http.NewServeMux()
	mux.Handle("/api/", http.StripPrefix("/api", APIHandler(store, cfg)))
	mux.Handle("/", UIHandler(store, client, cfg))

	return mux
}
