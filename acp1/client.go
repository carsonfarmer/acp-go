package acp1

import (
	"context"
	"io"

	acp "github.com/ironpark/go-acp"
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
//	conn := acp1.NewClientSideConnection(func(c *acp1.ClientSideConnection) acp1.Client {
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

// UnimplementedClient provides the methods every [Client] needs for a client
// that only reads its turns: it drops session updates, which each [Turn] still
// collects, and answers permission requests with "method not found". Embed it
// and declare either method to override it:
//
//	type myClient struct{ acp1.UnimplementedClient }
//
// session/request_permission is part of every client in the protocol, so
// embed this only for agents that never ask.
type UnimplementedClient struct{}

// SessionUpdate ignores the update.
func (UnimplementedClient) SessionUpdate(context.Context, *SessionNotification) error { return nil }

// RequestPermission answers "method not found".
func (UnimplementedClient) RequestPermission(context.Context, *RequestPermissionRequest) (*RequestPermissionResponse, error) {
	return nil, acp.ErrMethodNotFound(schema.ClientMethodsSessionRequestPermission)
}

// initialize sends initialize, filling in a zero protocol version. The
// generated [ClientSideConnection.Initialize] goes through it.
func (c *ClientSideConnection) initialize(ctx context.Context, params *InitializeRequest) (*InitializeResponse, error) {
	request := InitializeRequest{}
	if params != nil {
		request = *params
	}
	if request.ProtocolVersion == 0 {
		request.ProtocolVersion = ProtocolVersion
	}
	return acpconn.Call[InitializeResponse](ctx, c.conn, schema.AgentMethodsInitialize, &request)
}
