package acp1

import (
	"context"
	"fmt"

	acp "github.com/ironpark/acp-go"
	"github.com/ironpark/acp-go/internal/acpconn"
)

// SessionStore is [acp.SessionStore] keyed by v1 session ids.
type SessionStore[T any] = acp.SessionStore[SessionID, T]

// MemoryStore is [acp.MemoryStore] keyed by v1 session ids.
type MemoryStore[T any] = acp.MemoryStore[SessionID, T]

// NewMemoryStore creates an empty in-memory store.
func NewMemoryStore[T any]() *MemoryStore[T] { return acp.NewMemoryStore[SessionID, T]() }

// FileStore is [acp.FileStore] keyed by v1 session ids.
type FileStore[T any] = acp.FileStore[SessionID, T]

// NewFileStore opens a store in dir and loads the sessions saved there.
func NewFileStore[T any](dir string) (*FileStore[T], error) {
	return acp.NewFileStore[SessionID, T](dir)
}

// GenerateSessionID returns a new id of the form "session_<UUIDv7>".
func GenerateSessionID() SessionID { return SessionID(acp.GenerateSessionID()) }

// GenerateMessageID returns a new id of the form "message_<UUIDv7>".
func GenerateMessageID() MessageID { return MessageID(acp.GenerateID("message")) }

// GenerateToolCallID returns a new id of the form "call_<UUIDv7>".
// Tool call ids must be unique within a session, across its turns.
func GenerateToolCallID() ToolCallID { return ToolCallID(acp.GenerateID("call")) }

// SessionInfoLister is [acp.SessionInfoLister] for v1 session descriptions:
// a store that implements it answers the [SessionManager]'s session/list
// pages itself.
type SessionInfoLister = acp.SessionInfoLister[SessionInfo]

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
// Embedding it satisfies [Agent]'s NewSession and CancelSession plus
// [SessionDeleter], [SessionResumer] and [SessionCloser]; override any of
// them by declaring the method on the agent itself. [CapabilitiesOf]
// advertises what the agent ends up implementing. CancelSession stops the context of
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
	store    SessionStore[T]
	factory  SessionFactory[T]
	turns    acp.TurnTracker[SessionID]
	pageSize int
}

// defaultSessionListPageSize is how many sessions [SessionManager.List] returns
// per page before it hands back a next cursor.
const defaultSessionListPageSize = 100

// SessionManagerOption configures a [SessionManager].
type SessionManagerOption func(*sessionManagerOptions)

type sessionManagerOptions struct{ pageSize int }

// WithSessionListPageSize sets how many sessions [SessionManager.List] returns
// per page before it hands back a next cursor, which it caps at this size. The
// default is [defaultSessionListPageSize]; zero or less returns every match in
// one page with no cursor.
func WithSessionListPageSize(size int) SessionManagerOption {
	return func(o *sessionManagerOptions) { o.pageSize = size }
}

// NewSessionManager pairs a store with the factory that fills it.
func NewSessionManager[T any](store SessionStore[T], factory SessionFactory[T], opts ...SessionManagerOption) *SessionManager[T] {
	o := sessionManagerOptions{pageSize: defaultSessionListPageSize}
	for _, opt := range opts {
		opt(&o)
	}
	return &SessionManager[T]{store: store, factory: factory, pageSize: o.pageSize}
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
		return session, acp.ResourceNotFound(fmt.Sprintf("session %s", id))
	}
	return session, nil
}

// RunTurn answers a prompt with one turn on the session: it looks the session
// up, begins its turn, runs run with the turn's context and ends the turn.
// The response carries the stop reason run returns, or [StopReasonCancelled]
// once [SessionManager.CancelSession] has cancelled the turn, whatever run returned,
// as the protocol asks. An unknown session, an overlapping prompt
// ([acp.ErrTurnInProgress]) and any other error from run are returned as is:
//
//	func (a *myAgent) Prompt(ctx context.Context, params *acp1.PromptRequest) (*acp1.PromptResponse, error) {
//		return a.RunTurn(ctx, params.SessionID, func(ctx context.Context, s *session) (acp1.StopReason, error) {
//			// ... stream updates with ctx ...
//			return acp1.StopReasonEndTurn, nil
//		})
//	}
//
// Use [SessionManager.BeginTurn] for a response with more than a stop reason.
func (m *SessionManager[T]) RunTurn(ctx context.Context, id SessionID, run func(ctx context.Context, session T) (StopReason, error)) (*PromptResponse, error) {
	session, err := m.Lookup(ctx, id)
	if err != nil {
		return nil, err
	}
	turn, done, err := m.BeginTurn(ctx, id)
	if err != nil {
		return nil, err
	}
	defer done()
	reason, err := run(turn, session)
	if context.Cause(turn) == acp.ErrTurnCancelled {
		return &PromptResponse{StopReason: StopReasonCancelled}, nil
	}
	if err != nil {
		return nil, err
	}
	return &PromptResponse{StopReason: reason}, nil
}

// BeginTurn starts a prompt turn on a session. Run the turn's work with the
// returned context, which [SessionManager.CancelSession] cancels with
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
//			return &acp1.PromptResponse{StopReason: acp1.StopReasonCancelled}, nil
//		}
//	}
func (m *SessionManager[T]) BeginTurn(ctx context.Context, id SessionID) (context.Context, func(), error) {
	return m.turns.Begin(ctx, id)
}

// CancelSession cancels the session's turn in progress, if any.
func (m *SessionManager[T]) CancelSession(_ context.Context, params *CancelNotification) error {
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
// the most recently updated sessions first. It paginates with the request's
// cursor, returning at most [WithSessionListPageSize] sessions per page and a
// next cursor while more remain. A store that implements [SessionInfoLister]
// answers each page with one query; any other store has every session read and
// described per page. An agent that lists sessions forwards to it:
//
//	func (a *myAgent) ListSessions(ctx context.Context, params *acp1.ListSessionsRequest) (*acp1.ListSessionsResponse, error) {
//		return a.List(ctx, params)
//	}
func (m *SessionManager[T]) List(ctx context.Context, params *ListSessionsRequest) (*ListSessionsResponse, error) {
	pager, err := acpconn.NewSessionPager(params.GetCursor(), m.pageSize, sessionPosition)
	if err != nil {
		return nil, err
	}
	add := func(info SessionInfo) {
		if params.Cwd == nil || info.Cwd == *params.Cwd {
			pager.Add(info)
		}
	}
	// The lister's rows still pass through add and the pager, which trim the
	// extra row into the next cursor and keep a store that bends the
	// contract from breaking the page order.
	if lister, ok := m.store.(SessionInfoLister); ok {
		sessions, err := lister.ListSessionInfo(ctx, acp.SessionListQuery{
			Cwd:   params.GetCwd(),
			After: (*acp.SessionListPosition)(pager.After()),
			Limit: pager.Limit(),
		})
		if err != nil {
			return nil, err
		}
		for _, info := range sessions {
			add(info)
		}
	} else if err := m.describeSessions(ctx, add); err != nil {
		return nil, err
	}
	page, next := pager.Page()
	response := &ListSessionsResponse{Sessions: page}
	if next != "" {
		response.NextCursor = &next
	}
	return response, nil
}

// describeSessions passes every stored session's [SessionInfoReporter]
// description to add, for a store that is not a [SessionInfoLister].
func (m *SessionManager[T]) describeSessions(ctx context.Context, add func(SessionInfo)) error {
	ids, err := m.store.List(ctx)
	if err != nil {
		return err
	}
	for _, id := range ids {
		session, ok, err := m.store.Get(ctx, id)
		if err != nil {
			return err
		}
		if !ok { // deleted since List
			continue
		}
		reporter, ok := any(session).(SessionInfoReporter)
		if !ok {
			return acp.InternalError("session state does not implement SessionInfoReporter")
		}
		info := reporter.SessionInfo()
		info.SessionID = id
		add(info)
	}
	return nil
}

// sessionPosition is a session's position for [acpconn.SessionPager].
func sessionPosition(info SessionInfo) acpconn.SessionPosition {
	return acpconn.SessionPosition{UpdatedAt: info.GetUpdatedAt(), SessionID: string(info.SessionID)}
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
