package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/youssefsiam38/agentpg/tool"
)

// MCPTool bridges an MCP server tool to the agentpg tool.Tool interface.
// It is created by MCPServer during tool discovery.
type MCPTool struct {
	server         *MCPServer
	mcpName        string // Original MCP tool name (used for CallTool)
	namespacedName string // Prefixed name (used for RegisterTool)
	description    string
	schema         tool.ToolSchema
}

// Name returns the namespaced tool name.
func (t *MCPTool) Name() string { return t.namespacedName }

// Description returns the tool description from the MCP server.
func (t *MCPTool) Description() string { return t.description }

// InputSchema returns the tool's input schema converted to agentpg format.
func (t *MCPTool) InputSchema() tool.ToolSchema { return t.schema }

// Execute calls the MCP server's tool and returns the text result.
func (t *MCPTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	return t.server.callTool(ctx, t.mcpName, input)
}

// convertMCPInputSchema converts the MCP Tool.InputSchema (which is any,
// deserialized as map[string]any on the client side) to an agentpg tool.ToolSchema.
func convertMCPInputSchema(schema any) tool.ToolSchema {
	if schema == nil {
		return tool.ToolSchema{Type: "object"}
	}

	m, ok := schema.(map[string]any)
	if !ok {
		return tool.ToolSchema{Type: "object"}
	}

	ts := tool.ToolSchema{
		Type: "object",
	}

	if desc, ok := m["description"].(string); ok {
		ts.Description = desc
	}

	if props, ok := m["properties"].(map[string]any); ok {
		ts.Properties = make(map[string]tool.PropertyDef)
		for name, propRaw := range props {
			if propMap, ok := propRaw.(map[string]any); ok {
				ts.Properties[name] = convertMCPPropertyDef(propMap)
			}
		}
	}

	if req, ok := m["required"].([]any); ok {
		for _, r := range req {
			if s, ok := r.(string); ok {
				ts.Required = append(ts.Required, s)
			}
		}
	}

	return ts
}

// convertMCPPropertyDef recursively converts a property definition map
// to an agentpg tool.PropertyDef.
func convertMCPPropertyDef(m map[string]any) tool.PropertyDef {
	pd := tool.PropertyDef{}

	if t, ok := m["type"].(string); ok {
		pd.Type = t
	}

	if desc, ok := m["description"].(string); ok {
		pd.Description = desc
	}

	// Enum values
	if enumRaw, ok := m["enum"].([]any); ok {
		for _, e := range enumRaw {
			if s, ok := e.(string); ok {
				pd.Enum = append(pd.Enum, s)
			}
		}
	}

	// Default value
	if def, ok := m["default"]; ok {
		pd.Default = def
	}

	// Numeric constraints
	if v, ok := toFloat64(m["minimum"]); ok {
		pd.Minimum = &v
	}
	if v, ok := toFloat64(m["maximum"]); ok {
		pd.Maximum = &v
	}
	if v, ok := toFloat64(m["exclusiveMinimum"]); ok {
		pd.ExclusiveMinimum = &v
	}
	if v, ok := toFloat64(m["exclusiveMaximum"]); ok {
		pd.ExclusiveMaximum = &v
	}

	// String constraints
	if v, ok := toInt(m["minLength"]); ok {
		pd.MinLength = &v
	}
	if v, ok := toInt(m["maxLength"]); ok {
		pd.MaxLength = &v
	}
	if v, ok := m["pattern"].(string); ok {
		pd.Pattern = v
	}

	// Array constraints
	if items, ok := m["items"].(map[string]any); ok {
		itemDef := convertMCPPropertyDef(items)
		pd.Items = &itemDef
	}
	if v, ok := toInt(m["minItems"]); ok {
		pd.MinItems = &v
	}
	if v, ok := toInt(m["maxItems"]); ok {
		pd.MaxItems = &v
	}

	// Nested object properties
	if props, ok := m["properties"].(map[string]any); ok {
		pd.Properties = make(map[string]tool.PropertyDef)
		for name, propRaw := range props {
			if propMap, ok := propRaw.(map[string]any); ok {
				pd.Properties[name] = convertMCPPropertyDef(propMap)
			}
		}
	}
	if req, ok := m["required"].([]any); ok {
		for _, r := range req {
			if s, ok := r.(string); ok {
				pd.Required = append(pd.Required, s)
			}
		}
	}

	return pd
}

// extractTextContent concatenates text from MCP Content blocks.
func extractTextContent(contents []mcpsdk.Content) string {
	var result string
	for _, c := range contents {
		if tc, ok := c.(*mcpsdk.TextContent); ok {
			if result != "" {
				result += "\n"
			}
			result += tc.Text
		}
	}
	return result
}

// toolName returns the namespaced tool name for registration.
func toolName(serverName, mcpToolName string, disablePrefix bool) string {
	if disablePrefix {
		return mcpToolName
	}
	return fmt.Sprintf("%s__%s", serverName, mcpToolName)
}

// toFloat64 converts a JSON number (float64) from map[string]any.
func toFloat64(v any) (float64, bool) {
	if v == nil {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	default:
		return 0, false
	}
}

// toInt converts a JSON number to int from map[string]any.
func toInt(v any) (int, bool) {
	if v == nil {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case int64:
		return int(n), true
	default:
		return 0, false
	}
}
