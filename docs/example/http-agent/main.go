// Command http-agent serves the echo agent over HTTP instead of stdio.
//
//	go run ./docs/example/http-agent        # listens on localhost:8000
//	go run ./docs/example/http-client       # in another terminal
//
// acp.HTTPServerTransport carries one connection, so serve one client at a
// time: the client posts JSON-RPC
// messages to /message and reads the agent's messages from the /events
// Server-Sent Events stream. Any transport plugs into a connection through
// acp.WithTransport; the agent code does not change.
package main

import (
	"context"
	"flag"
	"log"
	"net/http"

	acp "github.com/ironpark/go-acp"
	"github.com/ironpark/go-acp/acpv1"
	schema "github.com/ironpark/go-acp/schema/v1"
)

// echoAgent is the echo example's agent, replying with an "echo: " prefix.
type echoAgent struct {
	client acpv1.Client
}

func (a *echoAgent) Initialize(_ context.Context, _ *acpv1.InitializeRequest) (*acpv1.InitializeResponse, error) {
	return &acpv1.InitializeResponse{ProtocolVersion: acpv1.ProtocolVersion}, nil
}

func (a *echoAgent) NewSession(_ context.Context, _ *acpv1.NewSessionRequest) (*acpv1.NewSessionResponse, error) {
	return &acpv1.NewSessionResponse{SessionID: acpv1.GenerateSessionID()}, nil
}

func (a *echoAgent) Prompt(ctx context.Context, params *acpv1.PromptRequest) (*acpv1.PromptResponse, error) {
	stream := acpv1.NewSessionStream(a.client, params.SessionID)
	for text := range acpv1.Texts(params.Prompt) {
		if err := stream.SendText(ctx, "echo: "+text); err != nil {
			return nil, err
		}
	}
	return &acpv1.PromptResponse{StopReason: schema.StopReasonEndTurn}, nil
}

func (a *echoAgent) Cancel(context.Context, *acpv1.CancelNotification) error { return nil }

func main() {
	addr := flag.String("addr", "localhost:8000", "address to listen on")
	flag.Parse()

	transport := acp.NewHTTPServerTransport()
	go func() {
		log.Printf("serving an ACP agent on http://%s", *addr)
		log.Fatal(http.ListenAndServe(*addr, transport.Handler()))
	}()

	// The reader and writer are unused: the transport replaces stdio.
	conn := acpv1.NewAgentSideConnection(func(c *acpv1.AgentSideConnection) acpv1.Agent {
		return &echoAgent{client: c}
	}, nil, nil, acp.WithTransport(transport))
	if err := conn.Start(context.Background()); err != nil {
		log.Fatal(err)
	}
}
