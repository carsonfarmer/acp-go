// Emission of top-level definitions: aliases, enums and intersection expansion.

package tsgen

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/ironpark/go-acp/internal/cmd/schema/tsdef"
)

func (g *generator) add(name string, t *tsdef.Type) string {
	base := name
	for i := 2; g.names[name]; i++ {
		name = fmt.Sprintf("%s%d", base, i)
	}
	g.names[name] = true
	g.pending = append(g.pending, tsdef.Definition{Name: name, Type: t})
	return name
}

// form is how definition() emits a top-level definition.
type form int

const (
	formEnum     form = iota // literal union: named scalar with constants
	formOpenEnum             // literal union that also admits its base type
	formNamed                // distinct scalar type, e.g. SessionID string
	formStruct               // object with fields
	formUnion                // union wrapper
	formAlias                // type X = expr
)

// form classifies t, expanding intersections first. It is the single source
// for both emission and canonical's alias following.
func (g *generator) form(t *tsdef.Type) (form, *tsdef.Type, error) {
	if _, ok := literals(t); ok {
		return formEnum, t, nil
	}
	if _, _, ok := openEnum(t); ok {
		return formOpenEnum, t, nil
	}
	if t.Kind == "intersection" {
		expanded, err := g.expand(t, map[string]bool{})
		if err != nil {
			return 0, nil, err
		}
		t = expanded
	}
	switch t.Kind {
	case "object":
		if len(t.Fields) == 0 {
			return formAlias, t, nil // records alias map[string]T
		}
		return formStruct, t, nil
	case "union":
		if nonnull, isNull := nullable(t); isNull && nonnull.Kind != "union" {
			return formAlias, t, nil
		}
		return formUnion, t, nil
	case "string", "number", "boolean":
		return formNamed, t, nil
	}
	return formAlias, t, nil
}

// aliasDefinition reports whether definition() emits d as "type X = ..."
// rather than as a distinct named type.
func (g *generator) aliasDefinition(d *tsdef.Type) bool {
	f, _, err := g.form(d)
	return err == nil && f == formAlias
}

func (g *generator) definition(d tsdef.Definition) error {
	if g.isAbsorbed(d.Name) {
		return nil // declared by the tagged union that took it over as a variant
	}
	f, t, err := g.form(d.Type)
	if err != nil {
		return err
	}
	file := fileTypes
	switch {
	case f == formEnum || f == formOpenEnum || f == formNamed:
		file = fileEnums
	case envelope(d.Name):
		file = fileEnvelope
	case f == formUnion:
		file = fileUnions
	}
	g.use(file)
	g.write("\n")
	switch f {
	case formEnum:
		kind, _ := literals(t)
		members := t.Members
		if t.Kind == "literal" {
			members = []*tsdef.Type{t}
		}
		return g.enum(d.Name, d.Comment, kind, members, false)
	case formOpenEnum:
		base, members, _ := openEnum(t)
		kind, _ := literals(members[0])
		if base.Kind == "number" && base.Number != "" {
			kind = base.Number
		}
		return g.enum(d.Name, d.Comment, kind, members, true)
	case formStruct:
		g.write("%s", doc(d.Name, d.Comment))
		return g.structType(d.Name, t, "")
	case formUnion:
		return g.union(d.Name, d.Comment, t)
	}
	g.write("%s", doc(d.Name, d.Comment))
	expr, err := g.expr(t, d.Name+"Value")
	if err != nil {
		return err
	}
	if f == formNamed {
		// Distinct identifier types (SessionID vs ToolCallID) cannot be mixed
		// up, and they can carry their own Zod unmarshaler.
		g.write("type %s %s\n", d.Name, expr)
		return nil
	}
	g.alias(d.Name, expr)
	return nil
}

// alias emits "type name = expr" and records it so Zod rules skip the alias:
// it shares a reflect.Type with expr.
func (g *generator) alias(name, expr string) {
	g.aliases[name] = true
	g.write("type %s = %s\n", name, expr)
}

// enum emits a documented named scalar type with one constant per literal. Open enums
// also accept values outside the listed constants.
func (g *generator) enum(typeName, sdkDoc, kind string, members []*tsdef.Type, open bool) error {
	var note string
	if open {
		note = typeName + " also accepts values outside the listed constants; use [" + typeName + ".Known] to check."
	}
	g.write("%stype %s %s\n", doc(typeName, sdkDoc, note), typeName, kind)
	var names []string
	g.write("const (\n")
	for _, m := range members {
		value := m.Literal
		label := value
		if kind == "string" {
			var err error
			label, err = strconv.Unquote(value)
			if err != nil {
				return err
			}
		}
		name := typeName + Name(label)
		if override, ok := literalNames[typeName][value]; ok {
			name = typeName + override // used as written
		}
		if g.names[name] {
			return fmt.Errorf("enum constant collision %s", name)
		}
		g.names[name] = true
		names = append(names, name)
		g.write("%s %s = %s\n", name, typeName, value)
	}
	g.write(")\n")
	if open {
		g.write("// Known reports whether v is one of the protocol-defined constants.\n")
		g.write("func (v %s) Known() bool {\nswitch v {\ncase %s:\nreturn true\n}\nreturn false\n}\n", typeName, strings.Join(names, ", "))
	}
	return nil
}

// expand distributes intersections over unions and merges object members.
func (g *generator) expand(t *tsdef.Type, seen map[string]bool) (*tsdef.Type, error) {
	if t.Kind == "ref" {
		if seen[t.Name] {
			return nil, fmt.Errorf("cyclic intersection through %s", t.Name)
		}
		target, ok := g.defs[t.Name]
		if !ok {
			return nil, fmt.Errorf("unresolved type %s", t.Name)
		}
		seen[t.Name] = true
		out, err := g.expand(target, seen)
		delete(seen, t.Name)
		return out, err
	}
	if t.Kind == "union" {
		var members []*tsdef.Type
		for _, member := range t.Members {
			expanded, err := g.expand(member, seen)
			if err != nil {
				return nil, err
			}
			if expanded.Kind == "union" {
				members = append(members, expanded.Members...)
			} else {
				members = append(members, expanded)
			}
		}
		return &tsdef.Type{Kind: "union", Members: members}, nil
	}
	if t.Kind != "intersection" {
		return t, nil
	}
	variants := []*tsdef.Type{{Kind: "object"}}
	for _, member := range t.Members {
		m, err := g.expand(member, seen)
		if err != nil {
			return nil, err
		}
		alternatives := []*tsdef.Type{m}
		if m.Kind == "union" {
			alternatives = m.Members
		}
		var next []*tsdef.Type
		for _, base := range variants {
			for _, alt := range alternatives {
				a, err := g.expand(alt, seen)
				if err != nil {
					return nil, err
				}
				if a.Kind != "object" {
					return nil, fmt.Errorf("intersection member is %s, expected object", a.Kind)
				}
				merged := &tsdef.Type{Kind: "object", Fields: append([]tsdef.Field(nil), base.Fields...), Element: base.Element}
				if a.Element != nil {
					merged.Element = a.Element
				}
				for _, f := range a.Fields {
					found := false
					for i, old := range merged.Fields {
						if old.Name == f.Name {
							found = true
							f.Optional = f.Optional && old.Optional
							if !reflect.DeepEqual(f.Type, old.Type) {
								if f.Type.Kind == "literal" && old.Type.Kind == "string" {
								} else if old.Type.Kind == "literal" && f.Type.Kind == "string" {
									f.Type = old.Type
								} else {
									return nil, fmt.Errorf("unsupported intersection for property %s", f.Name)
								}
							}
							merged.Fields[i] = f
							break
						}
					}
					if !found {
						merged.Fields = append(merged.Fields, f)
					}
				}
				next = append(next, merged)
			}
		}
		variants = next
	}
	if len(variants) == 1 {
		return variants[0], nil
	}
	return &tsdef.Type{Kind: "union", Members: variants}, nil
}

func (g *generator) reserve(name string) error {
	if g.names[name] {
		return fmt.Errorf("duplicate Go declaration %s", name)
	}
	g.names[name] = true
	return nil
}
