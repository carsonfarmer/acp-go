package acp1

import (
	"context"

	acp "github.com/ironpark/acp-go"
	"github.com/ironpark/acp-go/internal/acpconn"
	"github.com/ironpark/acp-go/internal/jsonrpc"
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
//	conn := acp1.NewAgentSideConnection(func(c *acp1.AgentSideConnection) acp1.Agent {
//		return &myAgent{client: c}
//	}, acp.NewStdioTransport(os.Stdin, os.Stdout))
//	err := conn.Start(ctx)
//
// transport carries the messages to and from the client; for a stdio agent it
// is [acp.NewStdioTransport] over os.Stdin and os.Stdout.
//
// See protocol docs: [Communication Model](https://agentclientprotocol.com/protocol/overview#communication-model)
func NewAgentSideConnection(newAgent func(*AgentSideConnection) Agent, transport acp.Transport, opts ...acp.Option) *AgentSideConnection {
	c := &AgentSideConnection{}
	c.agent = newAgent(c)
	c.conn = acpconn.NewAgentConnection(c.handleRequest, c.handleNotification, transport, opts)
	return c
}

// Client returns the peer client. The connection itself implements [Client].
func (c *AgentSideConnection) Client() Client { return c }

// NewTerminal is CreateTerminal plus a [TerminalHandle] bound to the new
// terminal, which is usually what an agent wants.
func (c *AgentSideConnection) NewTerminal(ctx context.Context, params *CreateTerminalRequest) (*TerminalHandle, error) {
	return newTerminal(ctx, c, params)
}

func newTerminal(ctx context.Context, terminals TerminalHandler, params *CreateTerminalRequest) (*TerminalHandle, error) {
	response, err := terminals.CreateTerminal(ctx, params)
	if err != nil {
		return nil, err
	}
	return NewTerminalHandle(response.TerminalID, params.SessionID, terminals), nil
}
