package jsonrpc

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"testing"
)

// FuzzIDKey checks that canonicalizing a request id is stable, so a response
// keyed by IDKey always matches the pending entry it belongs to.
func FuzzIDKey(f *testing.F) {
	for _, seed := range []string{"1", "1.0", "-0", "0", `"abc"`, "", "not json", " 1 ", "1e3"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, id string) {
		key := IDKey(jsontext.Value(id))
		if again := IDKey(jsontext.Value(key)); again != key {
			t.Fatalf("IDKey is not idempotent: %q -> %q -> %q", id, key, again)
		}
	})
}

// FuzzDecodeWireMessage checks that decoding an arbitrary message never panics,
// however malformed the input.
func FuzzDecodeWireMessage(f *testing.F) {
	f.Add(`{"jsonrpc":"2.0","id":1,"method":"x","params":{}}`)
	f.Add(`{"jsonrpc":"2.0","id":1,"result":{}}`)
	f.Add(`[]`)
	f.Add(``)
	f.Fuzz(func(t *testing.T, data string) {
		var msg wireMessage
		_ = json.Unmarshal([]byte(data), &msg)
	})
}
