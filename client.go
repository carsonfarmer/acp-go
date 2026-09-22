package acp

import (
	"context"
	"encoding/json/jsontext"
	"io"

	"github.com/ironpark/go-acp/internal/acpconn"
	"github.com/ironpark/go-acp/internal/jsonrpc"
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
}

var _ Agent = (*ClientSideConnection)(nil)

// NewClientSideConnection connects a client to an agent.
//
// newClient receives the connection being built, so the client can call the
// agent while handling one of its requests:
//
//	conn := acp.NewClientSideConnection(func(c *acp.ClientSideConnection) acp.Client {
//		return &myClient{agent: c}
//	}, agentStdout, agentStdin)
//	go conn.Start(ctx)
//
// reader carries messages from the agent and writer carries messages to it;
// when spawning an agent process those are its stdout and stdin.
//
// See protocol docs: [Communication Model](https://agentclientprotocol.com/protocol/overview#communication-model)
func NewClientSideConnection(newClient func(*ClientSideConnection) Client, reader io.Reader, writer io.Writer, opts ...Option) *ClientSideConnection {
	c := &ClientSideConnection{}
	c.client = newClient(c)
	c.conn = acpconn.NewConnection(c.handleRequest, c.handleNotification, reader, writer, opts)
	return c
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
