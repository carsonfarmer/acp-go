package acp1

import (
	"context"

	acp "github.com/ironpark/acp-go"
	"github.com/ironpark/acp-go/internal/acpconn"
)

// RemoteAgent is a connection to an agent outside this process: a child
// process from [SpawnAgent], or a transport such as Streamable HTTP from
// [ConnectAgent]. It embeds the connection, so every agent method is called
// on it directly.
type RemoteAgent struct {
	*ClientSideConnection
	wait  func() error
	close func() error
}

// ConnectAgent connects to an agent over transport and starts the read loop:
//
//	agent := acp1.ConnectAgent(ctx, acphttp.NewClientTransport("https://host/acp"), newClient)
//	defer agent.Close()
//	init, err := agent.Initialize(ctx, &acp1.InitializeRequest{})
//
// The agent owns transport from then on: it is closed when the connection
// stops, which for Streamable HTTP deletes the connection on the server.
// Cancelling ctx stops the connection too.
func ConnectAgent(ctx context.Context, transport acp.Transport, newClient func(*ClientSideConnection) Client, opts ...acp.Option) *RemoteAgent {
	conn := NewClientSideConnection(newClient, transport, opts...)
	wait := acpconn.Run(ctx, conn, transport)
	return &RemoteAgent{ClientSideConnection: conn, wait: wait, close: func() error {
		err := conn.Close()
		_ = wait()
		return err
	}}
}

// Close stops the connection. For [ConnectAgent] it also closes the
// transport, which for Streamable HTTP deletes the connection on the server.
func (a *RemoteAgent) Close() error { return a.close() }

// Wait blocks until the connection has stopped: for [SpawnAgent] until the
// process has exited too, for [ConnectAgent] until the transport is closed.
// It reports why, or nil after Close of a connected agent.
func (a *RemoteAgent) Wait() error { return a.wait() }
