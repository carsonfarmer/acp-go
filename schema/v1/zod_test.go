package schema

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"os"
	"strings"
	"testing"

	"github.com/ironpark/acp-go/schema/internal/zod"
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

// A tagged union picks its member by the tag; these inputs reach the edges of
// that shortcut, where it must decide as trying each member in turn would.
func TestZodTaggedUnion(t *testing.T) {
	cases := []struct {
		input string
		ok    bool
	}{
		{`{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"hi"}}`, true},
		// An escaped tag is the same string.
		{`{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"hi"}}`, true},
		// A known tag with a body that does not fit it.
		{`{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":1}}`, false},
		// A duplicate tag is malformed, whichever value comes first.
		{`{"sessionUpdate":"agent_message_chunk","sessionUpdate":"plan","content":{"type":"text","text":"hi"}}`, false},
		// No tag, or one that is not a string.
		{`{"content":{"type":"text","text":"hi"}}`, false},
		{`{"sessionUpdate":1}`, false},
	}
	for _, c := range cases {
		_, err := zodSchemas.Normalize("zSessionUpdate", []byte(c.input))
		if (err == nil) != c.ok {
			t.Errorf("%s: err = %v, want ok=%v", c.input, err, c.ok)
		}
	}
	// The error names the member the tag picked, not every member.
	_, err := zodSchemas.Normalize("zSessionUpdate", []byte(`{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":1}}`))
	if err == nil || !strings.Contains(err.Error(), `$["content"]["text"]: expected string`) || strings.Contains(err.Error(), "no union alternative") {
		t.Errorf("error = %v, want the text member's error alone", err)
	}
}
