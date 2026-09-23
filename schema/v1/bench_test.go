package schema

import (
	"encoding/json/v2"
	"testing"
)

// The two benchmarks decode the same session notification; the difference is
// the cost the Zod validation rules add on top of the union unmarshalers.

func BenchmarkValidatedSessionNotification(b *testing.B) {
	data := []byte(`{"sessionId":"session_1","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"hello"}}}`)
	for b.Loop() {
		var notification SessionNotification
		if err := json.Unmarshal(data, &notification, Validated()); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkUnvalidatedSessionNotification(b *testing.B) {
	data := []byte(`{"sessionId":"session_1","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"hello"}}}`)
	opts := json.WithUnmarshalers(Unmarshalers())
	for b.Loop() {
		var notification SessionNotification
		if err := json.Unmarshal(data, &notification, opts); err != nil {
			b.Fatal(err)
		}
	}
}
