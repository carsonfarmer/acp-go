package acpconn

import (
	"errors"
	"testing"
)

var errRejected = errors.New("rejected")

func TestSettleKeepsATurnAnyRequestAccepted(t *testing.T) {
	for _, starterFirst := range []bool{true, false} {
		var turns Turns[string, int, string]
		first, created := turns.Join("s")
		second, joined := turns.Join("s")
		if !created || joined || first != second {
			t.Fatal("the second request should join the first one's turn")
		}
		// The starter is rejected, before or after the joiner is accepted.
		if starterFirst {
			turns.Settle("s", first, errRejected)
			turns.Settle("s", second, nil)
		} else {
			turns.Settle("s", second, nil)
			turns.Settle("s", first, errRejected)
		}
		select {
		case <-first.Done():
			t.Fatalf("starterFirst=%v: a turn with an accepted request ended on a rejection", starterFirst)
		default:
		}
		turns.End("s", first, "end_turn", nil)
		if result, err := first.Wait(); result != "end_turn" || err != nil {
			t.Fatalf("starterFirst=%v: got %q %v", starterFirst, result, err)
		}
	}
}

func TestSettleEndsATurnEveryRequestRejected(t *testing.T) {
	var turns Turns[string, int, string]
	first, created := turns.Join("s")
	if !created {
		t.Fatal("first Join should create the turn")
	}
	second, _ := turns.Join("s")
	turns.Settle("s", first, errRejected)
	turns.Settle("s", second, errRejected)
	if _, err := first.Wait(); !errors.Is(err, errRejected) {
		t.Fatalf("got %v", err)
	}
	if next, created := turns.Join("s"); !created || next == first {
		t.Fatal("a failed turn must leave the session")
	}
}
