package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/shared/constant"
)

// Registry manages tools and converts them to Anthropic format
type Registry struct {
	tools map[string]Tool
	mu    sync.RWMutex
}

// NewRegistry creates a new tool registry
func NewRegistry() *Registry {
	return &Registry{
		tools: make(map[string]Tool),
	}
}

// Register adds a tool to the registry
func (r *Registry) Register(tool Tool) error {
	if tool == nil {
		return fmt.Errorf("tool cannot be nil")
	}

	name := tool.Name()
	if name == "" {
		return fmt.Errorf("tool name cannot be empty")
	}

	// Validate schema
	schema := tool.InputSchema()
	if schema.Type != "object" {
		return fmt.Errorf("tool %s: schema type must be 'object', got %s", name, schema.Type)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.tools[name]; exists {
		return fmt.Errorf("tool %s already registered", name)
	}

	r.tools[name] = tool
	return nil
}

// RegisterAll adds multiple tools to the registry
func (r *Registry) RegisterAll(tools []Tool) error {
	for _, tool := range tools {
		if err := r.Register(tool); err != nil {
			return err
		}
	}
	return nil
}

// Get retrieves a tool by name
func (r *Registry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	tool, exists := r.tools[name]
	return tool, exists
}

// Has checks if a tool is registered
func (r *Registry) Has(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, exists := r.tools[name]
	return exists
}

// List returns all registered tool names
func (r *Registry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	return names
}

// Count returns the number of registered tools
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.tools)
}

// ToAnthropicTools converts all registered tools to Anthropic tool parameters
func (r *Registry) ToAnthropicTools() []anthropic.ToolParam {
	r.mu.RLock()
	defer r.mu.RUnlock()

	params := make([]anthropic.ToolParam, 0, len(r.tools))

	for _, tool := range r.tools {
		params = append(params, r.convertToolToParam(tool))
	}

	return params
}

// convertToolToParam converts a single tool to Anthropic format
func (r *Registry) convertToolToParam(tool Tool) anthropic.ToolParam {
	schema := tool.InputSchema()

	// Convert properties
	properties := make(map[string]interface{})
	for propName, propDef := range schema.Properties {
		properties[propName] = r.convertPropertyDef(propDef)
	}

	// Build input schema
	inputSchema := anthropic.ToolInputSchemaParam{
		Type:       constant.Object("object"),
		Properties: properties,
	}

	// Add required fields if present
	if len(schema.Required) > 0 {
		inputSchema.Required = schema.Required
	}

	return anthropic.ToolParam{
		Name:        tool.Name(),
		Description: anthropic.String(tool.Description()),
		InputSchema: inputSchema,
	}
}

// convertPropertyDef converts a property definition to Anthropic format
func (r *Registry) convertPropertyDef(def PropertyDef) map[string]interface{} {
	prop := map[string]interface{}{
		"type": def.Type,
	}

	if def.Description != "" {
		prop["description"] = def.Description
	}

	if len(def.Enum) > 0 {
		prop["enum"] = def.Enum
	}

	if def.Minimum != nil {
		prop["minimum"] = *def.Minimum
	}

	if def.Maximum != nil {
		prop["maximum"] = *def.Maximum
	}

	if def.MinLength != nil {
		prop["minLength"] = *def.MinLength
	}

	if def.MaxLength != nil {
		prop["maxLength"] = *def.MaxLength
	}

	// Handle array items
	if def.Items != nil {
		prop["items"] = r.convertPropertyDef(*def.Items)
	}

	// Handle nested object properties
	if len(def.Properties) > 0 {
		nestedProps := make(map[string]interface{})
		for key, nestedDef := range def.Properties {
			nestedProps[key] = r.convertPropertyDef(nestedDef)
		}
		prop["properties"] = nestedProps
	}

	return prop
}

// Execute executes a tool by name
func (r *Registry) Execute(ctx context.Context, toolName string, input json.RawMessage) (string, error) {
	tool, exists := r.Get(toolName)
	if !exists {
		return "", fmt.Errorf("tool not found: %s", toolName)
	}

	return tool.Execute(ctx, input)
}

// ToAnthropicToolUnions converts tools to union parameters
func (r *Registry) ToAnthropicToolUnions() []anthropic.ToolUnionParam {
	params := r.ToAnthropicTools()
	unions := make([]anthropic.ToolUnionParam, len(params))

	for i, param := range params {
		unions[i] = anthropic.ToolUnionParam{
			OfTool: &param,
		}
	}

	return unions
}
