package main

import (
	"context"
	"fmt"

	acp "github.com/ironpark/go-acp"
	"github.com/ironpark/go-acp/acp1"
)

// v1Client only receives updates; each Turn collects its own.
type v1Client struct{}

func (v1Client) SessionUpdate(context.Context, *acp1.SessionNotification) error { return nil }

func (v1Client) RequestPermission(context.Context, *acp1.RequestPermissionRequest) (*acp1.RequestPermissionResponse, error) {
	return nil, acp.ErrMethodNotFound("session/request_permission")
}

// promptV1 runs one turn in a new session: the prompt response ends it.
func promptV1(ctx context.Context, agent *acp1.RemoteAgent, cwd, prompt string) error {
	session, err := agent.StartSession(ctx, &acp1.NewSessionRequest{Cwd: cwd})
	if err != nil {
		return err
	}
	turn, err := session.Prompt(ctx, acp1.TextBlock(prompt))
	if err != nil {
		return err
	}
	text, response, err := turn.Text()
	if err != nil {
		return err
	}
	fmt.Printf("<< %s\nstop reason: %s\n", text, response.StopReason)
	return nil
}
