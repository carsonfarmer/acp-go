package main

import (
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"

	"github.com/ironpark/go-acp/internal/cmd/schema/codegen"
	"github.com/ironpark/go-acp/internal/cmd/schema/godef"
	"github.com/kaptinlin/jsonschema"
)

const (
	DefaultFilePermissions = 0644
	EmptyEnumName          = "Empty"
	DiscriminatorFieldName = "discriminator"
	JSONTag                = "json"
	OmitEmptyTag           = ",omitempty"
	JSONRawMessageType     = "json.RawMessage"
)

type OneOfVariant struct {
	FieldName string
	TypeName  string
	TypeValue string
}

// Generator handles schema to Go code generation
type Generator struct {
	config        *Config
	schema        *jsonschema.Schema
	metadata      *Metadata
	builder       *codegen.Builder
	generatedCode []byte
	skippedCount  int
	skippedItems  []string
}

// NewGenerator creates a new generator instance
func NewGenerator(config *Config) *Generator {
	return &Generator{
		config:  config,
		builder: codegen.NewBuilder(config.PackageName),
	}
}

// LoadSchema loads JSON schema from input file
func (g *Generator) LoadSchema() error {
	data, err := os.ReadFile(g.config.InputFile)
	if err != nil {
		return fmt.Errorf("failed to read schema file %s: %w", g.config.InputFile, err)
	}

	compiler := jsonschema.NewCompiler()
	schema, err := compiler.Compile(data)
	if err != nil {
		return fmt.Errorf("failed to compile schema file %s: %w", g.config.InputFile, err)
	}

	g.schema = schema
	return nil
}

// LoadMetadata loads metadata from meta.json file if provided
func (g *Generator) LoadMetadata() error {
	if g.config.MetaFile == "" {
		return nil // No metadata file provided
	}

	meta, err := LoadMetadata(g.config.MetaFile)
	if err != nil {
		return fmt.Errorf("failed to load metadata: %w", err)
	}

	g.metadata = meta
	return nil
}

// Generate generates Go code from the loaded schema
func (g *Generator) Generate() error {
	if g.schema == nil {
		return fmt.Errorf("schema not loaded, call LoadSchema() first")
	}

	definitions := godef.GetDefinitions(g.schema)
	var skippedDefinitions []string

	for _, definition := range definitions {
		// Skip ignored types
		if g.isTypeIgnored(definition.Name) {
			g.addSkippedItem(definition.Name)
			skippedDefinitions = append(skippedDefinitions, definition.Name)
			continue
		}
		
		if err := g.generateDefinition(definition); err != nil {
			if g.config.IgnoreErrors {
				fmt.Printf("Warning: Skipping definition %s due to error: %v\n", definition.Name, err)
				g.addSkippedItem(definition.Name)
				skippedDefinitions = append(skippedDefinitions, definition.Name)
				continue
			}
			return fmt.Errorf("failed to generate definition %s: %w", definition.Name, err)
		}
		
		// Mark internal types with appropriate comments
		if g.metadata != nil {
			g.markInternalType(definition.Name)
		}
	}

	if len(skippedDefinitions) > 0 {
		fmt.Printf("Successfully generated types, skipped %d definitions: %v\n",
			len(skippedDefinitions), skippedDefinitions)
	}

	// Generate constants from metadata if available (after all type definitions)
	if g.metadata != nil {
		g.generateConstants()
	}

	return nil
}

// SaveToFile saves the generated code to output file or stdout
func (g *Generator) SaveToFile() error {
	data, err := g.builder.Build()
	if err != nil {
		return fmt.Errorf("failed to build code: %w", err)
	}

	// If no output file specified, write to stdout
	if g.config.OutputFile == "" {
		_, err := os.Stdout.Write(data)
		return err
	}

	if err := os.WriteFile(g.config.OutputFile, data, DefaultFilePermissions); err != nil {
		return fmt.Errorf("failed to write output file: %w", err)
	}

	return nil
}

// generateDefinition generates code for a single definition
func (g *Generator) generateDefinition(definition godef.Definition) error {
	switch definition.Type {
	case godef.Primitive:
		return g.generatePrimitive(definition)
	case godef.Enum:
		return g.generateEnum(definition.Name, definition.Schema)
	case godef.Struct:
		desc := strings.Split(definition.GetDefinition(), "\n")
		return g.generateStruct(definition.Name, definition.Schema, desc...)
	case godef.ComplexStruct:
		return g.generateComplexStruct(definition.Name, definition.Schema)
	case godef.Ref:
		return g.generateTypeAlias(definition)
	case godef.Union:
		return g.generateUnion(definition)
	default:
		return fmt.Errorf("unsupported definition type: %s", definition.Type)
	}
}

// generatePrimitive generates a primitive type alias
func (g *Generator) generatePrimitive(def godef.Definition) error {
	typeDecl := g.builder.CreateType(def.Name, def.GetFieldType())

	// Add comment if available
	if comment := def.GetDefinition(); comment != "" {
		typeDecl.Comment = &codegen.Comment{Text: comment}
	}

	return nil
}

// generateEnum generates an enum type with constants
func (g *Generator) generateEnum(name string, def *jsonschema.Schema) error {
	enumValues, err := g.extractEnumValues(def)
	if err != nil {
		return fmt.Errorf("failed to extract enum values: %w", err)
	}

	if len(enumValues) == 0 {
		return fmt.Errorf("no enum values found for %s", name)
	}

	// Create type using CreateType
	typeDecl := g.builder.CreateType(name, "string")

	// Add type comment if available
	if def.Description != nil && *def.Description != "" {
		typeDecl.Comment = &codegen.Comment{Text: *def.Description}
	}

	// Create constants using CreateConstBlock
	constants := g.builder.CreateConstBlock()
	for _, enumValue := range enumValues {
		constName := name + enumValue.Name
		value := `"` + enumValue.Value + `"`
		constants.AddConst(constName, name, value, enumValue.Comment)
	}

	return nil
}

// EnumValue represents an enum value for generation
type EnumValue struct {
	Name    string
	Value   string
	Comment string
}

// extractEnumValues extracts enum values from schema
func (g *Generator) extractEnumValues(def *jsonschema.Schema) ([]EnumValue, error) {
	var enumValues []EnumValue

	// Handle Enum field
	for _, enum := range def.Enum {
		if str, ok := enum.(string); ok {
			enumName := toTitleCase(str)
			if enumName == "" {
				enumName = EmptyEnumName
			}
			enumValues = append(enumValues, EnumValue{
				Name:  enumName,
				Value: str,
			})
		}
	}

	// Handle OneOf field
	for _, enum := range def.OneOf {
		if enum.Const != nil && enum.Const.Value != nil {
			if str, ok := enum.Const.Value.(string); ok {
				enumName := toTitleCase(str)
				if enumName == "" {
					enumName = EmptyEnumName
				}
				comment := ""
				if enum.Description != nil {
					comment = *enum.Description
				}
				enumValues = append(enumValues, EnumValue{
					Name:    enumName,
					Value:   str,
					Comment: comment,
				})
			}
		}
	}

	return enumValues, nil
}

// isFieldOptional determines if a field should have omitempty tag
// A field is optional if:
// 1. It's nullable (has null in type or in anyOf/oneOf)
// 2. It's not in the schema's required array
func (g *Generator) isFieldOptional(propName string, schema *jsonschema.Schema, def godef.Definition) bool {
	// Check if field is nullable
	if def.NullAble {
		return true
	}

	// Check if field is not in required array
	if schema.Required != nil {
		if slices.Contains(schema.Required, propName) {
			return false // Field is required, so not optional
		}
		return true // Field is not in required array, so it's optional
	}

	// No required array means all fields are optional
	return true
}

// generateStruct generates a struct type
func (g *Generator) generateStruct(name string, schema *jsonschema.Schema, comments ...string) error {
	if schema.Properties == nil {
		return fmt.Errorf("no properties found for struct %s", name)
	}

	// Create struct using new API
	st := g.builder.CreateStruct(name)

	// Add comment if provided
	if len(comments) > 0 {
		st.Comment = &codegen.Comment{
			Text: strings.Join(comments, "\n"),
		}
	}

	// Add fields using new API
	if err := g.addStructFields(st, schema); err != nil {
		return fmt.Errorf("failed to add struct fields: %w", err)
	}

	return nil
}

// addStructFields adds fields to struct using new API
func (g *Generator) addStructFields(st *codegen.StructDecl, schema *jsonschema.Schema) error {
	// Sort property names alphabetically for consistent field ordering
	var propNames []string
	for propName := range *schema.Properties {
		propNames = append(propNames, propName)
	}
	slices.Sort(propNames)

	for _, propName := range propNames {
		propSchema := (*schema.Properties)[propName]
		def := godef.GetDefinition(propName, propSchema)

		if err := g.addStructField(st, propName, propSchema, def, schema); err != nil {
			return err
		}
	}

	return nil
}

// addStructField adds a single field to the struct
func (g *Generator) addStructField(st *codegen.StructDecl, propName string, propSchema *jsonschema.Schema, def godef.Definition, parentSchema *jsonschema.Schema) error {
	fieldName := toTitleCase(propName)
	tag := g.createJSONTag(propName, parentSchema, def)

	switch def.Type {
	case godef.Primitive:
		return g.addPrimitiveField(st, fieldName, propSchema, tag)
	case godef.Struct:
		return g.addStructFieldNested(st, fieldName, propName, propSchema, tag)
	case godef.Array:
		return g.addArrayField(st, fieldName, def, tag)
	case godef.Ref:
		return g.addRefField(st, fieldName, propName, parentSchema, def, tag)
	default:
		return g.addDefaultField(st, fieldName, propSchema, tag, propName, def)
	}
}

// createJSONTag creates JSON tag for field
func (g *Generator) createJSONTag(propName string, schema *jsonschema.Schema, def godef.Definition) string {
	jsonTag := propName
	if g.isFieldOptional(propName, schema, def) {
		jsonTag += OmitEmptyTag
	}
	return `json:"` + jsonTag + `"`
}

// addPrimitiveField adds a primitive type field
func (g *Generator) addPrimitiveField(st *codegen.StructDecl, fieldName string, propSchema *jsonschema.Schema, tag string) error {
	fieldType := g.getGoTypeName(propSchema)
	if _, err := st.CreateField(fieldName, fieldType, tag); err != nil {
		return fmt.Errorf("failed to create primitive field %s: %w", fieldName, err)
	}
	return nil
}

// addStructFieldNested adds a nested struct field
func (g *Generator) addStructFieldNested(st *codegen.StructDecl, fieldName, propName string, propSchema *jsonschema.Schema, tag string) error {
	// Generate nested struct
	desc := []string{}
	if propSchema.Description != nil {
		desc = strings.Split(*propSchema.Description, "\n")
	}
	if err := g.generateStruct(propName, propSchema, desc...); err != nil {
		return fmt.Errorf("failed to generate nested struct %s: %w", propName, err)
	}
	fieldType := g.getGoTypeName(propSchema)
	if _, err := st.CreateField(fieldName, fieldType, tag); err != nil {
		return fmt.Errorf("failed to create struct field %s: %w", fieldName, err)
	}
	return nil
}

// addArrayField adds an array type field
func (g *Generator) addArrayField(st *codegen.StructDecl, fieldName string, def godef.Definition, tag string) error {
	fieldType := def.GetFieldType()
	if _, err := st.CreateField(fieldName, fieldType, tag); err != nil {
		return fmt.Errorf("failed to create array field %s: %w", fieldName, err)
	}
	return nil
}

// addRefField adds a reference type field
func (g *Generator) addRefField(st *codegen.StructDecl, fieldName, propName string, schema *jsonschema.Schema, def godef.Definition, tag string) error {
	fieldType := def.GetFieldType()
	// For optional reference fields, make them pointer types
	if g.isFieldOptional(propName, schema, def) && !strings.HasPrefix(fieldType, "*") {
		fieldType = "*" + fieldType
	}
	if _, err := st.CreateField(fieldName, fieldType, tag); err != nil {
		return fmt.Errorf("failed to create ref field %s: %w", fieldName, err)
	}
	return nil
}

// addDefaultField adds a field for unsupported or no-type schemas
func (g *Generator) addDefaultField(st *codegen.StructDecl, fieldName string, propSchema *jsonschema.Schema, tag, propName string, def godef.Definition) error {
	if g.isNoTypeSchema(propSchema) {
		if _, err := st.CreateField(fieldName, JSONRawMessageType, tag); err != nil {
			return fmt.Errorf("failed to create %s field %s: %w", JSONRawMessageType, fieldName, err)
		}
		return nil
	}
	return fmt.Errorf("unsupported field type %s for property %s", def.Type, propName)
}

// generateTypeAlias generates a type alias for single reference types
func (g *Generator) generateTypeAlias(definition godef.Definition) error {
	targetType := definition.GetFieldType()
	typeDecl := g.builder.CreateType(definition.Name, targetType)

	// Add comment if available
	if comment := definition.GetDefinition(); comment != "" {
		typeDecl.Comment = &codegen.Comment{Text: comment}
	}

	return nil
}

// generateUnion generates a union type (interface{}) for types without discriminator
func (g *Generator) generateUnion(definition godef.Definition) error {
	schema := definition.Schema
	allVariants := append([]*jsonschema.Schema{}, schema.AnyOf...)
	allVariants = append(allVariants, schema.OneOf...)
	
	return g.generateUnionInterface(definition.Name, schema, allVariants)
}

// generateComplexStruct generates a complex struct with AnyOf/OneOf variants
func (g *Generator) generateComplexStruct(name string, schema *jsonschema.Schema) error {
	// Detect discriminator field automatically from both AnyOf and OneOf
	allVariants := append([]*jsonschema.Schema{}, schema.AnyOf...)
	allVariants = append(allVariants, schema.OneOf...)
	discriminatorField := g.detectDiscriminatorField(allVariants)

	// Create struct using new API
	st := g.builder.CreateStruct(name)

	// Add comment if provided
	if schema.Description != nil {
		st.Comment = &codegen.Comment{
			Text: *schema.Description,
		}
	}

	// Process variants
	variants, hasTypeDiscriminator := g.processVariants(name, st, schema.AnyOf, schema.OneOf, discriminatorField)

	// Add discriminator field and JSON methods if needed
	if hasTypeDiscriminator {
		g.addDiscriminatorField(st)
		if len(variants) > 0 {
			g.addJSONMethodsWithDiscriminator(name, variants, discriminatorField)
			g.addAccessorMethods(name, variants)
		}
	}

	return nil
}

// isAllRefs checks if all variants in the list are $ref types or null types
func (g *Generator) isAllRefs(variants []*jsonschema.Schema) bool {
	if len(variants) == 0 {
		return false
	}
	for _, variant := range variants {
		// Allow $ref types and null types
		if variant.Ref == "" && !g.isNullType(variant) {
			return false
		}
	}
	return true
}

// isNullType checks if a schema represents a null type
func (g *Generator) isNullType(schema *jsonschema.Schema) bool {
	return len(schema.Type) == 1 && schema.Type[0] == "null"
}

// generateUnionInterface generates an interface type for union types without discriminator
func (g *Generator) generateUnionInterface(name string, schema *jsonschema.Schema, variants []*jsonschema.Schema) error {
	// Extract type names from $ref and null types
	var typeNames []string
	hasNull := false
	
	for _, variant := range variants {
		if variant.Ref != "" {
			typeName := strings.TrimPrefix(variant.Ref, "#/$defs/")
			typeNames = append(typeNames, typeName)
		} else if g.isNullType(variant) {
			hasNull = true
		}
	}

	// Create type alias to interface{}
	typeDecl := g.builder.CreateType(name, "interface{}")

	// Build comment with description and possible types
	var commentLines []string
	if schema.Description != nil {
		commentLines = append(commentLines, strings.Split(*schema.Description, "\n")...)
		commentLines = append(commentLines, "")
	}
	
	if len(typeNames) > 0 || hasNull {
		commentLines = append(commentLines, "Possible types:")
		for _, typeName := range typeNames {
			commentLines = append(commentLines, "- "+typeName)
		}
		if hasNull {
			commentLines = append(commentLines, "- null")
		}
	}

	if len(commentLines) > 0 {
		typeDecl.Comment = &codegen.Comment{Text: strings.Join(commentLines, "\n")}
	}

	return nil
}

// processVariants processes AnyOf and OneOf variants to generate struct fields
func (g *Generator) processVariants(name string, st *codegen.StructDecl, anyOfSchemas, oneOfSchemas []*jsonschema.Schema, discriminatorField string) ([]OneOfVariant, bool) {
	var variants []OneOfVariant
	hasTypeDiscriminator := false

	// Process AnyOf variants
	for _, schema := range anyOfSchemas {
		variant, hasDiscriminator := g.processVariant(name, st, schema, discriminatorField, "AnyOf")
		if hasDiscriminator {
			variants = append(variants, variant)
			hasTypeDiscriminator = true
		}
	}

	// Process OneOf variants
	for _, schema := range oneOfSchemas {
		variant, hasDiscriminator := g.processVariant(name, st, schema, discriminatorField, "OneOf")
		if hasDiscriminator {
			variants = append(variants, variant)
			hasTypeDiscriminator = true
		}
	}

	return variants, hasTypeDiscriminator
}

// processVariant processes a single variant schema
func (g *Generator) processVariant(name string, st *codegen.StructDecl, schema *jsonschema.Schema, discriminatorField, variantType string) (OneOfVariant, bool) {
	def := godef.GetDefinition(name, schema)
	if def.Type != godef.Struct || schema.Properties == nil {
		return OneOfVariant{}, false
	}

	props := *schema.Properties
	if discriminatorField == "" {
		return OneOfVariant{}, false
	}

	v, ok := props[discriminatorField]
	if !ok || v.Const == nil {
		return OneOfVariant{}, false
	}

	value := v.Const.Value.(string)
	variantName := toTitleCase(name) + toTitleCase(strings.ReplaceAll(value, "_", ""))

	desc := []string{variantType + " " + variantName}
	if schema.Description != nil {
		desc = strings.Split(*schema.Description, "\n")
	}

	if err := g.generateStruct(variantName, schema, desc...); err != nil {
		return OneOfVariant{}, false
	}

	fieldName := strings.ToLower(strings.ReplaceAll(value, "_", ""))
	if _, err := st.CreateField(fieldName, "*"+variantName); err != nil {
		return OneOfVariant{}, false
	}

	return OneOfVariant{
		FieldName: fieldName,
		TypeName:  variantName,
		TypeValue: value,
	}, true
}

// addDiscriminatorField adds the discriminator field to the struct
func (g *Generator) addDiscriminatorField(st *codegen.StructDecl) {
	// Insert discriminator field at the beginning
	if _, err := st.CreateField(DiscriminatorFieldName, "string"); err != nil {
		return
	}

	// Move discriminator field to the beginning
	if len(st.Fields) > 1 {
		// Move the last added field (discriminator) to the beginning
		discriminator := st.Fields[len(st.Fields)-1]
		st.Fields = st.Fields[:len(st.Fields)-1]
		st.Fields = append([]*codegen.Field{discriminator}, st.Fields...)
	}
}

// addJSONMethodsWithDiscriminator generates JSON marshaling methods for OneOf structs with custom discriminator
func (g *Generator) addJSONMethodsWithDiscriminator(typeName string, variants []OneOfVariant, discriminatorField string) {
	// Ensure required imports
	g.builder.AddImport("encoding/json")
	g.builder.AddImport("fmt")

	// Find the struct declaration
	structDecl := g.builder.GetStructDeclaration(typeName)
	if structDecl != nil {
		// Generate and add MarshalJSON method
		marshalMethod := g.generateMarshalJSONMethod(typeName, variants)
		unmarshalMethod := g.generateUnmarshalJSONMethod(typeName, variants, discriminatorField)

		structDecl.AddMethod(marshalMethod)
		structDecl.AddMethod(unmarshalMethod)
	}
}

// addAccessorMethods generates getter and checker methods for oneOf variant fields
func (g *Generator) addAccessorMethods(typeName string, variants []OneOfVariant) {
	// Find the struct declaration
	structDecl := g.builder.GetStructDeclaration(typeName)
	if structDecl == nil {
		return
	}

	receiverVar := strings.ToLower(typeName[:1])

	for _, variant := range variants {
		// Generate getter method (e.g., GetCancelled() *RequestPermissionOutcomeCancelled)
		getterName := "Get" + toTitleCase(variant.FieldName)
		getterBody := fmt.Sprintf("return %s.%s", receiverVar, variant.FieldName)

		getterMethod := &codegen.MethodDecl{
			Receiver: &codegen.Field{
				Name: receiverVar,
				Type: &codegen.PointerType{Elem: &codegen.BasicType{Name: typeName}},
			},
			Name: getterName,
			Results: []*codegen.Field{
				{Type: &codegen.PointerType{Elem: &codegen.BasicType{Name: variant.TypeName}}},
			},
			Body: getterBody,
		}

		structDecl.AddMethod(getterMethod)

		// Generate checker method (e.g., IsCancelled() bool)
		checkerName := "Is" + toTitleCase(variant.FieldName)
		checkerBody := fmt.Sprintf("return %s.%s != nil", receiverVar, variant.FieldName)

		checkerMethod := &codegen.MethodDecl{
			Receiver: &codegen.Field{
				Name: receiverVar,
				Type: &codegen.PointerType{Elem: &codegen.BasicType{Name: typeName}},
			},
			Name: checkerName,
			Results: []*codegen.Field{
				{Type: &codegen.BasicType{Name: "bool"}},
			},
			Body: checkerBody,
		}

		structDecl.AddMethod(checkerMethod)
	}
}

// generateMarshalJSONMethod creates MarshalJSON method implementation
func (g *Generator) generateMarshalJSONMethod(typeName string, variants []OneOfVariant) *codegen.MethodDecl {
	receiverVar := strings.ToLower(typeName[:1])
	body := fmt.Sprintf(`switch %s.discriminator {
%s	}
	return nil, fmt.Errorf("no variant is set for %s")`,
		receiverVar, g.generateMarshalCases(receiverVar, variants), typeName)

	return &codegen.MethodDecl{
		Receiver: &codegen.Field{
			Name: receiverVar,
			Type: &codegen.BasicType{Name: typeName},
		},
		Name: "MarshalJSON",
		Results: []*codegen.Field{
			{Type: &codegen.SliceType{Elem: &codegen.BasicType{Name: "byte"}}},
			{Type: &codegen.BasicType{Name: "error"}},
		},
		Body: body,
	}
}

// generateUnmarshalJSONMethod creates UnmarshalJSON method implementation
func (g *Generator) generateUnmarshalJSONMethod(typeName string, variants []OneOfVariant, discriminatorField string) *codegen.MethodDecl {
	receiverVar := strings.ToLower(typeName[:1])

	body := fmt.Sprintf(`var discriminator struct {
		%s string `+"`json:\"%s\"`"+`
	}
	if err := json.Unmarshal(data, &discriminator); err != nil {
		return err
	}

	switch discriminator.%s {
%s	}
	return fmt.Errorf("unknown discriminator value: %%s", discriminator.%s)`,
		toTitleCase(discriminatorField), discriminatorField, toTitleCase(discriminatorField),
		g.generateUnmarshalCases(receiverVar, variants),
		toTitleCase(discriminatorField))

	return &codegen.MethodDecl{
		Receiver: &codegen.Field{
			Name: receiverVar,
			Type: &codegen.PointerType{Elem: &codegen.BasicType{Name: typeName}},
		},
		Name: "UnmarshalJSON",
		Params: []*codegen.Field{
			{Name: "data", Type: &codegen.SliceType{Elem: &codegen.BasicType{Name: "byte"}}},
		},
		Results: []*codegen.Field{
			{Type: &codegen.BasicType{Name: "error"}},
		},
		Body: body,
	}
}

// generateMarshalCases generates switch cases for MarshalJSON
func (g *Generator) generateMarshalCases(receiverVar string, variants []OneOfVariant) string {
	var cases []string

	for _, variant := range variants {
		caseBody := fmt.Sprintf(`case "%s":
		return json.Marshal(%s.%s)`,
			variant.TypeValue,
			receiverVar, variant.FieldName)
		cases = append(cases, "\t"+caseBody)
	}

	return strings.Join(cases, "\n")
}

// generateUnmarshalCases generates switch cases for UnmarshalJSON
func (g *Generator) generateUnmarshalCases(receiverVar string, variants []OneOfVariant) string {
	var cases []string

	for _, variant := range variants {
		caseBody := fmt.Sprintf(`case "%s":
		var v %s
		if err := json.Unmarshal(data, &v); err != nil {
			return err
		}
		%s.%s = &v
		%s.discriminator = "%s"
		return nil`,
			variant.TypeValue, variant.TypeName,
			receiverVar, variant.FieldName,
			receiverVar, variant.TypeValue)
		cases = append(cases, "\t"+caseBody)
	}

	return strings.Join(cases, "\n")
}

// detectDiscriminatorField analyzes oneOf variants to automatically detect discriminator field
func (g *Generator) detectDiscriminatorField(oneOfSchemas []*jsonschema.Schema) string {
	if len(oneOfSchemas) < 2 {
		return ""
	}

	// Collect all property names that have const values across all variants
	candidateFields := make(map[string]map[string]bool) // fieldName -> set of const values

	for _, schema := range oneOfSchemas {
		if schema.Properties == nil {
			continue
		}

		props := *schema.Properties
		for propName, propSchema := range props {
			if propSchema.Const != nil {
				// Only consider string and integer const values
				switch propSchema.Const.Value.(type) {
				case string, int, int64, float64:
					if candidateFields[propName] == nil {
						candidateFields[propName] = make(map[string]bool)
					}
					valueStr := fmt.Sprintf("%v", propSchema.Const.Value)
					candidateFields[propName][valueStr] = true
				}
			}
		}
	}

	// Find fields that appear in ALL variants with DIFFERENT values
	for fieldName, values := range candidateFields {
		// Count how many variants have this field with const values
		variantCount := 0
		for _, schema := range oneOfSchemas {
			if schema.Properties != nil {
				props := *schema.Properties
				if prop, exists := props[fieldName]; exists && prop.Const != nil {
					variantCount++
				}
			}
		}

		// If this field appears in all variants and has different values, it's a discriminator
		if variantCount == len(oneOfSchemas) && len(values) == len(oneOfSchemas) {
			return fieldName
		}
	}

	return ""
}

// getGoTypeName converts JSON schema type to Go type name
func (g *Generator) getGoTypeName(schema *jsonschema.Schema) string {
	if len(schema.Type) == 0 {
		if schema.Ref != "" {
			return strings.TrimPrefix(schema.Ref, "#/$defs/")
		}
		return "any"
	}

	nullAble := len(schema.Type) == 2 && slices.Contains(schema.Type, "null")
	typeName := schema.Type[0]
	if nullAble {
		for _, t := range schema.Type {
			if t != "null" {
				typeName = t
				break
			}
		}
	}

	switch typeName {
	case "string":
		typeName = "string"
	case "boolean":
		typeName = "bool"
	case "number":
		typeName = "float64"
	case "integer":
		typeName = "int64"
	}

	if nullAble {
		switch typeName {
		case "bool", "int64", "float64":
			typeName = "*" + typeName
		}
	}

	return typeName
}

// isNoTypeSchema checks if schema has no type information
func (g *Generator) isNoTypeSchema(schema *jsonschema.Schema) bool {
	return len(schema.Type) == 0 &&
		schema.Ref == "" &&
		schema.Properties == nil &&
		schema.AnyOf == nil &&
		schema.OneOf == nil
}

// GetGeneratedContent returns the generated code as bytes
func (g *Generator) GetGeneratedContent() []byte {
	if g.generatedCode != nil {
		return g.generatedCode
	}

	// Generate content if not cached
	data, err := g.builder.Build()
	if err != nil {
		return nil
	}

	g.generatedCode = data
	return data
}

// GetSkippedCount returns the number of skipped definitions
func (g *Generator) GetSkippedCount() int {
	return g.skippedCount
}

// GetSkippedItems returns the list of skipped definition names
func (g *Generator) GetSkippedItems() []string {
	return g.skippedItems
}

// generateConstants generates constants from metadata
func (g *Generator) generateConstants() {
	if g.metadata == nil {
		return
	}

	// Generate protocol version constant
	constants := g.builder.CreateConstBlock()
	constants.AddConst("CurrentProtocolVersion", "int", fmt.Sprintf("%d", g.metadata.Version), "Current protocol version from metadata")

	// Generate agent methods structured constant
	if len(g.metadata.AgentMethods) > 0 {
		g.generateStructuredConstants("AgentMethods", g.metadata.AgentMethods, "Agent method names")
	}

	// Generate client methods structured constant
	if len(g.metadata.ClientMethods) > 0 {
		g.generateStructuredConstants("ClientMethods", g.metadata.ClientMethods, "Client method names")
	}
}

// generateStructuredConstants generates a structured constant similar to TypeScript objects
func (g *Generator) generateStructuredConstants(varName string, methods map[string]string, comment string) {
	// Sort keys for consistent ordering
	keys := make([]string, 0, len(methods))
	for key := range methods {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	
	// Create a variable with anonymous struct type and literal
	varDecl := &codegen.VariableDecl{
		Comment: &codegen.Comment{Text: comment},
		Name:    varName,
		Type:    "", // Will be inferred from value
		Value:   g.buildAnonymousStructLiteral(methods, keys),
	}
	
	g.builder.AddDeclaration(varDecl)
}

// buildAnonymousStructLiteral builds an anonymous struct literal string
func (g *Generator) buildAnonymousStructLiteral(methods map[string]string, keys []string) string {
	var structLiteral strings.Builder
	structLiteral.WriteString("struct{\n")
	
	// Define struct fields
	for _, key := range keys {
		fieldName := g.formatFieldName(key)
		structLiteral.WriteString(fmt.Sprintf("\t%s string\n", fieldName))
	}
	
	structLiteral.WriteString("}{\n")
	
	// Initialize struct fields
	for _, key := range keys {
		value := methods[key]
		fieldName := g.formatFieldName(key)
		structLiteral.WriteString(fmt.Sprintf("\t%s: \"%s\",\n", fieldName, value))
	}
	
	structLiteral.WriteString("}")
	return structLiteral.String()
}

// formatFieldName formats field names consistently
func (g *Generator) formatFieldName(key string) string {
	// Convert snake_case to PascalCase with proper handling
	parts := strings.Split(key, "_")
	var result strings.Builder
	
	for _, part := range parts {
		if part != "" {
			// Capitalize first letter and keep the rest as is
			result.WriteString(strings.ToUpper(part[:1]) + strings.ToLower(part[1:]))
		}
	}
	
	return result.String()
}

// markInternalType marks a type as internal if it's in the internal types list
func (g *Generator) markInternalType(typeName string) {
	if g.metadata == nil {
		return
	}

	internalTypes := g.metadata.GetInternalTypes()
	for _, internal := range internalTypes {
		if typeName == internal {
			// Find and mark the type declaration as internal
			if typeDecl := g.builder.GetTypeDeclaration(typeName); typeDecl != nil {
				if typeDecl.Comment == nil {
					typeDecl.Comment = &codegen.Comment{}
				}
				typeDecl.Comment.Text = "@internal\n" + typeDecl.Comment.Text
			}
			if structDecl := g.builder.GetStructDeclaration(typeName); structDecl != nil {
				if structDecl.Comment == nil {
					structDecl.Comment = &codegen.Comment{}
				}
				structDecl.Comment.Text = "@internal\n" + structDecl.Comment.Text
			}
			break
		}
	}
}

// isTypeIgnored checks if a type should be ignored during generation
func (g *Generator) isTypeIgnored(typeName string) bool {
	for _, ignoredType := range g.config.IgnoreTypes {
		if typeName == ignoredType {
			return true
		}
	}
	return false
}

// addSkippedItem adds a definition to the skipped items list
func (g *Generator) addSkippedItem(name string) {
	g.skippedCount++
	g.skippedItems = append(g.skippedItems, name)
}
