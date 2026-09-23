// Command http-client connects to the http-agent example over HTTP and runs
// one prompt turn. Start the agent first:
//
//	go run ./docs/example/http-agent
//	go run ./docs/example/http-client
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
	url := flag.String("url", "http://localhost:8000", "base URL of the agent")
	flag.Parse()
	if err := run(context.Background(), *url); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, url string) error {
	// Connect opens the /events stream; it must run before the connection
	// sends anything, or the replies would have nowhere to go.
	transport := acp.NewHTTPClientTransport(url)
	if err := transport.Connect(ctx); err != nil {
		return err
	}
	agent := acpv1.NewClientSideConnection(func(*acpv1.ClientSideConnection) acpv1.Client {
		return echoClient{}
	}, nil, nil, acp.WithTransport(transport))
	// Start runs the read loop until the connection closes; wait for it on
	// the way out so the transport is done before the program exits.
	started := make(chan error, 1)
	go func() { started <- agent.Start(ctx) }()
	defer func() {
		agent.Close()
		<-started
	}()

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

	turn, err := session.Prompt(ctx, acpv1.TextBlock("hello over http"))
	if err != nil {
		return fmt.Errorf("prompt: %w", err)
	}
	// Text collects the agent's message chunks until the turn ends.
	text, err := turn.Text()
	if err != nil {
		return fmt.Errorf("prompt: %w", err)
	}
	result, _ := turn.Wait() // already ended; Wait returns the same result again
	fmt.Printf("<< %s\nstop reason: %s\n", text, result.StopReason)
	return nil
}
