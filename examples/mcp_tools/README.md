# MCP Tools Examples

These examples demonstrate registering tools from MCP (Model Context Protocol) servers with AgentPG using the `mcp` sub-module.

MCP servers expose tools via a standardized protocol, allowing agents to use tools from the growing ecosystem of MCP servers (GitHub, filesystem, web fetch, etc.) without writing custom Go tool implementations.

## Prerequisites

- Node.js and npm installed (for `npx` to run MCP servers)
- PostgreSQL database running with migrations applied
- `ANTHROPIC_API_KEY` and `DATABASE_URL` environment variables set

## Examples

| Example | Description |
|---------|-------------|
| [01_everything_server](./01_everything_server/) | Reference MCP server with echo, add, and other test tools. No auth required. Best for testing. |
| [02_filesystem_server](./02_filesystem_server/) | File operations (read, write, list) via MCP. No auth required. |
| [03_github_server](./03_github_server/) | GitHub integration (issues, PRs, repos) via MCP. Requires `GITHUB_TOKEN`. |
| [04_multi_server](./04_multi_server/) | Multiple MCP servers + local tools combined in one agent. |

## Learning Path

1. Start with **01_everything_server** to verify MCP integration works
2. Try **02_filesystem_server** for a practical file operations example
3. Explore **03_github_server** to see auth configuration with `Env`
4. See **04_multi_server** for combining multiple MCP servers with local tools

## Running Examples

```bash
# Install the mcp sub-module dependency
cd examples/mcp_tools/01_everything_server
go run main.go
```

## How It Works

```
1. RegisterServer() spawns MCP server subprocess (or connects via HTTP)
2. Discovers available tools via MCP protocol (tools/list)
3. Wraps each tool as a tool.Tool implementation (MCPTool)
4. Registers with client.RegisterTool() — same as local tools
5. Agent uses tools normally — MCPTool.Execute() calls MCP server (tools/call)
```
