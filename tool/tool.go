package tool

import (
	"context"
	"encoding/json"
)

// Tool is the interface that all tools must implement
type Tool interface {
	// Name returns the tool name (used in API calls)
	Name() string

	// Description returns a human-readable description of what the tool does
	Description() string

	// InputSchema returns the JSON Schema for the tool's input parameters
	// Must include "type", "properties", and optionally "required" array
	InputSchema() ToolSchema

	// Execute runs the tool with the provided input and returns the result
	Execute(ctx context.Context, input json.RawMessage) (string, error)
}

// ToolSchema defines the JSON Schema for a tool's input parameters
type ToolSchema struct {
	// Type must be "object"
	Type string `json:"type"`

	// Properties defines the tool's parameters
	Properties map[string]PropertyDef `json:"properties"`

	// Required lists the names of required parameters
	Required []string `json:"required,omitempty"`
}

// PropertyDef defines a single property in the tool schema
type PropertyDef struct {
	// Type is the JSON Schema type (string, number, boolean, array, object)
	Type string `json:"type"`

	// Description explains what this parameter is for
	Description string `json:"description,omitempty"`

	// Enum restricts the parameter to specific values
	Enum []string `json:"enum,omitempty"`

	// Items defines the schema for array items (when Type is "array")
	Items *PropertyDef `json:"items,omitempty"`

	// Properties defines nested object properties (when Type is "object")
	Properties map[string]PropertyDef `json:"properties,omitempty"`

	// Minimum/Maximum for number types
	Minimum *float64 `json:"minimum,omitempty"`
	Maximum *float64 `json:"maximum,omitempty"`

	// MinLength/MaxLength for string types
	MinLength *int `json:"minLength,omitempty"`
	MaxLength *int `json:"maxLength,omitempty"`
}

// ToMap converts the schema to a map[string]any representation.
// This is useful for storing in JSONB columns.
func (s ToolSchema) ToMap() map[string]any {
	result := map[string]any{
		"type": s.Type,
	}

	if len(s.Properties) > 0 {
		props := make(map[string]any)
		for k, v := range s.Properties {
			props[k] = v.toMap()
		}
		result["properties"] = props
	}

	if len(s.Required) > 0 {
		result["required"] = s.Required
	}

	return result
}

// toMap converts a PropertyDef to a map[string]any representation.
func (p PropertyDef) toMap() map[string]any {
	result := map[string]any{
		"type": p.Type,
	}

	if p.Description != "" {
		result["description"] = p.Description
	}

	if len(p.Enum) > 0 {
		result["enum"] = p.Enum
	}

	if p.Items != nil {
		result["items"] = p.Items.toMap()
	}

	if len(p.Properties) > 0 {
		props := make(map[string]any)
		for k, v := range p.Properties {
			props[k] = v.toMap()
		}
		result["properties"] = props
	}

	if p.Minimum != nil {
		result["minimum"] = *p.Minimum
	}
	if p.Maximum != nil {
		result["maximum"] = *p.Maximum
	}
	if p.MinLength != nil {
		result["minLength"] = *p.MinLength
	}
	if p.MaxLength != nil {
		result["maxLength"] = *p.MaxLength
	}

	return result
}

// funcTool is a simple Tool implementation using a function
type funcTool struct {
	name        string
	description string
	schema      ToolSchema
	fn          func(context.Context, json.RawMessage) (string, error)
}

// Name implements Tool
func (t *funcTool) Name() string {
	return t.name
}

// Description implements Tool
func (t *funcTool) Description() string {
	return t.description
}

// InputSchema implements Tool
func (t *funcTool) InputSchema() ToolSchema {
	return t.schema
}

// Execute implements Tool
func (t *funcTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	return t.fn(ctx, input)
}

// NewFuncTool creates a Tool from a function
// This is useful for simple tools where you don't want to create a full struct
func NewFuncTool(
	name string,
	description string,
	schema ToolSchema,
	fn func(context.Context, json.RawMessage) (string, error),
) Tool {
	return &funcTool{
		name:        name,
		description: description,
		schema:      schema,
		fn:          fn,
	}
}
