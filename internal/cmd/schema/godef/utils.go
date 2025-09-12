package godef

import (
	"github.com/kaptinlin/jsonschema"
	"github.com/samber/lo"
)

// isString checks if the schema represents a string type
func isString(def *jsonschema.Schema) bool {
	if len(def.Type) == 0 {
		return false
	}
	return def.Type[0] == "string"
}

func getGoTypeName(def *jsonschema.Schema) string {
	if len(def.Type) == 0 {
		return "any"
	}
	typeName := lo.Filter(def.Type, func(item string, _ int) bool {
		return item != "null"
	})[0]
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
	return typeName
}


func getNotNullTypes(defs []*jsonschema.Schema) []*jsonschema.Schema {
	return lo.Filter(defs, func(item *jsonschema.Schema, _ int) bool {
		if len(item.Type) == 0 || item.Type[0] != "null" {
			return true
		}
		return false
	})
}
