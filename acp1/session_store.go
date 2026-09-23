package acp1

import (
	"cmp"
	"context"
	"fmt"
	"slices"

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

// SessionModesReporter is implemented by session state that has modes. The
// [SessionManager] reports them in its session/new and session/resume
// responses; an agent's own LoadSession or ForkSession reports them the same
// way.
type SessionModesReporter interface {
	SessionModes() *SessionModeState
}

// SessionConfigOptionsReporter is implemented by session state that has
// config options, which the [SessionManager] reports like
// [SessionModesReporter]'s modes.
type SessionConfigOptionsReporter interface {
	SessionConfigOptions() []SessionConfigOption
}

// SessionInfoReporter is implemented by session state that can describe
// itself in a session/list response. SessionInfo must set at least the
// session's working directory; the [SessionManager] sets the id.
type SessionInfoReporter interface {
	SessionInfo() SessionInfo
}

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
//			return acp1.GenerateSessionID(), &mySession{cwd: params.Cwd}, nil
//		},
//	)}
//
// Embedding it satisfies [Agent]'s NewSession and Cancel plus
// [SessionDeleter], [SessionResumer] and [SessionCloser]; override any of
// them by declaring the method on the agent itself. [CapabilitiesOf]
// advertises what the agent ends up implementing. Cancel stops the context of
// the turn started with [SessionManager.BeginTurn]. When the session state
// implements [SessionModesReporter] or [SessionConfigOptionsReporter], the
// session/new and session/resume responses carry its modes and config
// options.
//
// The manager leaves out the two methods it cannot serve from the store
// alone. session/load must replay the conversation, which only the agent
// knows, so an agent that can load implements [SessionLoader] itself.
// session/list is optional in v1 and needs session state that implements
// [SessionInfoReporter], so an agent that lists implements [SessionLister] by
// forwarding to [SessionManager.List].
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

// Lookup returns the state for a session id, or an error to return as is: the
// store's, or resource-not-found when there is no such session.
func (m *SessionManager[T]) Lookup(ctx context.Context, id SessionID) (T, error) {
	session, ok, err := m.store.Get(ctx, id)
	if err != nil {
		return session, err
	}
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
	if err := m.store.Set(ctx, id, session); err != nil {
		return nil, err
	}
	modes, options := sessionState(session)
	return &NewSessionResponse{SessionID: id, Modes: modes, ConfigOptions: options}, nil
}

// List answers session/list from the store, describing each session with its
// state's [SessionInfoReporter]. It applies the request's cwd filter and lists
// the most recently updated sessions first. It ignores the cursor and returns
// every match in one page. An agent that lists sessions forwards to it:
//
//	func (a *myAgent) ListSessions(ctx context.Context, params *acp1.ListSessionsRequest) (*acp1.ListSessionsResponse, error) {
//		return a.List(ctx, params)
//	}
func (m *SessionManager[T]) List(ctx context.Context, params *ListSessionsRequest) (*ListSessionsResponse, error) {
	ids, err := m.store.List(ctx)
	if err != nil {
		return nil, err
	}
	sessions := []SessionInfo{}
	for _, id := range ids {
		session, ok, err := m.store.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		if !ok { // deleted since List
			continue
		}
		reporter, ok := any(session).(SessionInfoReporter)
		if !ok {
			return nil, acp.ErrInternalError(nil, "session state does not implement SessionInfoReporter")
		}
		info := reporter.SessionInfo()
		info.SessionID = id
		if params.Cwd != nil && info.Cwd != *params.Cwd {
			continue
		}
		sessions = append(sessions, info)
	}
	slices.SortFunc(sessions, func(a, b SessionInfo) int {
		return cmp.Or(
			cmp.Compare(b.GetUpdatedAt(), a.GetUpdatedAt()),
			cmp.Compare(a.SessionID, b.SessionID),
		)
	})
	return &ListSessionsResponse{Sessions: sessions}, nil
}

// DeleteSession cancels the session's turn in progress and removes the session
// from the store. Deleting an unknown session succeeds, as the protocol asks.
func (m *SessionManager[T]) DeleteSession(ctx context.Context, params *DeleteSessionRequest) (*DeleteSessionResponse, error) {
	m.turns.Cancel(params.SessionID)
	if err := m.store.Delete(ctx, params.SessionID); err != nil {
		return nil, err
	}
	return &DeleteSessionResponse{}, nil
}

// ResumeSession continues a stored session without replaying its history,
// which is what session/resume means in v1.
func (m *SessionManager[T]) ResumeSession(ctx context.Context, params *ResumeSessionRequest) (*ResumeSessionResponse, error) {
	session, err := m.Lookup(ctx, params.SessionID)
	if err != nil {
		return nil, err
	}
	modes, options := sessionState(session)
	return &ResumeSessionResponse{Modes: modes, ConfigOptions: options}, nil
}

// CloseSession cancels the session's turn in progress. The session stays in
// the store, so a client can resume it later; DeleteSession removes it.
func (m *SessionManager[T]) CloseSession(ctx context.Context, params *CloseSessionRequest) (*CloseSessionResponse, error) {
	if _, err := m.Lookup(ctx, params.SessionID); err != nil {
		return nil, err
	}
	m.turns.Cancel(params.SessionID)
	return &CloseSessionResponse{}, nil
}

// sessionState returns the modes and config options session reports, if any.
func sessionState(session any) (*SessionModeState, []SessionConfigOption) {
	var modes *SessionModeState
	var options []SessionConfigOption
	if r, ok := session.(SessionModesReporter); ok {
		modes = r.SessionModes()
	}
	if r, ok := session.(SessionConfigOptionsReporter); ok {
		options = r.SessionConfigOptions()
	}
	return modes, options
}
