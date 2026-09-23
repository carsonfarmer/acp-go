// Emission of object types as Go structs.

package tsgen

import (
	"fmt"
	"strings"

	"github.com/ironpark/go-acp/internal/cmd/schema/tsdef"
)

func (g *generator) object(name string, t *tsdef.Type) error {
	return g.structType(name, t, "")
}

// structType emits a struct declaration, omitting the JSON member named skip
// (used for union discriminators that are implied by the Go type).
func (g *generator) structType(name string, t *tsdef.Type, skip string) error {
	if len(t.Fields) == 0 {
		expr, err := g.expr(t, name+"Value")
		if err != nil {
			return err
		}
		g.alias(name, expr)
		return nil
	}
	g.write("type %s struct {\n", name)
	names := map[string]bool{}
	for _, f := range t.Fields {
		if f.Name == skip {
			continue
		}
		field := Name(f.Name)
		if field == "AdditionalProperties" && t.Element != nil {
			return fmt.Errorf("reserved field %s", field)
		}
		if field == "Tag" && skip != "" {
			return fmt.Errorf("reserved field %s in union variant", field)
		}
		if names[field] {
			return fmt.Errorf("field collision %s", field)
		}
		names[field] = true
		expr, err := g.expr(f.Type, name+field)
		if err != nil {
			return err
		}
		if f.Optional {
			// omitzero already distinguishes nil collections from empty ones, so
			// optional slices and maps do not need a pointer. Optional null and
			// absence share the nil representation.
			if collection(strings.TrimPrefix(expr, "*")) || g.isUnion(f.Type) {
				expr = strings.TrimPrefix(expr, "*")
			} else if !strings.HasPrefix(expr, "*") && expr != "jsontext.Value" {
				expr = "*" + expr
			}
		}
		tag := f.Name
		if f.Optional {
			tag += ",omitzero"
		}
		g.write("%s%s %s `json:%q`\n", comment(f.Comment), field, expr, tag)
	}
	if t.Element != nil {
		element, err := g.expr(t.Element, name+"AdditionalProperty")
		if err != nil {
			return err
		}
		g.write("AdditionalProperties map[string]%s `json:\",embed\"`\n", element)
	}
	g.write("}\n")
	var fixed []string
	for _, f := range t.Fields {
		if f.Name != skip && !f.Optional && f.Type.Kind == "literal" {
			fixed = append(fixed, fmt.Sprintf("v.%s = %s; ", Name(f.Name), f.Type.Literal))
		}
	}
	if len(fixed) > 0 {
		// The literal members are part of the wire shape, so a zero value still
		// encodes as this type and raw-union constructors need no splicing.
		set := strings.Join(fixed, "")
		g.write("// MarshalJSON encodes v with its literal members fixed.\n")
		g.write("func (v %s) MarshalJSON() ([]byte, error) { %stype plain %s; return json.Marshal(plain(v)) }\n", name, set, name)
		g.write("func (v %s) MarshalJSONTo(enc *jsontext.Encoder) error { %stype plain %s; return json.MarshalEncode(enc, plain(v)) }\n", name, set, name)
	}
	return nil
}
