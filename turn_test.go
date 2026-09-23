package acp

import (
	"context"
	"testing"
)

func TestTurnTracker(t *testing.T) {
	var turns TurnTracker[string]
	ctx, done := turns.Begin(context.Background(), "s1")
	if !turns.Cancel("s1") {
		t.Fatal("no turn to cancel")
	}
	if context.Cause(ctx) != ErrTurnCancelled {
		t.Fatalf("cause = %v", context.Cause(ctx))
	}
	done()
	if turns.Cancel("s1") {
		t.Fatal("cancelled a finished turn")
	}

	first, _ := turns.Begin(context.Background(), "s2")
	second, done2 := turns.Begin(context.Background(), "s2")
	if first.Err() == nil || second.Err() != nil {
		t.Fatal("a new turn should supersede the running one")
	}
	done2()
	if second.Err() == nil {
		t.Fatal("done should release the turn context")
	}
}
