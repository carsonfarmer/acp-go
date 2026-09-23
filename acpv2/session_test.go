package acpv2_test

import (
	"context"
	"testing"

	acp "github.com/ironpark/go-acp"
	"github.com/ironpark/go-acp/acpv2"
	schema "github.com/ironpark/go-acp/schema/v2"
)

// v2Agent follows the v2 prompt lifecycle: the prompt response only accepts
// the message, and the turn ends with an idle state update. With idleFirst,
// the whole turn is reported before the response is sent.
type v2Agent struct {
	*testAgent
	idleFirst bool
}

func (a *v2Agent) Prompt(ctx context.Context, params *acpv2.PromptRequest) (*acpv2.PromptResponse, error) {
	stream := acpv2.NewSessionStream(a.client, params.SessionID)
	work := func() {
		ctx := context.Background()
		_ = stream.Running(ctx)
		_ = stream.SendText(ctx, "m1", "draft")
		_ = stream.Send(ctx, schema.NewSessionUpdate(schema.SessionUpdateAgentMessage{MessageID: "m1", Content: []acpv2.ContentBlock{acpv2.TextBlock("Hello")}}))
		_ = stream.SendText(ctx, "m1", ", world")
		_ = stream.Idle(ctx, schema.StopReasonEndTurn)
	}
	if a.idleFirst {
		work()
	} else {
		go work()
	}
	return &acpv2.PromptResponse{MessageID: "user_1"}, nil
}

func TestTurnEndsOnIdle(t *testing.T) {
	for _, idleFirst := range []bool{false, true} {
		agent := &v2Agent{testAgent: newTestAgent(), idleFirst: idleFirst}
		_, conn := acpv2.Pipe(t.Context(), func(c *acpv2.AgentSideConnection) acpv2.Agent {
			agent.client = c
			return agent
		}, func(*acpv2.ClientSideConnection) acpv2.Client { return newTestClient() })

		session, err := conn.StartSession(t.Context(), &acpv2.NewSessionRequest{Cwd: "/tmp"})
		if err != nil {
			t.Fatal(err)
		}
		turn, err := session.Prompt(t.Context(), acpv2.TextBlock("hi"))
		if err != nil {
			t.Fatal(err)
		}
		text, err := turn.Text()
		if err != nil || text != "Hello, world" {
			t.Fatalf("idleFirst=%v: got %q %v", idleFirst, text, err)
		}
		result, _ := turn.Wait()
		if result.MessageID != "user_1" || result.StopReason == nil || *result.StopReason != schema.StopReasonEndTurn {
			t.Fatalf("idleFirst=%v: got %+v", idleFirst, result)
		}
		// The session accepts the next prompt once the turn has ended.
		if next, err := session.Prompt(t.Context(), acpv2.TextBlock("again")); err != nil {
			t.Fatal(err)
		} else if _, err := next.Wait(); err != nil {
			t.Fatal(err)
		}
	}
}

// cancellableAgent runs each turn in the background, as v2 allows, and relies
// on the embedded manager's CancelSession to stop it.
type cancellableAgent struct {
	*acpv2.SessionManager[struct{}]
	client  acpv2.Client
	started chan struct{}
}

func (cancellableAgent) Initialize(context.Context, *acpv2.InitializeRequest) (*acpv2.InitializeResponse, error) {
	return &acpv2.InitializeResponse{ProtocolVersion: acpv2.ProtocolVersion}, nil
}

func (a *cancellableAgent) Prompt(ctx context.Context, params *acpv2.PromptRequest) (*acpv2.PromptResponse, error) {
	turn, done := a.BeginTurn(ctx, params.SessionID)
	stream := acpv2.NewSessionStream(a.client, params.SessionID)
	go func() {
		defer done()
		_ = stream.Running(turn)
		close(a.started)
		<-turn.Done()
		reason := schema.StopReasonEndTurn
		if context.Cause(turn) == acp.ErrTurnCancelled {
			reason = schema.StopReasonCancelled
		}
		_ = stream.Idle(context.Background(), reason)
	}()
	return &acpv2.PromptResponse{MessageID: "user_1"}, nil
}

func TestSessionManagerCancelSessionStopsTheTurn(t *testing.T) {
	agent := &cancellableAgent{
		SessionManager: acpv2.NewSessionManager(acpv2.NewMemoryStore[struct{}](),
			func(context.Context, *acpv2.NewSessionRequest) (acpv2.SessionID, struct{}, error) {
				return acpv2.GenerateSessionID(), struct{}{}, nil
			}),
		started: make(chan struct{}),
	}
	_, conn := acpv2.Pipe(t.Context(), func(c *acpv2.AgentSideConnection) acpv2.Agent {
		agent.client = c
		return agent
	}, func(*acpv2.ClientSideConnection) acpv2.Client { return newTestClient() })

	session, err := conn.StartSession(t.Context(), &acpv2.NewSessionRequest{Cwd: "/tmp"})
	if err != nil {
		t.Fatal(err)
	}
	turn, err := session.Prompt(t.Context(), acpv2.TextBlock("work"))
	if err != nil {
		t.Fatal(err)
	}
	<-agent.started
	if err := session.Cancel(t.Context()); err != nil {
		t.Fatal(err)
	}
	result, err := turn.Wait()
	if err != nil || result.StopReason == nil || *result.StopReason != schema.StopReasonCancelled {
		t.Fatalf("got %+v %v", result, err)
	}
}
