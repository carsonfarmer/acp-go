package tsgen

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/ironpark/go-acp/internal/cmd/schema/tsdef"
)

// ZodRuntime is the import path of the shared rule evaluator.
const ZodRuntime = "github.com/ironpark/go-acp/schema/zod"

// UnionRuntime is the import path of the shared raw-union alternative matcher.
const UnionRuntime = "github.com/ironpark/go-acp/schema/union"

// zodKinds maps parsed builder names to zod.Kind constant names. Unknown
// builders fail generation instead of producing an unsupported rule.
var zodKinds = map[string]string{
	"ref": "Ref", "optional": "Optional", "nullish": "Nullish", "nullable": "Nullable",
	"default": "Default", "catch": "Catch", "requiredCatch": "RequiredCatch",
	"unknown": "Unknown", "any": "Any", "never": "Never",
	"union": "Union", "intersection": "Intersection",
	"excludeTags": "ExcludeTags", "preserve": "Preserve",
	"min": "Min", "max": "Max", "gte": "Gte", "lte": "Lte", "regex": "Regex", "int": "Int",
	"null": "Null", "string": "String", "boolean": "Boolean", "number": "Number", "literal": "Literal",
	"url": "URL", "datetime": "DateTime",
	"array": "Array", "skipArray": "SkipArray", "object": "Object", "record": "Record",
}

// zod writes the rule registry and per-type Decode/Validate functions.
func (g *generator) zod(schema *tsdef.Schema, pkg string) error {
	if len(schema.Validators) == 0 {
		return nil
	}
	keys := make([]string, 0, len(schema.Validators))
	for k := range schema.Validators {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	g.write("// zodSchemas holds the SDK Zod rules; one rule tree per schema name.\nvar zodSchemas = zod.Registry{\n")
	for _, k := range keys {
		rule, err := zodLiteral(schema.Validators[k])
		if err != nil {
			return fmt.Errorf("%s: %w", k, err)
		}
		g.write("%s: %s,\n", strconv.Quote(k), rule)
	}
	g.write("}\n\n")

	defs := append([]tsdef.Definition(nil), schema.Types...)
	sort.Slice(defs, func(i, j int) bool { return defs[i].Name < defs[j].Name })
	var types, unmarshalers []string
	for _, d := range defs {
		key := "z" + d.Name
		if schema.Validators[key] == nil {
			return fmt.Errorf("missing Zod schema for %s", d.Name)
		}
		name := Name(d.Name)
		if g.aliases[name] {
			// Aliases share their reflect.Type with the underlying type, so a
			// rule registered for them would apply to every value of that type.
			continue
		}
		types = append(types, fmt.Sprintf("reflect.TypeFor[%s](): %q", name, key))
		unmarshalers = append(unmarshalers, fmt.Sprintf("zod.Unmarshaler[%s](zodSchemas, %q)", name, key))
	}
	g.write("// zodTypes maps generated Go types to their Zod rule. Type aliases are not\n// listed; they share a reflect.Type with their underlying type.\nvar zodTypes = map[reflect.Type]string{\n%s,\n}\n\n", strings.Join(types, ",\n"))
	g.write("// Validated is a json.Options value that applies the SDK Zod validation, default and\n// recovery rules to every generated type encountered while unmarshaling:\n//\n//\tjson.Unmarshal(data, &v, schema.Validated)\n//\n// Type aliases are decoded as their underlying type.\nvar Validated = json.WithUnmarshalers(json.JoinUnmarshalers(\n%s,\n))\n\n", strings.Join(unmarshalers, ",\n"))
	g.out.WriteString(`// zodRule returns the Zod rule registered for T.
func zodRule[T any]() (string, error) {
	name, ok := zodTypes[reflect.TypeFor[T]()]
	if !ok {
		var zero T
		return "", fmt.Errorf("no Zod rule for %T", zero)
	}
	return name, nil
}

// Decode applies the supported SDK Zod validation, defaults and recovery rules
// for T, then decodes the normalized value. T must be a generated non-alias type.
func Decode[T any](raw []byte) (T, error) {
	name, err := zodRule[T]()
	if err != nil {
		var zero T
		return zero, err
	}
	return zod.Decode[T](zodSchemas, name, raw)
}

// Validate reports whether the supported SDK Zod parser accepts raw as a T.
// Recovery and defaults are applied; use Decode to obtain the normalized value.
func Validate[T any](raw []byte) error {
	name, err := zodRule[T]()
	if err != nil {
		return err
	}
	_, err = zodSchemas.Normalize(name, raw)
	return err
}
`)
	return nil
}

// zodLiteral renders one rule tree as a single-line Go composite literal so
// gofmt keeps each schema on its own line.
func zodLiteral(z *tsdef.Zod) (string, error) {
	if z == nil {
		return "nil", nil
	}
	kind, ok := zodKinds[z.Kind]
	if !ok {
		return "", fmt.Errorf("unsupported Zod rule %q", z.Kind)
	}
	var parts []string
	parts = append(parts, "Kind: zod.Kind"+kind)
	if z.Ref != "" {
		parts = append(parts, "Ref: "+strconv.Quote(z.Ref))
	}
	if z.Inner != nil {
		inner, err := zodLiteral(z.Inner)
		if err != nil {
			return "", err
		}
		parts = append(parts, "Inner: "+inner)
	}
	if len(z.Members) > 0 {
		var members []string
		for _, m := range z.Members {
			s, err := zodLiteral(m)
			if err != nil {
				return "", err
			}
			members = append(members, s)
		}
		parts = append(parts, "Members: []*zod.Rule{"+strings.Join(members, ", ")+"}")
	}
	if len(z.Fields) > 0 {
		var fields []string
		for _, f := range z.Fields {
			s, err := zodLiteral(f.Schema)
			if err != nil {
				return "", err
			}
			fields = append(fields, fmt.Sprintf("{Name: %s, Schema: %s}", strconv.Quote(f.Name), s))
		}
		parts = append(parts, "Fields: []zod.Field{"+strings.Join(fields, ", ")+"}")
	}
	if z.Key != nil {
		key, err := zodLiteral(z.Key)
		if err != nil {
			return "", err
		}
		parts = append(parts, "Key: "+key)
	}
	if z.Value != nil {
		parts = append(parts, "Value: jsontext.Value("+goString(string(z.Value))+")")
	}
	if z.Tag != "" {
		parts = append(parts, "Tag: "+strconv.Quote(z.Tag))
	}
	if len(z.Tags) > 0 {
		var tags []string
		for _, t := range z.Tags {
			tags = append(tags, strconv.Quote(t))
		}
		parts = append(parts, "Tags: []string{"+strings.Join(tags, ", ")+"}")
	}
	if z.Pattern != "" {
		parts = append(parts, "Regexp: regexp.MustCompile("+goString(z.Pattern)+")")
	}
	if z.Offset {
		parts = append(parts, "Offset: true")
	}
	return "&zod.Rule{" + strings.Join(parts, ", ") + "}", nil
}

// goString prefers raw string literals for readability when the value allows it.
func goString(s string) string {
	if !strings.ContainsAny(s, "`\n") {
		return "`" + s + "`"
	}
	return strconv.Quote(s)
}
