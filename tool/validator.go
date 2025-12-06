package tool

import (
	"encoding/json"
	"fmt"
	"reflect"
)

// Validator validates tool inputs against their schemas
type Validator struct{}

// NewValidator creates a new validator
func NewValidator() *Validator {
	return &Validator{}
}

// ValidateInput validates input against a tool's schema
func (v *Validator) ValidateInput(schema ToolSchema, input json.RawMessage) error {
	if schema.Type != "object" {
		return fmt.Errorf("schema type must be 'object', got '%s'", schema.Type)
	}

	var inputMap map[string]any
	if err := json.Unmarshal(input, &inputMap); err != nil {
		return fmt.Errorf("invalid JSON input: %w", err)
	}

	// Check required fields
	for _, required := range schema.Required {
		if _, exists := inputMap[required]; !exists {
			return fmt.Errorf("missing required field: %s", required)
		}
	}

	// Validate each property
	for propName, propDef := range schema.Properties {
		value, exists := inputMap[propName]
		if !exists {
			continue // Optional field not provided
		}

		if err := v.validateProperty(propName, propDef, value); err != nil {
			return err
		}
	}

	return nil
}

func (v *Validator) validateProperty(name string, def PropertyDef, value any) error {
	if value == nil {
		return nil // Allow null values
	}

	// Type validation
	if err := v.validateType(name, def.Type, value); err != nil {
		return err
	}

	// Enum validation
	if len(def.Enum) > 0 {
		strVal, ok := value.(string)
		if !ok {
			return fmt.Errorf("field '%s': expected string for enum validation, got %T", name, value)
		}
		valid := false
		for _, e := range def.Enum {
			if strVal == e {
				valid = true
				break
			}
		}
		if !valid {
			return fmt.Errorf("field '%s': value '%s' not in allowed values %v", name, strVal, def.Enum)
		}
	}

	// Range validation for numbers
	if def.Type == "number" || def.Type == "integer" {
		numVal, err := toFloat64(value)
		if err != nil {
			return fmt.Errorf("field '%s': %w", name, err)
		}
		if def.Minimum != nil && numVal < *def.Minimum {
			return fmt.Errorf("field '%s': value %v is less than minimum %v", name, numVal, *def.Minimum)
		}
		if def.Maximum != nil && numVal > *def.Maximum {
			return fmt.Errorf("field '%s': value %v exceeds maximum %v", name, numVal, *def.Maximum)
		}
	}

	// String length validation
	if def.Type == "string" {
		strVal, ok := value.(string)
		if ok {
			if def.MinLength != nil && len(strVal) < *def.MinLength {
				return fmt.Errorf("field '%s': string length %d is less than minimum %d", name, len(strVal), *def.MinLength)
			}
			if def.MaxLength != nil && len(strVal) > *def.MaxLength {
				return fmt.Errorf("field '%s': string length %d exceeds maximum %d", name, len(strVal), *def.MaxLength)
			}
		}
	}

	// Array items validation
	if def.Type == "array" && def.Items != nil {
		arr, ok := value.([]any)
		if ok {
			for i, item := range arr {
				if err := v.validateProperty(fmt.Sprintf("%s[%d]", name, i), *def.Items, item); err != nil {
					return err
				}
			}
		}
	}

	// Nested object validation
	if def.Type == "object" && def.Properties != nil {
		obj, ok := value.(map[string]any)
		if ok {
			for propName, propDef := range def.Properties {
				if propVal, exists := obj[propName]; exists {
					if err := v.validateProperty(fmt.Sprintf("%s.%s", name, propName), propDef, propVal); err != nil {
						return err
					}
				}
			}
		}
	}

	return nil
}

func (v *Validator) validateType(name string, expectedType string, value any) error {
	if value == nil {
		return nil
	}

	actualType := reflect.TypeOf(value)
	if actualType == nil {
		return nil
	}

	switch expectedType {
	case "string":
		if _, ok := value.(string); !ok {
			return fmt.Errorf("field '%s': expected string, got %T", name, value)
		}
	case "number":
		switch value.(type) {
		case float64, float32, int, int64, int32, json.Number:
			// Valid number types
		default:
			return fmt.Errorf("field '%s': expected number, got %T", name, value)
		}
	case "integer":
		switch v := value.(type) {
		case float64:
			if v != float64(int64(v)) {
				return fmt.Errorf("field '%s': expected integer, got float %v", name, v)
			}
		case int, int64, int32:
			// Valid integer types
		default:
			return fmt.Errorf("field '%s': expected integer, got %T", name, value)
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("field '%s': expected boolean, got %T", name, value)
		}
	case "array":
		if _, ok := value.([]any); !ok {
			return fmt.Errorf("field '%s': expected array, got %T", name, value)
		}
	case "object":
		if _, ok := value.(map[string]any); !ok {
			return fmt.Errorf("field '%s': expected object, got %T", name, value)
		}
	}

	return nil
}

func toFloat64(v any) (float64, error) {
	switch val := v.(type) {
	case float64:
		return val, nil
	case float32:
		return float64(val), nil
	case int:
		return float64(val), nil
	case int64:
		return float64(val), nil
	case int32:
		return float64(val), nil
	case json.Number:
		return val.Float64()
	default:
		return 0, fmt.Errorf("cannot convert %T to float64", v)
	}
}
