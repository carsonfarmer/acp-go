package acp

import (
	"context"
	"errors"
	"sync"
)

// ErrTurnCancelled is the cause of a turn context cancelled through
// [TurnTracker.Cancel], which is how an agent tells a client's session/cancel
// apart from other cancellation:
//
//	if context.Cause(ctx) == acp.ErrTurnCancelled {
//		return &acpv1.PromptResponse{StopReason: schema.StopReasonCancelled}, nil
//	}
var ErrTurnCancelled = errors.New("acp: turn cancelled by the client")

// TurnTracker holds a cancellable context for the turn in progress on each
// session, so a cancel notification can stop the work a prompt started. The
// zero value is ready to use and it is safe for concurrent use.
type TurnTracker[ID comparable] struct {
	mu    sync.Mutex
	turns map[ID]*turn
}

type turn struct{ cancel context.CancelCauseFunc }

// Begin starts a turn on a session and returns its context and a done func to
// call when the turn ends. A turn still running on the session is cancelled:
// a new prompt supersedes it.
func (t *TurnTracker[ID]) Begin(ctx context.Context, id ID) (context.Context, func()) {
	ctx, cancel := context.WithCancelCause(ctx)
	token := &turn{cancel}
	t.mu.Lock()
	if prev := t.turns[id]; prev != nil {
		prev.cancel(ErrTurnCancelled)
	}
	if t.turns == nil {
		t.turns = map[ID]*turn{}
	}
	t.turns[id] = token
	t.mu.Unlock()
	return ctx, func() {
		t.mu.Lock()
		if t.turns[id] == token {
			delete(t.turns, id)
		}
		t.mu.Unlock()
		cancel(nil)
	}
}

// Cancel cancels the session's turn in progress with [ErrTurnCancelled] and
// reports whether there was one.
func (t *TurnTracker[ID]) Cancel(id ID) bool {
	t.mu.Lock()
	running := t.turns[id]
	delete(t.turns, id)
	t.mu.Unlock()
	if running != nil {
		running.cancel(ErrTurnCancelled)
	}
	return running != nil
}
