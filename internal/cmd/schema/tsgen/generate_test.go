package tsgen

import (
	"bytes"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode"

	"github.com/ironpark/acp-go/internal/cmd/schema/tsdef"
)

func TestGeneratedWireTypes(t *testing.T) {
	s, err := tsdef.Parse("fixture.ts", []byte(`
 export type Text = { text: string; };
 export type Detail = { level: "info" } | { level: "warn"; code: number } | { level: string; [key: string]: unknown; };
 export type Message = (Text & { kind: "text" }) | (Detail & { kind: "detail" }) | { kind: string; [key: string]: unknown; };
 export type Shape = { form: "circle"; r: number } | { form: "square"; side: number };
 export type Options = { enabled?: boolean; count: number | null; tags?: Array<string>; label?: string; };
 export type Holder = { options?: Options; status?: Status; };
 export type Status = "pending" | "done";
 export type Kind = "read" | "write" | string;
 export type Ident = string;
 export type Payload = string | unknown;
 export type LiteralChoice = "yes" | number;
 export type NullableChoice = string | null | number;
 export type Extras = { fixed: number; [key: string]: number; };
 export type WithNullable = { value: string | null; } | { done: true; };
 export type Either = A | B | string;
 export type A = unknown;
 export type B = unknown;
 export type Answer = "yes" | number | "no";
 export type Names = string[];
 export type MoreNames = string[];
 export type Listy = Names | MoreNames | number;
 export type ServerHttp = { url: string; };
 export type ServerStdio = { command: string; };
 export type Server = (ServerHttp & { type: "http" }) | ServerStdio;
 export type ScopeA = { a: string; };
 export type ScopeB = { b: string; };
 export type Scoped = (ScopeA | ScopeB) & { x: number; };
 export type Job = (Scoped & { kind: "scoped" }) | { kind: "plain"; y: number; };
 export type Ask = (ScopeA | ScopeB) & { mode: "form"; };
 export const PROTOCOL_VERSION = 2;
 `))
	if err != nil {
		t.Fatal(err)
	}
	a, err := Generate(s, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	b, err := Generate(s, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	// No validators, so no zod file; the other kinds are all present.
	want := []string{"methods.gen.go", "enums.gen.go", "types.gen.go", "unions.gen.go", "getters.gen.go"}
	if len(a) != len(want) {
		t.Fatalf("generated %d files, want %v", len(a), want)
	}
	for _, name := range want {
		if !bytes.Equal(a[name], b[name]) {
			t.Fatalf("non-deterministic generation of %s", name)
		}
	}
	dir := t.TempDir()
	wireTests, err := os.ReadFile("testdata/wire_test.go.txt")
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, dir, a, wireTests)
	cmd := exec.Command("go", "test", "./...")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated wire tests: %v\n%s", err, out)
	}
}

// TestNoNumberedTypes guards the pinned schemas against types told apart by a
// number or a Variant suffix (MCPServerHTTP2, MCPServerHTTPVariant beside
// MCPServerHTTP): the generator should either reuse the existing type or name
// the difference. It also makes a change in which types are taken over by a
// union visible, since that renames them.
func TestNoNumberedTypes(t *testing.T) {
	declared := regexp.MustCompile(`(?m)^type (\w+)`)
	for _, version := range []string{"v1", "v2"} {
		s, err := tsdef.ParseDir("../../../../schema/typescript/" + version)
		if err != nil {
			t.Fatal(err)
		}
		files, err := Generate(s, "schema")
		if err != nil {
			t.Fatal(err)
		}
		names := map[string]bool{}
		for _, src := range files {
			for _, m := range declared.FindAllSubmatch(src, -1) {
				names[string(m[1])] = true
			}
		}
		for name := range names {
			if base := strings.TrimRightFunc(name, unicode.IsDigit); base != name && names[base] {
				t.Errorf("%s: %s is %s with a number", version, name, base)
			}
			// A union's own interface is <Union>Variant; any other type with
			// the suffix is a variant renamed because its name was taken.
			if base, ok := strings.CutSuffix(name, "Variant"); ok && names[base] && !strings.Contains(string(files["unions.gen.go"]), "type "+name+" interface") {
				t.Errorf("%s: %s is %s renamed to avoid a collision", version, name, base)
			}
		}
	}
}

func TestGenerationErrors(t *testing.T) {
	for _, source := range []string{`export type X = Missing;`, `export type X = { url: string; URL: number; };`, `export type X = {a:string} & {a:number};`, `export type State = "ready"; export type StateReady = string;`, `export type X = string | number; export type NewX = string;`, `export type X = string | number; export type XAlternative = string;`} {
		s, err := tsdef.Parse("fixture.ts", []byte(source))
		if err != nil {
			t.Fatal(err)
		}
		if _, err = Generate(s, "fixture"); err == nil {
			t.Fatalf("expected generation error for %s", source)
		}
	}
	if _, err := Generate(&tsdef.Schema{}, "package"); err == nil {
		t.Fatal("accepted invalid package")
	}
}

func TestName(t *testing.T) {
	for input, want := range map[string]string{
		"AGENT_METHODS": "AgentMethods", "PROTOCOL_VERSION": "ProtocolVersion",
		"session_new": "SessionNew", "RequestId": "RequestID", "McpServerHttp": "MCPServerHTTP",
		"URL": "URL", "utf8": "UTF8", "utf16": "UTF16", "LlmProtocol": "LLMProtocol", "Utf8Text": "UTF8Text", "éclair": "Éclair",
	} {
		if got := Name(input); got != want {
			t.Errorf("Name(%q) = %q; want %q", input, got, want)
		}
	}
}

// writeFixture writes generated files plus a go.mod that resolves the shared
// Zod runtime through this repository.
func writeFixture(t *testing.T, dir string, files Files, tests []byte) {
	t.Helper()
	repo, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	gomod := "module fixture\n\ngo 1.27.0\n\nrequire github.com/ironpark/acp-go v0.0.0\n\nreplace github.com/ironpark/acp-go => " + repo + "\n"
	all := map[string][]byte{"go.mod": []byte(gomod), "schema_test.go": tests}
	maps.Copy(all, files)
	for name, data := range all {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0644); err != nil {
			t.Fatal(err)
		}
	}
}
