package main

import (
	"context"

	"github.com/ironpark/acp-go/acp1"
)

// v1Agent answers each prompt within the session/prompt request.
type v1Agent struct {
	client acp1.Client
}

func (a *v1Agent) Initialize(context.Context, *acp1.InitializeRequest) (*acp1.InitializeResponse, error) {
	return &acp1.InitializeResponse{
		ProtocolVersion: acp1.ProtocolVersion,
		AgentInfo:       &acp1.Implementation{Name: info.name, Version: info.version},
	}, nil
}

func (a *v1Agent) NewSession(context.Context, *acp1.NewSessionRequest) (*acp1.NewSessionResponse, error) {
	return &acp1.NewSessionResponse{SessionID: acp1.GenerateSessionID()}, nil
}

func (a *v1Agent) Prompt(ctx context.Context, params *acp1.PromptRequest) (*acp1.PromptResponse, error) {
	stream := acp1.NewSessionStream(a.client, params.SessionID)
	for text := range acp1.Texts(params.Prompt) {
		if err := stream.SendText(ctx, "v1 echo: "+text); err != nil {
			return nil, err
		}
	}
	return &acp1.PromptResponse{StopReason: acp1.StopReasonEndTurn}, nil
}

func (a *v1Agent) Cancel(context.Context, *acp1.CancelNotification) error { return nil }
