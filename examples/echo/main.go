// Command echo is the smallest useful ACP agent: it streams each prompt back
// to the client as an agent message.
//
// It implements only the four methods every agent needs. Start from here, or
// from the agent example for sessions, tool calls and permission requests.
package main

import (
	"context"
	"log"
	"os"

	acp "github.com/ironpark/acp-go"
	"github.com/ironpark/acp-go/acp1"
)

type echoAgent struct {
	client acp1.Client
}

func (a *echoAgent) Initialize(_ context.Context, _ *acp1.InitializeRequest) (*acp1.InitializeResponse, error) {
	return &acp1.InitializeResponse{ProtocolVersion: acp1.ProtocolVersion}, nil
}

func (a *echoAgent) NewSession(_ context.Context, _ *acp1.NewSessionRequest) (*acp1.NewSessionResponse, error) {
	return &acp1.NewSessionResponse{SessionID: acp1.GenerateSessionID()}, nil
}

func (a *echoAgent) Prompt(ctx context.Context, params *acp1.PromptRequest) (*acp1.PromptResponse, error) {
	stream := acp1.NewSessionStream(a.client, params.SessionID)
	for text := range acp1.Texts(params.Prompt) {
		if err := stream.SendText(ctx, text); err != nil {
			return nil, err
		}
	}
	return &acp1.PromptResponse{StopReason: acp1.StopReasonEndTurn}, nil
}

// CancelSession has nothing to stop: Prompt never waits.
func (a *echoAgent) CancelSession(context.Context, *acp1.CancelNotification) error { return nil }

func main() {
	conn := acp1.NewAgentSideConnection(func(c *acp1.AgentSideConnection) acp1.Agent {
		return &echoAgent{client: c}
	}, acp.NewStdioTransport(os.Stdin, os.Stdout))
	if err := conn.Start(context.Background()); err != nil {
		log.Fatal(err)
	}
}
