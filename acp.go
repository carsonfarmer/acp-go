// Package acp implements the Agent Client Protocol (ACP) v1 for Go.
//
// The wire types are generated from the upstream TypeScript SDK and live in
// [github.com/ironpark/go-acp/schema/v1]; this package adds the JSON-RPC
// runtime and the two connection façades:
//
//   - [AgentSideConnection] serves an [Agent] and calls the peer [Client].
//   - [ClientSideConnection] serves a [Client] and calls the peer [Agent].
//
// Incoming parameters are validated with the SDK's Zod rules before a handler
// sees them, so handlers receive normalized values.
//
// See the protocol docs: https://agentclientprotocol.com
package acp

//go:generate sh -c "cd internal/cmd/schema && go run . -source ../../../schema/typescript -out ../../../schema -facade ../../.."

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
