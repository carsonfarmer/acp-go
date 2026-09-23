package acpconn

import (
	"iter"
	"sync"

	"github.com/ironpark/go-acp/internal/jsonrpc"
)

// ErrTurnInProgress reports a prompt on a session whose previous turn has not
// ended yet. It is an invalid-request error, so an agent can return it to the
// client as is.
var ErrTurnInProgress = jsonrpc.InvalidRequest(nil, "session already has a prompt turn in progress")

// Turn buffers the session updates of one prompt turn for a single reader and
// holds the turn's result once it ends. The buffer is unbounded so that the
// connection's read loop, which delivers updates, never waits on the reader.
type Turn[U, R any] struct {
	mu       sync.Mutex
	queue    []U
	finished bool
	result   R
	err      error
	signal   chan struct{} // wakes a reader waiting for updates
	done     chan struct{} // closed once the turn ends

	// Guarded by the owning Turns' mutex: requests joined but not yet
	// settled, and whether any was accepted.
	pending  int
	accepted bool
}

// NewTurn returns an empty turn.
func NewTurn[U, R any]() *Turn[U, R] {
	return &Turn[U, R]{signal: make(chan struct{}, 1), done: make(chan struct{})}
}

// Push appends an update. Updates pushed after the turn ends are dropped.
func (t *Turn[U, R]) Push(u U) {
	t.mu.Lock()
	if !t.finished {
		t.queue = append(t.queue, u)
	}
	t.mu.Unlock()
	t.wake()
}

// Finish ends the turn with its result. Only the first call has an effect.
func (t *Turn[U, R]) Finish(result R, err error) {
	t.mu.Lock()
	if t.finished {
		t.mu.Unlock()
		return
	}
	t.finished, t.result, t.err = true, result, err
	t.mu.Unlock()
	close(t.done)
	t.wake()
}

func (t *Turn[U, R]) wake() {
	select {
	case t.signal <- struct{}{}:
	default:
	}
}

// Updates yields the turn's updates in arrival order, including those received
// before the call, and stops once the turn has ended and every update has been
// yielded. It is meant for one reader.
func (t *Turn[U, R]) Updates() iter.Seq[U] {
	return func(yield func(U) bool) {
		for {
			t.mu.Lock()
			batch, finished := t.queue, t.finished
			t.queue = nil
			t.mu.Unlock()
			for _, u := range batch {
				if !yield(u) {
					return
				}
			}
			if len(batch) == 0 {
				if finished {
					return
				}
				<-t.signal
			}
		}
	}
}

// Wait blocks until the turn ends and returns its result.
func (t *Turn[U, R]) Wait() (R, error) {
	<-t.done
	return t.result, t.err
}

// Done is closed once the turn ends.
func (t *Turn[U, R]) Done() <-chan struct{} { return t.done }

// Turns tracks the turn in progress on each session of a client connection.
type Turns[ID comparable, U, R any] struct {
	mu     sync.Mutex
	active map[ID]*Turn[U, R]
}

// Begin registers a new turn for a session, failing with ErrTurnInProgress if
// one is already running. Register before sending the prompt so that no
// update of the turn is missed.
func (r *Turns[ID, U, R]) Begin(id ID) (*Turn[U, R], error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, busy := r.active[id]; busy {
		return nil, ErrTurnInProgress
	}
	if r.active == nil {
		r.active = map[ID]*Turn[U, R]{}
	}
	t := NewTurn[U, R]()
	r.active[id] = t
	return t, nil
}

// Join returns the session's turn in progress, or registers a new one;
// created reports which. The caller then sends its request and must report
// the outcome with Settle.
func (r *Turns[ID, U, R]) Join(id ID) (t *Turn[U, R], created bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if t = r.active[id]; t == nil {
		if r.active == nil {
			r.active = map[ID]*Turn[U, R]{}
		}
		t = NewTurn[U, R]()
		r.active[id] = t
		created = true
	}
	t.pending++
	return t, created
}

// Settle records the outcome of a request that joined t. Once one request is
// accepted the turn runs until End; if every request that joined it failed,
// the turn ends with the last error. The turn leaves the session in the same
// step, so a later Join never lands on a turn that is about to fail.
func (r *Turns[ID, U, R]) Settle(id ID, t *Turn[U, R], err error) {
	r.mu.Lock()
	t.pending--
	if err == nil {
		t.accepted = true
	}
	failed := t.pending == 0 && !t.accepted
	if failed && r.active[id] == t {
		delete(r.active, id)
	}
	r.mu.Unlock()
	if failed {
		var zero R
		t.Finish(zero, err)
	}
}

// Deliver pushes an update to the session's turn in progress and returns that
// turn, or nil when the session has none.
func (r *Turns[ID, U, R]) Deliver(id ID, u U) *Turn[U, R] {
	r.mu.Lock()
	t := r.active[id]
	r.mu.Unlock()
	if t != nil {
		t.Push(u)
	}
	return t
}

// End finishes t and, if it is still the session's turn in progress, frees
// the session for the next prompt.
func (r *Turns[ID, U, R]) End(id ID, t *Turn[U, R], result R, err error) {
	r.mu.Lock()
	if r.active[id] == t {
		delete(r.active, id)
	}
	r.mu.Unlock()
	t.Finish(result, err)
}
