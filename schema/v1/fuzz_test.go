package schema

import (
	"encoding/json/v2"
	"testing"
)

// FuzzValidatedDecode checks that decoding arbitrary input through the Zod
// validation rules resolves to success or an error and never panics, including
// for the tagged-union fields that a plain decoder would let through.
func FuzzValidatedDecode(f *testing.F) {
	f.Add(`{"cwd":"/"}`)
	f.Add(`{"cwd":"/","mcpServers":[]}`)
	f.Add(`{"sessionId":"s","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"hi"}}}`)
	f.Add(`{"protocolVersion":1}`)
	f.Add(`{}`)
	f.Add(`null`)
	f.Add(`[]`)
	f.Add(`{"cwd":123}`)
	f.Fuzz(func(t *testing.T, data string) {
		raw := []byte(data)
		var newSession NewSessionRequest
		_ = json.Unmarshal(raw, &newSession, Validated())
		var notification SessionNotification
		_ = json.Unmarshal(raw, &notification, Validated())
		var init InitializeRequest
		_ = json.Unmarshal(raw, &init, Validated())
	})
}
