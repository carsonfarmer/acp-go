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

	"github.com/ironpark/go-acp/acpv1"
	schema "github.com/ironpark/go-acp/schema/v1"
)

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
		if err := stream.SendText(ctx, text); err != nil {
			return nil, err
		}
	}
	return &acpv1.PromptResponse{StopReason: schema.StopReasonEndTurn}, nil
}

// Cancel has nothing to stop: Prompt never waits.
func (a *echoAgent) Cancel(context.Context, *acpv1.CancelNotification) error { return nil }

func main() {
	conn := acpv1.NewAgentSideConnection(func(c *acpv1.AgentSideConnection) acpv1.Agent {
		return &echoAgent{client: c}
	}, os.Stdin, os.Stdout)
	if err := conn.Start(context.Background()); err != nil {
		log.Fatal(err)
	}
}
