package main

import (
	"context"
	"fmt"

	"github.com/ironpark/acp-go/acp1"
)

// v1Client only receives updates, and each Turn collects its own, so
// UnimplementedClient covers it.
type v1Client struct{ acp1.UnimplementedClient }

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
