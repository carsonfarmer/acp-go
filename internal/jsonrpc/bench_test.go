package jsonrpc

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"testing"
)

func BenchmarkIDKey(b *testing.B) {
	for _, id := range []jsontext.Value{jsontext.Value("1"), jsontext.Value("1.0"), jsontext.Value(`"abc"`)} {
		b.Run(string(id), func(b *testing.B) {
			for b.Loop() {
				_ = IDKey(id)
			}
		})
	}
}

func BenchmarkMarshalWireMessage(b *testing.B) {
	msg := wireMessage{
		JSONRPC: Version,
		ID:      jsontext.Value("1"),
		Method:  "session/update",
		Params:  jsontext.Value(`{"sessionId":"session_1","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"hello"}}}`),
	}
	for b.Loop() {
		if _, err := json.Marshal(&msg); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkUnmarshalWireMessage(b *testing.B) {
	data := []byte(`{"jsonrpc":"2.0","id":1,"method":"session/update","params":{"sessionId":"session_1","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"hello"}}}}`)
	for b.Loop() {
		var msg wireMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			b.Fatal(err)
		}
	}
}
