# REST API Reference

AgentPG provides a REST API for programmatic access to sessions, runs, tool executions, and monitoring data.

## Setup

```go
import "github.com/youssefsiam38/agentpg/ui"

// Mount API at /api/
apiConfig := &ui.Config{
    PageSize: 25,
}
http.Handle("/api/", http.StripPrefix("/api", ui.APIHandler(drv.Store(), apiConfig)))
```

## Response Format

All responses follow a consistent JSON structure:

### Success Response

```json
{
  "data": { ... },
  "meta": {
    "total_count": 100,
    "has_more": true,
    "limit": 25,
    "offset": 0
  }
}
```

### Error Response

```json
{
  "error": {
    "code": "not_found",
    "message": "Session not found"
  }
}
```

### Error Codes

| Code | HTTP Status | Description |
|------|-------------|-------------|
| `invalid_id` | 400 | Invalid UUID format |
| `invalid_body` | 400 | Invalid JSON in request body |
| `invalid_name` | 400 | Missing required name parameter |
| `missing_param` | 400 | Required query parameter not provided |
| `not_found` | 404 | Resource not found |
| `internal_error` | 500 | Server error |
| `sse_not_supported` | 500 | SSE not supported |

## Pagination

All list endpoints support pagination:

| Parameter | Type | Default | Max | Description |
|-----------|------|---------|-----|-------------|
| `limit` | int | 25 | 1000 | Page size |
| `offset` | int | 0 | - | Skip N records |
| `order_by` | string | varies | - | Sort field (resource-specific) |
| `order_dir` | string | - | - | `asc` or `desc` |

---

## Dashboard

### Get Dashboard Statistics

```
GET /dashboard
```

Returns aggregated statistics for the system.

**Response:**

```json
{
  "data": {
    "total_sessions": 150,
    "active_sessions": 12,
    "sessions_today": 8,
    "total_runs": 1200,
    "active_runs": 5,
    "pending_runs": 2,
    "completed_runs_24h": 45,
    "failed_runs_24h": 3,
    "pending_tools": 1,
    "running_tools": 2,
    "failed_tools_24h": 1,
    "active_instances": 3,
    "leader_instance_id": "worker-1",
    "runs_by_state": {
      "pending": 2,
      "completed": 1193,
      "failed": 5
    },
    "tools_by_state": {
      "pending": 1,
      "completed": 500,
      "failed": 5
    },
    "recent_runs": [...],
    "recent_tool_errors": [...],
    "recent_sessions": [...],
    "total_tokens_24h": 125000,
    "avg_tokens_per_run": 2500,
    "avg_run_duration_ms": 3500,
    "success_rate_24h": 0.94,
    "avg_iterations_per_run": 2.3,
    "runs_by_agent": {
      "assistant": 800,
      "researcher": 400
    },
    "top_agents": [...],
    "top_tools": [...]
  }
}
```

### Dashboard Events (SSE)

```
GET /dashboard/events
```

Server-Sent Events stream for real-time dashboard updates.

---

## Sessions

### List Sessions

```
GET /sessions
```

**Query Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `tenant_id` | string | Filter by tenant (admin mode) |
| `limit` | int | Page size |
| `offset` | int | Pagination offset |
| `order_by` | string | `created_at` or `updated_at` |
| `order_dir` | string | `asc` or `desc` |

**Response:**

```json
{
  "data": {
    "sessions": [
      {
        "id": "550e8400-e29b-41d4-a716-446655440000",
        "tenant_id": "tenant-1",
        "user_id": "user-123",
        "agent_name": "assistant",
        "depth": 0,
        "run_count": 5,
        "message_count": 20,
        "compaction_count": 0,
        "last_activity_at": "2024-01-15T10:30:00Z",
        "created_at": "2024-01-15T09:00:00Z"
      }
    ],
    "total_count": 150,
    "has_more": true
  }
}
```

### Get Session

```
GET /sessions/{id}
```

**Response:**

```json
{
  "data": {
    "session": {
      "id": "550e8400-e29b-41d4-a716-446655440000",
      "tenant_id": "tenant-1",
      "user_id": "user-123",
      "parent_session_id": null,
      "depth": 0,
      "metadata": {},
      "compaction_count": 0,
      "created_at": "2024-01-15T09:00:00Z",
      "updated_at": "2024-01-15T10:30:00Z"
    },
    "parent_session": null,
    "child_sessions": [],
    "run_count": 5,
    "message_count": 20,
    "token_usage": {
      "input_tokens": 5000,
      "output_tokens": 3000,
      "total_tokens": 8000,
      "cache_hit_rate": 0.35
    },
    "recent_runs": [...],
    "recent_messages": [...]
  }
}
```

### Create Session

```
POST /sessions
```

**Request Body:**

```json
{
  "tenant_id": "tenant-1",
  "user_id": "user-123",
  "agent_name": "assistant",
  "metadata": {
    "source": "web"
  }
}
```

**Response:** `201 Created`

```json
{
  "data": {
    "id": "550e8400-e29b-41d4-a716-446655440000",
    "tenant_id": "tenant-1",
    "user_id": "user-123",
    "created_at": "2024-01-15T09:00:00Z"
  }
}
```

---

## Runs

### List Runs

```
GET /runs
```

**Query Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `tenant_id` | string | Filter by tenant (admin mode) |
| `session_id` | UUID | Filter by session |
| `agent_name` | string | Filter by agent |
| `state` | string | Filter by state |
| `run_mode` | string | `batch` or `streaming` |
| `limit` | int | Page size |
| `offset` | int | Pagination offset |
| `order_by` | string | `created_at` or `finalized_at` |
| `order_dir` | string | `asc` or `desc` |

**Run States:**
- `pending` - Waiting for worker
- `batch_submitting` - Submitting to Batch API
- `batch_pending` - Batch submitted, waiting
- `batch_processing` - Claude processing
- `streaming` - Streaming API processing
- `pending_tools` - Waiting for tool executions
- `awaiting_input` - Needs continuation
- `completed` - Success (terminal)
- `cancelled` - Cancelled (terminal)
- `failed` - Error (terminal)

**Response:**

```json
{
  "data": {
    "runs": [
      {
        "id": "660e8400-e29b-41d4-a716-446655440001",
        "session_id": "550e8400-e29b-41d4-a716-446655440000",
        "agent_name": "assistant",
        "run_mode": "streaming",
        "state": "completed",
        "depth": 0,
        "has_parent": false,
        "iteration_count": 2,
        "tool_iterations": 1,
        "total_tokens": 2500,
        "duration": "3.5s",
        "error_message": null,
        "created_at": "2024-01-15T10:00:00Z",
        "finalized_at": "2024-01-15T10:00:03Z"
      }
    ],
    "total_count": 1200,
    "has_more": true
  }
}
```

### Get Run

```
GET /runs/{id}
```

**Response:**

```json
{
  "data": {
    "run": {
      "id": "660e8400-e29b-41d4-a716-446655440001",
      "session_id": "550e8400-e29b-41d4-a716-446655440000",
      "agent_name": "assistant",
      "run_mode": "streaming",
      "state": "completed",
      "depth": 0,
      "prompt": "Hello, how are you?",
      "response_text": "I'm doing well, thank you!",
      "stop_reason": "end_turn",
      "input_tokens": 1500,
      "output_tokens": 1000,
      "iteration_count": 2,
      "tool_iterations": 1,
      "created_at": "2024-01-15T10:00:00Z",
      "finalized_at": "2024-01-15T10:00:03Z"
    },
    "session": {...},
    "iterations": [...],
    "tool_executions": [...],
    "messages": [...],
    "parent_run": null,
    "child_runs": []
  }
}
```

### Get Run Hierarchy

```
GET /runs/{id}/hierarchy
```

Returns the tree structure for nested agents (agent-as-tool pattern).

**Response:**

```json
{
  "data": {
    "root": {
      "run": {
        "id": "run-pm",
        "agent_name": "project-manager",
        "depth": 0
      },
      "children": [
        {
          "run": {
            "id": "run-lead",
            "agent_name": "tech-lead",
            "depth": 1
          },
          "children": [
            {
              "run": {
                "id": "run-dev",
                "agent_name": "frontend-dev",
                "depth": 2
              },
              "children": []
            }
          ]
        }
      ]
    }
  }
}
```

### Get Run Iterations

```
GET /runs/{id}/iterations
```

**Response:**

```json
{
  "data": {
    "iterations": [
      {
        "id": "770e8400-e29b-41d4-a716-446655440002",
        "run_id": "660e8400-e29b-41d4-a716-446655440001",
        "iteration_number": 1,
        "is_streaming": true,
        "trigger_type": "user_prompt",
        "stop_reason": "tool_use",
        "has_tool_use": true,
        "tool_count": 1,
        "input_tokens": 800,
        "output_tokens": 400,
        "duration": "1.2s",
        "created_at": "2024-01-15T10:00:00Z",
        "completed_at": "2024-01-15T10:00:01Z"
      }
    ]
  }
}
```

### Get Run Tool Executions

```
GET /runs/{id}/tool-executions
```

**Response:**

```json
{
  "data": {
    "tool_executions": [
      {
        "id": "880e8400-e29b-41d4-a716-446655440003",
        "run_id": "660e8400-e29b-41d4-a716-446655440001",
        "iteration_id": "770e8400-e29b-41d4-a716-446655440002",
        "tool_use_id": "toolu_01xyz",
        "tool_name": "get_weather",
        "tool_input": {"city": "NYC"},
        "state": "completed",
        "is_agent_tool": false,
        "is_error": false,
        "attempt_count": 1,
        "max_attempts": 2,
        "duration": "0.5s",
        "created_at": "2024-01-15T10:00:01Z",
        "completed_at": "2024-01-15T10:00:02Z"
      }
    ]
  }
}
```

### Get Run Messages

```
GET /runs/{id}/messages
```

**Response:**

```json
{
  "data": {
    "messages": [
      {
        "id": "990e8400-e29b-41d4-a716-446655440004",
        "session_id": "550e8400-e29b-41d4-a716-446655440000",
        "run_id": "660e8400-e29b-41d4-a716-446655440001",
        "role": "user",
        "preview_text": "Hello, how are you?",
        "block_count": 1,
        "has_tool_use": false,
        "has_tool_result": false,
        "total_tokens": 10,
        "created_at": "2024-01-15T10:00:00Z"
      }
    ]
  }
}
```

---

## Iterations

### List Iterations

```
GET /iterations?run_id={run_id}
```

**Query Parameters:**

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `run_id` | UUID | Yes | Filter by run |
| `limit` | int | No | Page size |
| `offset` | int | No | Pagination offset |

### Get Iteration

```
GET /iterations/{id}
```

**Response:**

```json
{
  "data": {
    "iteration": {
      "id": "770e8400-e29b-41d4-a716-446655440002",
      "run_id": "660e8400-e29b-41d4-a716-446655440001",
      "iteration_number": 1,
      "is_streaming": true,
      "trigger_type": "user_prompt",
      "batch_id": null,
      "batch_status": null,
      "stop_reason": "tool_use",
      "has_tool_use": true,
      "tool_execution_count": 1,
      "input_tokens": 800,
      "output_tokens": 400,
      "created_at": "2024-01-15T10:00:00Z",
      "completed_at": "2024-01-15T10:00:01Z"
    },
    "run": {...},
    "tool_executions": [...],
    "request_message": {...},
    "response_message": {...}
  }
}
```

---

## Tool Executions

### List Tool Executions

```
GET /tool-executions
```

**Query Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `run_id` | UUID | Filter by run |
| `iteration_id` | UUID | Filter by iteration |
| `tool_name` | string | Filter by tool name |
| `state` | string | `pending`, `running`, `completed`, `failed`, `skipped` |
| `is_agent_tool` | string | `true` or `false` |
| `limit` | int | Page size |
| `offset` | int | Pagination offset |

### Get Tool Execution

```
GET /tool-executions/{id}
```

**Response:**

```json
{
  "data": {
    "execution": {
      "id": "880e8400-e29b-41d4-a716-446655440003",
      "run_id": "660e8400-e29b-41d4-a716-446655440001",
      "iteration_id": "770e8400-e29b-41d4-a716-446655440002",
      "tool_use_id": "toolu_01xyz",
      "tool_name": "get_weather",
      "tool_input": {"city": "NYC"},
      "tool_output": "Weather in NYC: 72°F, sunny",
      "state": "completed",
      "is_agent_tool": false,
      "agent_name": null,
      "child_run_id": null,
      "is_error": false,
      "error_message": null,
      "attempt_count": 1,
      "max_attempts": 2,
      "scheduled_at": "2024-01-15T10:00:01Z",
      "snooze_count": 0,
      "created_at": "2024-01-15T10:00:01Z",
      "completed_at": "2024-01-15T10:00:02Z"
    },
    "run": {...},
    "iteration": {...},
    "child_run": null
  }
}
```

---

## Messages

### List Messages

```
GET /messages?session_id={session_id}
```

**Query Parameters:**

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `session_id` | UUID | Yes | Session to fetch messages from |
| `limit` | int | No | Max messages (default: 100, max: 1000) |

**Response:**

```json
{
  "data": {
    "session_id": "550e8400-e29b-41d4-a716-446655440000",
    "session": {...},
    "agent_name": "assistant",
    "messages": [
      {
        "message": {
          "id": "990e8400-e29b-41d4-a716-446655440004",
          "role": "user",
          "created_at": "2024-01-15T10:00:00Z"
        },
        "content_blocks": [
          {
            "type": "text",
            "text": "Hello, how are you?"
          }
        ]
      },
      {
        "message": {
          "id": "990e8400-e29b-41d4-a716-446655440005",
          "role": "assistant",
          "created_at": "2024-01-15T10:00:03Z"
        },
        "content_blocks": [
          {
            "type": "text",
            "text": "I'm doing well, thank you!"
          }
        ]
      }
    ],
    "tool_results": {
      "toolu_01xyz": {
        "type": "tool_result",
        "tool_result_for_use_id": "toolu_01xyz",
        "tool_content": "Weather: 72°F",
        "is_error": false
      }
    },
    "total_tokens": 2500,
    "message_count": 2
  }
}
```

### Get Message

```
GET /messages/{id}
```

**Response:**

```json
{
  "data": {
    "message": {
      "id": "990e8400-e29b-41d4-a716-446655440004",
      "session_id": "550e8400-e29b-41d4-a716-446655440000",
      "run_id": "660e8400-e29b-41d4-a716-446655440001",
      "role": "user",
      "is_preserved": false,
      "is_summary": false,
      "created_at": "2024-01-15T10:00:00Z"
    },
    "content_blocks": [
      {
        "type": "text",
        "text": "Hello, how are you?"
      }
    ],
    "run_info": {...}
  }
}
```

---

## Agents

### List Agents

```
GET /agents
```

Returns all registered agents with statistics.

**Response:**

```json
{
  "data": {
    "agents": [
      {
        "agent": {
          "name": "assistant",
          "model": "claude-sonnet-4-5-20250929",
          "system_prompt": "You are a helpful assistant.",
          "tools": ["get_weather", "calculator"],
          "agents": []
        },
        "total_runs": 800,
        "active_runs": 2,
        "completed_runs": 790,
        "failed_runs": 8,
        "avg_tokens_per_run": 2500,
        "registered_on": ["worker-1", "worker-2"],
        "is_active": true
      }
    ]
  }
}
```

### Get Agent

```
GET /agents/{name}
```

---

## Tools

### List Tools

```
GET /tools
```

Returns all registered tools with statistics.

**Response:**

```json
{
  "data": {
    "tools": [
      {
        "tool": {
          "name": "get_weather",
          "description": "Get current weather for a city",
          "input_schema": {
            "type": "object",
            "properties": {
              "city": {
                "type": "string",
                "description": "City name"
              }
            },
            "required": ["city"]
          }
        },
        "total_executions": 500,
        "pending_count": 1,
        "completed_count": 495,
        "failed_count": 4,
        "avg_duration": "0.5s",
        "registered_on": ["worker-1", "worker-2"],
        "is_active": true
      }
    ]
  }
}
```

### Get Tool

```
GET /tools/{name}
```

---

## Instances

### List Instances

```
GET /instances
```

Returns all active worker instances with health status.

**Response:**

```json
{
  "data": {
    "instances": [
      {
        "instance": {
          "id": "worker-1",
          "name": "worker",
          "hostname": "pod-abc123",
          "pid": 12345,
          "version": "1.0.0",
          "max_concurrent_runs": 10,
          "max_concurrent_tools": 50,
          "active_run_count": 2,
          "active_tool_count": 3,
          "created_at": "2024-01-15T08:00:00Z",
          "last_heartbeat_at": "2024-01-15T10:30:00Z"
        },
        "status": "healthy",
        "time_since_heartbeat": "5s",
        "agent_names": ["assistant", "researcher"],
        "tool_names": ["get_weather", "calculator"]
      }
    ]
  }
}
```

**Health Status:**
- `healthy` - Heartbeat within 30 seconds
- `warning` - Heartbeat 30-60 seconds ago
- `unhealthy` - No heartbeat for 60+ seconds

### Get Instance

```
GET /instances/{id}
```

**Response:**

```json
{
  "data": {
    "instance": {...},
    "status": "healthy",
    "time_since_heartbeat": "5s",
    "agent_names": ["assistant", "researcher"],
    "tool_names": ["get_weather", "calculator"],
    "is_leader": true
  }
}
```

---

## Compaction Events

### List Compaction Events

```
GET /compaction-events
```

**Query Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `session_id` | UUID | Filter by session |
| `limit` | int | Page size |
| `offset` | int | Pagination offset |

**Response:**

```json
{
  "data": {
    "events": [
      {
        "id": "aa0e8400-e29b-41d4-a716-446655440005",
        "session_id": "550e8400-e29b-41d4-a716-446655440000",
        "strategy": "hybrid",
        "original_tokens": 180000,
        "compacted_tokens": 80000,
        "token_reduction": 0.556,
        "messages_removed": 45,
        "summary_created": true,
        "duration": "2.5s",
        "created_at": "2024-01-15T10:00:00Z"
      }
    ],
    "total_count": 10,
    "has_more": false
  }
}
```

### Get Compaction Event

```
GET /compaction-events/{id}?session_id={session_id}
```

**Query Parameters:**

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `session_id` | UUID | Yes | Session ID for the event |

**Response:**

```json
{
  "data": {
    "event": {
      "id": "aa0e8400-e29b-41d4-a716-446655440005",
      "session_id": "550e8400-e29b-41d4-a716-446655440000",
      "strategy": "hybrid",
      "original_tokens": 180000,
      "compacted_tokens": 80000,
      "messages_removed": 45,
      "summary_content": "Summary of conversation...",
      "preserved_message_ids": [...],
      "model_used": "claude-3-5-haiku-20241022",
      "duration_ms": 2500,
      "created_at": "2024-01-15T10:00:00Z"
    },
    "session": {...},
    "token_reduction": 0.556,
    "duration": "2.5s",
    "archived_message_ids": [...]
  }
}
```

---

## Tenants

### List Tenants (Admin Mode Only)

```
GET /tenants
```

Returns all tenants with session and run counts. Only available when `TenantID` is empty in config (admin mode).

**Response:**

```json
{
  "data": {
    "tenants": [
      {
        "tenant_id": "tenant-1",
        "session_count": 50,
        "run_count": 400
      },
      {
        "tenant_id": "tenant-2",
        "session_count": 100,
        "run_count": 800
      }
    ]
  }
}
```
