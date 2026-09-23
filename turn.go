package acp

import (
	"context"
	"sync"

	"github.com/ironpark/acp-go/internal/acpconn"
)

// ErrTurnCancelled is the cause of a turn context cancelled by the client's
// session/cancel, through [TurnTracker.Cancel] or before the turn began, which
// is how an agent tells it apart from other cancellation:
//
//	if context.Cause(ctx) == acp.ErrTurnCancelled {
//		return &acp1.PromptResponse{StopReason: schema.StopReasonCancelled}, nil
//	}
var ErrTurnCancelled = acpconn.ErrTurnCancelled

// ErrTurnInProgress reports a prompt on a session whose turn has not ended.
// v1 allows one prompt turn per session at a time, so both sides refuse it.
var ErrTurnInProgress = acpconn.ErrTurnInProgress

// TurnTracker holds a cancellable context for the turn in progress on each
// session, so a cancel notification can stop the work a prompt started. The
// zero value is ready to use and it is safe for concurrent use.
//
// A turn started from a session/prompt request's context is also cancelled by
// a session/cancel that arrived after the prompt but before the turn began,
// so an agent may do work, such as loading the session, before it starts the
// turn without losing an early cancel.
//
// The two ways to start a turn follow the protocol versions: in v1 a prompt
// occupies the session until it ends, so [TurnTracker.Begin] refuses a second
// one; in v2 a prompt may contribute to foreground work already running, so
// [TurnTracker.Join] hands back the running turn.
type TurnTracker[ID comparable] struct {
	mu    sync.Mutex
	turns map[ID]*turn
}

type turn struct {
	ctx    context.Context
	cancel context.CancelCauseFunc
}

// Begin starts a turn on a session and returns its context and a done func
// to call when the turn ends. It fails with [ErrTurnInProgress] while the
// session has a turn running.
func (t *TurnTracker[ID]) Begin(ctx context.Context, id ID) (context.Context, func(), error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, busy := t.turns[id]; busy {
		return nil, nil, ErrTurnInProgress
	}
	turnCtx, done := t.start(ctx, id)
	return turnCtx, done, nil
}

// Join returns the session's running turn, or starts one when there is none.
// joined reports which: a joined caller gets a no-op done, since the turn
// belongs to the caller that started it.
func (t *TurnTracker[ID]) Join(ctx context.Context, id ID) (turnCtx context.Context, done func(), joined bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if running := t.turns[id]; running != nil {
		return running.ctx, func() {}, true
	}
	turnCtx, done = t.start(ctx, id)
	return turnCtx, done, false
}

// start registers a new turn; t.mu must be held.
func (t *TurnTracker[ID]) start(ctx context.Context, id ID) (context.Context, func()) {
	signal := acpconn.PromptCancelSignal(ctx)
	ctx, cancel := context.WithCancelCause(ctx)
	stop := func() bool { return false }
	if signal != nil {
		if signal.Err() != nil {
			cancel(ErrTurnCancelled)
		} else {
			stop = context.AfterFunc(signal, func() { cancel(ErrTurnCancelled) })
		}
	}
	token := &turn{ctx: ctx, cancel: cancel}
	if t.turns == nil {
		t.turns = map[ID]*turn{}
	}
	t.turns[id] = token
	return ctx, func() {
		t.mu.Lock()
		if t.turns[id] == token {
			delete(t.turns, id)
		}
		t.mu.Unlock()
		stop()
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
