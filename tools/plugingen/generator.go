// Package plugingen provides code generation tools for type-safe plugin boundaries.
package plugingen

import (
	"bytes"
	"fmt"
	"go/format"
	"strings"
	"text/template"
)

// Generator generates strongly-typed Go code from Schema definitions.
//
// This eliminates runtime reflection overhead by generating compile-time
// conversion code between map[string]any and Go structs.
type Generator struct {
	schema *PluginSchema
	buf    *bytes.Buffer
}

// NewGenerator creates a new code generator for the given schema.
func NewGenerator(schema *PluginSchema) (*Generator, error) {
	if err := schema.Validate(); err != nil {
		return nil, fmt.Errorf("invalid schema: %w", err)
	}

	return &Generator{
		schema: schema,
		buf:    new(bytes.Buffer),
	}, nil
}

// Generate generates the complete Go source code.
//
// Returns formatted Go code ready to be written to a file.
func (g *Generator) Generate() (string, error) {
	g.buf.Reset()

	// Generate package declaration and imports
	if err := g.generateHeader(); err != nil {
		return "", fmt.Errorf("failed to generate header: %w", err)
	}

	// Generate struct definitions
	if err := g.generateStructs(); err != nil {
		return "", fmt.Errorf("failed to generate structs: %w", err)
	}

	// Generate conversion functions
	if err := g.generateConversions(); err != nil {
		return "", fmt.Errorf("failed to generate conversions: %w", err)
	}

	// Format the generated code
	formatted, err := format.Source(g.buf.Bytes())
	if err != nil {
		return "", fmt.Errorf("failed to format code: %w\nGenerated code:\n%s", err, g.buf.String())
	}

	return string(formatted), nil
}

// generateHeader generates package declaration and imports.
func (g *Generator) generateHeader() error {
	tmpl, err := template.New("header").Parse(headerTemplate)
	if err != nil {
		return err
	}

	data := headerData{
		PackageName: g.schema.PackageName,
		Description: g.schema.Description,
		PluginName:  g.schema.PluginName,
		Version:     g.schema.Version,
		Imports:     g.collectImports(),
	}

	return tmpl.Execute(g.buf, data)
}

// collectImports collects all required imports.
func (g *Generator) collectImports() []string {
	imports := make(map[string]bool)

	// Always need fmt and errors
	imports["fmt"] = true
	imports["errors"] = true

	// Add schema-specified imports
	for _, imp := range g.schema.Imports {
		imports[imp] = true
	}

	// Collect imports from type definitions
	g.collectTypeImports(g.schema.InputType, imports)
	g.collectTypeImports(g.schema.OutputType, imports)

	// Convert to slice
	result := make([]string, 0, len(imports))
	for imp := range imports {
		result = append(result, imp)
	}

	return result
}

// collectTypeImports recursively collects imports from type definitions.
func (g *Generator) collectTypeImports(t *TypeDef, imports map[string]bool) {
	if t == nil {
		return
	}

	// Check if type contains time.Time
	if t.Kind == TypeKindPrimitive && t.GoType == "time.Time" {
		imports["time"] = true
	}

	// Recurse into fields
	for _, field := range t.Fields {
		g.collectTypeImports(field.Type, imports)
	}

	// Recurse into element and key types
	g.collectTypeImports(t.ElementType, imports)
	g.collectTypeImports(t.KeyType, imports)
}

// generateStructs generates struct type definitions.
func (g *Generator) generateStructs() error {
	// Generate input struct and its nested types
	if err := g.generateStructRecursive(g.schema.InputType); err != nil {
		return fmt.Errorf("failed to generate input struct: %w", err)
	}

	// Generate output struct and its nested types
	if err := g.generateStructRecursive(g.schema.OutputType); err != nil {
		return fmt.Errorf("failed to generate output struct: %w", err)
	}

	return nil
}

// generateStructRecursive generates a struct and all its nested structs.
func (g *Generator) generateStructRecursive(t *TypeDef) error {
	if t == nil || t.Kind != TypeKindStruct {
		return nil
	}

	// Generate nested structs first
	for _, field := range t.Fields {
		if err := g.generateNestedStructs(field.Type); err != nil {
			return err
		}
	}

	// Generate this struct
	return g.generateStruct(t)
}

// generateNestedStructs generates nested struct types.
func (g *Generator) generateNestedStructs(t *TypeDef) error {
	if t == nil {
		return nil
	}

	switch t.Kind {
	case TypeKindStruct:
		return g.generateStructRecursive(t)
	case TypeKindSlice, TypeKindPointer:
		return g.generateNestedStructs(t.ElementType)
	case TypeKindMap:
		if err := g.generateNestedStructs(t.KeyType); err != nil {
			return err
		}
		return g.generateNestedStructs(t.ElementType)
	}

	return nil
}

// generateStruct generates a single struct definition.
func (g *Generator) generateStruct(t *TypeDef) error {
	if t.Kind != TypeKindStruct {
		return nil // Only generate code for struct types
	}

	tmpl, err := template.New("struct").Funcs(template.FuncMap{
		"goType":      func(td *TypeDef) string { return td.GoTypeName() },
		"jsonTag":     g.jsonTag,
		"description": g.formatDescription,
		"toLower":     strings.ToLower,
	}).Parse(structTemplate)
	if err != nil {
		return err
	}

	data := structData{
		Name:   t.Name,
		Fields: t.Fields,
	}

	return tmpl.Execute(g.buf, data)
}

// generateConversions generates conversion functions (FromMap, ToMap).
func (g *Generator) generateConversions() error {
	// Generate input conversions and nested types
	if err := g.generateConversionsRecursive(g.schema.InputType); err != nil {
		return fmt.Errorf("failed to generate input conversions: %w", err)
	}

	// Generate output conversions and nested types
	if err := g.generateConversionsRecursive(g.schema.OutputType); err != nil {
		return fmt.Errorf("failed to generate output conversions: %w", err)
	}

	return nil
}

// generateConversionsRecursive generates conversion functions for a type and its nested types.
func (g *Generator) generateConversionsRecursive(t *TypeDef) error {
	if t == nil || t.Kind != TypeKindStruct {
		return nil
	}

	// Generate conversions for nested structs first
	for _, field := range t.Fields {
		if err := g.generateNestedConversions(field.Type); err != nil {
			return err
		}
	}

	// Generate conversions for this struct
	if err := g.generateFromMap(t); err != nil {
		return err
	}
	if err := g.generateToMap(t); err != nil {
		return err
	}

	return nil
}

// generateNestedConversions generates conversion functions for nested types.
func (g *Generator) generateNestedConversions(t *TypeDef) error {
	if t == nil {
		return nil
	}

	switch t.Kind {
	case TypeKindStruct:
		return g.generateConversionsRecursive(t)
	case TypeKindSlice, TypeKindPointer:
		return g.generateNestedConversions(t.ElementType)
	case TypeKindMap:
		if err := g.generateNestedConversions(t.KeyType); err != nil {
			return err
		}
		return g.generateNestedConversions(t.ElementType)
	}

	return nil
}

// generateFromMap generates the FromMap conversion function.
func (g *Generator) generateFromMap(t *TypeDef) error {
	if t.Kind != TypeKindStruct {
		return nil
	}

	tmpl, err := template.New("frommap").Funcs(template.FuncMap{
		"convertField": g.generateFieldConversion,
	}).Parse(fromMapTemplate)
	if err != nil {
		return err
	}

	data := fromMapData{
		TypeName: t.Name,
		Fields:   t.Fields,
	}

	return tmpl.Execute(g.buf, data)
}

// generateToMap generates the ToMap serialization function.
func (g *Generator) generateToMap(t *TypeDef) error {
	if t.Kind != TypeKindStruct {
		return nil
	}

	tmpl, err := template.New("tomap").Funcs(template.FuncMap{
		"serializeField": g.generateFieldSerialization,
	}).Parse(toMapTemplate)
	if err != nil {
		return err
	}

	data := toMapData{
		TypeName: t.Name,
		Fields:   t.Fields,
	}

	return tmpl.Execute(g.buf, data)
}

// generateFieldConversion generates conversion code for a single field.
func (g *Generator) generateFieldConversion(field *FieldDef) string {
	var buf bytes.Buffer

	jsonKey := field.JSONKey
	fieldName := field.Name
	typeName := field.Type.GoTypeName()

	// Check if field exists in map
	fmt.Fprintf(&buf, "\tif val, ok := data[\"%s\"]; ok {\n", jsonKey)

	// Generate type-specific conversion
	switch field.Type.Kind {
	case TypeKindPrimitive:
		g.generatePrimitiveConversion(&buf, fieldName, typeName, "val")

	case TypeKindSlice:
		g.generateSliceConversion(&buf, fieldName, field.Type)

	case TypeKindMap:
		g.generateMapConversion(&buf, fieldName, field.Type)

	case TypeKindPointer:
		g.generatePointerConversion(&buf, fieldName, field.Type)

	case TypeKindStruct:
		g.generateNestedStructConversion(&buf, fieldName, field.Type)
	}

	buf.WriteString("\t}\n")

	// Check required fields - only check if not set
	if field.Required {
		// For primitive types, check zero value differently
		switch field.Type.Kind {
		case TypeKindPrimitive:
			needsCheck := true
			switch field.Type.GoType {
			case "string":
				fmt.Fprintf(&buf, "\tif result.%s == \"\" {\n", fieldName)
			case "int", "int8", "int16", "int32", "int64",
				"uint", "uint8", "uint16", "uint32", "uint64",
				"float32", "float64":
				fmt.Fprintf(&buf, "\tif result.%s == 0 {\n", fieldName)
			case "bool":
				// For bool, we can't really check if it's set or not with a simple check
				// Skip validation for bool types
				needsCheck = false
			default:
				// For other types (time.Time, etc.), check against zero value
				fmt.Fprintf(&buf, "\tif result.%s == (%s{}) {\n", fieldName, typeName)
			}
			if needsCheck {
				fmt.Fprintf(&buf, "\t\treturn nil, fmt.Errorf(\"required field '%s' is missing\")\n", jsonKey)
				buf.WriteString("\t}\n")
			}
		case TypeKindPointer:
			fmt.Fprintf(&buf, "\tif result.%s == nil {\n", fieldName)
			fmt.Fprintf(&buf, "\t\treturn nil, fmt.Errorf(\"required field '%s' is missing\")\n", jsonKey)
			buf.WriteString("\t}\n")
		default:
			// For slices, maps, structs - check zero value
			fmt.Fprintf(&buf, "\tif result.%s == (%s{}) {\n", fieldName, typeName)
			fmt.Fprintf(&buf, "\t\treturn nil, fmt.Errorf(\"required field '%s' is missing\")\n", jsonKey)
			buf.WriteString("\t}\n")
		}
	}

	return buf.String()
}

// generatePrimitiveConversion generates conversion for primitive types.
func (g *Generator) generatePrimitiveConversion(buf *bytes.Buffer, fieldName, typeName, valName string) {
	fmt.Fprintf(buf, "\t\tif typed, ok := %s.(%s); ok {\n", valName, typeName)
	fmt.Fprintf(buf, "\t\t\tresult.%s = typed\n", fieldName)
	buf.WriteString("\t\t} else {\n")
	fmt.Fprintf(buf, "\t\t\treturn nil, fmt.Errorf(\"field '%s' has wrong type, expected %s\")\n", fieldName, typeName)
	buf.WriteString("\t\t}\n")
}

// generateSliceConversion generates conversion for slice types.
func (g *Generator) generateSliceConversion(buf *bytes.Buffer, fieldName string, t *TypeDef) {
	elemType := t.ElementType.GoTypeName()

	buf.WriteString("\t\tif slice, ok := val.([]interface{}); ok {\n")
	fmt.Fprintf(buf, "\t\t\tresult.%s = make([]%s, len(slice))\n", fieldName, elemType)
	buf.WriteString("\t\t\tfor i, item := range slice {\n")

	if t.ElementType.Kind == TypeKindPrimitive {
		fmt.Fprintf(buf, "\t\t\t\tif typed, ok := item.(%s); ok {\n", elemType)
		fmt.Fprintf(buf, "\t\t\t\t\tresult.%s[i] = typed\n", fieldName)
		buf.WriteString("\t\t\t\t} else {\n")
		buf.WriteString("\t\t\t\t\treturn nil, fmt.Errorf(\"slice element %d has wrong type\", i)\n")
		buf.WriteString("\t\t\t\t}\n")
	} else {
		buf.WriteString("\t\t\t\t// Complex type conversion needed\n")
		fmt.Fprintf(buf, "\t\t\t\tresult.%s[i] = item.(%s)\n", fieldName, elemType)
	}

	buf.WriteString("\t\t\t}\n")
	buf.WriteString("\t\t} else {\n")
	fmt.Fprintf(buf, "\t\t\treturn nil, fmt.Errorf(\"field '%s' is not a slice\")\n", fieldName)
	buf.WriteString("\t\t}\n")
}

// generateMapConversion generates conversion for map types.
func (g *Generator) generateMapConversion(buf *bytes.Buffer, fieldName string, t *TypeDef) {
	keyType := t.KeyType.GoTypeName()
	elemType := t.ElementType.GoTypeName()

	buf.WriteString("\t\tif m, ok := val.(map[string]interface{}); ok {\n")
	fmt.Fprintf(buf, "\t\t\tresult.%s = make(map[%s]%s)\n", fieldName, keyType, elemType)
	buf.WriteString("\t\t\tfor k, v := range m {\n")

	if t.ElementType.Kind == TypeKindPrimitive {
		fmt.Fprintf(buf, "\t\t\t\tif typed, ok := v.(%s); ok {\n", elemType)
		fmt.Fprintf(buf, "\t\t\t\t\tresult.%s[k] = typed\n", fieldName)
		buf.WriteString("\t\t\t\t}\n")
	} else {
		fmt.Fprintf(buf, "\t\t\t\tresult.%s[k] = v.(%s)\n", fieldName, elemType)
	}

	buf.WriteString("\t\t\t}\n")
	buf.WriteString("\t\t} else {\n")
	fmt.Fprintf(buf, "\t\t\treturn nil, fmt.Errorf(\"field '%s' is not a map\")\n", fieldName)
	buf.WriteString("\t\t}\n")
}

// generatePointerConversion generates conversion for pointer types.
func (g *Generator) generatePointerConversion(buf *bytes.Buffer, fieldName string, t *TypeDef) {
	elemType := t.ElementType.GoTypeName()

	buf.WriteString("\t\tif val != nil {\n")
	if t.ElementType.Kind == TypeKindPrimitive {
		fmt.Fprintf(buf, "\t\t\tif typed, ok := val.(%s); ok {\n", elemType)
		fmt.Fprintf(buf, "\t\t\t\tresult.%s = &typed\n", fieldName)
		buf.WriteString("\t\t\t}\n")
	} else {
		fmt.Fprintf(buf, "\t\t\ttyped := val.(%s)\n", elemType)
		fmt.Fprintf(buf, "\t\t\tresult.%s = &typed\n", fieldName)
	}
	buf.WriteString("\t\t}\n")
}

// generateNestedStructConversion generates conversion for nested struct types.
func (g *Generator) generateNestedStructConversion(buf *bytes.Buffer, fieldName string, t *TypeDef) {
	buf.WriteString("\t\tif nested, ok := val.(map[string]interface{}); ok {\n")
	fmt.Fprintf(buf, "\t\t\tif converted, err := %sFromMap(nested); err == nil {\n", t.Name)
	fmt.Fprintf(buf, "\t\t\t\tresult.%s = *converted\n", fieldName)
	buf.WriteString("\t\t\t}\n")
	buf.WriteString("\t\t}\n")
}

// generateFieldSerialization generates serialization code for a single field.
func (g *Generator) generateFieldSerialization(field *FieldDef) string {
	jsonKey := field.JSONKey
	fieldName := field.Name

	var buf bytes.Buffer

	switch field.Type.Kind {
	case TypeKindPrimitive:
		fmt.Fprintf(&buf, "\tresult[\"%s\"] = v.%s\n", jsonKey, fieldName)

	case TypeKindSlice:
		fmt.Fprintf(&buf, "\tif v.%s != nil {\n", fieldName)
		fmt.Fprintf(&buf, "\t\tslice := make([]interface{}, len(v.%s))\n", fieldName)
		fmt.Fprintf(&buf, "\t\tfor i, item := range v.%s {\n", fieldName)
		buf.WriteString("\t\t\tslice[i] = item\n")
		buf.WriteString("\t\t}\n")
		fmt.Fprintf(&buf, "\t\tresult[\"%s\"] = slice\n", jsonKey)
		buf.WriteString("\t}\n")

	case TypeKindMap:
		fmt.Fprintf(&buf, "\tif v.%s != nil {\n", fieldName)
		buf.WriteString("\t\tm := make(map[string]interface{})\n")
		fmt.Fprintf(&buf, "\t\tfor k, val := range v.%s {\n", fieldName)
		buf.WriteString("\t\t\tm[k] = val\n")
		buf.WriteString("\t\t}\n")
		fmt.Fprintf(&buf, "\t\tresult[\"%s\"] = m\n", jsonKey)
		buf.WriteString("\t}\n")

	case TypeKindPointer:
		fmt.Fprintf(&buf, "\tif v.%s != nil {\n", fieldName)
		fmt.Fprintf(&buf, "\t\tresult[\"%s\"] = *v.%s\n", jsonKey, fieldName)
		buf.WriteString("\t}\n")

	case TypeKindStruct:
		fmt.Fprintf(&buf, "\tresult[\"%s\"] = %sToMap(&v.%s)\n", jsonKey, field.Type.Name, fieldName)
	}

	return buf.String()
}

// jsonTag formats the JSON struct tag for a field.
func (g *Generator) jsonTag(field *FieldDef) string {
	tag := field.JSONKey
	if !field.Required {
		tag += ",omitempty"
	}
	return fmt.Sprintf("`json:\"%s\"`", tag)
}

// formatDescription formats field descriptions as comments.
func (g *Generator) formatDescription(desc string) string {
	if desc == "" {
		return ""
	}
	lines := strings.Split(desc, "\n")
	var result strings.Builder
	for _, line := range lines {
		result.WriteString("\t// ")
		result.WriteString(line)
		result.WriteString("\n")
	}
	return result.String()
}
