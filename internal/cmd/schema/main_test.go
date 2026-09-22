package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/ironpark/go-acp/internal/cmd/schema/tsdef"
)

func TestPinnedSDKGeneration(t *testing.T) {
	source := "../../../schema/typescript"
	for version, count := range map[string]int{"v1": 268, "v2": 267} {
		s, err := tsdef.ParseDir(filepath.Join(source, version))
		if err != nil {
			t.Fatal(err)
		}
		if len(s.Types) != count || len(s.Constants) != 4 || len(s.Validators) != count {
			t.Fatalf("%s: unexpected snapshot: %d types, %d constants", version, len(s.Types), len(s.Constants))
		}
	}
	output := t.TempDir()
	args := []string{"-source", source, "-out", output}
	if err := run(args); err != nil {
		t.Fatal(err)
	}
	if err := run(append(args, "-check")); err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{"v1", "v2"} {
		generated, err := os.ReadFile(filepath.Join(output, version, "schema.gen.go"))
		if err != nil {
			t.Fatal(err)
		}
		checkedIn, err := os.ReadFile(filepath.Join("../../../schema", version, "schema.gen.go"))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(generated, checkedIn) {
			t.Fatalf("%s: checked-in output is stale; run go generate ./...", version)
		}
	}
	stale := filepath.Join(output, "v1", "schema.gen.go")
	if err := os.WriteFile(stale, []byte("stale"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := run(append(args, "-check")); err == nil {
		t.Fatal("check accepted stale output")
	}
	got, err := os.ReadFile(stale)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "stale" {
		t.Fatal("check modified output")
	}
}
func TestNoPartialOutputOnParseFailure(t *testing.T) {
	source := t.TempDir()
	output := t.TempDir()
	for _, version := range []string{"v1", "v2"} {
		dir := filepath.Join(source, version)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		for name, content := range map[string]string{"types.gen.ts": "export type X = string;", "index.ts": "export const VERSION = 1;", "zod.gen.ts": ""} {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := os.WriteFile(filepath.Join(source, "v2", "types.gen.ts"), []byte("export type X = Pick<Y, 'z'>;"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"-source", source, "-out", output}); err == nil {
		t.Fatal("accepted unsupported type")
	}
	entries, err := os.ReadDir(output)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatal("wrote partial output")
	}
}
