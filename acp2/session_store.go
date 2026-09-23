package acp2

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
//		*acp2.SessionManager[*mySession]
//	}
//
//	agent := &myAgent{SessionManager: acp2.NewSessionManager(
//		acp2.NewMemoryStore[*mySession](),
//		func(ctx context.Context, params *acp2.NewSessionRequest) (acp2.SessionID, *mySession, error) {
//			id := acp2.GenerateSessionID()
//			return id, &mySession{id: id, cwd: params.Cwd}, nil
//		},
//	)}
//
// Embedding it satisfies [Agent]'s NewSession and CancelSession plus
// [SessionLister], [SessionDeleter], [SessionResumer] and [SessionCloser];
// override any of them by declaring the method on the agent itself.
// CancelSession stops the context of the turn started with
// [SessionManager.JoinTurn]. v2 has no session/load: session/resume replays
// history instead, which an agent adds by overriding ResumeSession. The agent
// still advertises the matching capabilities from Initialize — the manager
// does not do that for it.
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

// JoinTurn returns the session's foreground work in progress, or starts it.
// In v2 a prompt may contribute to work that is already running, so a prompt
// that arrives mid-turn joins it: joined is true, done is a no-op, and the
// agent folds the new message into the running work. The starter runs the
// work and calls done once it reports idle.
//
// The turn outlives the prompt request, so its context keeps ctx's values but
// not its cancellation; [SessionManager.CancelSession] cancels it with
// [acp.ErrTurnCancelled]:
//
//	func (a *myAgent) Prompt(ctx context.Context, params *acp2.PromptRequest) (*acp2.PromptResponse, error) {
//		turn, done, joined := a.JoinTurn(ctx, params.SessionID)
//		id := a.insert(params) // the user message, now part of the conversation
//		if !joined {
//			go a.run(turn, params.SessionID, done) // report running … idle, then done()
//		}
//		return &acp2.PromptResponse{MessageID: id}, nil
//	}
func (m *SessionManager[T]) JoinTurn(ctx context.Context, id SessionID) (turn context.Context, done func(), joined bool) {
	return m.turns.Join(context.WithoutCancel(ctx), id)
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

// ResumeSession reports whether the session exists, continuing it without a
// replay. Replaying history when the request's ReplayFrom asks
// for it is the agent's job; override this method to do it.
func (m *SessionManager[T]) ResumeSession(_ context.Context, params *ResumeSessionRequest) (*ResumeSessionResponse, error) {
	if _, ok := m.store.Get(params.SessionID); !ok {
		return nil, acp.ErrResourceNotFound(fmt.Sprintf("session %s", params.SessionID))
	}
	return &ResumeSessionResponse{}, nil
}

// CloseSession cancels the session's turn in progress. The session stays in
// the store, so a client can resume it later; DeleteSession removes it.
func (m *SessionManager[T]) CloseSession(_ context.Context, params *CloseSessionRequest) (*CloseSessionResponse, error) {
	if _, ok := m.store.Get(params.SessionID); !ok {
		return nil, acp.ErrResourceNotFound(fmt.Sprintf("session %s", params.SessionID))
	}
	m.turns.Cancel(params.SessionID)
	return &CloseSessionResponse{}, nil
}
