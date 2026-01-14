// Package tool defines the interface for tools that can be used by agents.
package tool

import (
	"context"
	"encoding/json"
	"fmt"
)

// Tool is the interface that all tools must implement.
// Tools are functions that agents can call during their execution.
type Tool interface {
	// Name returns the tool's unique identifier.
	// Must be unique across all tools registered on a client.
	Name() string

	// Description explains what the tool does.
	// This description is shown to Claude to help it decide when to use the tool.
	Description() string

	// InputSchema returns the JSON Schema for the tool's input.
	// Must have Type = "object".
	InputSchema() ToolSchema

	// Execute runs the tool with the given input and returns the result.
	// The input is the raw JSON from Claude's tool_use block.
	// The result string will be sent back to Claude as a tool_result.
	// If an error is returned, it will be sent as a tool_result with is_error=true.
	Execute(ctx context.Context, input json.RawMessage) (string, error)
}

// ToolSchema represents a JSON Schema for tool input.
// The schema defines what parameters the tool accepts.
type ToolSchema struct {
	// Type must be "object" for tool schemas.
	Type string `json:"type"`

	// Properties defines the parameters the tool accepts.
	Properties map[string]PropertyDef `json:"properties,omitempty"`

	// Required lists the names of required parameters.
	Required []string `json:"required,omitempty"`

	// Description provides additional context about the schema.
	Description string `json:"description,omitempty"`
}

// PropertyDef defines a single property in the schema.
type PropertyDef struct {
	// Type is the JSON type: "string", "number", "integer", "boolean", "array", "object"
	Type string `json:"type"`

	// Description explains what this property is for.
	Description string `json:"description,omitempty"`

	// Enum restricts the value to a set of allowed values.
	Enum []string `json:"enum,omitempty"`

	// Default provides a default value.
	Default any `json:"default,omitempty"`

	// Numeric constraints
	Minimum          *float64 `json:"minimum,omitempty"`
	Maximum          *float64 `json:"maximum,omitempty"`
	ExclusiveMinimum *float64 `json:"exclusiveMinimum,omitempty"`
	ExclusiveMaximum *float64 `json:"exclusiveMaximum,omitempty"`

	// String constraints
	MinLength *int   `json:"minLength,omitempty"`
	MaxLength *int   `json:"maxLength,omitempty"`
	Pattern   string `json:"pattern,omitempty"`

	// Array constraints
	Items    *PropertyDef `json:"items,omitempty"`
	MinItems *int         `json:"minItems,omitempty"`
	MaxItems *int         `json:"maxItems,omitempty"`

	// Object constraints (for nested objects)
	Properties map[string]PropertyDef `json:"properties,omitempty"`
	Required   []string               `json:"required,omitempty"`
}

// Validate validates the tool schema.
// Returns an error if the schema is invalid.
func (s *ToolSchema) Validate() error {
	if s.Type != "object" {
		return fmt.Errorf("schema type must be 'object', got '%s'", s.Type)
	}
	return nil
}

// ToJSON converts the schema to a JSON-serializable map.
// This is used to convert the schema to the format expected by Claude's API.
func (s *ToolSchema) ToJSON() map[string]any {
	result := map[string]any{
		"type": s.Type,
	}

	if s.Description != "" {
		result["description"] = s.Description
	}

	if len(s.Properties) > 0 {
		props := make(map[string]any)
		for name, prop := range s.Properties {
			props[name] = prop.ToJSON()
		}
		result["properties"] = props
	}

	if len(s.Required) > 0 {
		result["required"] = s.Required
	}

	return result
}

// ToJSON converts the property definition to a JSON-serializable map.
func (p *PropertyDef) ToJSON() map[string]any {
	result := map[string]any{
		"type": p.Type,
	}

	if p.Description != "" {
		result["description"] = p.Description
	}

	if len(p.Enum) > 0 {
		result["enum"] = p.Enum
	}

	if p.Default != nil {
		result["default"] = p.Default
	}

	// Numeric constraints
	if p.Minimum != nil {
		result["minimum"] = *p.Minimum
	}
	if p.Maximum != nil {
		result["maximum"] = *p.Maximum
	}
	if p.ExclusiveMinimum != nil {
		result["exclusiveMinimum"] = *p.ExclusiveMinimum
	}
	if p.ExclusiveMaximum != nil {
		result["exclusiveMaximum"] = *p.ExclusiveMaximum
	}

	// String constraints
	if p.MinLength != nil {
		result["minLength"] = *p.MinLength
	}
	if p.MaxLength != nil {
		result["maxLength"] = *p.MaxLength
	}
	if p.Pattern != "" {
		result["pattern"] = p.Pattern
	}

	// Array constraints
	if p.Items != nil {
		result["items"] = p.Items.ToJSON()
	}
	if p.MinItems != nil {
		result["minItems"] = *p.MinItems
	}
	if p.MaxItems != nil {
		result["maxItems"] = *p.MaxItems
	}

	// Object constraints
	if len(p.Properties) > 0 {
		props := make(map[string]any)
		for name, prop := range p.Properties {
			props[name] = prop.ToJSON()
		}
		result["properties"] = props
	}
	if len(p.Required) > 0 {
		result["required"] = p.Required
	}

	return result
}

// FuncTool is a convenience implementation of Tool using a function.
type FuncTool struct {
	name        string
	description string
	schema      ToolSchema
	execute     func(ctx context.Context, input json.RawMessage) (string, error)
}

// NewFuncTool creates a new FuncTool with the given parameters.
func NewFuncTool(
	name string,
	description string,
	schema ToolSchema,
	execute func(ctx context.Context, input json.RawMessage) (string, error),
) *FuncTool {
	return &FuncTool{
		name:        name,
		description: description,
		schema:      schema,
		execute:     execute,
	}
}

// Name returns the tool's name.
func (t *FuncTool) Name() string {
	return t.name
}

// Description returns the tool's description.
func (t *FuncTool) Description() string {
	return t.description
}

// InputSchema returns the tool's input schema.
func (t *FuncTool) InputSchema() ToolSchema {
	return t.schema
}

// Execute runs the tool with the given input.
func (t *FuncTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	return t.execute(ctx, input)
}

// ToolResult represents the result of a tool execution.
type ToolResult struct {
	ToolUseID string `json:"tool_use_id"`
	Content   string `json:"content"`
	IsError   bool   `json:"is_error"`
}
