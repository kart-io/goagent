// Package plugingen provides code generation tools for type-safe plugin boundaries.
//
// This package generates strongly-typed conversion code from Schema definitions,
// eliminating runtime reflection overhead and providing compile-time type safety.
package plugingen

import (
	"fmt"
	"strings"
)

// PluginSchema defines the input/output types for a plugin.
//
// This is used to generate strongly-typed Go code that converts between
// map[string]any and Go structs without runtime reflection.
type PluginSchema struct {
	// PackageName is the Go package name for generated code
	PackageName string `yaml:"package" json:"package"`

	// PluginName is the name of the plugin
	PluginName string `yaml:"name" json:"name"`

	// Version is the plugin schema version
	Version string `yaml:"version" json:"version"`

	// Description provides documentation for the plugin
	Description string `yaml:"description,omitempty" json:"description,omitempty"`

	// InputType defines the input structure
	InputType *TypeDef `yaml:"input" json:"input"`

	// OutputType defines the output structure
	OutputType *TypeDef `yaml:"output" json:"output"`

	// Imports lists additional imports needed
	Imports []string `yaml:"imports,omitempty" json:"imports,omitempty"`
}

// TypeDef defines a Go type structure.
type TypeDef struct {
	// Name is the Go type name
	Name string `yaml:"name" json:"name"`

	// Kind is the type kind (struct, primitive, slice, map)
	Kind TypeKind `yaml:"kind" json:"kind"`

	// Fields for struct types
	Fields []*FieldDef `yaml:"fields,omitempty" json:"fields,omitempty"`

	// ElementType for slice/map types
	ElementType *TypeDef `yaml:"element,omitempty" json:"element,omitempty"`

	// KeyType for map types
	KeyType *TypeDef `yaml:"key,omitempty" json:"key,omitempty"`

	// GoType is the underlying Go type for primitives
	GoType string `yaml:"type,omitempty" json:"type,omitempty"`
}

// FieldDef defines a struct field.
type FieldDef struct {
	// Name is the Go field name
	Name string `yaml:"name" json:"name"`

	// Type is the field type
	Type *TypeDef `yaml:"type" json:"type"`

	// JSONKey is the JSON/map key name
	JSONKey string `yaml:"json" json:"json"`

	// Required indicates if the field is required
	Required bool `yaml:"required,omitempty" json:"required,omitempty"`

	// Description provides field documentation
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
}

// TypeKind represents the kind of type.
type TypeKind string

const (
	// TypeKindStruct represents a struct type
	TypeKindStruct TypeKind = "struct"

	// TypeKindPrimitive represents a primitive type (int, string, etc.)
	TypeKindPrimitive TypeKind = "primitive"

	// TypeKindSlice represents a slice type
	TypeKindSlice TypeKind = "slice"

	// TypeKindMap represents a map type
	TypeKindMap TypeKind = "map"

	// TypeKindPointer represents a pointer type
	TypeKindPointer TypeKind = "pointer"
)

// Validate validates the schema.
func (s *PluginSchema) Validate() error {
	if s.PackageName == "" {
		return fmt.Errorf("package name is required")
	}
	if s.PluginName == "" {
		return fmt.Errorf("plugin name is required")
	}
	if s.InputType == nil {
		return fmt.Errorf("input type is required")
	}
	if s.OutputType == nil {
		return fmt.Errorf("output type is required")
	}

	if err := s.InputType.Validate(); err != nil {
		return fmt.Errorf("input type validation failed: %w", err)
	}
	if err := s.OutputType.Validate(); err != nil {
		return fmt.Errorf("output type validation failed: %w", err)
	}

	return nil
}

// Validate validates a type definition.
func (t *TypeDef) Validate() error {
	if t.Name == "" {
		return fmt.Errorf("type name is required")
	}

	switch t.Kind {
	case TypeKindStruct:
		if len(t.Fields) == 0 {
			return fmt.Errorf("struct type must have fields")
		}
		for _, field := range t.Fields {
			if err := field.Validate(); err != nil {
				return fmt.Errorf("field %s validation failed: %w", field.Name, err)
			}
		}

	case TypeKindPrimitive:
		if t.GoType == "" {
			return fmt.Errorf("primitive type must specify go_type")
		}

	case TypeKindSlice:
		if t.ElementType == nil {
			return fmt.Errorf("slice type must specify element type")
		}
		if err := t.ElementType.Validate(); err != nil {
			return fmt.Errorf("slice element validation failed: %w", err)
		}

	case TypeKindMap:
		if t.KeyType == nil {
			return fmt.Errorf("map type must specify key type")
		}
		if t.ElementType == nil {
			return fmt.Errorf("map type must specify element type")
		}
		if err := t.KeyType.Validate(); err != nil {
			return fmt.Errorf("map key validation failed: %w", err)
		}
		if err := t.ElementType.Validate(); err != nil {
			return fmt.Errorf("map element validation failed: %w", err)
		}

	case TypeKindPointer:
		if t.ElementType == nil {
			return fmt.Errorf("pointer type must specify element type")
		}
		if err := t.ElementType.Validate(); err != nil {
			return fmt.Errorf("pointer element validation failed: %w", err)
		}

	default:
		return fmt.Errorf("unknown type kind: %s", t.Kind)
	}

	return nil
}

// Validate validates a field definition.
func (f *FieldDef) Validate() error {
	if f.Name == "" {
		return fmt.Errorf("field name is required")
	}
	if f.Type == nil {
		return fmt.Errorf("field type is required")
	}
	if f.JSONKey == "" {
		// Default to lowercase field name
		f.JSONKey = strings.ToLower(f.Name[:1]) + f.Name[1:]
	}

	return f.Type.Validate()
}

// GoTypeName returns the Go type name for this type.
func (t *TypeDef) GoTypeName() string {
	switch t.Kind {
	case TypeKindStruct:
		return t.Name

	case TypeKindPrimitive:
		return t.GoType

	case TypeKindSlice:
		return "[]" + t.ElementType.GoTypeName()

	case TypeKindMap:
		return fmt.Sprintf("map[%s]%s", t.KeyType.GoTypeName(), t.ElementType.GoTypeName())

	case TypeKindPointer:
		return "*" + t.ElementType.GoTypeName()

	default:
		return "interface{}"
	}
}

// IsSimpleType returns true if this is a simple type (no conversion needed).
func (t *TypeDef) IsSimpleType() bool {
	return t.Kind == TypeKindPrimitive
}
