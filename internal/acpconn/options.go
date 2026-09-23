// Package acpconn holds what the versioned ACP façades share: connection
// options, the JSON-RPC connection factory and the generic dispatch helpers.
// Each façade aliases these so one option value configures either version.
package acpconn

import "github.com/ironpark/acp-go/internal/jsonrpc"

// Option configures a connection of either protocol version.
type Option = jsonrpc.Option

// NewConnection builds the JSON-RPC connection behind a façade over transport.
func NewConnection(request jsonrpc.RequestHandler, notification jsonrpc.NotificationHandler, transport jsonrpc.Transport, opts []Option) *jsonrpc.Connection {
	return jsonrpc.New(request, notification, transport, opts...)
}
