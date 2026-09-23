package tsgen

import (
	"strings"
	"testing"

	"github.com/ironpark/go-acp/internal/cmd/schema/tsdef"
)

// parseGenerator builds a generator over source without emitting anything.
func parseGenerator(t *testing.T, source string) *generator {
	t.Helper()
	s, err := tsdef.Parse("fixture.ts", []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	g, err := newGenerator(s, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func TestCanonical(t *testing.T) {
	g := parseGenerator(t, `
 export type Raw = unknown;
 export type Chain = Raw;
 export type Names = string[];
 export type Maybe = string | null;
 export type Lookup = { [key: string]: Names };
 export type ID = string;
 export type Mode = "a" | "b";
 export type Obj = { x: number };
 export type Loop = Loop[];
 export type Union = Raw | Chain | Names | Maybe | Lookup | ID | Mode | Obj | Loop | { inline: string } | number | null;
 `)
	want := []string{
		"jsontext.Value", "jsontext.Value", "[]string", "*string", "map[string][]string",
		"ID", "Mode", "Obj", "fallback", "fallback", "float64", "jsontext.Value",
	}
	members := g.defs["Union"].Members
	if len(members) != len(want) {
		t.Fatalf("got %d members, want %d", len(members), len(want))
	}
	for i, m := range members {
		if got := g.canonical(m, "fallback"); got != want[i] {
			t.Errorf("member %d (%s): got %q, want %q", i, m.Kind, got, want[i])
		}
	}
}

func TestAliasDefinition(t *testing.T) {
	g := parseGenerator(t, `
 export type Raw = unknown;
 export type Names = string[];
 export type Maybe = string | null;
 export type Either = string | number;
 export type MaybeEither = string | number | null;
 export type ID = string;
 export type Mode = "a" | "b";
 export type Open = "a" | (string & {});
 export type Obj = { x: number };
 export type Both = Obj & { y: number };
 export type Record = { [key: string]: number };
 `)
	want := map[string]bool{
		"Raw": true, "Names": true, "Maybe": true, "Record": true,
		"Either": false, "MaybeEither": false, "ID": false, "Mode": false, "Open": false, "Obj": false, "Both": false,
	}
	for name, alias := range want {
		if got := g.aliasDefinition(g.defs[name]); got != alias {
			t.Errorf("%s: aliasDefinition = %v, want %v", name, got, alias)
		}
	}
}

func TestAltRule(t *testing.T) {
	g := parseGenerator(t, `
 export type U = null | "yes" | number | { kind: "form"; name: string; note?: string; ref: string | null } | { [key: string]: number };
 `)
	want := []string{
		`union.Rule{Null: true}`,
		`union.Rule{NonNull: true, Literal: jsontext.Value("\"yes\"")}`,
		`union.Rule{NonNull: true}`,
		`union.Rule{NonNull: true, Required: []string{"kind", "name", "ref"}, NotNull: []string{"kind", "name", "note"}, Tags: []union.Tag{{Name: "kind", Value: jsontext.Value("\"form\"")}}}`,
		`union.Rule{NonNull: true}`,
	}
	for i, m := range g.defs["U"].Members {
		expanded, err := g.expand(m, map[string]bool{})
		if err != nil {
			t.Fatal(err)
		}
		if got := g.altRule(expanded); got != want[i] {
			t.Errorf("member %d: got %s, want %s", i, got, want[i])
		}
	}
}

func TestUsedImports(t *testing.T) {
	body := `// fmt.Errorf is only mentioned in this comment; zod.Rule too.
type X struct { raw jsontext.Value }
func (x X) MarshalJSON() ([]byte, error) { return json.Marshal(x.raw) }
var table = union.Table()
func f() { s := strings.ToUpper("x"); _ = s }
`
	got, err := usedImports([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	want := "encoding/json/jsontext encoding/json/v2 github.com/ironpark/go-acp/schema/union"
	if strings.Join(got, " ") != want {
		t.Fatalf("got %v, want %s", got, want)
	}
	if _, err := usedImports([]byte("func {")); err == nil {
		t.Fatal("invalid body accepted")
	}
}
