package acp1

import (
	"context"
	"fmt"

	acp "github.com/ironpark/acp-go"
)

// SessionStore is [acp.SessionStore] keyed by v1 session ids.
type SessionStore[T any] = acp.SessionStore[SessionID, T]

// MemoryStore is [acp.MemoryStore] keyed by v1 session ids.
type MemoryStore[T any] = acp.MemoryStore[SessionID, T]

// NewMemoryStore creates an empty in-memory store.
func NewMemoryStore[T any]() *MemoryStore[T] { return acp.NewMemoryStore[SessionID, T]() }

// GenerateSessionID returns a random id of the form "session_<32 hex chars>".
func GenerateSessionID() SessionID { return SessionID(acp.GenerateSessionID()) }

// GenerateMessageID returns a random id of the form "message_<32 hex chars>".
func GenerateMessageID() MessageID { return MessageID(acp.GenerateID("message")) }

// GenerateToolCallID returns a random id of the form "call_<32 hex chars>".
// Tool call ids must be unique within a session, across its turns.
func GenerateToolCallID() ToolCallID { return ToolCallID(acp.GenerateID("call")) }

// SessionFactory creates the state for a new session along with its id.
// Use [GenerateSessionID] unless the agent has its own id scheme.
type SessionFactory[T any] func(ctx context.Context, params *NewSessionRequest) (SessionID, T, error)

// SessionManager implements the session lifecycle methods on top of a
// [SessionStore], so an agent can embed it instead of writing them:
//
//	type myAgent struct {
//		*acp1.SessionManager[*mySession]
//	}
//
//	agent := &myAgent{SessionManager: acp1.NewSessionManager(
//		acp1.NewMemoryStore[*mySession](),
//		func(ctx context.Context, params *acp1.NewSessionRequest) (acp1.SessionID, *mySession, error) {
//			id := acp1.GenerateSessionID()
//			return id, &mySession{id: id, cwd: params.Cwd}, nil
//		},
//	)}
//
// Embedding it satisfies [Agent]'s NewSession and Cancel plus [SessionLoader],
// [SessionLister], [SessionDeleter], [SessionResumer] and [SessionCloser]; override any of them by declaring the
// method on the agent itself. Cancel stops the context of the turn started
// with [SessionManager.BeginTurn]. The agent still advertises the matching
// capabilities from Initialize — the manager does not do that for it.
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

// Lookup returns the state for a session id, or a resource-not-found error to
// return as is when there is no such session.
func (m *SessionManager[T]) Lookup(id SessionID) (T, error) {
	session, ok := m.store.Get(id)
	if !ok {
		return session, acp.ErrResourceNotFound(fmt.Sprintf("session %s", id))
	}
	return session, nil
}

// BeginTurn starts a prompt turn on a session. Run the turn's work with the
// returned context, which [SessionManager.Cancel] cancels with
// [acp.ErrTurnCancelled], and call done when Prompt returns. A v1 session runs
// one turn at a time, so a prompt that overlaps a running turn gets
// [acp.ErrTurnInProgress], an invalid-request error to return as is:
//
//	func (a *myAgent) Prompt(ctx context.Context, params *acp1.PromptRequest) (*acp1.PromptResponse, error) {
//		ctx, done, err := a.BeginTurn(ctx, params.SessionID)
//		if err != nil {
//			return nil, err
//		}
//		defer done()
//		// ... stream updates with ctx ...
//		if context.Cause(ctx) == acp.ErrTurnCancelled {
//			return &acp1.PromptResponse{StopReason: schema.StopReasonCancelled}, nil
//		}
//	}
func (m *SessionManager[T]) BeginTurn(ctx context.Context, id SessionID) (context.Context, func(), error) {
	return m.turns.Begin(ctx, id)
}

// Cancel cancels the session's turn in progress, if any.
func (m *SessionManager[T]) Cancel(_ context.Context, params *CancelNotification) error {
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

// LoadSession reports whether the session exists. Replaying its history is the
// agent's job; override this method to do it.
func (m *SessionManager[T]) LoadSession(_ context.Context, params *LoadSessionRequest) (*LoadSessionResponse, error) {
	if _, err := m.Lookup(params.SessionID); err != nil {
		return nil, err
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

// DeleteSession cancels the session's turn in progress and removes the session
// from the store.
func (m *SessionManager[T]) DeleteSession(_ context.Context, params *DeleteSessionRequest) (*DeleteSessionResponse, error) {
	if _, err := m.Lookup(params.SessionID); err != nil {
		return nil, err
	}
	m.turns.Cancel(params.SessionID)
	m.store.Delete(params.SessionID)
	return &DeleteSessionResponse{}, nil
}

// ResumeSession reports whether the session exists, continuing it without a
// replay. Override it to restore state the store does not
// hold.
func (m *SessionManager[T]) ResumeSession(_ context.Context, params *ResumeSessionRequest) (*ResumeSessionResponse, error) {
	if _, err := m.Lookup(params.SessionID); err != nil {
		return nil, err
	}
	return &ResumeSessionResponse{}, nil
}

// CloseSession cancels the session's turn in progress. The session stays in
// the store, so a client can resume it later; DeleteSession removes it.
func (m *SessionManager[T]) CloseSession(_ context.Context, params *CloseSessionRequest) (*CloseSessionResponse, error) {
	if _, err := m.Lookup(params.SessionID); err != nil {
		return nil, err
	}
	m.turns.Cancel(params.SessionID)
	return &CloseSessionResponse{}, nil
}
