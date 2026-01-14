// Package frontend provides SSR frontend handlers for the AgentPG admin UI.
//
// The frontend uses HTMX for interactivity and Tailwind CSS for styling,
// both loaded via CDN for simplicity.
//
// # Routes
//
// Main Pages:
//   - GET / - Redirect to dashboard
//   - GET /dashboard - Dashboard with stats
//   - GET /sessions - Sessions list
//   - GET /sessions/{id} - Session detail
//   - GET /runs - Runs list
//   - GET /runs/{id} - Run detail
//   - GET /runs/{id}/conversation - Conversation view
//   - GET /tool-executions - Tool executions list
//   - GET /tool-executions/{id} - Tool execution detail
//   - GET /agents - Agents registry
//   - GET /instances - Instances monitoring
//   - GET /compaction - Compaction events
//
// Chat Interface:
//   - GET /chat - Chat interface
//   - GET /chat/new - New session with agent selection
//   - POST /chat/send - Send message (HTMX)
//   - GET /chat/poll/{runId} - Poll for response (HTMX)
//
// HTMX Fragments:
//   - GET /fragments/* - Partial HTML fragments for HTMX updates
//
// Static Assets:
//   - GET /static/* - Embedded static files (JS, CSS)
package frontend
