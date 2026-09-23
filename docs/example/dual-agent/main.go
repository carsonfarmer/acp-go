// Command dual-agent serves the echo agent in both ACP v1 and the draft v2
// from one binary.
//
// router.ProtocolRouter reads the client's initialize request and hands the
// connection to the highest version both sides support. Each version has its
// own façade package and its own agent type; they share nothing but the
// router. Try it with the dual-client example, which prefers v2, and the
// client example, which speaks v1.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync/atomic"

	"github.com/ironpark/go-acp/acpv1"
	"github.com/ironpark/go-acp/acpv2"
	"github.com/ironpark/go-acp/router"
)

var info = struct{ name, version string }{"dual-agent", "0.1.0"}

// v1Agent answers each prompt within the session/prompt request.
type v1Agent struct {
	client acpv1.Client
}

func (a *v1Agent) Initialize(context.Context, *acpv1.InitializeRequest) (*acpv1.InitializeResponse, error) {
	return &acpv1.InitializeResponse{
		ProtocolVersion: acpv1.ProtocolVersion,
		AgentInfo:       &acpv1.Implementation{Name: info.name, Version: info.version},
	}, nil
}

func (a *v1Agent) NewSession(context.Context, *acpv1.NewSessionRequest) (*acpv1.NewSessionResponse, error) {
	return &acpv1.NewSessionResponse{SessionID: acpv1.GenerateSessionID()}, nil
}

func (a *v1Agent) Prompt(ctx context.Context, params *acpv1.PromptRequest) (*acpv1.PromptResponse, error) {
	stream := acpv1.NewSessionStream(a.client, params.SessionID)
	for text := range acpv1.Texts(params.Prompt) {
		if err := stream.SendText(ctx, "v1 echo: "+text); err != nil {
			return nil, err
		}
	}
	return &acpv1.PromptResponse{StopReason: acpv1.StopReasonEndTurn}, nil
}

func (a *v1Agent) Cancel(context.Context, *acpv1.CancelNotification) error { return nil }

// v2Agent follows the v2 prompt lifecycle: the prompt response only accepts
// the user message, and the agent reports everything else, including the end
// of the turn, as session updates keyed by message id.
type v2Agent struct {
	client   acpv2.Client
	messages atomic.Int64
}

func (a *v2Agent) Initialize(context.Context, *acpv2.InitializeRequest) (*acpv2.InitializeResponse, error) {
	return &acpv2.InitializeResponse{
		ProtocolVersion: acpv2.ProtocolVersion,
		Info:            acpv2.Implementation{Name: info.name, Version: info.version},
	}, nil
}

func (a *v2Agent) NewSession(context.Context, *acpv2.NewSessionRequest) (*acpv2.NewSessionResponse, error) {
	return &acpv2.NewSessionResponse{SessionID: acpv2.GenerateSessionID()}, nil
}

func (a *v2Agent) Prompt(ctx context.Context, params *acpv2.PromptRequest) (*acpv2.PromptResponse, error) {
	userMessage := a.nextMessageID("user")
	reply := a.nextMessageID("agent")
	stream := acpv2.NewSessionStream(a.client, params.SessionID)

	// The agent must echo the user message it accepted, then report the
	// work: running, the reply, and idle with a stop reason to end the turn.
	// These updates may also follow the response, from another goroutine.
	if err := stream.SendUserMessage(ctx, userMessage, params.Prompt...); err != nil {
		return nil, err
	}
	if err := stream.Running(ctx); err != nil {
		return nil, err
	}
	for text := range acpv2.Texts(params.Prompt) {
		if err := stream.SendText(ctx, reply, "v2 echo: "+text); err != nil {
			return nil, err
		}
	}
	if err := stream.Idle(ctx, acpv2.StopReasonEndTurn); err != nil {
		return nil, err
	}
	return &acpv2.PromptResponse{MessageID: userMessage}, nil
}

func (a *v2Agent) CancelSession(context.Context, *acpv2.CancelSessionNotification) error {
	return nil
}

func (a *v2Agent) nextMessageID(role string) acpv2.MessageID {
	return acpv2.MessageID(fmt.Sprintf("%s_%d", role, a.messages.Add(1)))
}

func main() {
	r := router.New().
		WithV1(func(c *acpv1.AgentSideConnection) acpv1.Agent { return &v1Agent{client: c} }).
		WithV2(func(c *acpv2.AgentSideConnection) acpv2.Agent { return &v2Agent{client: c} })
	if err := r.ServeStdio(context.Background(), os.Stdin, os.Stdout); err != nil {
		log.Fatal(err)
	}
}
