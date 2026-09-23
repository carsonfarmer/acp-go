package acp2

import (
	"context"
	"io"

	acp "github.com/ironpark/go-acp"
	"github.com/ironpark/go-acp/internal/acpconn"
	"github.com/ironpark/go-acp/internal/jsonrpc"
	schema "github.com/ironpark/go-acp/schema/v2"
)

// ClientSideConnection is the client's view of an ACP v2 connection. It serves
// a [Client] to the peer agent and exposes every agent method for outgoing
// calls.
type ClientSideConnection struct {
	conn   *jsonrpc.Connection
	client Client
	turns  acpconn.Turns[SessionID, SessionUpdate, StopReason]
}

var _ Agent = (*ClientSideConnection)(nil)

// NewClientSideConnection connects a client to an agent. newClient receives
// the connection being built so the client can call the agent from its own
// handlers. reader carries messages from the agent and writer carries messages
// to it; when spawning an agent process those are its stdout and stdin.
func NewClientSideConnection(newClient func(*ClientSideConnection) Client, reader io.Reader, writer io.Writer, opts ...acp.Option) *ClientSideConnection {
	c := &ClientSideConnection{}
	c.client = newClient(c)
	c.conn = acpconn.NewConnection(c.handleRequest, c.handleNotification, reader, writer, opts)
	return c
}

// sessionUpdate copies each session update to the session's [Turn] in
// progress and ends the turn when the agent reports idle, then hands the
// update to the client. The generated dispatch routes session/update here.
func (c *ClientSideConnection) sessionUpdate(ctx context.Context, n *UpdateSessionNotification) error {
	if t := c.turns.Deliver(n.SessionID, n.Update); t != nil {
		if state, ok := n.Update.As[schema.SessionUpdateStateUpdate](); ok {
			if idle, ok := state.Value.As[schema.StateUpdateIdle](); ok {
				c.turns.End(n.SessionID, t, idle.GetStopReason(), nil)
			}
		}
	}
	return c.client.SessionUpdate(ctx, n)
}

// UnimplementedClient provides the methods every [Client] needs for a client
// that only reads its turns: it drops session updates, which each [Turn] still
// collects, and answers permission requests with "method not found". Embed it
// and declare either method to override it:
//
//	type myClient struct{ acp2.UnimplementedClient }
//
// session/request_permission is part of every client in the protocol, so
// embed this only for agents that never ask.
type UnimplementedClient struct{}

// SessionUpdate ignores the update.
func (UnimplementedClient) SessionUpdate(context.Context, *UpdateSessionNotification) error {
	return nil
}

// RequestPermission answers "method not found".
func (UnimplementedClient) RequestPermission(context.Context, *RequestPermissionRequest) (*RequestPermissionResponse, error) {
	return nil, acp.ErrMethodNotFound(schema.ClientMethodsSessionRequestPermission)
}

// initialize sends initialize, filling in a zero protocol version. The
// generated [ClientSideConnection.Initialize] goes through it.
func (c *ClientSideConnection) initialize(ctx context.Context, params *InitializeRequest) (*InitializeResponse, error) {
	var request InitializeRequest
	if params != nil {
		request = *params
	}
	if request.ProtocolVersion == 0 {
		request.ProtocolVersion = ProtocolVersion
	}
	return acpconn.Call[InitializeResponse](ctx, c.conn, schema.AgentMethodsInitialize, &request)
}
