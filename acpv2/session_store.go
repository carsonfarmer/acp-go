package acpv2

import (
	"context"
	"fmt"

	acp "github.com/ironpark/go-acp"
)

// SessionStore is [acp.SessionStore] keyed by v2 session ids.
type SessionStore[T any] = acp.SessionStore[SessionID, T]

// MemoryStore is [acp.MemoryStore] keyed by v2 session ids.
type MemoryStore[T any] = acp.MemoryStore[SessionID, T]

// NewMemoryStore creates an empty in-memory store.
func NewMemoryStore[T any]() *MemoryStore[T] { return acp.NewMemoryStore[SessionID, T]() }

// GenerateSessionID returns a random id of the form "session_<32 hex chars>".
func GenerateSessionID() SessionID { return SessionID(acp.GenerateSessionID()) }

// SessionFactory creates the state for a new session along with its id.
// Use [GenerateSessionID] unless the agent has its own id scheme.
type SessionFactory[T any] func(ctx context.Context, params *NewSessionRequest) (SessionID, T, error)

// SessionManager implements the session lifecycle methods on top of a
// [SessionStore], so an agent can embed it instead of writing them:
//
//	type myAgent struct {
//		*acpv2.SessionManager[*mySession]
//	}
//
//	agent := &myAgent{SessionManager: acpv2.NewSessionManager(
//		acpv2.NewMemoryStore[*mySession](),
//		func(ctx context.Context, params *acpv2.NewSessionRequest) (acpv2.SessionID, *mySession, error) {
//			id := acpv2.GenerateSessionID()
//			return id, &mySession{id: id, cwd: params.Cwd}, nil
//		},
//	)}
//
// Embedding it satisfies [Agent]'s NewSession and CancelSession plus
// [SessionLister] and [SessionDeleter]; override any of them by declaring the
// method on the agent itself. CancelSession stops the context of the turn
// started with [SessionManager.BeginTurn]. There is no session/load in v2. The agent still advertises the
// matching capabilities from Initialize — the manager does not do that for it.
type SessionManager[T any] struct {
	store   SessionStore[T]
	factory SessionFactory[T]
	turns   acp.TurnTracker[SessionID]
}

// NewSessionManager pairs a store with the factory that fills it.
func NewSessionManager[T any](store SessionStore[T], factory SessionFactory[T]) *SessionManager[T] {
	return &SessionManager[T]{store: store, factory: factory}
}

// Store returns the underlying store, for state the RPC methods do not cover.
func (m *SessionManager[T]) Store() SessionStore[T] { return m.store }

// Session returns the state for a session id.
func (m *SessionManager[T]) Session(id SessionID) (T, bool) { return m.store.Get(id) }

// BeginTurn starts a turn of foreground work on a session and returns its
// context and a done func to call once the agent reports idle. A v2 turn
// outlives the prompt request, so the context keeps ctx's values but not its
// cancellation; [SessionManager.CancelSession] cancels it with
// [acp.ErrTurnCancelled]:
//
//	func (a *myAgent) Prompt(ctx context.Context, params *acpv2.PromptRequest) (*acpv2.PromptResponse, error) {
//		turn, done := a.BeginTurn(ctx, params.SessionID)
//		go func() {
//			defer done()
//			// ... stream updates with turn, then report idle ...
//		}()
//		return &acpv2.PromptResponse{MessageID: id}, nil
//	}
func (m *SessionManager[T]) BeginTurn(ctx context.Context, id SessionID) (context.Context, func()) {
	return m.turns.Begin(context.WithoutCancel(ctx), id)
}

// CancelSession cancels the session's turn in progress, if any.
func (m *SessionManager[T]) CancelSession(_ context.Context, params *CancelSessionNotification) error {
	m.turns.Cancel(params.SessionID)
	return nil
}

// NewSession creates a session with the factory and stores it.
func (m *SessionManager[T]) NewSession(ctx context.Context, params *NewSessionRequest) (*NewSessionResponse, error) {
	id, session, err := m.factory(ctx, params)
	if err != nil {
		return nil, err
	}
	m.store.Set(id, session)
	return &NewSessionResponse{SessionID: id}, nil
}

// ListSessions lists the stored sessions. It ignores the request's cwd filter
// and cursor, since the store holds no metadata to filter or page on.
func (m *SessionManager[T]) ListSessions(_ context.Context, _ *ListSessionsRequest) (*ListSessionsResponse, error) {
	ids := m.store.List()
	sessions := make([]SessionInfo, len(ids))
	for i, id := range ids {
		sessions[i] = SessionInfo{SessionID: id}
	}
	return &ListSessionsResponse{Sessions: sessions}, nil
}

// DeleteSession cancels the session's turn in progress and removes the session
// from the store.
func (m *SessionManager[T]) DeleteSession(_ context.Context, params *DeleteSessionRequest) (*DeleteSessionResponse, error) {
	if _, ok := m.store.Get(params.SessionID); !ok {
		return nil, acp.ErrResourceNotFound(fmt.Sprintf("session %s", params.SessionID))
	}
	m.turns.Cancel(params.SessionID)
	m.store.Delete(params.SessionID)
	return &DeleteSessionResponse{}, nil
}
