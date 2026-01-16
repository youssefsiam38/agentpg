// Package service provides the shared business logic for the AgentPG web UI.
//
// The service layer is HTTP-agnostic and used by both the REST API and
// SSR frontend handlers. This ensures consistency and avoids duplication.
//
// # Usage
//
//	store := driver.Store()
//	svc := service.New(store)
//
//	// Get dashboard stats
//	stats, err := svc.GetDashboardStats(ctx)
//
//	// List sessions with filtering
//	sessions, err := svc.ListSessions(ctx, service.SessionListParams{
//	    MetadataFilter: map[string]any{"tenant_id": "my-tenant"},
//	    Limit:          25,
//	    Offset:         0,
//	})
//
// # Design
//
// The service layer:
//   - Uses the driver.Store interface for all database operations
//   - Returns DTOs (Data Transfer Objects) optimized for UI display
//   - Handles pagination, filtering, and aggregation
//   - Is transaction-aware but doesn't manage transactions
package service
