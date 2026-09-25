package acp2_test

import (
	"context"
	"testing"
	"time"

	"github.com/ironpark/acp-go/acp2"
	schema "github.com/ironpark/acp-go/schema/v2"
)

// panickingAgent runs each turn with StartTurn and work that panics on the
// first prompt only.
type panickingAgent struct {
	*acp2.SessionManager[bareSession]
	client   acp2.Client
	panicked bool
}

func (*panickingAgent) Initialize(context.Context, *acp2.InitializeRequest) (*acp2.InitializeResponse, error) {
	return &acp2.InitializeResponse{ProtocolVersion: acp2.ProtocolVersion}, nil
}

func (a *panickingAgent) Prompt(ctx context.Context, params *acp2.PromptRequest) (*acp2.PromptResponse, error) {
	stream := acp2.NewSessionStream(a.client, params.SessionID)
	if _, err := a.StartTurn(ctx, params.SessionID, stream, func(context.Context, bareSession) acp2.StopReason {
		if !a.panicked {
			a.panicked = true
			panic("boom")
		}
		return acp2.StopReasonEndTurn
	}); err != nil {
		return nil, err
	}
	return &acp2.PromptResponse{MessageID: acp2.GenerateMessageID()}, nil
}

// A panic in a turn's work runs on StartTurn's own goroutine, out of the
// connection's reach: it is recovered, so it neither ends the process nor
// leaves the client's turn waiting for an idle report.
func TestStartTurnRecoversPanickingWork(t *testing.T) {
	agent := &panickingAgent{SessionManager: acp2.NewSessionManager(acp2.NewMemoryStore[bareSession](),
		func(context.Context, *acp2.NewSessionRequest) (acp2.SessionID, bareSession, error) {
			return acp2.GenerateSessionID(), bareSession{}, nil
		},
	)}
	_, conn := acp2.Pipe(t.Context(), func(c *acp2.AgentSideConnection) acp2.Agent {
		agent.client = c
		return agent
	}, func(*acp2.ClientSideConnection) acp2.Client { return newTestClient() })

	session, err := conn.StartSession(t.Context(), &acp2.NewSessionRequest{Cwd: "/tmp"})
	if err != nil {
		t.Fatal(err)
	}
	turn, _, err := session.Prompt(t.Context(), acp2.TextBlock("crash"))
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-turn.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("the turn never ended")
	}
	if reason, err := turn.Wait(); err != nil || reason != schema.StopReasonEndTurn {
		t.Fatalf("turn ended with %q %v, want end_turn", reason, err)
	}

	// The session is free for its next turn.
	next, _, err := session.Prompt(t.Context(), acp2.TextBlock("again"))
	if err != nil {
		t.Fatal(err)
	}
	if reason, err := next.Wait(); err != nil || reason != schema.StopReasonEndTurn {
		t.Fatalf("next turn ended with %q %v", reason, err)
	}
}
