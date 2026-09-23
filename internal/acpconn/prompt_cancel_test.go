package acpconn

import (
	"context"
	"encoding/json/jsontext"
	"errors"
	"testing"
)

func TestSessionIDOf(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{`{"sessionId":"s1"}`, "s1"},
		{`{"sessionId":""}`, ""},
		{`{}`, ""},
		{`not json`, ""},
		{``, ""},
	}
	for _, c := range cases {
		if got := sessionIDOf(jsontext.Value(c.raw)); got != c.want {
			t.Errorf("sessionIDOf(%q) = %q, want %q", c.raw, got, c.want)
		}
	}
}

func TestPromptCancelSignal(t *testing.T) {
	p := &pendingPrompts{sessions: map[string][]*pendingPrompt{}}
	ctx := p.accept(context.Background(), methodSessionPrompt, jsontext.Value(`{"sessionId":"s1"}`))

	signal := PromptCancelSignal(ctx)
	if signal == nil {
		t.Fatal("a prompt request should carry a cancel signal")
	}
	select {
	case <-signal.Done():
		t.Fatal("the signal was cancelled before any session/cancel")
	default:
	}

	p.cancel("s1")
	if cause := context.Cause(signal); !errors.Is(cause, ErrTurnCancelled) {
		t.Fatalf("signal cause = %v, want ErrTurnCancelled", cause)
	}
}

func TestPromptCancelIgnoresOtherMethods(t *testing.T) {
	p := &pendingPrompts{sessions: map[string][]*pendingPrompt{}}
	ctx := p.accept(context.Background(), methodSessionCancel, jsontext.Value(`{"sessionId":"s1"}`))
	if PromptCancelSignal(ctx) != nil {
		t.Fatal("a non-prompt request should not carry a cancel signal")
	}
	if len(p.sessions) != 0 {
		t.Fatalf("a non-prompt request was tracked: %v", p.sessions)
	}
}

func TestAcceptIgnoresPromptWithoutSession(t *testing.T) {
	p := &pendingPrompts{sessions: map[string][]*pendingPrompt{}}
	ctx := p.accept(context.Background(), methodSessionPrompt, jsontext.Value(`{}`))
	if PromptCancelSignal(ctx) != nil {
		t.Fatal("a prompt without a session id should not carry a cancel signal")
	}
	if len(p.sessions) != 0 {
		t.Fatalf("a prompt without a session id was tracked: %v", p.sessions)
	}
}

func TestRemoveDropsOnlyTheNamedPrompt(t *testing.T) {
	p := &pendingPrompts{sessions: map[string][]*pendingPrompt{}}
	p.accept(context.Background(), methodSessionPrompt, jsontext.Value(`{"sessionId":"s1"}`))
	p.accept(context.Background(), methodSessionPrompt, jsontext.Value(`{"sessionId":"s1"}`))

	p.mu.Lock()
	first := p.sessions["s1"][0]
	p.mu.Unlock()

	p.remove("s1", first)
	p.mu.Lock()
	remaining := len(p.sessions["s1"])
	p.mu.Unlock()
	if remaining != 1 {
		t.Fatalf("remove left %d prompts, want 1", remaining)
	}

	p.mu.Lock()
	last := p.sessions["s1"][0]
	p.mu.Unlock()
	p.remove("s1", last)
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.sessions["s1"]; ok {
		t.Fatal("removing the last prompt should drop the session entry")
	}
}
