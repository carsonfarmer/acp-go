package acpv1

import (
	"context"
	"io"

	acp "github.com/ironpark/go-acp"
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
//	conn := acpv1.NewAgentSideConnection(func(c *acpv1.AgentSideConnection) acpv1.Agent {
//		return &myAgent{client: c}
//	}, os.Stdin, os.Stdout)
//	err := conn.Start(ctx)
//
// reader carries messages from the client and writer carries messages to it;
// for a stdio agent those are os.Stdin and os.Stdout.
//
// See protocol docs: [Communication Model](https://agentclientprotocol.com/protocol/overview#communication-model)
func NewAgentSideConnection(newAgent func(*AgentSideConnection) Agent, reader io.Reader, writer io.Writer, opts ...acp.Option) *AgentSideConnection {
	c := &AgentSideConnection{}
	c.agent = newAgent(c)
	c.conn = acpconn.NewConnection(c.handleRequest, c.handleNotification, reader, writer, opts)
	return c
}

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
