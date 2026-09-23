package schema

import (
	"encoding/json/v2"
	"testing"
)

// A newer peer may send variants this SDK predates; they must decode into the
// Unknown variant and re-encode unchanged instead of failing the message.
func TestUnknownVariantsRoundTrip(t *testing.T) {
	raw := `{"sessionId":"s","update":{"sessionUpdate":"future_update","content":{"type":"hologram","depth":3}}}`
	var n SessionNotification
	if err := json.Unmarshal([]byte(raw), &n, Validated); err != nil {
		t.Fatal(err)
	}
	if _, ok := n.Update.Variant().(SessionUpdateUnknown); !ok || n.Update.Tag() != "future_update" {
		t.Fatalf("got %T with tag %q", n.Update.Variant(), n.Update.Tag())
	}
	out, err := json.Marshal(n)
	if err != nil || string(out) != raw {
		t.Fatalf("round trip changed the message: %s %v", out, err)
	}

	// Known tags are still validated in full.
	bad := `{"sessionId":"s","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":7}}}`
	if err := json.Unmarshal([]byte(bad), &n, Validated); err == nil {
		t.Fatal("invalid known variant accepted")
	}

	var c ContentBlock
	if err := json.Unmarshal([]byte(`{"type":"hologram","depth":3}`), &c, Validated); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Variant().(ContentBlockUnknown); !ok || c.Tag() != "hologram" {
		t.Fatalf("got %T with tag %q", c.Variant(), c.Tag())
	}
}
