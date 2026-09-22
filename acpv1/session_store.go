package acpv1

import (
	"context"
	"fmt"

	acp "github.com/ironpark/go-acp"
)

// SessionStore is [acp.SessionStore] keyed by v1 session ids.
type SessionStore[T any] = acp.SessionStore[SessionID, T]

// MemoryStore is [acp.MemoryStore] keyed by v1 session ids.
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
//		*acpv1.SessionManager[*mySession]
//	}
//
//	agent := &myAgent{SessionManager: acpv1.NewSessionManager(
//		acpv1.NewMemoryStore[*mySession](),
//		func(ctx context.Context, params *acpv1.NewSessionRequest) (acpv1.SessionID, *mySession, error) {
//			id := acpv1.GenerateSessionID()
//			return id, &mySession{id: id, cwd: params.Cwd}, nil
//		},
//	)}
//
// Embedding it satisfies [Agent]'s NewSession plus [SessionLoader],
// [SessionLister] and [SessionDeleter]; override any of them by declaring the
// method on the agent itself. The agent still advertises the matching
// capabilities from Initialize — the manager does not do that for it.
type SessionManager[T any] struct {
	store   SessionStore[T]
	factory SessionFactory[T]
}

// NewSessionManager pairs a store with the factory that fills it.
func NewSessionManager[T any](store SessionStore[T], factory SessionFactory[T]) *SessionManager[T] {
	return &SessionManager[T]{store: store, factory: factory}
}

// Store returns the underlying store, for state the RPC methods do not cover.
func (m *SessionManager[T]) Store() SessionStore[T] { return m.store }

// Session returns the state for a session id.
func (m *SessionManager[T]) Session(id SessionID) (T, bool) { return m.store.Get(id) }

// NewSession creates a session with the factory and stores it.
func (m *SessionManager[T]) NewSession(ctx context.Context, params *NewSessionRequest) (*NewSessionResponse, error) {
	id, session, err := m.factory(ctx, params)
	if err != nil {
		return nil, err
	}
	m.store.Set(id, session)
	return &NewSessionResponse{SessionID: id}, nil
}

// LoadSession reports whether the session exists. Replaying its history is the
// agent's job; override this method to do it.
func (m *SessionManager[T]) LoadSession(_ context.Context, params *LoadSessionRequest) (*LoadSessionResponse, error) {
	if _, ok := m.store.Get(params.SessionID); !ok {
		return nil, acp.ErrResourceNotFound(fmt.Sprintf("session %s", params.SessionID))
	}
	return &LoadSessionResponse{}, nil
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

// DeleteSession removes a session from the store.
func (m *SessionManager[T]) DeleteSession(_ context.Context, params *DeleteSessionRequest) (*DeleteSessionResponse, error) {
	if _, ok := m.store.Get(params.SessionID); !ok {
		return nil, acp.ErrResourceNotFound(fmt.Sprintf("session %s", params.SessionID))
	}
	m.store.Delete(params.SessionID)
	return &DeleteSessionResponse{}, nil
}
