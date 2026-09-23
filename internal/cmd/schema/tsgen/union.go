// Emission of tagged and raw unions.

package tsgen

import (
	"fmt"
	"maps"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/ironpark/go-acp/internal/cmd/schema/tsdef"
)

// taggedMember is one alternative of a discriminated object union.
type taggedMember struct {
	object *tsdef.Type // expanded object shape; nil for nested members
	nested string      // TypeScript name of a union payload combined with the tag
	value  string      // quoted literal for known variants; empty for the catch-all
}

// nestedTagged recognizes `Ref & { tag: "literal" }` where Ref is itself a
// union, which cannot be flattened into one struct without losing its variants.
func (g *generator) nestedTagged(m *tsdef.Type) (ref, tag, value string, ok bool) {
	if m.Kind != "intersection" || len(m.Members) != 2 {
		return "", "", "", false
	}
	for i, part := range m.Members {
		other := m.Members[1-i]
		if part.Kind != "ref" || other.Kind != "object" || len(other.Fields) != 1 || other.Element != nil {
			continue
		}
		f := other.Fields[0]
		if f.Optional || f.Type.Kind != "literal" {
			continue
		}
		if k, _ := literals(f.Type); k != "string" {
			continue
		}
		target := g.defs[part.Name]
		for target != nil && target.Kind == "ref" {
			target = g.defs[target.Name]
		}
		if target == nil || target.Kind != "union" {
			continue
		}
		return part.Name, f.Name, f.Type.Literal, true
	}
	return "", "", "", false
}

// tagged recognizes unions whose members are all objects sharing one required
// string discriminator: each known member fixes it to a distinct literal and at
// most one catch-all member leaves it as string alongside an index signature.
func (g *generator) tagged(t *tsdef.Type) (tag string, members []taggedMember, ok bool, err error) {
	if len(t.Members) < 2 {
		return "", nil, false, nil
	}
	type shape struct {
		object            *tsdef.Type
		nested, nestedTag string
		nestedValue       string
	}
	var shapes []shape
	candidates := map[string]bool{}
	for _, m := range t.Members {
		if ref, tagName, value, ok := g.nestedTagged(m); ok {
			shapes = append(shapes, shape{nested: ref, nestedTag: tagName, nestedValue: value})
			candidates[tagName] = true
			continue
		}
		expanded, err := g.expand(m, map[string]bool{})
		if err != nil {
			return "", nil, false, err
		}
		if expanded.Kind != "object" {
			return "", nil, false, nil
		}
		shapes = append(shapes, shape{object: expanded})
		for _, f := range requiredLiterals(expanded) {
			if k, _ := literals(f.Type); k == "string" {
				candidates[f.Name] = true
			}
		}
	}
	for _, tag := range slices.Sorted(maps.Keys(candidates)) {
		members = nil
		seen := map[string]bool{}
		custom := 0
		valid := true
		for _, sh := range shapes {
			if sh.nested != "" {
				if sh.nestedTag != tag || seen[sh.nestedValue] {
					valid = false
					break
				}
				seen[sh.nestedValue] = true
				members = append(members, taggedMember{nested: sh.nested, value: sh.nestedValue})
				continue
			}
			var field *tsdef.Field
			for i := range sh.object.Fields {
				if sh.object.Fields[i].Name == tag {
					field = &sh.object.Fields[i]
				}
			}
			if field == nil || field.Optional {
				valid = false
				break
			}
			switch {
			case field.Type.Kind == "literal":
				if seen[field.Type.Literal] {
					valid = false
				}
				seen[field.Type.Literal] = true
				members = append(members, taggedMember{object: sh.object, value: field.Type.Literal})
			case field.Type.Kind == "string" && sh.object.Element != nil:
				custom++
				members = append(members, taggedMember{object: sh.object})
			default:
				valid = false
			}
			if !valid {
				break
			}
		}
		if valid && custom <= 1 && len(members)-custom >= 1 {
			return tag, members, true, nil
		}
	}
	return "", nil, false, nil
}

func lowerFirst(s string) string {
	r := []rune(s)
	r[0] = unicode.ToLower(r[0])
	return string(r)
}

// taggedUnion emits a wrapper struct holding a sealed interface value. Each
// variant is a plain struct whose discriminator is implied by its Go type and
// written by its own JSON methods, so type switches replace As… probing.
func (g *generator) taggedUnion(name, tag string, members []taggedMember) error {
	iface := name + "Variant"
	for _, n := range []string{iface, "New" + name} {
		if err := g.reserve(n); err != nil {
			return err
		}
	}
	marker := lowerFirst(name) + "Variant"
	unmarshalFn := "unmarshal" + iface
	if err := g.reserve(unmarshalFn); err != nil {
		return err
	}
	g.unmarshalers = append(g.unmarshalers, "json.UnmarshalFromFunc("+unmarshalFn+")")
	var variantNames []string
	for _, m := range members {
		label := "Custom"
		if m.value != "" {
			value, err := strconv.Unquote(m.value)
			if err != nil {
				return err
			}
			label = Name(value)
		}
		vname := name + label
		if g.names[vname] {
			// The SDK often names the payload type after the variant (AuthMethodTerminal),
			// so the merged variant struct takes a suffix instead of failing.
			vname += "Variant"
		}
		variantNames = append(variantNames, vname)
	}
	g.write("// %s is a tagged union discriminated by the %q member. The zero value\n// encodes as null; use New%s or a type switch on Variant to work with it.\n", name, tag, name)
	g.write("type %s struct{ value %s }\n", name, iface)
	g.write("// %s is implemented by %s.\n", iface, strings.Join(variantNames, ", "))
	g.write("type %s interface { %s(); Tag() string }\n", iface, marker)
	g.write("// New%s wraps a variant; a nil variant yields the zero value.\n", name)
	g.write("func New%s(v %s) %s { return %s{value: v} }\n", name, iface, name, name)
	g.write("// Variant returns the wrapped variant, or nil for the zero value.\n")
	g.write("func (u %s) Variant() %s { return u.value }\n", name, iface)
	g.write("// As returns the variant if it is a T, or reports which variant is held.\n")
	g.write("func (u %s) As[T %s]() (T, error) { v, ok := u.value.(T); if !ok { var zero T; return zero, fmt.Errorf(\"%s: holds %%q, not %%T\", u.Tag(), zero) }; return v, nil }\n", name, iface, name)
	g.write("// Tag returns the %q discriminator, or \"\" for the zero value.\n", tag)
	g.write("func (u %s) Tag() string { if u.value == nil { return \"\" }; return u.value.Tag() }\n", name)
	g.write("// IsZero reports whether no variant is set, so omitzero omits the field.\n")
	g.write("func (u %s) IsZero() bool { return u.value == nil }\n", name)
	g.write("func (u %s) MarshalJSONTo(enc *jsontext.Encoder) error { if u.value == nil { return enc.WriteToken(jsontext.Null) }; return json.MarshalEncode(enc, u.value) }\n", name)
	g.write("func (u *%s) UnmarshalJSONFrom(dec *jsontext.Decoder) error { return %s(dec, &u.value) }\n", name, unmarshalFn)
	g.write("// %s decodes a %s by its %q member; it backs Unmarshalers.\n", unmarshalFn, iface, tag)
	g.write("func %s(dec *jsontext.Decoder, out *%s) error {\n", unmarshalFn, iface)
	g.write("raw, err := dec.ReadValue(); if err != nil { return err }\n")
	g.write("if raw.Kind() == 'n' { *out = nil; return nil }\n")
	g.write("if raw.Kind() != '{' { return fmt.Errorf(\"%s: expected object, got %%s\", raw.Kind()) }\n", name)
	g.write("var probe struct{ Tag string `json:%q` }\n", tag)
	g.write("if err := json.Unmarshal(raw, &probe, json.JoinOptions(dec.Options(), json.RejectUnknownMembers(false))); err != nil { return fmt.Errorf(\"%s: %%w\", err) }\n", name)
	g.write("switch probe.Tag {\n")
	customIndex := -1
	for i, m := range members {
		if m.value == "" {
			customIndex = i
			continue
		}
		g.write("case %s: var v %s; if err := json.Unmarshal(raw, &v, dec.Options()); err != nil { return err }; *out = v\n", m.value, variantNames[i])
	}
	if customIndex >= 0 {
		g.write("default: var v %s; if err := json.Unmarshal(raw, &v, dec.Options()); err != nil { return err }; *out = v\n", variantNames[customIndex])
	} else {
		g.write("default: return fmt.Errorf(\"%s: unknown %s %%q\", probe.Tag)\n", name, tag)
	}
	g.write("}\nreturn nil\n}\n")

	for i, m := range members {
		vname := variantNames[i]
		if err := g.reserve(vname); err != nil {
			return err
		}
		if m.value == "" {
			g.write("// %s carries a %s value with an unrecognized %q, keeping every member.\n", vname, name, tag)
			if err := g.structType(vname, m.object, ""); err != nil {
				return fmt.Errorf("%s: %w", vname, err)
			}
			g.write("func (%s) %s() {}\n", vname, marker)
			g.write("func (v %s) Tag() string { return v.%s }\n", vname, Name(tag))
			continue
		}
		if m.nested != "" {
			payload := Name(m.nested)
			g.write("// %s is the %s variant with %s %s; its payload is the %s union\n// whose members are written alongside the discriminator.\n", vname, name, tag, m.value, payload)
			g.write("type %s struct { Value %s }\n", vname, payload)
			g.write("func (%s) %s() {}\n", vname, marker)
			g.write("// Tag returns %s.\n", m.value)
			g.write("func (%s) Tag() string { return %s }\n", vname, m.value)
			g.write("func (v %s) MarshalJSONTo(enc *jsontext.Encoder) error { return union.SpliceTag(enc, %q, %s, v.Value) }\n", vname, tag, m.value)
			g.write("func (v *%s) UnmarshalJSONFrom(dec *jsontext.Decoder) error { return union.UnspliceTag(dec, %q, %s, &v.Value) }\n", vname, tag, m.value)
			continue
		}
		g.write("// %s is the %s variant with %s %s.\n", vname, name, tag, m.value)
		if err := g.structType(vname, m.object, tag); err != nil {
			return fmt.Errorf("%s: %w", vname, err)
		}
		fields := lowerFirst(vname) + "Fields"
		wire := lowerFirst(vname) + "Wire"
		g.write("func (%s) %s() {}\n", vname, marker)
		g.write("// Tag returns %s.\n", m.value)
		g.write("func (%s) Tag() string { return %s }\n", vname, m.value)
		g.write("type %s %s\n", fields, vname)
		g.write("type %s struct { Tag string `json:%q`; %s `json:\",inline\"` }\n", wire, tag, fields)
		g.write("func (v %s) MarshalJSONTo(enc *jsontext.Encoder) error { return json.MarshalEncode(enc, %s{%s, %s(v)}) }\n", vname, wire, m.value, fields)
		g.write("func (v *%s) UnmarshalJSONFrom(dec *jsontext.Decoder) error {\n", vname)
		g.write("var w %s; if err := json.UnmarshalDecode(dec, &w); err != nil { return err }\n", wire)
		g.write("if w.Tag != %s { return fmt.Errorf(\"%s: expected %s %s, got %%q\", w.Tag) }\n", m.value, vname, tag, strings.ReplaceAll(m.value, `"`, `\"`))
		g.write("*v = %s(w.%s); return nil\n}\n", vname, fields)
	}
	return nil
}

// memberLabel names a union alternative for generated identifiers.
func (g *generator) memberLabel(m, expanded *tsdef.Type, siblings []*tsdef.Type) (string, error) {
	switch m.Kind {
	case "ref":
		return Name(m.Name), nil
	case "literal":
		if k, _ := literals(m); k == "string" {
			value, err := strconv.Unquote(m.Literal)
			if err != nil {
				return "", err
			}
			return Name(value), nil
		}
		return Name(m.Literal), nil
	case "null":
		return "Null", nil
	case "string":
		return "String", nil
	case "number":
		return "Number", nil
	case "boolean":
		return "Bool", nil
	case "unknown", "any":
		return "Unknown", nil
	case "intersection":
		var label strings.Builder
		for _, part := range m.Members {
			if part.Kind == "object" {
				label.WriteString(literalLabel(part))
			}
		}
		if label.String() != "" {
			return label.String(), nil
		}
	case "array":
		element, err := g.memberLabel(m.Element, m.Element, nil)
		if err != nil {
			return "", err
		}
		return element + "List", nil
	}
	if expanded.Kind == "object" {
		label := literalLabel(expanded)
		if label != "" {
			return label, nil
		}
		// Fall back to required members that no sibling declares.
		for _, f := range expanded.Fields {
			if f.Optional {
				continue
			}
			unique := true
			for _, s := range siblings {
				if s == m {
					continue
				}
				other, err := g.expand(s, map[string]bool{})
				if err != nil || other.Kind != "object" {
					continue
				}
				for _, sf := range other.Fields {
					if sf.Name == f.Name {
						unique = false
					}
				}
			}
			if unique {
				label += Name(f.Name)
			}
		}
		if label != "" {
			return label, nil
		}
		return "Object", nil
	}
	return "Variant", nil
}

func (g *generator) union(name string, t *tsdef.Type) error {
	tag, members, ok, err := g.tagged(t)
	if err != nil {
		return err
	}
	if ok {
		return g.taggedUnion(name, tag, members)
	}
	constraint := name + "Alternative"
	table := lowerFirst(name) + "Alternatives"
	for _, n := range []string{"Parse" + name, "New" + name, constraint, table} {
		if err := g.reserve(n); err != nil {
			return err
		}
	}
	// One rule per alternative, grouped by the canonical Go type so aliases of
	// the same type become one type-set term.
	var terms []string
	rules := map[string][]string{}
	used := map[string]bool{}
	for i, m := range t.Members {
		expanded, err := g.expand(m, map[string]bool{})
		if err != nil {
			return err
		}
		label, err := g.memberLabel(m, expanded, t.Members)
		if err != nil {
			return err
		}
		if used[label] {
			label += strconv.Itoa(i + 1)
		}
		used[label] = true
		expr, err := g.expr(m, name+label)
		if err != nil {
			return err
		}
		key := g.canonical(m, expr)
		if _, ok := rules[key]; !ok {
			terms = append(terms, key)
		}
		if rule := g.altRule(expanded); !slices.Contains(rules[key], rule) {
			rules[key] = append(rules[key], rule)
		}
	}
	g.write("// %s preserves the complete JSON payload, including future variants.\n", name)
	g.write("// Use As to read one alternative and New%s to build one.\n", name)
	g.write("type %s struct { raw jsontext.Value }\n", name)
	g.write("// %s is the set of Go types a %s can hold.\n", constraint, name)
	g.write("type %s interface { %s }\n", constraint, strings.Join(terms, " | "))
	g.write("var %s = union.Table(\n", table)
	for _, key := range terms {
		g.write("union.Alt[%s](%s),\n", key, strings.Join(rules[key], ", "))
	}
	g.write(")\n")
	g.write("// New%s encodes value as a %s, adding any literal members the alternative\n// requires and rejecting values that are not that alternative.\n", name, name)
	g.write("func New%s[T %s](value T) (%s, error) { raw, err := union.New(%q, %s, value); return %s{raw: raw}, err }\n", name, constraint, name, name, table, name)
	g.write("// As decodes the payload as the alternative T, or reports why it is not one.\n")
	g.write("func (v %s) As[T %s]() (T, error) { return union.As[T](%q, %s, v.raw) }\n", name, constraint, name, table)
	g.write("func (v %s) MarshalJSON() ([]byte,error) {if len(v.raw)==0{return []byte(\"null\"),nil};return v.raw.Clone(),nil}\n", name)
	g.write("func (v *%s) UnmarshalJSON(b []byte) error {if !jsontext.Value(b).IsValid(){return fmt.Errorf(\"invalid %s JSON\")};v.raw=jsontext.Value(b).Clone();return nil}\n", name, name)
	g.write("func (v %s) RawJSON() jsontext.Value {return v.raw.Clone()}\n", name)
	g.write("// IsZero reports whether no payload is stored, so omitzero omits the field.\n")
	g.write("func (v %s) IsZero() bool {return len(v.raw)==0}\n", name)
	g.write("func (v %s) MarshalJSONTo(enc *jsontext.Encoder) error {if len(v.raw)==0{return enc.WriteValue(jsontext.Value(\"null\"))};return enc.WriteValue(v.raw)}\n", name)
	g.write("func (v *%s) UnmarshalJSONFrom(dec *jsontext.Decoder) error {raw,err:=dec.ReadValue();if err!=nil{return err};v.raw=raw.Clone();return nil}\n", name)
	g.write("func Parse%s(b []byte) (%s,error) {var v %s;err:=json.Unmarshal(b,&v);return v,err}\n", name, name, name)
	return nil
}

// altRule renders the union.Rule literal that recognizes one union alternative.
func (g *generator) altRule(expanded *tsdef.Type) string {
	var parts []string
	if expanded.Kind == "null" {
		parts = append(parts, "Null: true")
	}
	if !g.acceptsNull(expanded, map[string]bool{}) {
		parts = append(parts, "NonNull: true")
	}
	if expanded.Kind == "literal" {
		parts = append(parts, fmt.Sprintf("Literal: jsontext.Value(%q)", expanded.Literal))
	}
	if expanded.Kind == "object" {
		var required, notNull, tags []string
		for _, f := range expanded.Fields {
			if !f.Optional {
				required = append(required, strconv.Quote(f.Name))
			}
			if !g.acceptsNull(f.Type, map[string]bool{}) {
				notNull = append(notNull, strconv.Quote(f.Name))
			}
		}
		for _, f := range requiredLiterals(expanded) {
			tags = append(tags, fmt.Sprintf("{Name: %q, Value: jsontext.Value(%q)}", f.Name, f.Type.Literal))
		}
		if len(required) > 0 {
			parts = append(parts, "Required: []string{"+strings.Join(required, ", ")+"}")
		}
		if len(notNull) > 0 {
			parts = append(parts, "NotNull: []string{"+strings.Join(notNull, ", ")+"}")
		}
		if len(tags) > 0 {
			sort.Strings(tags)
			parts = append(parts, "Tags: []union.Tag{"+strings.Join(tags, ", ")+"}")
		}
	}
	return "union.Rule{" + strings.Join(parts, ", ") + "}"
}
