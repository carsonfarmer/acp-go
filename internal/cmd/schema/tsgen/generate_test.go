package tsgen

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ironpark/go-acp/internal/cmd/schema/tsdef"
)

func TestGeneratedWireTypes(t *testing.T) {
	s, err := tsdef.Parse("fixture.ts", []byte(`
 export type Text = { text: string; };
 export type Detail = { level: "info" } | { level: "warn"; code: number } | { level: string; [key: string]: unknown; };
 export type Message = (Text & { kind: "text" }) | (Detail & { kind: "detail" }) | { kind: string; [key: string]: unknown; };
 export type Options = { enabled?: boolean; count: number | null; tags?: Array<string>; label?: string; };
 export type Status = "pending" | "done";
 export type Kind = "read" | "write" | string;
 export type Payload = string | unknown;
 export type LiteralChoice = "yes" | number;
 export type NullableChoice = string | null | number;
 export type Extras = { fixed: number; [key: string]: number; };
 export type WithNullable = { value: string | null; } | { done: true; };
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
	if !bytes.Equal(a["schema.gen.go"], b["schema.gen.go"]) || len(a) != 1 {
		t.Fatal("non-deterministic generation")
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
func TestGenerationErrors(t *testing.T) {
	for _, source := range []string{`export type X = Missing;`, `export type X = { url: string; URL: number; };`, `export type X = {a:string} & {a:number};`, `export type State = "ready"; export type StateReady = string;`, `export type X = string | number; export type ParseX = string;`, `export type X = string | number; export type NewXString = string;`} {
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
		"URL": "URL", "utf8": "Utf8", "éclair": "Éclair",
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
	gomod := "module fixture\n\ngo 1.27.0\n\nrequire github.com/ironpark/go-acp v0.0.0\n\nreplace github.com/ironpark/go-acp => " + repo + "\n"
	all := map[string][]byte{"go.mod": []byte(gomod), "schema_test.go": tests}
	for name, data := range files {
		all[name] = data
	}
	for name, data := range all {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0644); err != nil {
			t.Fatal(err)
		}
	}
}
