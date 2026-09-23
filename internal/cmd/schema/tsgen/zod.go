package tsgen

import (
	"encoding/json/jsontext"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/ironpark/acp-go/internal/cmd/schema/tsdef"
)

// ZodRuntime is the import path of the shared rule evaluator.
const ZodRuntime = "github.com/ironpark/acp-go/schema/internal/zod"

// UnionRuntime is the import path of the shared raw-union alternative matcher.
const UnionRuntime = "github.com/ironpark/acp-go/schema/internal/union"

// MetaRuntime is the import path of the shared _meta map type.
const MetaRuntime = "github.com/ironpark/acp-go/schema/meta"

// zodKinds maps parsed builder names to zod.Kind constant names. Unknown
// builders fail generation instead of producing an unsupported rule.
var zodKinds = map[string]string{
	"ref": "Ref", "optional": "Optional", "nullish": "Nullish", "nullable": "Nullable",
	"default": "Default", "catch": "Catch", "requiredCatch": "RequiredCatch",
	"unknown": "Unknown", "any": "Any", "never": "Never",
	"union": "Union", "intersection": "Intersection",
	"excludeTags": "ExcludeTags", "preserve": "Preserve", "openTags": "OpenTags",
	"min": "Min", "max": "Max", "gte": "Gte", "lte": "Lte", "regex": "Regex", "int": "Int",
	"null": "Null", "string": "String", "boolean": "Boolean", "number": "Number", "literal": "Literal",
	"url": "URL", "datetime": "DateTime",
	"array": "Array", "skipArray": "SkipArray", "object": "Object", "record": "Record",
}

// zod writes the rule registry and per-type Decode/Validate functions.
func (g *generator) zod(schema *tsdef.Schema) error {
	if len(schema.Validators) == 0 {
		return nil
	}
	keys := make([]string, 0, len(schema.Validators))
	for k := range schema.Validators {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var registry []string
	for _, k := range keys {
		z := schema.Validators[k]
		if open, ok := g.openTags[Name(strings.TrimPrefix(k, "z"))]; ok {
			z = &tsdef.Zod{Kind: "openTags", Tag: open.tag, Tags: open.values, Inner: z}
		}
		rule, err := zodLiteral(z)
		if err != nil {
			return fmt.Errorf("%s: %w", k, err)
		}
		registry = append(registry, fmt.Sprintf("%s: %s,", strconv.Quote(k), rule))
	}

	defs := append([]tsdef.Definition(nil), schema.Types...)
	sort.Slice(defs, func(i, j int) bool { return defs[i].Name < defs[j].Name })
	var types, unmarshalers []string
	register := func(goName, key string) {
		types = append(types, fmt.Sprintf("reflect.TypeFor[%s](): %q", goName, key))
		unmarshalers = append(unmarshalers, fmt.Sprintf("zod.Unmarshaler[%s](zodSchemas, %q)", goName, key))
	}
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
		if g.isAbsorbed(name) {
			continue // registered below under the rule its variant carries
		}
		register(name, key)
	}
	for _, goName := range slices.Sorted(maps.Keys(g.absorbed)) {
		v := g.absorbed[goName]
		if v.variant == "" {
			continue // the union was never emitted
		}
		// The variant writes its tag, so its rule is the absorbed type's
		// own rule intersected with that tag.
		key := fmt.Sprintf("z%s&%s=%s", v.base, v.tag, v.value)
		rule, err := zodLiteral(&tsdef.Zod{Kind: "intersection", Members: []*tsdef.Zod{
			{Kind: "ref", Ref: "z" + v.base},
			{Kind: "object", Fields: []tsdef.ZodField{{Name: v.tag, Schema: &tsdef.Zod{Kind: "literal", Value: jsontext.Value(v.value)}}}},
		}})
		if err != nil {
			return err
		}
		registry = append(registry, fmt.Sprintf("%s: %s,", strconv.Quote(key), rule))
		register(v.variant, key)
	}
	g.write("// zodSchemas holds the SDK Zod rules; one rule tree per schema name.\nvar zodSchemas = zod.Registry{\n%s\n}\n\n", strings.Join(registry, "\n"))
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
// Unlike the SDK, unknown tags of tagged unions are accepted: they decode into
// the union's Unknown variant.
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
