// Command inprocess runs an agent and a client in one process, connected in
// memory with acp1.Pipe instead of a child process's stdio:
//
//	go run ./examples/inprocess
//
// The agent and client code is the same as over any transport. Use this to
// test an agent or a client without spawning anything, or to embed an agent
// in an application that also drives it.
package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/ironpark/acp-go/acp1"
)

// shoutAgent answers each prompt in capitals.
type shoutAgent struct {
	client acp1.Client
}

func (a *shoutAgent) Initialize(context.Context, *acp1.InitializeRequest) (*acp1.InitializeResponse, error) {
	return &acp1.InitializeResponse{ProtocolVersion: acp1.ProtocolVersion}, nil
}

func (a *shoutAgent) NewSession(context.Context, *acp1.NewSessionRequest) (*acp1.NewSessionResponse, error) {
	return &acp1.NewSessionResponse{SessionID: acp1.GenerateSessionID()}, nil
}

func (a *shoutAgent) Prompt(ctx context.Context, params *acp1.PromptRequest) (*acp1.PromptResponse, error) {
	stream := acp1.NewSessionStream(a.client, params.SessionID)
	for text := range acp1.Texts(params.Prompt) {
		if err := stream.SendText(ctx, strings.ToUpper(text)); err != nil {
			return nil, err
		}
	}
	return &acp1.PromptResponse{StopReason: acp1.StopReasonEndTurn}, nil
}

func (a *shoutAgent) CancelSession(context.Context, *acp1.CancelNotification) error { return nil }

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // stops both sides

	// Pipe starts both connections; each side gets its peer's calls.
	_, agent := acp1.Pipe(ctx,
		func(c *acp1.AgentSideConnection) acp1.Agent { return &shoutAgent{client: c} },
		// Each Turn collects its own updates, so the client needs no code.
		func(*acp1.ClientSideConnection) acp1.Client { return acp1.UnimplementedClient{} })

	if _, err := agent.Initialize(ctx, &acp1.InitializeRequest{}); err != nil {
		log.Fatal(err)
	}
	session, err := agent.StartSession(ctx, &acp1.NewSessionRequest{Cwd: "/"})
	if err != nil {
		log.Fatal(err)
	}
	for _, prompt := range []string{"hello", "same process, no child"} {
		turn, err := session.Prompt(ctx, acp1.TextBlock(prompt))
		if err != nil {
			log.Fatal(err)
		}
		text, _, err := turn.Text()
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf(">> %s\n<< %s\n", prompt, text)
	}
}
