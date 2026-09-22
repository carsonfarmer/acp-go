package acpv2

import (
	"context"
	"encoding/json/jsontext"
	acp "github.com/ironpark/go-acp"
	"io"

	"github.com/ironpark/go-acp/internal/acpconn"
	"github.com/ironpark/go-acp/internal/jsonrpc"
)

// AgentSideConnection is the agent's view of an ACP v2 connection. It serves
// an [Agent] to the peer and implements [Client] for calls back to it.
type AgentSideConnection struct {
	conn  *jsonrpc.Connection
	agent Agent
}

var _ Client = (*AgentSideConnection)(nil)

// NewAgentSideConnection connects an agent to a client. newAgent receives the
// connection being built so the agent can keep it as its [Client]. reader
// carries messages from the client and writer carries messages to it.
func NewAgentSideConnection(newAgent func(*AgentSideConnection) Agent, reader io.Reader, writer io.Writer, opts ...acp.Option) *AgentSideConnection {
	c := &AgentSideConnection{}
	c.agent = newAgent(c)
	c.conn = acpconn.NewConnection(c.handleRequest, c.handleNotification, reader, writer, opts)
	return c
}

// Start processes messages until the peer disconnects or ctx is cancelled.
func (c *AgentSideConnection) Start(ctx context.Context) error { return c.conn.Start(ctx) }

// Close shuts the connection down, waiting for in-flight handlers.
func (c *AgentSideConnection) Close() error { return c.conn.Close() }

// Done is closed once the connection stops.
func (c *AgentSideConnection) Done() <-chan struct{} { return c.conn.Done() }

// Client returns the peer client. The connection itself implements [Client].
func (c *AgentSideConnection) Client() Client { return c }

// ExtMethod sends a request outside the spec and returns its raw result.
func (c *AgentSideConnection) ExtMethod(ctx context.Context, method string, params any) (jsontext.Value, error) {
	return c.conn.SendRequest(ctx, method, params)
}

// ExtNotification sends a notification outside the spec.
func (c *AgentSideConnection) ExtNotification(ctx context.Context, method string, params any) error {
	return c.conn.SendNotification(ctx, method, params)
}
