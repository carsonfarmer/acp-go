package acp

import (
	"context"
	"encoding/json/jsontext"
	"io"

	"github.com/ironpark/go-acp/internal/acpconn"
	"github.com/ironpark/go-acp/internal/jsonrpc"
)

// AgentSideConnection is the agent's view of an ACP connection.
//
// It serves an [Agent] to the peer and implements [Client] for calls back to
// it, so an agent needs no other handle to stream updates, ask for
// permissions, read files or run terminals.
//
// See protocol docs: [Agent](https://agentclientprotocol.com/protocol/overview#agent)
type AgentSideConnection struct {
	conn  *jsonrpc.Connection
	agent Agent
}

var _ Client = (*AgentSideConnection)(nil)

// NewAgentSideConnection connects an agent to a client.
//
// newAgent receives the connection being built, so the agent can keep it and
// call the client while handling a request:
//
//	conn := acp.NewAgentSideConnection(func(c *acp.AgentSideConnection) acp.Agent {
//		return &myAgent{client: c}
//	}, os.Stdin, os.Stdout)
//	err := conn.Start(ctx)
//
// reader carries messages from the client and writer carries messages to it;
// for a stdio agent those are os.Stdin and os.Stdout.
//
// See protocol docs: [Communication Model](https://agentclientprotocol.com/protocol/overview#communication-model)
func NewAgentSideConnection(newAgent func(*AgentSideConnection) Agent, reader io.Reader, writer io.Writer, opts ...Option) *AgentSideConnection {
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

// NewTerminal is CreateTerminal plus a [TerminalHandle] bound to the new
// terminal, which is usually what an agent wants.
func (c *AgentSideConnection) NewTerminal(ctx context.Context, params *CreateTerminalRequest) (*TerminalHandle, error) {
	response, err := c.CreateTerminal(ctx, params)
	if err != nil {
		return nil, err
	}
	return NewTerminalHandle(response.TerminalID, params.SessionID, c), nil
}

// ExtMethod sends a request outside the spec and returns its raw result.
func (c *AgentSideConnection) ExtMethod(ctx context.Context, method string, params any) (jsontext.Value, error) {
	return c.conn.SendRequest(ctx, method, params)
}

// ExtNotification sends a notification outside the spec.
func (c *AgentSideConnection) ExtNotification(ctx context.Context, method string, params any) error {
	return c.conn.SendNotification(ctx, method, params)
}
