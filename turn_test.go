package acp

import (
	"context"
	"errors"
	"testing"
)

func TestTurnTrackerBegin(t *testing.T) {
	var turns TurnTracker[string]
	ctx, done, err := turns.Begin(context.Background(), "s1")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := turns.Begin(context.Background(), "s1"); !errors.Is(err, ErrTurnInProgress) {
		t.Fatalf("second Begin: %v", err)
	}
	if ctx.Err() != nil {
		t.Fatal("a refused Begin must not disturb the running turn")
	}
	if !turns.Cancel("s1") || context.Cause(ctx) != ErrTurnCancelled {
		t.Fatalf("cause = %v", context.Cause(ctx))
	}
	done()
	if turns.Cancel("s1") {
		t.Fatal("cancelled a finished turn")
	}
	if _, done, err := turns.Begin(context.Background(), "s1"); err != nil {
		t.Fatalf("session not freed: %v", err)
	} else {
		done()
	}
}

func TestTurnTrackerJoin(t *testing.T) {
	var turns TurnTracker[string]
	first, done, joined := turns.Join(context.Background(), "s")
	if joined {
		t.Fatal("first Join should start the turn")
	}
	second, noop, joined := turns.Join(context.Background(), "s")
	if !joined || second != first {
		t.Fatal("second Join should return the running turn")
	}
	noop()
	if first.Err() != nil {
		t.Fatal("a joined caller's done must not end the turn")
	}
	done()
	if first.Err() == nil {
		t.Fatal("the starter's done should end the turn")
	}
}
