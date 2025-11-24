package plugingen

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewGenerator tests generator creation.
func TestNewGenerator(t *testing.T) {
	tests := []struct {
		name    string
		schema  *PluginSchema
		wantErr bool
	}{
		{
			name: "valid schema",
			schema: &PluginSchema{
				PackageName: "test",
				PluginName:  "TestPlugin",
				Version:     "v1.0.0",
				InputType: &TypeDef{
					Name: "Input",
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
					Name: "Output",
					Kind: TypeKindStruct,
					Fields: []*FieldDef{
						{
							Name:    "Result",
							JSONKey: "result",
							Type: &TypeDef{
								Name:   "int",
								Kind:   TypeKindPrimitive,
								GoType: "int",
							},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "invalid schema - missing package name",
			schema: &PluginSchema{
				PluginName: "TestPlugin",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gen, err := NewGenerator(tt.schema)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, gen)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, gen)
			}
		})
	}
}

// TestGenerate_SimpleStruct tests code generation for simple struct.
func TestGenerate_SimpleStruct(t *testing.T) {
	schema := &PluginSchema{
		PackageName: "simple",
		PluginName:  "SimplePlugin",
		Version:     "v1.0.0",
		Description: "A simple test plugin",
		InputType: &TypeDef{
			Name: "SimpleInput",
			Kind: TypeKindStruct,
			Fields: []*FieldDef{
				{
					Name:     "Name",
					JSONKey:  "name",
					Required: true,
					Type: &TypeDef{
						Name:   "string",
						Kind:   TypeKindPrimitive,
						GoType: "string",
					},
				},
				{
					Name:     "Age",
					JSONKey:  "age",
					Required: false,
					Type: &TypeDef{
						Name:   "int",
						Kind:   TypeKindPrimitive,
						GoType: "int",
					},
				},
			},
		},
		OutputType: &TypeDef{
			Name: "SimpleOutput",
			Kind: TypeKindStruct,
			Fields: []*FieldDef{
				{
					Name:     "Message",
					JSONKey:  "message",
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

	gen, err := NewGenerator(schema)
	require.NoError(t, err)

	code, err := gen.Generate()
	require.NoError(t, err)
	require.NotEmpty(t, code)

	// Verify generated code contains expected elements
	assert.Contains(t, code, "package simple")
	assert.Contains(t, code, "type SimpleInput struct")
	assert.Contains(t, code, "type SimpleOutput struct")
	assert.Contains(t, code, "SimpleInputFromMap")
	assert.Contains(t, code, "SimpleInputToMap")
	assert.Contains(t, code, "SimpleOutputFromMap")
	assert.Contains(t, code, "SimpleOutputToMap")
	assert.Contains(t, code, `json:"name"`)
	assert.Contains(t, code, `json:"age,omitempty"`)
}

// TestGenerate_ComplexTypes tests code generation with complex types.
func TestGenerate_ComplexTypes(t *testing.T) {
	schema := &PluginSchema{
		PackageName: "complex",
		PluginName:  "ComplexPlugin",
		Version:     "v2.0.0",
		Imports:     []string{"time"},
		InputType: &TypeDef{
			Name: "ComplexInput",
			Kind: TypeKindStruct,
			Fields: []*FieldDef{
				{
					Name:     "Tags",
					JSONKey:  "tags",
					Required: false,
					Type: &TypeDef{
						Name: "Tags",
						Kind: TypeKindSlice,
						ElementType: &TypeDef{
							Name:   "string",
							Kind:   TypeKindPrimitive,
							GoType: "string",
						},
					},
				},
				{
					Name:     "Metadata",
					JSONKey:  "metadata",
					Required: false,
					Type: &TypeDef{
						Name: "Metadata",
						Kind: TypeKindMap,
						KeyType: &TypeDef{
							Name:   "string",
							Kind:   TypeKindPrimitive,
							GoType: "string",
						},
						ElementType: &TypeDef{
							Name:   "string",
							Kind:   TypeKindPrimitive,
							GoType: "string",
						},
					},
				},
				{
					Name:     "Timestamp",
					JSONKey:  "timestamp",
					Required: false,
					Type: &TypeDef{
						Name:   "Time",
						Kind:   TypeKindPrimitive,
						GoType: "time.Time",
					},
				},
			},
		},
		OutputType: &TypeDef{
			Name: "ComplexOutput",
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

	gen, err := NewGenerator(schema)
	require.NoError(t, err)

	code, err := gen.Generate()
	require.NoError(t, err)
	require.NotEmpty(t, code)

	// Verify complex type handling
	assert.Contains(t, code, "[]string")
	assert.Contains(t, code, "map[string]string")
	assert.Contains(t, code, "time.Time")
	assert.Contains(t, code, `"time"`)
}

// TestGenerate_PointerTypes tests pointer type handling.
func TestGenerate_PointerTypes(t *testing.T) {
	schema := &PluginSchema{
		PackageName: "pointers",
		PluginName:  "PointerPlugin",
		Version:     "v1.0.0",
		InputType: &TypeDef{
			Name: "PointerInput",
			Kind: TypeKindStruct,
			Fields: []*FieldDef{
				{
					Name:     "OptionalField",
					JSONKey:  "optional_field",
					Required: false,
					Type: &TypeDef{
						Name: "string",
						Kind: TypeKindPointer,
						ElementType: &TypeDef{
							Name:   "string",
							Kind:   TypeKindPrimitive,
							GoType: "string",
						},
					},
				},
			},
		},
		OutputType: &TypeDef{
			Name: "PointerOutput",
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

	gen, err := NewGenerator(schema)
	require.NoError(t, err)

	code, err := gen.Generate()
	require.NoError(t, err)
	require.NotEmpty(t, code)

	// Verify pointer type handling
	assert.Contains(t, code, "*string")
}

// TestGenerate_NestedStructs tests nested struct handling.
func TestGenerate_NestedStructs(t *testing.T) {
	schema := &PluginSchema{
		PackageName: "nested",
		PluginName:  "NestedPlugin",
		Version:     "v1.0.0",
		InputType: &TypeDef{
			Name: "NestedInput",
			Kind: TypeKindStruct,
			Fields: []*FieldDef{
				{
					Name:     "Config",
					JSONKey:  "config",
					Required: true,
					Type: &TypeDef{
						Name: "Config",
						Kind: TypeKindStruct,
						Fields: []*FieldDef{
							{
								Name:     "Timeout",
								JSONKey:  "timeout",
								Required: true,
								Type: &TypeDef{
									Name:   "int",
									Kind:   TypeKindPrimitive,
									GoType: "int",
								},
							},
						},
					},
				},
			},
		},
		OutputType: &TypeDef{
			Name: "NestedOutput",
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

	gen, err := NewGenerator(schema)
	require.NoError(t, err)

	code, err := gen.Generate()
	require.NoError(t, err)
	require.NotEmpty(t, code)

	// Verify nested struct handling
	assert.Contains(t, code, "type Config struct")
	assert.Contains(t, code, "ConfigFromMap")
}

// TestCollectImports tests import collection logic.
func TestCollectImports(t *testing.T) {
	schema := &PluginSchema{
		PackageName: "test",
		PluginName:  "TestPlugin",
		Version:     "v1.0.0",
		Imports:     []string{"github.com/example/custom"},
		InputType: &TypeDef{
			Name: "Input",
			Kind: TypeKindStruct,
			Fields: []*FieldDef{
				{
					Name:    "Timestamp",
					JSONKey: "timestamp",
					Type: &TypeDef{
						Name:   "Time",
						Kind:   TypeKindPrimitive,
						GoType: "time.Time",
					},
				},
			},
		},
		OutputType: &TypeDef{
			Name: "Output",
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

	gen, err := NewGenerator(schema)
	require.NoError(t, err)

	imports := gen.collectImports()

	// Always includes fmt and errors
	assert.Contains(t, imports, "fmt")
	assert.Contains(t, imports, "errors")

	// Includes time because of time.Time usage
	assert.Contains(t, imports, "time")

	// Includes custom import from schema
	assert.Contains(t, imports, "github.com/example/custom")
}

// TestJSONTag tests JSON tag generation.
func TestJSONTag(t *testing.T) {
	gen := &Generator{}

	tests := []struct {
		name     string
		field    *FieldDef
		expected string
	}{
		{
			name: "required field",
			field: &FieldDef{
				JSONKey:  "name",
				Required: true,
			},
			expected: "`json:\"name\"`",
		},
		{
			name: "optional field",
			field: &FieldDef{
				JSONKey:  "age",
				Required: false,
			},
			expected: "`json:\"age,omitempty\"`",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := gen.jsonTag(tt.field)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestFormatDescription tests description formatting.
func TestFormatDescription(t *testing.T) {
	gen := &Generator{}

	tests := []struct {
		name     string
		desc     string
		expected string
	}{
		{
			name:     "empty description",
			desc:     "",
			expected: "",
		},
		{
			name:     "single line",
			desc:     "This is a field",
			expected: "\t// This is a field\n",
		},
		{
			name:     "multi line",
			desc:     "Line 1\nLine 2",
			expected: "\t// Line 1\n\t// Line 2\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := gen.formatDescription(tt.desc)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestGeneratedCodeCompiles tests that generated code is valid Go.
func TestGeneratedCodeCompiles(t *testing.T) {
	schema := &PluginSchema{
		PackageName: "testpkg",
		PluginName:  "TestPlugin",
		Version:     "v1.0.0",
		InputType: &TypeDef{
			Name: "TestInput",
			Kind: TypeKindStruct,
			Fields: []*FieldDef{
				{
					Name:     "Value",
					JSONKey:  "value",
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

	gen, err := NewGenerator(schema)
	require.NoError(t, err)

	code, err := gen.Generate()
	require.NoError(t, err)

	// Verify code is formatted (format.Source was successful)
	assert.True(t, strings.HasPrefix(code, "// Code generated"))
	assert.Contains(t, code, "package testpkg")

	// Code should be properly indented
	lines := strings.Split(code, "\n")
	for _, line := range lines {
		// Check that there are no tabs mixed with spaces
		if strings.Contains(line, "\t") && strings.Contains(line, "    ") {
			t.Errorf("Mixed tabs and spaces in line: %s", line)
		}
	}
}
