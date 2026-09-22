package acp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
)

// SessionStore keeps per-session state for an agent. Implementations must be
// safe for concurrent use.
type SessionStore[T any] interface {
	// Get returns the session with the given id, or false if there is none.
	Get(id SessionID) (T, bool)
	// Set stores a session, replacing any session with the same id.
	Set(id SessionID, session T)
	// Delete removes a session. Deleting an unknown id is not an error.
	Delete(id SessionID)
	// List returns the stored session ids.
	List() []SessionID
}

// MemoryStore keeps sessions in memory for the life of the process.
type MemoryStore[T any] struct {
	mu       sync.RWMutex
	sessions map[SessionID]T
}

// NewMemoryStore creates an empty in-memory store.
func NewMemoryStore[T any]() *MemoryStore[T] {
	return &MemoryStore[T]{sessions: make(map[SessionID]T)}
}

func (s *MemoryStore[T]) Get(id SessionID) (T, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	session, ok := s.sessions[id]
	return session, ok
}

func (s *MemoryStore[T]) Set(id SessionID, session T) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[id] = session
}

func (s *MemoryStore[T]) Delete(id SessionID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, id)
}

func (s *MemoryStore[T]) List() []SessionID {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ids := make([]SessionID, 0, len(s.sessions))
	for id := range s.sessions {
		ids = append(ids, id)
	}
	return ids
}

// SessionFactory creates the state for a new session along with its id.
// Use [GenerateSessionID] unless the agent has its own id scheme.
type SessionFactory[T any] func(ctx context.Context, params *NewSessionRequest) (SessionID, T, error)

// SessionManager implements the session lifecycle methods on top of a
// [SessionStore], so an agent can embed it instead of writing them:
//
//	type myAgent struct {
//		*acp.SessionManager[*mySession]
//	}
//
//	agent := &myAgent{SessionManager: acp.NewSessionManager(
//		acp.NewMemoryStore[*mySession](),
//		func(ctx context.Context, params *acp.NewSessionRequest) (acp.SessionID, *mySession, error) {
//			id := acp.GenerateSessionID()
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
		return nil, ErrResourceNotFound(fmt.Sprintf("session %s", params.SessionID))
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
		return nil, ErrResourceNotFound(fmt.Sprintf("session %s", params.SessionID))
	}
	m.store.Delete(params.SessionID)
	return &DeleteSessionResponse{}, nil
}

// GenerateSessionID returns a random id of the form "session_<32 hex chars>".
func GenerateSessionID() SessionID {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("acp: generate session id: " + err.Error())
	}
	return SessionID("session_" + hex.EncodeToString(b))
}
