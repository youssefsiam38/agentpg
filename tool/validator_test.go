package tool

import (
	"encoding/json"
	"testing"
)

func TestValidator_ValidateInput(t *testing.T) {
	validator := NewValidator()

	tests := []struct {
		name    string
		schema  ToolSchema
		input   string
		wantErr bool
	}{
		{
			name: "valid string",
			schema: ToolSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"name": {Type: "string"},
				},
				Required: []string{"name"},
			},
			input:   `{"name": "test"}`,
			wantErr: false,
		},
		{
			name: "wrong type - expected string got number",
			schema: ToolSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"name": {Type: "string"},
				},
			},
			input:   `{"name": 123}`,
			wantErr: true,
		},
		{
			name: "missing required field",
			schema: ToolSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"name": {Type: "string"},
				},
				Required: []string{"name"},
			},
			input:   `{}`,
			wantErr: true,
		},
		{
			name: "enum validation pass",
			schema: ToolSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"status": {Type: "string", Enum: []string{"active", "inactive"}},
				},
			},
			input:   `{"status": "active"}`,
			wantErr: false,
		},
		{
			name: "enum validation fail",
			schema: ToolSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"status": {Type: "string", Enum: []string{"active", "inactive"}},
				},
			},
			input:   `{"status": "unknown"}`,
			wantErr: true,
		},
		{
			name: "number minimum pass",
			schema: ToolSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"age": {Type: "number", Minimum: ptr(0.0)},
				},
			},
			input:   `{"age": 25}`,
			wantErr: false,
		},
		{
			name: "number minimum fail",
			schema: ToolSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"age": {Type: "number", Minimum: ptr(0.0)},
				},
			},
			input:   `{"age": -5}`,
			wantErr: true,
		},
		{
			name: "number maximum fail",
			schema: ToolSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"percent": {Type: "number", Maximum: ptr(100.0)},
				},
			},
			input:   `{"percent": 150}`,
			wantErr: true,
		},
		{
			name: "string minLength pass",
			schema: ToolSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"password": {Type: "string", MinLength: intPtr(8)},
				},
			},
			input:   `{"password": "secure123"}`,
			wantErr: false,
		},
		{
			name: "string minLength fail",
			schema: ToolSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"password": {Type: "string", MinLength: intPtr(8)},
				},
			},
			input:   `{"password": "short"}`,
			wantErr: true,
		},
		{
			name: "string maxLength fail",
			schema: ToolSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"code": {Type: "string", MaxLength: intPtr(4)},
				},
			},
			input:   `{"code": "toolong"}`,
			wantErr: true,
		},
		{
			name: "array of strings valid",
			schema: ToolSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"tags": {Type: "array", Items: &PropertyDef{Type: "string"}},
				},
			},
			input:   `{"tags": ["a", "b", "c"]}`,
			wantErr: false,
		},
		{
			name: "array of strings invalid item",
			schema: ToolSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"tags": {Type: "array", Items: &PropertyDef{Type: "string"}},
				},
			},
			input:   `{"tags": ["a", 123, "c"]}`,
			wantErr: true,
		},
		{
			name: "integer valid",
			schema: ToolSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"count": {Type: "integer"},
				},
			},
			input:   `{"count": 42}`,
			wantErr: false,
		},
		{
			name: "integer invalid - is float",
			schema: ToolSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"count": {Type: "integer"},
				},
			},
			input:   `{"count": 3.14}`,
			wantErr: true,
		},
		{
			name: "boolean valid",
			schema: ToolSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"enabled": {Type: "boolean"},
				},
			},
			input:   `{"enabled": true}`,
			wantErr: false,
		},
		{
			name: "boolean invalid",
			schema: ToolSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"enabled": {Type: "boolean"},
				},
			},
			input:   `{"enabled": "yes"}`,
			wantErr: true,
		},
		{
			name: "optional field missing is ok",
			schema: ToolSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"name":     {Type: "string"},
					"optional": {Type: "string"},
				},
				Required: []string{"name"},
			},
			input:   `{"name": "test"}`,
			wantErr: false,
		},
		{
			name: "null value is ok",
			schema: ToolSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"value": {Type: "string"},
				},
			},
			input:   `{"value": null}`,
			wantErr: false,
		},
		{
			name: "invalid JSON",
			schema: ToolSchema{
				Type:       "object",
				Properties: map[string]PropertyDef{},
			},
			input:   `{invalid}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateInput(tt.schema, json.RawMessage(tt.input))
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateInput() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidator_InvalidSchemaType(t *testing.T) {
	validator := NewValidator()

	schema := ToolSchema{
		Type: "array", // Must be "object"
	}

	err := validator.ValidateInput(schema, json.RawMessage(`[]`))
	if err == nil {
		t.Error("Expected error for non-object schema type")
	}
}

func TestValidator_NestedObject(t *testing.T) {
	validator := NewValidator()

	schema := ToolSchema{
		Type: "object",
		Properties: map[string]PropertyDef{
			"user": {
				Type: "object",
				Properties: map[string]PropertyDef{
					"name": {Type: "string"},
					"age":  {Type: "number", Minimum: ptr(0.0)},
				},
			},
		},
	}

	// Valid nested object
	err := validator.ValidateInput(schema, json.RawMessage(`{"user": {"name": "Alice", "age": 30}}`))
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	// Invalid nested object (age below minimum)
	err = validator.ValidateInput(schema, json.RawMessage(`{"user": {"name": "Bob", "age": -5}}`))
	if err == nil {
		t.Error("Expected error for negative age")
	}
}

// Helper functions for pointer values
func ptr(f float64) *float64 {
	return &f
}

func intPtr(i int) *int {
	return &i
}
