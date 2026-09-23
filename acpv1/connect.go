package acpv1

import (
	"context"

	acp "github.com/ironpark/go-acp"
	"github.com/ironpark/go-acp/internal/acpconn"
)

// RemoteAgent is a connection to an agent over a transport the caller
// provided, such as Streamable HTTP. It embeds the connection, so every agent
// method is called on it directly.
type RemoteAgent struct {
	*ClientSideConnection
	wait func() error
}

// ConnectAgent connects to an agent over transport and starts the read loop:
//
//	agent := acpv1.ConnectAgent(ctx, acp.NewHTTPClientTransport("https://host/acp"), newClient)
//	defer agent.Close()
//	init, err := agent.Initialize(ctx, &acpv1.InitializeRequest{ProtocolVersion: acpv1.ProtocolVersion})
//
// The agent owns transport from then on: it is closed when the connection
// stops, which for Streamable HTTP deletes the connection on the server.
// Cancelling ctx stops the connection too.
func ConnectAgent(ctx context.Context, transport acp.Transport, newClient func(*ClientSideConnection) Client, opts ...acp.Option) *RemoteAgent {
	conn := NewClientSideConnection(newClient, nil, nil, append(opts, acp.WithTransport(transport))...)
	return &RemoteAgent{ClientSideConnection: conn, wait: acpconn.Run(ctx, conn, transport)}
}

// Close stops the connection and closes its transport.
func (a *RemoteAgent) Close() error {
	err := a.ClientSideConnection.Close()
	_ = a.wait()
	return err
}

// Wait blocks until the connection has stopped and its transport is closed.
// It reports the read loop's error, such as the transport's, or nil after
// Close.
func (a *RemoteAgent) Wait() error { return a.wait() }
