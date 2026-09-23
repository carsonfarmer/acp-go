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

func (g *generator) definition(d tsdef.Definition) error {
	t := d.Type
	file := fileTypes
	if envelope(d.Name) {
		file = fileEnvelope
	}
	if kind, ok := literals(t); ok {
		members := t.Members
		if t.Kind == "literal" {
			members = []*tsdef.Type{t}
		}
		g.use(fileEnums)
		g.write("\n%s", comment(d.Comment))
		return g.enum(d.Name, kind, members, false)
	}
	if base, members, ok := openEnum(t); ok {
		kind, _ := literals(members[0])
		if base.Kind == "number" && base.Number != "" {
			kind = base.Number
		}
		g.use(fileEnums)
		g.write("\n%s", comment(d.Comment))
		return g.enum(d.Name, kind, members, true)
	}
	if t.Kind == "intersection" {
		expanded, err := g.expand(t, map[string]bool{})
		if err != nil {
			return err
		}
		t = expanded
	}
	switch t.Kind {
	case "object":
		g.use(file)
		g.write("\n%s", comment(d.Comment))
		return g.object(d.Name, t)
	case "union":
		nonnull, isNull := nullable(t)
		if isNull && nonnull.Kind != "union" {
			expr, err := g.expr(t, d.Name+"Value")
			if err != nil {
				return err
			}
			g.use(file)
			g.write("\n%s", comment(d.Comment))
			g.alias(d.Name, expr)
			return nil
		}
		if file == fileTypes {
			file = fileUnions
		}
		g.use(file)
		g.write("\n%s", comment(d.Comment))
		return g.union(d.Name, t)
	default:
		expr, err := g.expr(t, d.Name+"Value")
		if err != nil {
			return err
		}
		switch t.Kind {
		case "string", "number", "boolean":
			// Distinct identifier types (SessionID vs ToolCallID) cannot be mixed
			// up, and they can carry their own Zod unmarshaler.
			g.use(fileEnums)
			g.write("\n%s", comment(d.Comment))
			g.write("type %s %s\n", d.Name, expr)
			return nil
		}
		g.use(file)
		g.write("\n%s", comment(d.Comment))
		g.alias(d.Name, expr)
		return nil
	}
}

// alias emits "type name = expr" and records it so Zod rules skip the alias:
// it shares a reflect.Type with expr.
func (g *generator) alias(name, expr string) {
	g.aliases[name] = true
	g.write("type %s = %s\n", name, expr)
}

// enum emits a named scalar type with one constant per literal. Open enums
// also accept values outside the listed constants.
func (g *generator) enum(typeName, kind string, members []*tsdef.Type, open bool) error {
	if open {
		g.write("// %s also accepts values outside the listed constants; use Known to check.\n", typeName)
	}
	g.write("type %s %s\n", typeName, kind)
	if open {
		if err := g.reserve(typeName + "Values"); err != nil {
			return err
		}
	}
	used := map[string]bool{}
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
		if override, ok := literalNames[typeName][value]; ok {
			label = override
		}
		name := typeName + Name(label)
		if used[name] || g.names[name] {
			return fmt.Errorf("enum constant collision %s", name)
		}
		used[name] = true
		g.names[name] = true
		names = append(names, name)
		g.write("%s %s = %s\n", name, typeName, value)
	}
	g.write(")\n")
	if open {
		g.write("// %sValues lists the constants defined by the protocol.\n", typeName)
		g.write("var %sValues = []%s{%s}\n", typeName, typeName, strings.Join(names, ", "))
		g.write("// Known reports whether v is one of the protocol-defined constants.\n")
		g.write("func (v %s) Known() bool { return slices.Contains(%sValues, v) }\n", typeName, typeName)
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
