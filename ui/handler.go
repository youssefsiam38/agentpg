package ui

import (
	"net/http"

	"github.com/youssefsiam38/agentpg"
	"github.com/youssefsiam38/agentpg/driver"
	"github.com/youssefsiam38/agentpg/ui/frontend"
	"github.com/youssefsiam38/agentpg/ui/service"
)

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
		BasePath:            cfg.BasePath,
		MetadataFilter:      cfg.MetadataFilter,
		MetadataDisplayKeys: cfg.MetadataDisplayKeys,
		MetadataFilterKeys:  cfg.MetadataFilterKeys,
		ReadOnly:            cfg.ReadOnly,
		PageSize:            cfg.PageSize,
		RefreshInterval:     cfg.RefreshInterval,
		Logger:              cfg.Logger,
	})

	return handler
}
