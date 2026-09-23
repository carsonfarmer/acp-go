package acpconn

import (
	"errors"
	"testing"
)

var errRejected = errors.New("rejected")

func TestSettleKeepsATurnAnyRequestAccepted(t *testing.T) {
	var turns Turns[string, int, string]
	first, created := turns.Join("s")
	second, joined := turns.Join("s")
	if !created || joined || first != second {
		t.Fatal("the second request should join the first one's turn")
	}
	turns.Settle("s", first, errRejected) // the starter fails first
	turns.Settle("s", second, nil)        // but the joiner was accepted
	select {
	case <-first.Done():
		t.Fatal("a turn with an accepted request ended on a rejection")
	default:
	}
	turns.End("s", first, "end_turn", nil)
	if result, err := first.Wait(); result != "end_turn" || err != nil {
		t.Fatalf("got %q %v", result, err)
	}
}

func TestSettleEndsATurnEveryRequestRejected(t *testing.T) {
	var turns Turns[string, int, string]
	first, _ := turns.Join("s")
	second, _ := turns.Join("s")
	turns.Settle("s", second, nil)
	turns.Settle("s", first, errRejected)
	// Accepted requests keep it; only a turn nobody got into ends.
	third, created := turns.Join("t")
	fourth, _ := turns.Join("t")
	turns.Settle("t", third, errRejected)
	turns.Settle("t", fourth, errRejected)
	if _, err := third.Wait(); !errors.Is(err, errRejected) {
		t.Fatalf("got %v", err)
	}
	if !created {
		t.Fatal("first Join should create the turn")
	}
	if next, created := turns.Join("t"); !created || next == third {
		t.Fatal("a failed turn must leave the session")
	}
	select {
	case <-first.Done():
		t.Fatal("turn s ended despite an accepted request")
	default:
	}
}
