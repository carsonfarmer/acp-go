// Package acp1 implements the Agent Client Protocol (ACP) v1 for Go.
//
// The wire types are generated from the upstream TypeScript SDK and live in
// [github.com/ironpark/acp-go/schema/v1]; this package adds the two
// connection façades on the shared runtime in [github.com/ironpark/acp-go]:
//
//   - [AgentSideConnection] serves an [Agent] and calls the peer [Client].
//   - [ClientSideConnection] serves a [Client] and calls the peer [Agent].
//
// Incoming parameters are validated with the SDK's Zod rules before a handler
// sees them, so handlers receive normalized values.
//
// Connection options, transports, middleware and [github.com/ironpark/acp-go.RequestError] come from
// the root package.
//
// See the protocol docs: https://agentclientprotocol.com
package acp1

import (
	"context"
	"encoding/json/jsontext"
)

// ExtMethodHandler handles methods outside the spec, including the mcp/*
// methods, which the reference SDKs also leave to extensions. Prefix custom
// methods with a unique identifier such as a domain name.
//
// See protocol docs: [Extensibility](https://agentclientprotocol.com/protocol/extensibility)
type ExtMethodHandler interface {
	ExtMethod(ctx context.Context, method string, params jsontext.Value) (any, error)
}

// ExtNotificationHandler handles notifications outside the spec.
//
// The connection answers $/cancel_request itself, so it never reaches here.
type ExtNotificationHandler interface {
	ExtNotification(ctx context.Context, method string, params jsontext.Value) error
}
