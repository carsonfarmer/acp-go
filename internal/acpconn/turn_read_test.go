package acpconn

import (
	"slices"
	"testing"
)

func TestTurnBuffersUpdatesForALateReader(t *testing.T) {
	turn := NewTurn[int, string]()
	turn.Push(1)
	turn.Push(2)
	turn.Finish("done", nil)
	turn.Push(3) // dropped: the turn has ended

	var got []int
	for u := range turn.Updates() {
		got = append(got, u)
	}
	if !slices.Equal(got, []int{1, 2}) {
		t.Fatalf("Updates yielded %v, want [1 2]", got)
	}
	if result, err := turn.Wait(); result != "done" || err != nil {
		t.Fatalf("Wait = %q, %v", result, err)
	}
	select {
	case <-turn.Done():
	default:
		t.Fatal("Done is not closed after the turn ended")
	}
}

func TestTurnStreamsToAWaitingReader(t *testing.T) {
	turn := NewTurn[int, string]()
	done := make(chan []int, 1)
	go func() {
		var got []int
		for u := range turn.Updates() {
			got = append(got, u)
		}
		done <- got
	}()
	turn.Push(7)
	turn.Finish("x", nil)
	if got := <-done; !slices.Equal(got, []int{7}) {
		t.Fatalf("Updates yielded %v, want [7]", got)
	}
}

func TestFinishIsIdempotent(t *testing.T) {
	turn := NewTurn[int, string]()
	turn.Finish("first", nil)
	turn.Finish("second", errRejected) // ignored
	if result, err := turn.Wait(); result != "first" || err != nil {
		t.Fatalf("Wait = %q, %v; want the first result", result, err)
	}
}

func TestDeliverOnlyReachesAnActiveSession(t *testing.T) {
	var turns Turns[string, int, string]
	if got := turns.Deliver("s", 1); got != nil {
		t.Fatalf("Deliver on an idle session = %v, want nil", got)
	}
	turn, created := turns.Join("s")
	if !created {
		t.Fatal("Join should create the first turn")
	}
	if got := turns.Deliver("s", 5); got != turn {
		t.Fatalf("Deliver = %p, want the joined turn %p", got, turn)
	}
	turns.End("s", turn, "stop", nil)
	if got := turns.Deliver("s", 6); got != nil {
		t.Fatal("Deliver after End should not reach the finished turn")
	}
	turn.Push(9) // no-op on a finished turn
	if result, err := turn.Wait(); result != "stop" || err != nil {
		t.Fatalf("Wait = %q, %v", result, err)
	}
}
