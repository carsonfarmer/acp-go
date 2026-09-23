// Package acp holds what every Agent Client Protocol version shares: the
// connection options, transports, middleware, error type, session store and
// turn tracker, and typed extension methods ([CallExt], [ExtRouter]).
//
// The protocol façades live in versioned sibling packages, each generated from
// the upstream schema of that version:
//
//   - [github.com/ironpark/go-acp/acp1] — ACP v1, the stable protocol
//   - [github.com/ironpark/go-acp/acp2] — ACP v2, still a draft
//
// [github.com/ironpark/go-acp/router] serves both versions on one endpoint.
// The wire types are in [github.com/ironpark/go-acp/schema/v1] and
// [github.com/ironpark/go-acp/schema/v2].
//
// See the protocol docs: https://agentclientprotocol.com
package acp

//go:generate sh -c "cd internal/cmd/schema && go run . -source ../../../schema/typescript -out ../../../schema -facade ../../.."
