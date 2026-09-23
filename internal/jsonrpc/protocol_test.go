package jsonrpc

import (
	"testing"

	schema1 "github.com/ironpark/acp-go/schema/v1"
	schema2 "github.com/ironpark/acp-go/schema/v2"
)

// TestCancelRequestMethodMatchesSchemas keeps the connection's own protocol
// method in line with the SDK's PROTOCOL_METHODS table in every version.
func TestCancelRequestMethodMatchesSchemas(t *testing.T) {
	for version, method := range map[string]string{"v1": schema1.ProtocolMethodsCancelRequest, "v2": schema2.ProtocolMethodsCancelRequest} {
		if method != CancelRequestMethod {
			t.Errorf("%s: PROTOCOL_METHODS cancel_request is %q, connection handles %q", version, method, CancelRequestMethod)
		}
	}
}
