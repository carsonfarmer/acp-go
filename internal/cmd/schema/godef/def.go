package godef

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/kaptinlin/jsonschema"
	"github.com/samber/lo"
)

// GenerateType represents the type of Go construct to generate from a JSON schema definition
type GenerateType int

const (
	Unknown GenerateType = iota
	Enum
	ComplexStruct
	Struct
	Primitive
	Array
	Ref
	Union
)

func (t GenerateType) String() string {
	return []string{"Unknown", "Enum", "ComplexStruct", "Struct", "Primitive", "Array", "Ref", "Union"}[t]
}

// Definition represents a JSON schema definition with its corresponding Go type
type Definition struct {
	Name     string             // Name of the definition
	TypeName string             // Type name of the definition
	Type     GenerateType       // Type of Go construct to generate
	NullAble bool               // Whether the type is nullable
	Schema   *jsonschema.Schema // JSON schema definition
}

func (d Definition) GetDefinition() string {
	desc := d.Schema.Description
	if desc != nil {
		return *desc
	}
	return ""
}
func findRef(schema *jsonschema.Schema) string {
	if len(schema.AnyOf) > 0 {
		for _, anyOf := range schema.AnyOf {
			if anyOf.Ref != "" {
				return anyOf.Ref
			}
		}
	}
	if len(schema.OneOf) > 0 {
		for _, oneOf := range schema.OneOf {
			if oneOf.Ref != "" {
				return oneOf.Ref
			}
		}
	}
	return ""
}
func (d Definition) GetFieldType() string {
	if d.TypeName != "" {
		return d.TypeName
	}

	switch d.Type {
	case Ref:
		if d.TypeName == "" {
			ref := findRef(d.Schema)
			if ref != "" {
				d.TypeName = strings.TrimPrefix(ref, "#/$defs/")
			} else {
				fmt.Printf("d.TypeName is empty for %s\n", d.Name)
				return "unknown"
			}
		}
		if d.NullAble {
			return "*" + d.TypeName
		}
		return d.TypeName
	case Array:
		ftype := GetDefinition(d.Schema.Items.Ref, d.Schema.Items).GetFieldType()
		return "[]" + ftype
	case Primitive:
		typeName := getGoTypeName(d.Schema)
		if d.NullAble && typeName != "string" {
			return "*" + typeName
		}
		return typeName
	}
	return "unknown"
}

func (d Definition) PropExist(propName string) bool {
	return d.Schema.Properties != nil && (*d.Schema.Properties)[propName] != nil
}

func (d Definition) Prop(propName string) *jsonschema.Schema {
	return (*d.Schema.Properties)[propName]
}

func (d Definition) IsNullAble() bool {
	return d.NullAble
}

func GetDefinitions(schema *jsonschema.Schema) []Definition {
	definitions := []Definition{}
	for name, schema := range schema.Defs {
		definitions = append(definitions, GetDefinition(name, schema))
	}
	sort.Slice(definitions, func(i, j int) bool {
		if definitions[i].Type == definitions[j].Type {
			return definitions[i].Name < definitions[j].Name
		}
		return definitions[i].Type < definitions[j].Type
	})
	return definitions
}

func GetDefinition(name string, schema *jsonschema.Schema) Definition {
	genType, nullAble := getDefinitionType(schema)
	def := Definition{
		Name:     name,
		Type:     genType,
		NullAble: nullAble,
		Schema:   schema,
	}
	if schema.Ref != "" {
		def.TypeName = strings.TrimPrefix(schema.Ref, "#/$defs/")
	}
	if def.Type == Struct && schema.Properties == nil {
		notNullTypes := getNotNullTypes(schema.AnyOf)
		if len(notNullTypes) == 1 {
			def.Schema = notNullTypes[0]
		} else {
			notNullTypes = getNotNullTypes(schema.OneOf)
			if len(notNullTypes) == 1 {
				def.Schema = notNullTypes[0]
			} else {
				fmt.Printf("schema.Properties is nil for %s\n", name)

			}
		}
	}
	return def
}

func isNullAble(def *jsonschema.Schema) bool {
	if slices.Contains(def.Type, "null") {
		return true
	}
	if len(def.OneOf) > 0 {
		if slices.ContainsFunc(def.OneOf, func(oneOf *jsonschema.Schema) bool {
			return isNullAble(oneOf)
		}) {
			return true
		}
	}
	if len(def.AnyOf) > 0 {
		if slices.ContainsFunc(def.AnyOf, func(anyOf *jsonschema.Schema) bool {
			return isNullAble(anyOf)
		}) {
			return true
		}
	}
	return false
}

// getDefinitionType determines the appropriate Go type for a JSON schema definition
func getDefinitionType(def *jsonschema.Schema) (genType GenerateType, nullAble bool) {
	nullAble = isNullAble(def)
	switch len(def.Type) {
	case 0:
		if len(def.OneOf) > 0 {
			// Check if it's a single $ref - this should be treated as a type alias
			if len(def.OneOf) == 1 && def.OneOf[0].Ref != "" {
				return Ref, false
			}
			allString := true
			for _, oneOf := range def.OneOf {
				if !isString(oneOf) || oneOf.Const == nil {
					allString = false
				}
			}
			if allString {
				return Enum, false
			}
			// Check if this is a union of $refs (without discriminator)
			if isUnionOfRefs(def.OneOf) {
				return Union, nullAble
			}
			return ComplexStruct, false
		}
		if len(def.AnyOf) > 0 {
			notNullTypes := getNotNullTypes(def.AnyOf)
			if len(notNullTypes) == 0 {
				return Unknown, false
			}
			if len(notNullTypes) == 1 {
				// Check if it's a single $ref - this should be treated as a type alias
				if notNullTypes[0].Ref != "" {
					return Ref, false
				}
				gt, _ := getDefinitionType(notNullTypes[0])
				return gt, nullAble
			}
			// Check if this is a union of $refs (without discriminator)
			if isUnionOfRefs(def.AnyOf) {
				return Union, nullAble
			}
			return ComplexStruct, false
		}
	case 1:
		defType := def.Type[0]
		if defType == "object" {
			return Struct, false
		}
		if defType == "array" {
			return Array, nullAble
		}
		// enum
		if len(def.Enum) > 0 {
			return Enum, false
		}
		return Primitive, false
	case 2:
		typeName := lo.Filter(def.Type, func(item string, _ int) bool {
			return item != "null"
		})[0]
		if typeName == "object" {
			return Struct, nullAble
		}
		if typeName == "array" {
			return Array, nullAble
		}
		return Primitive, nullAble
	}
	if def.Ref != "" {
		return Ref, false
	}
	return Unknown, false
}

// isUnionOfRefs checks if all schemas in the list are $ref types or null types
func isUnionOfRefs(schemas []*jsonschema.Schema) bool {
	if len(schemas) <= 1 {
		return false
	}
	
	hasRef := false
	for _, schema := range schemas {
		if schema.Ref != "" {
			hasRef = true
		} else if len(schema.Type) == 1 && schema.Type[0] == "null" {
			// null type is allowed
		} else {
			return false
		}
	}
	return hasRef
}
