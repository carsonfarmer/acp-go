package main

import (
	"context"
	"slices"
	"sync"

	acp "github.com/ironpark/acp-go"
	"github.com/ironpark/acp-go/acp2"
)

// v2Agent follows the v2 prompt lifecycle: the prompt response only accepts
// the user message, and the agent reports everything else, including the end
// of the turn, as session updates keyed by message id.
//
// Its embedded SessionManager serves the v2 session baseline: new, list,
// resume, close, delete and cancel. The agent keeps each session's history so
// that session/resume can replay it.
type v2Agent struct {
	*acp2.SessionManager[*v2Session]
	client acp2.Client
}

// v2Session is a session's conversation so far.
type v2Session struct {
	mu      sync.Mutex
	history []v2Exchange
}

// v2Exchange is one prompt and the agent's reply, with their message ids.
type v2Exchange struct {
	userMessage acp2.MessageID
	prompt      []acp2.ContentBlock
	reply       acp2.MessageID
	text        string
}

func newV2Agent(client acp2.Client) *v2Agent {
	return &v2Agent{
		SessionManager: acp2.NewSessionManager(acp2.NewMemoryStore[*v2Session](),
			func(context.Context, *acp2.NewSessionRequest) (acp2.SessionID, *v2Session, error) {
				return acp2.GenerateSessionID(), &v2Session{}, nil
			}),
		client: client,
	}
}

func (a *v2Agent) Initialize(context.Context, *acp2.InitializeRequest) (*acp2.InitializeResponse, error) {
	return &acp2.InitializeResponse{
		ProtocolVersion: acp2.ProtocolVersion,
		Info:            acp2.Implementation{Name: info.name, Version: info.version},
		Capabilities:    acp2.CapabilitiesOf(a),
	}, nil
}

func (a *v2Agent) Prompt(ctx context.Context, params *acp2.PromptRequest) (*acp2.PromptResponse, error) {
	session, err := a.Lookup(params.SessionID)
	if err != nil {
		return nil, err
	}
	userMessage, reply := acp2.GenerateMessageID(), acp2.GenerateMessageID()
	stream := acp2.NewSessionStream(a.client, params.SessionID)

	// The agent must echo the user message it accepted, then report the
	// work: running, the reply, and idle with a stop reason to end the turn.
	// These updates may also follow the response, from another goroutine.
	if err := stream.SendUserMessage(ctx, userMessage, params.Prompt...); err != nil {
		return nil, err
	}
	if err := stream.Running(ctx); err != nil {
		return nil, err
	}
	text := "v2 echo: " + acp2.JoinTexts(params.Prompt)
	if err := stream.SendText(ctx, reply, text); err != nil {
		return nil, err
	}
	session.mu.Lock()
	session.history = append(session.history, v2Exchange{userMessage, params.Prompt, reply, text})
	session.mu.Unlock()
	if err := stream.Idle(ctx, acp2.StopReasonEndTurn); err != nil {
		return nil, err
	}
	return &acp2.PromptResponse{MessageID: userMessage}, nil
}

// ResumeSession continues a session, first replaying its history when the
// client asks to replay from the start. In v2 this replaces session/load.
func (a *v2Agent) ResumeSession(ctx context.Context, params *acp2.ResumeSessionRequest) (*acp2.ResumeSessionResponse, error) {
	response, err := a.SessionManager.ResumeSession(ctx, params) // fails for an unknown session
	if err != nil {
		return nil, err
	}
	switch params.ReplayFrom.Variant().(type) {
	case nil:
		return response, nil // continue without a replay
	case acp2.ReplayFromStart:
	default:
		return nil, acp.ErrInvalidParams("unsupported replay cursor")
	}

	session, _ := a.Session(params.SessionID)
	session.mu.Lock()
	history := slices.Clone(session.history)
	session.mu.Unlock()
	// The replay goes out before the response, the same messages with the
	// same ids, so the client can rebuild the conversation.
	stream := acp2.NewSessionStream(a.client, params.SessionID)
	for _, exchange := range history {
		if err := stream.SendUserMessage(ctx, exchange.userMessage, exchange.prompt...); err != nil {
			return nil, err
		}
		if err := stream.SendText(ctx, exchange.reply, exchange.text); err != nil {
			return nil, err
		}
	}
	return response, nil
}
