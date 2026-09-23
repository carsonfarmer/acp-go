// Command http-agent serves the echo agent over Streamable HTTP instead of
// stdio, at http://localhost:8000/acp.
//
//	go run ./docs/example/http-agent        # listens on localhost:8000
//	go run ./docs/example/http-client       # in another terminal
//
// acp.HTTPServer implements the remote transport the other ACP SDKs use, in
// both its profiles on one endpoint: Streamable HTTP, where the client POSTs
// messages and reads the agent's from Server-Sent Events streams, and
// WebSocket. Each client gets its own connection and its own agent; the agent
// code is the same as over stdio.
package main

import (
	"context"
	"flag"
	"log"
	"net/http"

	acp "github.com/ironpark/go-acp"
	"github.com/ironpark/go-acp/acpv1"
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
	return &acpv1.PromptResponse{StopReason: acpv1.StopReasonEndTurn}, nil
}

func (a *echoAgent) Cancel(context.Context, *acpv1.CancelNotification) error { return nil }

func main() {
	addr := flag.String("addr", "localhost:8000", "address to listen on")
	flag.Parse()

	// serve runs once per connection, with that connection's transport.
	server := acp.NewHTTPServer(func(ctx context.Context, t acp.Transport) error {
		conn := acpv1.NewAgentSideConnection(func(c *acpv1.AgentSideConnection) acpv1.Agent {
			return &echoAgent{client: c}
		}, nil, nil, acp.WithTransport(t))
		return conn.Start(ctx)
	})
	defer server.Close()

	mux := http.NewServeMux()
	mux.Handle("/acp", server)
	// The protocol asks for HTTP/2; plain-text HTTP/2 needs it enabled, and
	// HTTP/1.1 keeps working for clients without it.
	httpServer := &http.Server{Addr: *addr, Handler: mux, Protocols: new(http.Protocols)}
	httpServer.Protocols.SetHTTP1(true)
	httpServer.Protocols.SetUnencryptedHTTP2(true)
	log.Printf("serving an ACP agent on http://%s/acp", *addr)
	log.Fatal(httpServer.ListenAndServe())
}
