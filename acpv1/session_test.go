package acpv1_test

import (
	"context"
	"errors"
	"testing"

	acp "github.com/ironpark/go-acp"
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
	if _, err := session.Prompt(ctx, acpv1.TextBlock("again")); !errors.Is(err, acp.ErrTurnInProgress) {
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
	if text, _, err := turn.Text(); err != nil || text != "Hello, world" {
		t.Fatalf("got %q %v", text, err)
	}
}

// cancellableAgent relies on the embedded manager's Cancel to stop its turns.
type cancellableAgent struct {
	*acpv1.SessionManager[struct{}]
	started chan struct{}
}

func (cancellableAgent) Initialize(context.Context, *acpv1.InitializeRequest) (*acpv1.InitializeResponse, error) {
	return &acpv1.InitializeResponse{ProtocolVersion: acpv1.ProtocolVersion}, nil
}

func (a cancellableAgent) Prompt(ctx context.Context, params *acpv1.PromptRequest) (*acpv1.PromptResponse, error) {
	ctx, done, err := a.BeginTurn(ctx, params.SessionID)
	if err != nil {
		return nil, err
	}
	defer done()
	close(a.started)
	<-ctx.Done()
	if context.Cause(ctx) == acp.ErrTurnCancelled {
		return &acpv1.PromptResponse{StopReason: schema.StopReasonCancelled}, nil
	}
	return nil, ctx.Err()
}

func TestSessionManagerCancelStopsTheTurn(t *testing.T) {
	agent := cancellableAgent{
		SessionManager: acpv1.NewSessionManager(acpv1.NewMemoryStore[struct{}](),
			func(context.Context, *acpv1.NewSessionRequest) (acpv1.SessionID, struct{}, error) {
				return acpv1.GenerateSessionID(), struct{}{}, nil
			}),
		started: make(chan struct{}),
	}
	_, conn := acpv1.Pipe(t.Context(), func(*acpv1.AgentSideConnection) acpv1.Agent { return agent },
		func(*acpv1.ClientSideConnection) acpv1.Client { return newTestClient() })

	session, err := conn.StartSession(t.Context(), &acpv1.NewSessionRequest{Cwd: "/tmp"})
	if err != nil {
		t.Fatal(err)
	}
	turn, err := session.Prompt(t.Context(), acpv1.TextBlock("work"))
	if err != nil {
		t.Fatal(err)
	}
	<-agent.started
	// A v1 client that ignores the one-turn rule gets an invalid-request error
	// from BeginTurn, and the running turn is left alone. Bypass ClientSession,
	// which would refuse locally.
	_, err = conn.Prompt(t.Context(), &acpv1.PromptRequest{SessionID: session.ID, Prompt: []acpv1.ContentBlock{acpv1.TextBlock("again")}})
	if !acp.IsCode(err, acp.ErrorCodeInvalidRequest) {
		t.Fatalf("overlapping prompt: %v", err)
	}
	if err := session.Cancel(t.Context()); err != nil {
		t.Fatal(err)
	}
	response, err := turn.Wait()
	if err != nil || response.StopReason != schema.StopReasonCancelled {
		t.Fatalf("got %+v %v", response, err)
	}
}
