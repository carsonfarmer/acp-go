package tsdef

import (
	"fmt"
	"strconv"
	"strings"

	ts "github.com/tree-sitter/go-tree-sitter"
	grammar "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

// ApplyNumericHints reads only Zod's numeric builders and bounds. It does not
// attempt to reproduce Zod validation, defaults or recovery behavior.
func ApplyNumericHints(schema *Schema, filename string, source []byte) error {
	p := ts.NewParser()
	defer p.Close()
	if err := p.SetLanguage(ts.NewLanguage(grammar.LanguageTypescript())); err != nil {
		return err
	}
	tree := p.Parse(source, nil)
	if tree == nil {
		return fmt.Errorf("%s: parser returned no tree", filename)
	}
	defer tree.Close()
	r := reader{filename, source}
	if tree.RootNode().HasError() {
		return r.fail(firstError(tree.RootNode()), "invalid TypeScript syntax")
	}
	defs := map[string]*Type{}
	for _, d := range schema.Types {
		defs["z"+d.Name] = d.Type
	}
	for _, n := range children(tree.RootNode()) {
		if n.Kind() != "export_statement" {
			continue
		}
		decl := n.ChildByFieldName("declaration")
		if decl == nil || decl.Kind() != "lexical_declaration" {
			continue
		}
		for _, v := range children(decl) {
			if v.Kind() != "variable_declarator" {
				continue
			}
			name := v.ChildByFieldName("name").Utf8Text(source)
			if typ := defs[name]; typ != nil {
				r.numericHints(typ, v.ChildByFieldName("value"))
			}
		}
	}
	return nil
}
func (r reader) numericHints(t *Type, n *ts.Node) {
	if n == nil {
		return
	}
	if t.Kind == KindUnion || t.Kind == KindIntersection {
		// z.union([...]) members align positionally with the TypeScript union.
		if elems := r.builderArray(n, "union"); elems != nil && len(elems) == len(t.Members) && t.Kind == KindUnion {
			for i, m := range t.Members {
				r.numericHints(m, elems[i])
			}
			return
		}
		for _, m := range t.Members {
			r.numericHints(m, n)
		}
		return
	}
	if t.Kind == KindNumber {
		integer, min, max := false, float64(-1), float64(0)
		var visit func(*ts.Node)
		visit = func(n *ts.Node) {
			if n.Kind() != "call_expression" {
				return
			}
			fn := n.ChildByFieldName("function")
			args := n.ChildByFieldName("arguments")
			if fn.Kind() == "member_expression" {
				method := fn.ChildByFieldName("property").Utf8Text(r.source)
				if method == "int" {
					integer = true
				}
				if (method == "gte" || method == "min" || method == "lte" || method == "max") && args.NamedChildCount() > 0 {
					value, err := strconv.ParseFloat(args.NamedChild(0).Utf8Text(r.source), 64)
					if err == nil {
						if method == "gte" || method == "min" {
							min = value
						} else {
							max = value
						}
					}
				}
				visit(fn.ChildByFieldName("object"))
			} else if fn.Kind() == "identifier" && strings.HasSuffix(fn.Utf8Text(r.source), "OnError") && args.NamedChildCount() > 0 {
				visit(args.NamedChild(0))
			}
		}
		visit(n)
		if integer {
			t.Number = "int64"
			if min >= 0 {
				t.Number = "uint64"
				if max > 0 && max <= 65535 {
					t.Number = "uint16"
				} else if max > 0 && max <= 4294967295 {
					t.Number = "uint32"
				}
			}
		}
		return
	}
	// Find object/array builders through wrappers and intersections. Do not cross
	// an object's properties: recurse with the matching property type instead.
	var walk func(*ts.Node)
	walk = func(n *ts.Node) {
		if n == nil {
			return
		}
		if n.Kind() == "call_expression" {
			fn := n.ChildByFieldName("function")
			args := n.ChildByFieldName("arguments")
			if fn.Kind() == "member_expression" && fn.ChildByFieldName("object").Utf8Text(r.source) == "z" {
				method := fn.ChildByFieldName("property").Utf8Text(r.source)
				if method == "object" && args.NamedChildCount() > 0 {
					if t.Kind != KindObject {
						return
					}
					for _, pair := range children(args.NamedChild(0)) {
						if pair.Kind() != "pair" {
							continue
						}
						key := strings.Trim(pair.ChildByFieldName("key").Utf8Text(r.source), "\"")
						for _, f := range t.Fields {
							if f.Name == key {
								r.numericHints(f.Type, pair.ChildByFieldName("value"))
							}
						}
					}
					return
				}
				if method == "array" && t.Kind == KindArray && args.NamedChildCount() > 0 {
					r.numericHints(t.Element, args.NamedChild(0))
					return
				}
			}
		}
		for _, c := range children(n) {
			walk(c)
		}
	}
	walk(n)
}

// builderArray returns the elements of z.<method>([...]) when n is that call.
func (r reader) builderArray(n *ts.Node, method string) []*ts.Node {
	if n == nil || n.Kind() != "call_expression" {
		return nil
	}
	fn := n.ChildByFieldName("function")
	args := n.ChildByFieldName("arguments")
	if fn.Kind() != "member_expression" || fn.ChildByFieldName("object").Utf8Text(r.source) != "z" || fn.ChildByFieldName("property").Utf8Text(r.source) != method || args.NamedChildCount() == 0 {
		return nil
	}
	arr := args.NamedChild(0)
	if arr.Kind() != "array" {
		return nil
	}
	var out []*ts.Node
	for _, c := range children(arr) {
		if c.IsNamed() && c.Kind() != "comment" {
			out = append(out, c)
		}
	}
	return out
}
