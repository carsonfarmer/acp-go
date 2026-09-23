package tsdef

import (
	"bytes"
	"fmt"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Overrides are corrections the generator applies to the TypeScript schema
// where it says less than the protocol means.
type Overrides struct {
	// Numbers gives the Go type of number members the TypeScript schema
	// leaves unconstrained, keyed by "Type.member" in TypeScript names: the
	// protocol's Rust schema declares them integers.
	Numbers map[string]string `yaml:"numbers"`
}

// numberTypes are the Go types a number override may choose.
var numberTypes = []string{"int32", "int64", "uint32", "uint64"}

// ParseOverrides reads overrides from YAML, rejecting unknown keys and types.
func ParseOverrides(data []byte) (*Overrides, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var o Overrides
	if err := dec.Decode(&o); err != nil {
		return nil, fmt.Errorf("overrides: %w", err)
	}
	for key, typ := range o.Numbers {
		if !slices.Contains(numberTypes, typ) {
			return nil, fmt.Errorf("overrides: numbers.%s: %q is not one of %v", key, typ, numberTypes)
		}
		if name, member, ok := strings.Cut(key, "."); !ok || name == "" || member == "" {
			return nil, fmt.Errorf("overrides: numbers.%s: want Type.member", key)
		}
	}
	return &o, nil
}

// Apply applies the overrides that name a member of schema and returns their
// keys. A schema version need not have every overridden member, but one it
// has must be a number, optionally nullable.
func (o *Overrides) Apply(schema *Schema) (map[string]bool, error) {
	defs := map[string]*Type{}
	for _, d := range schema.Types {
		defs[d.Name] = d.Type
	}
	applied := map[string]bool{}
	for key, typ := range o.Numbers {
		name, member, _ := strings.Cut(key, ".")
		t := defs[name]
		if t == nil || t.Kind != KindObject {
			continue
		}
		for i := range t.Fields {
			if t.Fields[i].Name != member {
				continue
			}
			number := numberMember(t.Fields[i].Type)
			if number == nil {
				return nil, fmt.Errorf("overrides: numbers.%s: member is %s, not a number", key, t.Fields[i].Type.Kind)
			}
			number.Number = typ
			applied[key] = true
		}
	}
	return applied, nil
}

// numberMember returns t when it is a number, or its number member when it
// is number | null.
func numberMember(t *Type) *Type {
	if t.Kind == KindNumber {
		return t
	}
	if t.Kind != KindUnion {
		return nil
	}
	var number *Type
	for _, m := range t.Members {
		switch m.Kind {
		case KindNumber:
			number = m
		case KindNull:
		default:
			return nil
		}
	}
	return number
}
