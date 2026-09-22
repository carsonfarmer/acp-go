package schema

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"os"
	"testing"
)

// Captured from the pinned TypeScript SDK using Zod 4.5.4. Normal Go tests
// need no Node.js installation; see ../testdata/README.md for provenance.
func TestZodSDKReference(t *testing.T) {
	data, err := os.ReadFile("../testdata/zod-v2.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Schema  string         `json:"schema"`
		Input   jsontext.Value `json:"input"`
		Success bool           `json:"success"`
		Output  jsontext.Value `json:"output"`
	}
	if err = json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		t.Run(c.Schema, func(t *testing.T) {
			output, err := normalizeZod(c.Schema, c.Input)
			if (err == nil) != c.Success {
				t.Fatalf("input=%s: expected success=%v, got %v", c.Input, c.Success, err)
			}
			if c.Success && !equalZod(output, c.Output) {
				t.Fatalf("input=%s: got %s, want %s", c.Input, output, c.Output)
			}
		})
	}
}
