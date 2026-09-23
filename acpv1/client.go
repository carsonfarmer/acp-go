package acpv1

import (
	"context"
	"encoding/json/jsontext"
	acp "github.com/ironpark/go-acp"
	"io"

	"github.com/ironpark/go-acp/internal/acpconn"
	"github.com/ironpark/go-acp/internal/jsonrpc"
	schema "github.com/ironpark/go-acp/schema/v1"
)

// ClientSideConnection is the client's view of an ACP connection.
//
// It serves a [Client] to the peer agent and exposes every agent method for
// outgoing calls, so an editor drives a session entirely through this type.
//
// See protocol docs: [Client](https://agentclientprotocol.com/protocol/overview#client)
type ClientSideConnection struct {
	conn   *jsonrpc.Connection
	client Client
	turns  acpconn.Turns[SessionID, SessionUpdate, *PromptResponse]
}

var _ Agent = (*ClientSideConnection)(nil)

// NewClientSideConnection connects a client to an agent.
//
// newClient receives the connection being built, so the client can call the
// agent while handling one of its requests:
//
//	conn := acpv1.NewClientSideConnection(func(c *acpv1.ClientSideConnection) acpv1.Client {
//		return &myClient{agent: c}
//	}, agentStdout, agentStdin)
//	go conn.Start(ctx)
//
// reader carries messages from the agent and writer carries messages to it;
// when spawning an agent process those are its stdout and stdin.
//
// See protocol docs: [Communication Model](https://agentclientprotocol.com/protocol/overview#communication-model)
func NewClientSideConnection(newClient func(*ClientSideConnection) Client, reader io.Reader, writer io.Writer, opts ...acp.Option) *ClientSideConnection {
	c := &ClientSideConnection{}
	c.client = newClient(c)
	c.conn = acpconn.NewConnection(c.handleRequest, c.routeNotification, reader, writer, opts)
	return c
}

// routeNotification copies each session update to the session's [Turn] in
// progress before the client sees it, then dispatches as generated.
func (c *ClientSideConnection) routeNotification(ctx context.Context, method string, params jsontext.Value) error {
	if method != schema.ClientMethodsSessionUpdate {
		return c.handleNotification(ctx, method, params)
	}
	return acpconn.Notify(ctx, schema.Validated, params, func(ctx context.Context, n *SessionNotification) error {
		c.turns.Deliver(n.SessionID, n.Update)
		return c.client.SessionUpdate(ctx, n)
	})
}

// Start processes messages until the peer disconnects or ctx is cancelled.
func (c *ClientSideConnection) Start(ctx context.Context) error { return c.conn.Start(ctx) }

// Close shuts the connection down, waiting for in-flight handlers.
func (c *ClientSideConnection) Close() error { return c.conn.Close() }

// Done is closed once the connection stops.
func (c *ClientSideConnection) Done() <-chan struct{} { return c.conn.Done() }

// ExtMethod sends a request outside the spec and returns its raw result.
func (c *ClientSideConnection) ExtMethod(ctx context.Context, method string, params any) (jsontext.Value, error) {
	return c.conn.SendRequest(ctx, method, params)
}

// ExtNotification sends a notification outside the spec.
func (c *ClientSideConnection) ExtNotification(ctx context.Context, method string, params any) error {
	return c.conn.SendNotification(ctx, method, params)
}
