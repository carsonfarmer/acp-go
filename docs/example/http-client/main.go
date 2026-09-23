// Command http-client connects to the http-agent example over Streamable HTTP,
// or over WebSocket with -ws, and runs one prompt turn. Start the agent first:
//
//	go run ./docs/example/http-agent
//	go run ./docs/example/http-client
//	go run ./docs/example/http-client -ws
//
// Both transports use the same endpoint, and the connection code is the same
// for either: only the transport passed to ConnectAgent differs.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"

	acp "github.com/ironpark/go-acp"
	"github.com/ironpark/go-acp/acpv1"
)

// echoClient only receives updates; an agent that asks for permission or
// files would get "method not found".
type echoClient struct{}

func (echoClient) SessionUpdate(context.Context, *acpv1.SessionNotification) error { return nil }

func (echoClient) RequestPermission(context.Context, *acpv1.RequestPermissionRequest) (*acpv1.RequestPermissionResponse, error) {
	return nil, acp.ErrMethodNotFound("session/request_permission")
}

func main() {
	url := flag.String("url", "http://localhost:8000/acp", "the agent's ACP endpoint")
	ws := flag.Bool("ws", false, "connect over WebSocket instead of Streamable HTTP")
	flag.Parse()
	if err := run(context.Background(), *url, *ws); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, url string, ws bool) error {
	// Streamable HTTP needs only the endpoint; a WebSocket is dialed up front.
	var transport acp.Transport = acp.NewHTTPClientTransport(url)
	greeting := "hello over Streamable HTTP"
	if ws {
		greeting = "hello over WebSocket"
		var err error
		if transport, err = acp.DialWebSocket(ctx, url); err != nil {
			return err
		}
	}
	// ConnectAgent starts the connection over any transport. Close also ends
	// the connection on the server.
	agent := acpv1.ConnectAgent(ctx, transport, func(*acpv1.ClientSideConnection) acpv1.Client {
		return echoClient{}
	})
	defer agent.Close()

	initialized, err := agent.Initialize(ctx, &acpv1.InitializeRequest{ProtocolVersion: acpv1.ProtocolVersion})
	if err != nil {
		return fmt.Errorf("initialize: %w", err)
	}
	fmt.Printf("initialized (protocol v%d)\n", initialized.ProtocolVersion)

	session, err := agent.StartSession(ctx, &acpv1.NewSessionRequest{Cwd: "/"})
	if err != nil {
		return fmt.Errorf("new session: %w", err)
	}
	fmt.Printf("session: %s\n", session.ID)

	turn, err := session.Prompt(ctx, acpv1.TextBlock(greeting))
	if err != nil {
		return fmt.Errorf("prompt: %w", err)
	}
	// Text collects the agent's message chunks until the turn ends.
	text, result, err := turn.Text()
	if err != nil {
		return fmt.Errorf("prompt: %w", err)
	}
	fmt.Printf("<< %s\nstop reason: %s\n", text, result.StopReason)
	return nil
}
