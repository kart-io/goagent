// Package plugingen provides code generation tools for type-safe plugin boundaries.
package plugingen

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/kart-io/goagent/utils/json"
)

// LoadSchema loads a PluginSchema from a file (YAML or JSON).
//
// The file format is determined by the file extension:
// - .yaml, .yml → YAML format
// - .json → JSON format
func LoadSchema(path string) (*PluginSchema, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read schema file: %w", err)
	}

	ext := strings.ToLower(filepath.Ext(path))
	var schema PluginSchema

	switch ext {
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(data, &schema); err != nil {
			return nil, fmt.Errorf("failed to parse YAML schema: %w", err)
		}

	case ".json":
		if err := json.Unmarshal(data, &schema); err != nil {
			return nil, fmt.Errorf("failed to parse JSON schema: %w", err)
		}

	default:
		return nil, fmt.Errorf("unsupported file format: %s (expected .yaml, .yml, or .json)", ext)
	}

	// Validate the loaded schema
	if err := schema.Validate(); err != nil {
		return nil, fmt.Errorf("schema validation failed: %w", err)
	}

	return &schema, nil
}

// LoadSchemaFromYAML loads a PluginSchema from YAML bytes.
func LoadSchemaFromYAML(data []byte) (*PluginSchema, error) {
	var schema PluginSchema
	if err := yaml.Unmarshal(data, &schema); err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}

	if err := schema.Validate(); err != nil {
		return nil, fmt.Errorf("schema validation failed: %w", err)
	}

	return &schema, nil
}

// LoadSchemaFromJSON loads a PluginSchema from JSON bytes.
func LoadSchemaFromJSON(data []byte) (*PluginSchema, error) {
	var schema PluginSchema
	if err := json.Unmarshal(data, &schema); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	if err := schema.Validate(); err != nil {
		return nil, fmt.Errorf("schema validation failed: %w", err)
	}

	return &schema, nil
}

// SaveSchema saves a PluginSchema to a file (YAML or JSON).
//
// The file format is determined by the file extension:
// - .yaml, .yml → YAML format
// - .json → JSON format
func SaveSchema(schema *PluginSchema, path string) error {
	if err := schema.Validate(); err != nil {
		return fmt.Errorf("schema validation failed: %w", err)
	}

	ext := strings.ToLower(filepath.Ext(path))
	var data []byte
	var err error

	switch ext {
	case ".yaml", ".yml":
		data, err = yaml.Marshal(schema)
		if err != nil {
			return fmt.Errorf("failed to marshal YAML: %w", err)
		}

	case ".json":
		data, err = json.MarshalIndent(schema, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal JSON: %w", err)
		}

	default:
		return fmt.Errorf("unsupported file format: %s (expected .yaml, .yml, or .json)", ext)
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

// SaveSchemaAsYAML saves a PluginSchema as YAML bytes.
func SaveSchemaAsYAML(schema *PluginSchema) ([]byte, error) {
	if err := schema.Validate(); err != nil {
		return nil, fmt.Errorf("schema validation failed: %w", err)
	}

	data, err := yaml.Marshal(schema)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal YAML: %w", err)
	}

	return data, nil
}

// SaveSchemaAsJSON saves a PluginSchema as JSON bytes.
func SaveSchemaAsJSON(schema *PluginSchema) ([]byte, error) {
	if err := schema.Validate(); err != nil {
		return nil, fmt.Errorf("schema validation failed: %w", err)
	}

	data, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal JSON: %w", err)
	}

	return data, nil
}
