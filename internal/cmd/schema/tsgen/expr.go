// Type queries over the parsed schema and rendering of Go type expressions.

package tsgen

import (
	"fmt"
	"strings"

	"github.com/ironpark/acp-go/internal/cmd/schema/tsdef"
)

func nullable(t *tsdef.Type) (*tsdef.Type, bool) {
	if t.Kind != tsdef.KindUnion {
		return t, t.Kind == tsdef.KindNull
	}
	var members []*tsdef.Type
	null := false
	for _, m := range t.Members {
		if m.Kind == tsdef.KindNull {
			null = true
		} else {
			members = append(members, m)
		}
	}
	if len(members) == 1 {
		return members[0], null
	}
	return &tsdef.Type{Kind: tsdef.KindUnion, Members: members}, null
}

func literals(t *tsdef.Type) (string, bool) {
	if t.Kind == tsdef.KindLiteral {
		if strings.HasPrefix(t.Literal, "\"") || strings.HasPrefix(t.Literal, "'") {
			return "string", true
		}
		if t.Literal == "true" || t.Literal == "false" {
			return "bool", true
		}
		return "float64", true
	}
	if t.Kind != tsdef.KindUnion || len(t.Members) == 0 {
		return "", false
	}
	kind := ""
	for _, m := range t.Members {
		k, ok := literals(m)
		if !ok || (kind != "" && k != kind) {
			return "", false
		}
		kind = k
	}
	return kind, true
}

// openEnum reports literal unions that also admit any value of the same
// primitive type, such as "a" | "b" | string. The primitive member is returned
// so its numeric hint can be used for the Go base type.
func openEnum(t *tsdef.Type) (base *tsdef.Type, members []*tsdef.Type, ok bool) {
	if t.Kind != tsdef.KindUnion {
		return nil, nil, false
	}
	kind := ""
	for _, m := range t.Members {
		switch m.Kind {
		case tsdef.KindLiteral:
			k, _ := literals(m)
			if kind != "" && k != kind {
				return nil, nil, false
			}
			kind = k
			members = append(members, m)
		case tsdef.KindString, tsdef.KindNumber, tsdef.KindBoolean:
			if base != nil {
				return nil, nil, false
			}
			base = m
		default:
			return nil, nil, false
		}
	}
	if base == nil || len(members) == 0 {
		return nil, nil, false
	}
	baseKind := map[tsdef.Kind]string{tsdef.KindString: "string", tsdef.KindNumber: "float64", tsdef.KindBoolean: "bool"}[base.Kind]
	if baseKind != kind {
		return nil, nil, false
	}
	return base, members, true
}

// literalNames names the constants of literals whose value says nothing
// about their meaning, keyed by Go type name and then by the literal as
// written in the schema.
var literalNames = map[string]map[string]string{
	"ErrorCode": {
		"-32700": "ParseError",
		"-32600": "InvalidRequest",
		"-32601": "MethodNotFound",
		"-32602": "InvalidParams",
		"-32603": "InternalError",
		"-32800": "RequestCancelled",
		"-32000": "AuthenticationRequired",
		"-32002": "ResourceNotFound",
	},
}

func (g *generator) expr(t *tsdef.Type, hint string) (string, error) {
	out, _, err := g.render(t, hint, nil)
	return out, err
}

// canonical resolves the Go type behind a union member so that two aliases of
// one type (ExtResponse and MessageMCPResponse are both jsontext.Value) share
// a single type-set term and rule group. It follows references through the
// schema rather than emitted aliases, so declaration order does not matter,
// and falls back to expr when the member involves an inline declaration.
func (g *generator) canonical(m *tsdef.Type, expr string) string {
	if out, ok, err := g.render(m, "", map[string]bool{}); ok && err == nil {
		return out
	}
	return expr
}

// render renders t as a Go type expression. With a nil seen set it declares
// inline objects and unions under hint, and references stop at the definition
// name. With a seen set it declares nothing: references that definition()
// emits as aliases are followed, and it reports false where t needs an inline
// declaration.
func (g *generator) render(t *tsdef.Type, hint string, seen map[string]bool) (string, bool, error) {
	static := seen != nil
	t, isNull := nullable(t)
	var out string
	switch t.Kind {
	case tsdef.KindRef:
		d, ok := g.defs[t.Name]
		if !ok {
			return "", false, fmt.Errorf("unresolved type %s", t.Name)
		}
		out = Name(t.Name)
		if !static {
			break
		}
		if f, target, err := g.form(d); err == nil && f == formAlias {
			if seen[t.Name] {
				return "", false, nil
			}
			seen[t.Name] = true
			if out, ok, err = g.render(target, "", seen); !ok || err != nil {
				return "", false, err
			}
		}
	case tsdef.KindString:
		out = "string"
	case tsdef.KindNumber:
		out = "float64"
		if t.Number != "" {
			out = t.Number
		}
	case tsdef.KindBoolean:
		out = "bool"
	case tsdef.KindUnknown, tsdef.KindAny, tsdef.KindNull, tsdef.KindNever:
		out = "jsontext.Value"
	case tsdef.KindLiteral:
		out, _ = literals(t)
	case tsdef.KindArray:
		e, ok, err := g.render(t.Element, hint+"Item", seen)
		if !ok || err != nil {
			return "", false, err
		}
		out = "[]" + e
	case tsdef.KindObject:
		if len(t.Fields) > 0 {
			if static {
				return "", false, nil
			}
			out = g.add(hint, t)
			break
		}
		out = "map[string]jsontext.Value"
		if t.Element != nil {
			e, ok, err := g.render(t.Element, hint+"Value", seen)
			if !ok || err != nil {
				return "", false, err
			}
			out = "map[string]" + e
		}
	case tsdef.KindUnion, tsdef.KindIntersection:
		if static {
			return "", false, nil
		}
		out = g.add(hint, t)
	default:
		return "", false, fmt.Errorf("unsupported IR type %s", t.Kind)
	}
	if isNull && out != "jsontext.Value" {
		out = "*" + out
	}
	return out, true, nil
}

// isUnion reports whether t (through references and an optional null) is
// generated as a union wrapper, which implements IsZero for omitzero.
func (g *generator) isUnion(t *tsdef.Type) bool {
	seen := map[string]bool{}
	for {
		t, _ = nullable(t)
		if t.Kind != tsdef.KindRef || seen[t.Name] {
			break
		}
		seen[t.Name] = true
		if t = g.defs[t.Name]; t == nil {
			return false
		}
	}
	// Unions become payload wrappers unless they are emitted as named enums.
	if t.Kind != tsdef.KindUnion {
		return false
	}
	_, lit := literals(t)
	_, _, open := openEnum(t)
	return !lit && !open
}

// requiredLiterals returns the required literal members of an object: the
// members implied by its Go type, which union rules match on as tags.
func requiredLiterals(t *tsdef.Type) []tsdef.Field {
	var out []tsdef.Field
	for _, f := range t.Fields {
		if !f.Optional && f.Type.Kind == tsdef.KindLiteral {
			out = append(out, f)
		}
	}
	return out
}

// literalLabel concatenates the required literal values of an object, used to
// name union members.
func literalLabel(t *tsdef.Type) string {
	var label strings.Builder
	for _, f := range requiredLiterals(t) {
		label.WriteString(Name(strings.Trim(f.Type.Literal, "\"")))
	}
	return label.String()
}

func collection(expr string) bool {
	return strings.HasPrefix(expr, "[]") || strings.HasPrefix(expr, "map[")
}

// acceptsNull follows references without expanding recursive object fields.
func (g *generator) acceptsNull(t *tsdef.Type, seen map[string]bool) bool {
	switch t.Kind {
	case tsdef.KindNull, tsdef.KindUnknown, tsdef.KindAny:
		return true
	case tsdef.KindRef:
		if seen[t.Name] {
			return false
		}
		seen[t.Name] = true
		defer delete(seen, t.Name)
		target := g.defs[t.Name]
		return target != nil && g.acceptsNull(target, seen)
	case tsdef.KindUnion:
		for _, m := range t.Members {
			if g.acceptsNull(m, seen) {
				return true
			}
		}
	case tsdef.KindIntersection:
		for _, m := range t.Members {
			if !g.acceptsNull(m, seen) {
				return false
			}
		}
		return true
	}
	return false
}
