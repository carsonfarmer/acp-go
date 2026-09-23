package acpv1

import (
	"context"
	"io"

	acp "github.com/ironpark/go-acp"
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
	c.conn = acpconn.NewConnection(c.handleRequest, c.handleNotification, reader, writer, opts)
	return c
}

// sessionUpdate copies each session update to the session's [Turn] in
// progress, then hands it to the client. The generated dispatch routes
// session/update here.
func (c *ClientSideConnection) sessionUpdate(ctx context.Context, n *SessionNotification) error {
	c.turns.Deliver(n.SessionID, n.Update)
	return c.client.SessionUpdate(ctx, n)
}
