package acpv1_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ironpark/go-acp/acpv1"
	schema "github.com/ironpark/go-acp/schema/v1"
)

// streamingAgent sends several chunks per turn and blocks the turn until
// release is closed, so tests can observe a turn in progress.
type streamingAgent struct {
	*testAgent
	release chan struct{}
}

func (a *streamingAgent) Prompt(ctx context.Context, params *acpv1.PromptRequest) (*acpv1.PromptResponse, error) {
	stream := acpv1.NewSessionStream(a.client, params.SessionID)
	for _, part := range []string{"Hel", "lo, ", "world"} {
		if err := stream.SendText(ctx, part); err != nil {
			return nil, err
		}
	}
	if err := stream.StartToolCall(ctx, "call_1", "Read", schema.ToolKindRead); err != nil {
		return nil, err
	}
	<-a.release
	return &acpv1.PromptResponse{StopReason: schema.StopReasonEndTurn}, nil
}

func TestTurnCollectsItsUpdates(t *testing.T) {
	agent := &streamingAgent{testAgent: newTestAgent(), release: make(chan struct{})}
	client := newTestClient()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	_, conn := acpv1.Pipe(ctx, func(c *acpv1.AgentSideConnection) acpv1.Agent {
		agent.client = c
		return agent
	}, func(*acpv1.ClientSideConnection) acpv1.Client { return client })

	session, err := conn.StartSession(ctx, &acpv1.NewSessionRequest{Cwd: "/tmp"})
	if err != nil {
		t.Fatal(err)
	}
	turn, err := session.Prompt(ctx, acpv1.TextBlock("hi"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.Prompt(ctx, acpv1.TextBlock("again")); !errors.Is(err, acpv1.ErrTurnInProgress) {
		t.Fatalf("second prompt during a turn: %v", err)
	}
	close(agent.release)

	var tags []string
	for update := range turn.Updates() {
		tags = append(tags, update.Tag())
	}
	want := []string{"agent_message_chunk", "agent_message_chunk", "agent_message_chunk", "tool_call"}
	if len(tags) != len(want) {
		t.Fatalf("got updates %v, want %v", tags, want)
	}
	response, err := turn.Wait()
	if err != nil || response.StopReason != schema.StopReasonEndTurn {
		t.Fatalf("got %+v %v", response, err)
	}
	if len(client.updates) != len(want) {
		t.Fatalf("client saw %d updates, want %d", len(client.updates), len(want))
	}

	// The session is free again, and Text gathers the message chunks.
	agent.release = make(chan struct{})
	close(agent.release)
	turn, err = session.Prompt(ctx, acpv1.TextBlock("hi"))
	if err != nil {
		t.Fatal(err)
	}
	if text, err := turn.Text(); err != nil || text != "Hello, world" {
		t.Fatalf("got %q %v", text, err)
	}
}
