package plugingen

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLoadSchema_YAML tests loading schema from YAML file.
func TestLoadSchema_YAML(t *testing.T) {
	yamlContent := `
package: test
name: TestPlugin
version: v1.0.0
description: Test plugin

input:
  name: TestInput
  kind: struct
  fields:
    - name: Field1
      json: field1
      required: true
      type:
        name: string
        kind: primitive
        type: string

output:
  name: TestOutput
  kind: struct
  fields:
    - name: Result
      json: result
      required: true
      type:
        name: string
        kind: primitive
        type: string
`

	// Create temporary file
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test.yaml")
	err := os.WriteFile(tmpFile, []byte(yamlContent), 0644)
	require.NoError(t, err)

	// Load schema
	schema, err := LoadSchema(tmpFile)
	require.NoError(t, err)
	require.NotNil(t, schema)

	// Verify schema
	assert.Equal(t, "test", schema.PackageName)
	assert.Equal(t, "TestPlugin", schema.PluginName)
	assert.Equal(t, "v1.0.0", schema.Version)
	assert.Equal(t, "Test plugin", schema.Description)
	assert.Equal(t, "TestInput", schema.InputType.Name)
	assert.Equal(t, "TestOutput", schema.OutputType.Name)
}

// TestLoadSchema_JSON tests loading schema from JSON file.
func TestLoadSchema_JSON(t *testing.T) {
	jsonContent := `{
  "package": "test",
  "name": "TestPlugin",
  "version": "v1.0.0",
  "input": {
    "name": "TestInput",
    "kind": "struct",
    "fields": [
      {
        "name": "Field1",
        "json": "field1",
        "required": true,
        "type": {
          "name": "string",
          "kind": "primitive",
          "type": "string"
        }
      }
    ]
  },
  "output": {
    "name": "TestOutput",
    "kind": "struct",
    "fields": [
      {
        "name": "Result",
        "json": "result",
        "required": true,
        "type": {
          "name": "string",
          "kind": "primitive",
          "type": "string"
        }
      }
    ]
  }
}`

	// Create temporary file
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test.json")
	err := os.WriteFile(tmpFile, []byte(jsonContent), 0644)
	require.NoError(t, err)

	// Load schema
	schema, err := LoadSchema(tmpFile)
	require.NoError(t, err)
	require.NotNil(t, schema)

	// Verify schema
	assert.Equal(t, "test", schema.PackageName)
	assert.Equal(t, "TestPlugin", schema.PluginName)
	assert.Equal(t, "v1.0.0", schema.Version)
}

// TestLoadSchema_InvalidFormat tests error handling for invalid formats.
func TestLoadSchema_InvalidFormat(t *testing.T) {
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test.txt")
	err := os.WriteFile(tmpFile, []byte("test"), 0644)
	require.NoError(t, err)

	_, err = LoadSchema(tmpFile)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported file format")
}

// TestLoadSchema_FileNotFound tests error handling for missing files.
func TestLoadSchema_FileNotFound(t *testing.T) {
	_, err := LoadSchema("/nonexistent/file.yaml")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read schema file")
}

// TestLoadSchema_InvalidYAML tests error handling for invalid YAML.
func TestLoadSchema_InvalidYAML(t *testing.T) {
	invalidYAML := `
package: test
name: [invalid yaml structure
`

	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "invalid.yaml")
	err := os.WriteFile(tmpFile, []byte(invalidYAML), 0644)
	require.NoError(t, err)

	_, err = LoadSchema(tmpFile)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse YAML")
}

// TestLoadSchema_ValidationFailure tests schema validation.
func TestLoadSchema_ValidationFailure(t *testing.T) {
	invalidSchema := `
package: test
# Missing required fields
version: v1.0.0
`

	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "invalid.yaml")
	err := os.WriteFile(tmpFile, []byte(invalidSchema), 0644)
	require.NoError(t, err)

	_, err = LoadSchema(tmpFile)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "validation failed")
}

// TestSaveSchema_YAML tests saving schema to YAML file.
func TestSaveSchema_YAML(t *testing.T) {
	schema := &PluginSchema{
		PackageName: "test",
		PluginName:  "TestPlugin",
		Version:     "v1.0.0",
		Description: "Test plugin",
		InputType: &TypeDef{
			Name: "TestInput",
			Kind: TypeKindStruct,
			Fields: []*FieldDef{
				{
					Name:     "Field1",
					JSONKey:  "field1",
					Required: true,
					Type: &TypeDef{
						Name:   "string",
						Kind:   TypeKindPrimitive,
						GoType: "string",
					},
				},
			},
		},
		OutputType: &TypeDef{
			Name: "TestOutput",
			Kind: TypeKindStruct,
			Fields: []*FieldDef{
				{
					Name:     "Result",
					JSONKey:  "result",
					Required: true,
					Type: &TypeDef{
						Name:   "string",
						Kind:   TypeKindPrimitive,
						GoType: "string",
					},
				},
			},
		},
	}

	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "output.yaml")

	err := SaveSchema(schema, tmpFile)
	require.NoError(t, err)

	// Verify file was created
	_, err = os.Stat(tmpFile)
	require.NoError(t, err)

	// Load it back and verify
	loaded, err := LoadSchema(tmpFile)
	require.NoError(t, err)
	assert.Equal(t, schema.PackageName, loaded.PackageName)
	assert.Equal(t, schema.PluginName, loaded.PluginName)
}

// TestSaveSchema_JSON tests saving schema to JSON file.
func TestSaveSchema_JSON(t *testing.T) {
	schema := &PluginSchema{
		PackageName: "test",
		PluginName:  "TestPlugin",
		Version:     "v1.0.0",
		InputType: &TypeDef{
			Name: "TestInput",
			Kind: TypeKindStruct,
			Fields: []*FieldDef{
				{
					Name:     "Field1",
					JSONKey:  "field1",
					Required: true,
					Type: &TypeDef{
						Name:   "string",
						Kind:   TypeKindPrimitive,
						GoType: "string",
					},
				},
			},
		},
		OutputType: &TypeDef{
			Name: "TestOutput",
			Kind: TypeKindStruct,
			Fields: []*FieldDef{
				{
					Name:     "Result",
					JSONKey:  "result",
					Required: true,
					Type: &TypeDef{
						Name:   "string",
						Kind:   TypeKindPrimitive,
						GoType: "string",
					},
				},
			},
		},
	}

	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "output.json")

	err := SaveSchema(schema, tmpFile)
	require.NoError(t, err)

	// Verify file was created
	_, err = os.Stat(tmpFile)
	require.NoError(t, err)

	// Load it back and verify
	loaded, err := LoadSchema(tmpFile)
	require.NoError(t, err)
	assert.Equal(t, schema.PackageName, loaded.PackageName)
	assert.Equal(t, schema.PluginName, loaded.PluginName)
}

// TestLoadSchemaFromYAML tests loading from YAML bytes.
func TestLoadSchemaFromYAML(t *testing.T) {
	yamlContent := []byte(`
package: test
name: TestPlugin
version: v1.0.0

input:
  name: TestInput
  kind: struct
  fields:
    - name: Field1
      json: field1
      required: true
      type:
        name: string
        kind: primitive
        type: string

output:
  name: TestOutput
  kind: struct
  fields:
    - name: Result
      json: result
      required: true
      type:
        name: string
        kind: primitive
        type: string
`)

	schema, err := LoadSchemaFromYAML(yamlContent)
	require.NoError(t, err)
	require.NotNil(t, schema)

	assert.Equal(t, "test", schema.PackageName)
	assert.Equal(t, "TestPlugin", schema.PluginName)
}

// TestLoadSchemaFromJSON tests loading from JSON bytes.
func TestLoadSchemaFromJSON(t *testing.T) {
	jsonContent := []byte(`{
  "package": "test",
  "name": "TestPlugin",
  "version": "v1.0.0",
  "input": {
    "name": "TestInput",
    "kind": "struct",
    "fields": [
      {
        "name": "Field1",
        "json": "field1",
        "required": true,
        "type": {
          "name": "string",
          "kind": "primitive",
          "type": "string"
        }
      }
    ]
  },
  "output": {
    "name": "TestOutput",
    "kind": "struct",
    "fields": [
      {
        "name": "Result",
        "json": "result",
        "required": true,
        "type": {
          "name": "string",
          "kind": "primitive",
          "type": "string"
        }
      }
    ]
  }
}`)

	schema, err := LoadSchemaFromJSON(jsonContent)
	require.NoError(t, err)
	require.NotNil(t, schema)

	assert.Equal(t, "test", schema.PackageName)
	assert.Equal(t, "TestPlugin", schema.PluginName)
}

// TestSaveSchemaAsYAML tests saving as YAML bytes.
func TestSaveSchemaAsYAML(t *testing.T) {
	schema := &PluginSchema{
		PackageName: "test",
		PluginName:  "TestPlugin",
		Version:     "v1.0.0",
		InputType: &TypeDef{
			Name: "TestInput",
			Kind: TypeKindStruct,
			Fields: []*FieldDef{
				{
					Name:    "Field1",
					JSONKey: "field1",
					Type: &TypeDef{
						Name:   "string",
						Kind:   TypeKindPrimitive,
						GoType: "string",
					},
				},
			},
		},
		OutputType: &TypeDef{
			Name: "TestOutput",
			Kind: TypeKindStruct,
			Fields: []*FieldDef{
				{
					Name:    "Result",
					JSONKey: "result",
					Type: &TypeDef{
						Name:   "string",
						Kind:   TypeKindPrimitive,
						GoType: "string",
					},
				},
			},
		},
	}

	data, err := SaveSchemaAsYAML(schema)
	require.NoError(t, err)
	require.NotEmpty(t, data)

	// Verify we can load it back
	loaded, err := LoadSchemaFromYAML(data)
	require.NoError(t, err)
	assert.Equal(t, schema.PackageName, loaded.PackageName)
}

// TestSaveSchemaAsJSON tests saving as JSON bytes.
func TestSaveSchemaAsJSON(t *testing.T) {
	schema := &PluginSchema{
		PackageName: "test",
		PluginName:  "TestPlugin",
		Version:     "v1.0.0",
		InputType: &TypeDef{
			Name: "TestInput",
			Kind: TypeKindStruct,
			Fields: []*FieldDef{
				{
					Name:    "Field1",
					JSONKey: "field1",
					Type: &TypeDef{
						Name:   "string",
						Kind:   TypeKindPrimitive,
						GoType: "string",
					},
				},
			},
		},
		OutputType: &TypeDef{
			Name: "TestOutput",
			Kind: TypeKindStruct,
			Fields: []*FieldDef{
				{
					Name:    "Result",
					JSONKey: "result",
					Type: &TypeDef{
						Name:   "string",
						Kind:   TypeKindPrimitive,
						GoType: "string",
					},
				},
			},
		},
	}

	data, err := SaveSchemaAsJSON(schema)
	require.NoError(t, err)
	require.NotEmpty(t, data)

	// Verify we can load it back
	loaded, err := LoadSchemaFromJSON(data)
	require.NoError(t, err)
	assert.Equal(t, schema.PackageName, loaded.PackageName)
}
