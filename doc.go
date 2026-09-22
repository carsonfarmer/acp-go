// Package acp holds what every Agent Client Protocol version shares: the
// connection options, transports, middleware, error type and session store.
//
// The protocol façades live in versioned sibling packages, each generated from
// the upstream schema of that version:
//
//   - [github.com/ironpark/go-acp/acpv1] — ACP v1, the stable protocol
//   - [github.com/ironpark/go-acp/acpv2] — ACP v2, still a draft
//
// [github.com/ironpark/go-acp/router] serves both versions on one endpoint.
// The wire types are in [github.com/ironpark/go-acp/schema/v1] and
// [github.com/ironpark/go-acp/schema/v2].
//
// See the protocol docs: https://agentclientprotocol.com
package acp

//go:generate sh -c "cd internal/cmd/schema && go run . -source ../../../schema/typescript -out ../../../schema -facade ../../.."
