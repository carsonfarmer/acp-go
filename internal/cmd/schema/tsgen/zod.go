package tsgen

import (
	_ "embed"
	"encoding/json/v2"
	"fmt"
	"sort"

	"github.com/ironpark/go-acp/internal/cmd/schema/tsdef"
)

//go:embed zod_runtime.go.txt
var zodRuntime string

func (g *generator) zod(schema *tsdef.Schema) error {
	if len(schema.Validators) == 0 {
		return nil
	}
	encoded, err := json.Marshal(schema.Validators, json.Deterministic(true))
	if err != nil {
		return fmt.Errorf("encode Zod metadata: %w", err)
	}
	g.write("\n%s\nvar zodSchemas = loadZodSchemas(%q)\n", zodRuntime, string(encoded))
	defs := append([]tsdef.Definition(nil), schema.Types...)
	sort.Slice(defs, func(i, j int) bool { return defs[i].Name < defs[j].Name })
	for _, d := range defs {
		key := "z" + d.Name
		if schema.Validators[key] == nil {
			return fmt.Errorf("missing Zod schema for %s", d.Name)
		}
		name := Name(d.Name)
		for _, prefix := range []string{"Decode", "Validate"} {
			if err := g.reserve(prefix + name + "JSON"); err != nil {
				return err
			}
		}
		g.write("// Decode%sJSON applies the supported SDK Zod validation, defaults and recovery rules.\n", name)
		g.write("func Decode%sJSON(raw []byte) (%s,error) {return decodeZod[%s](%q,raw)}\n", name, name, name, key)
		g.write("// Validate%sJSON checks whether the supported SDK Zod parser accepts raw.\n", name)
		g.write("// Recovery and defaults are applied; use Decode%sJSON to obtain the normalized value.\n", name)
		g.write("func Validate%sJSON(raw []byte) error {_,err:=normalizeZod(%q,raw);return err}\n", name, key)
	}
	return nil
}
