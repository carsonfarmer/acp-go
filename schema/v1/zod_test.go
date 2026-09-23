package schema

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"os"
	"testing"

	"github.com/ironpark/go-acp/schema/zod"
)

// Captured from the pinned TypeScript SDK using Zod 4.5.4. Normal Go tests
// need no Node.js installation; see ../testdata/README.md for provenance.
func TestZodSDKReference(t *testing.T) {
	data, err := os.ReadFile("../testdata/zod-v1.json")
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
			output, err := zodSchemas.Normalize(c.Schema, c.Input)
			if (err == nil) != c.Success {
				t.Fatalf("input=%s: expected success=%v, got %v", c.Input, c.Success, err)
			}
			if c.Success && !zod.Equal(output, c.Output) {
				t.Fatalf("input=%s: got %s, want %s", c.Input, output, c.Output)
			}
		})
	}
}

func TestZodIntegerWireNotation(t *testing.T) {
	for _, raw := range []string{"1.0", "1e0", "-0"} {
		value, err := Decode[ProtocolVersion]([]byte(raw))
		if err != nil {
			t.Fatalf("%s: %v", raw, err)
		}
		if raw != "-0" && value != 1 {
			t.Fatalf("%s: got %v", raw, value)
		}
	}
}
