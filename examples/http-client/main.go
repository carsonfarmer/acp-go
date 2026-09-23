// Command http-client connects to the http-agent example over Streamable HTTP,
// or over WebSocket with -ws, and runs one prompt turn. Start the agent first:
//
//	go run ./examples/http-agent
//	go run ./examples/http-client
//	go run ./examples/http-client -ws
//	go run ./examples/http-client -reconnect
//	go run ./examples/http-client -token secret  # for http-agent -token secret
//
// Both transports use the same endpoint, and the connection code is the same
// for either: only the transport passed to ConnectAgent differs.
//
// With -reconnect it then drops the connection and resumes the session the
// ACP v1 way: a new connection with the same cookies, initialize, and
// session/load, which replays the session's history.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http/cookiejar"
	"sync/atomic"

	acp "github.com/ironpark/acp-go"
	"github.com/ironpark/acp-go/acp1"
)

// echoClient prints the history a session/load replays; the turns read
// their own updates. An agent that asks for permission or files would get
// "method not found".
type echoClient struct {
	acp1.UnimplementedClient
	loading atomic.Bool
}

func (c *echoClient) SessionUpdate(_ context.Context, params *acp1.SessionNotification) error {
	if !c.loading.Load() {
		return nil
	}
	switch update := params.Update.Variant().(type) {
	case acp1.SessionUpdateUserMessageChunk:
		if text, ok := acp1.TextOf(update.Content); ok {
			fmt.Printf("   history >> %s\n", text)
		}
	case acp1.SessionUpdateAgentMessageChunk:
		if text, ok := acp1.TextOf(update.Content); ok {
			fmt.Printf("   history << %s\n", text)
		}
	}
	return nil
}

func main() {
	url := flag.String("url", "http://localhost:8000/acp", "the agent's ACP endpoint")
	ws := flag.Bool("ws", false, "connect over WebSocket instead of Streamable HTTP")
	reconnect := flag.Bool("reconnect", false, "reconnect and load the session after the first turn")
	token := flag.String("token", "", "bearer token to send, for http-agent -token")
	flag.Parse()
	// Every request carries the same headers, such as credentials, and
	// every connection keeps its cookies in one jar, so a load balancer
	// routes a reconnect to the backend that holds the session.
	jar, _ := cookiejar.New(nil)
	opts := []acp.HTTPClientOption{acp.WithCookieJar(jar)}
	if *token != "" {
		opts = append(opts, acp.WithHTTPHeader("Authorization", "Bearer "+*token))
	}
	if err := run(context.Background(), *url, *ws, *reconnect, opts); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, url string, ws, reconnect bool, opts []acp.HTTPClientOption) error {
	client := &echoClient{}
	agent, initialized, err := connect(ctx, url, ws, opts, client)
	if err != nil {
		return err
	}
	defer func() { agent.Close() }()
	fmt.Printf("initialized (protocol v%d)\n", initialized.ProtocolVersion)

	session, err := agent.StartSession(ctx, &acp1.NewSessionRequest{Cwd: "/"})
	if err != nil {
		return fmt.Errorf("new session: %w", err)
	}
	fmt.Printf("session: %s\n", session.ID)
	greeting := "hello over Streamable HTTP"
	if ws {
		greeting = "hello over WebSocket"
	}
	if err := prompt(ctx, session, greeting); err != nil {
		return err
	}
	if !reconnect {
		return nil
	}

	// Close ends the connection on the server; the session stays.
	agent.Close()
	fmt.Println("reconnecting")
	if agent, initialized, err = connect(ctx, url, ws, opts, client); err != nil {
		return err
	}
	if !initialized.GetAgentCapabilities().GetLoadSession() {
		return errors.New("the agent cannot load sessions")
	}
	// The replay arrives as session updates before LoadSession returns.
	client.loading.Store(true)
	_, err = agent.LoadSession(ctx, &acp1.LoadSessionRequest{SessionID: session.ID, Cwd: "/"})
	client.loading.Store(false)
	if err != nil {
		return fmt.Errorf("load session: %w", err)
	}
	fmt.Printf("loaded session: %s\n", session.ID)
	return prompt(ctx, agent.Session(session.ID), "hello again")
}

// connect opens a connection over Streamable HTTP or WebSocket and
// initializes it. ConnectAgent starts the connection over any transport;
// Close also ends the connection on the server.
func connect(ctx context.Context, url string, ws bool, opts []acp.HTTPClientOption, client acp1.Client) (*acp1.RemoteAgent, *acp1.InitializeResponse, error) {
	// Streamable HTTP needs only the endpoint; a WebSocket is dialed up front.
	var transport acp.Transport = acp.NewHTTPClientTransport(url, opts...)
	if ws {
		var err error
		if transport, err = acp.DialWebSocket(ctx, url, opts...); err != nil {
			return nil, nil, err
		}
	}
	agent := acp1.ConnectAgent(ctx, transport, func(*acp1.ClientSideConnection) acp1.Client { return client })
	initialized, err := agent.Initialize(ctx, &acp1.InitializeRequest{})
	if err != nil {
		agent.Close()
		return nil, nil, fmt.Errorf("initialize: %w", err)
	}
	return agent, initialized, nil
}

func prompt(ctx context.Context, session *acp1.ClientSession, text string) error {
	fmt.Printf(">> %s\n", text)
	turn, err := session.Prompt(ctx, acp1.TextBlock(text))
	if err != nil {
		return fmt.Errorf("prompt: %w", err)
	}
	// Text collects the agent's message chunks until the turn ends.
	reply, result, err := turn.Text()
	if err != nil {
		return fmt.Errorf("prompt: %w", err)
	}
	fmt.Printf("<< %s\nstop reason: %s\n", reply, result.StopReason)
	return nil
}
