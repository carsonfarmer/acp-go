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

import acp "github.com/ironpark/acp-go"

// ExtMethodHandler handles methods outside the spec; see [acp.ExtMethodHandler].
type ExtMethodHandler = acp.ExtMethodHandler

// ExtNotificationHandler handles notifications outside the spec; see
// [acp.ExtNotificationHandler].
type ExtNotificationHandler = acp.ExtNotificationHandler
