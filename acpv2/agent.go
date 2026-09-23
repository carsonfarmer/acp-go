package acpv2

import (
	"io"

	acp "github.com/ironpark/go-acp"
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

// Client returns the peer client. The connection itself implements [Client].
func (c *AgentSideConnection) Client() Client { return c }
