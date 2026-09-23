package acp1

import (
	"context"
	"io"
	"os/exec"

	acp "github.com/ironpark/acp-go"
	"github.com/ironpark/acp-go/internal/acpconn"
)

// SpawnAgent starts cmd and connects to the agent over its stdio. The
// connection is already processing messages when SpawnAgent returns:
//
//	agent, err := acp1.SpawnAgent(ctx, exec.Command("my-agent"), func(*acp1.ClientSideConnection) acp1.Client {
//		return &myClient{}
//	})
//	init, err := agent.Initialize(ctx, &acp1.InitializeRequest{})
//
// Set Dir, Env or Stderr on cmd before the call; a nil Stderr is sent to the
// parent's stderr. The process is killed when ctx is done, and the connection
// closes once the process exits. On Unix the agent runs in its own process
// group, so a terminal's Ctrl-C reaches only this process, which can turn it
// into a cancel; set cmd.SysProcAttr to opt out.
//
// The agent's Wait blocks until the process has exited and the connection has
// stopped. It reports ctx's error if ctx ended the process, the process's
// exit error if it failed, or the connection's read error.
func SpawnAgent(ctx context.Context, cmd *exec.Cmd, newClient func(*ClientSideConnection) Client, opts ...acp.Option) (*RemoteAgent, error) {
	var conn *ClientSideConnection
	wait, err := acpconn.Spawn(ctx, cmd, func(r io.Reader, w io.Writer) acpconn.Conn {
		conn = NewClientSideConnection(newClient, r, w, opts...)
		return conn
	})
	if err != nil {
		return nil, err
	}
	// Close closes the agent's stdin and returns: the agent exits on its own,
	// and Wait reports how.
	return &RemoteAgent{ClientSideConnection: conn, wait: wait, close: conn.Close}, nil
}

// Pipe connects an agent and a client in memory and starts both, for tests
// and for embedding an agent in the same process. Cancel ctx or close either
// side to stop both.
func Pipe(ctx context.Context, newAgent func(*AgentSideConnection) Agent, newClient func(*ClientSideConnection) Client, opts ...acp.Option) (*AgentSideConnection, *ClientSideConnection) {
	var agent *AgentSideConnection
	var client *ClientSideConnection
	acpconn.Pipe(ctx, func(r io.Reader, w io.Writer) acpconn.Conn {
		agent = NewAgentSideConnection(newAgent, r, w, opts...)
		return agent
	}, func(r io.Reader, w io.Writer) acpconn.Conn {
		client = NewClientSideConnection(newClient, r, w, opts...)
		return client
	})
	return agent, client
}
