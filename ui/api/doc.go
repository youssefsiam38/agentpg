// Package api provides REST API handlers for the AgentPG admin UI.
//
// The API layer provides JSON endpoints for programmatic access to
// AgentPG monitoring and management features.
//
// # Endpoints
//
// Dashboard:
//   - GET /dashboard - Dashboard statistics
//   - GET /dashboard/events - SSE stream for real-time updates
//
// Sessions:
//   - GET /sessions - List sessions (paginated)
//   - GET /sessions/{id} - Session detail
//   - POST /sessions - Create new session
//
// Runs:
//   - GET /runs - List runs (filtered, paginated)
//   - GET /runs/{id} - Run detail
//   - GET /runs/{id}/hierarchy - Run hierarchy tree
//
// Iterations:
//   - GET /iterations - List iterations
//   - GET /iterations/{id} - Iteration detail
//
// Tool Executions:
//   - GET /tool-executions - List tool executions
//   - GET /tool-executions/{id} - Tool execution detail
//
// Messages:
//   - GET /messages - List messages (conversation)
//
// Registry:
//   - GET /agents - List agents
//   - GET /tools - List tools
//
// Instances:
//   - GET /instances - List instances
//
// Compaction:
//   - GET /compaction-events - List compaction events
package api
