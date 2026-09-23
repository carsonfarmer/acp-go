package acp2

import (
	"context"
	"fmt"

	acp "github.com/ironpark/acp-go"
	"github.com/ironpark/acp-go/internal/acpconn"
	schema "github.com/ironpark/acp-go/schema/v2"
)

// SessionStore is [acp.SessionStore] keyed by v2 session ids.
type SessionStore[T any] = acp.SessionStore[SessionID, T]

// MemoryStore is [acp.MemoryStore] keyed by v2 session ids.
type MemoryStore[T any] = acp.MemoryStore[SessionID, T]

// NewMemoryStore creates an empty in-memory store.
func NewMemoryStore[T any]() *MemoryStore[T] { return acp.NewMemoryStore[SessionID, T]() }

// FileStore is [acp.FileStore] keyed by v2 session ids.
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

// SessionInfoLister is [acp.SessionInfoLister] for v2 session descriptions:
// a store that implements it answers the [SessionManager]'s session/list
// pages itself.
type SessionInfoLister = acp.SessionInfoLister[SessionInfo]

// SessionFactory creates the state for a new session along with its id.
// Use [GenerateSessionID] unless the agent has its own id scheme.
type SessionFactory[T any] func(ctx context.Context, params *NewSessionRequest) (SessionID, T, error)

// SessionConfigOptionsReporter is implemented by session state that has
// config options. The [SessionManager] reports them in its session/new and
// session/resume responses; an agent's own ForkSession or ResumeSession
// reports them the same way.
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
//		*acp2.SessionManager[*mySession]
//	}
//
//	agent := &myAgent{SessionManager: acp2.NewSessionManager(
//		acp2.NewMemoryStore[*mySession](),
//		func(ctx context.Context, params *acp2.NewSessionRequest) (acp2.SessionID, *mySession, error) {
//			return acp2.GenerateSessionID(), &mySession{cwd: params.Cwd}, nil
//		},
//	)}
//
// Embedding it satisfies [Agent]'s NewSession and CancelSession plus
// [SessionLister], [SessionDeleter], [SessionResumer] and [SessionCloser], the
// session baseline v2 requires; override any of them by declaring the method
// on the agent itself. [CapabilitiesOf] advertises what the agent ends up
// implementing. CancelSession stops the context of the turn started with
// [SessionManager.JoinTurn]. session/list needs session state that implements
// [SessionInfoReporter]. When the session state implements
// [SessionConfigOptionsReporter], the session/new and session/resume
// responses carry its config options.
//
// v2 has no session/load: session/resume replays the history the agent
// retains when the request's ReplayFrom asks for it. The manager retains no
// history, so its ResumeSession replays nothing; an agent that keeps history
// overrides it.
type SessionManager[T any] struct {
	store    SessionStore[T]
	factory  SessionFactory[T]
	turns    acp.TurnTracker[SessionID]
	pageSize int
}

// defaultSessionListPageSize is how many sessions [SessionManager.ListSessions]
// returns per page before it hands back a next cursor.
const defaultSessionListPageSize = 100

// SessionManagerOption configures a [SessionManager].
type SessionManagerOption func(*sessionManagerOptions)

type sessionManagerOptions struct{ pageSize int }

// WithSessionListPageSize sets how many sessions [SessionManager.ListSessions]
// returns per page before it hands back a next cursor, which it caps at this
// size. The default is [defaultSessionListPageSize]; zero or less returns
// every match in one page with no cursor.
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
		return session, acp.ErrResourceNotFound(fmt.Sprintf("session %s", id))
	}
	return session, nil
}

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
	if err := m.store.Set(ctx, id, session); err != nil {
		return nil, err
	}
	return &NewSessionResponse{SessionID: id, ConfigOptions: configOptions(session)}, nil
}

// ListSessions answers session/list from the store, describing each session
// with its state's [SessionInfoReporter]. It applies the request's cwd filter
// and lists the most recently updated sessions first. It paginates with the
// request's cursor, returning at most [WithSessionListPageSize] sessions per
// page and a next cursor while more remain. A store that implements
// [SessionInfoLister] answers each page with one query; any other store has
// every session read and described per page.
func (m *SessionManager[T]) ListSessions(ctx context.Context, params *ListSessionsRequest) (*ListSessionsResponse, error) {
	pager, err := acpconn.NewSessionPager(string(params.GetCursor()), m.pageSize, sessionPosition)
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
			Cwd:   string(params.GetCwd()),
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
		cursor := schema.SessionListCursor(next)
		response.NextCursor = &cursor
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
			return acp.ErrInternalError(nil, "session state does not implement SessionInfoReporter")
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

// ResumeSession continues a stored session. The manager retains no history,
// so there is nothing to replay whatever the request's ReplayFrom; an agent
// that keeps history overrides this method to replay it.
func (m *SessionManager[T]) ResumeSession(ctx context.Context, params *ResumeSessionRequest) (*ResumeSessionResponse, error) {
	session, err := m.Lookup(ctx, params.SessionID)
	if err != nil {
		return nil, err
	}
	return &ResumeSessionResponse{ConfigOptions: configOptions(session)}, nil
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

// configOptions returns the config options session reports, if any.
func configOptions(session any) []SessionConfigOption {
	if r, ok := session.(SessionConfigOptionsReporter); ok {
		return r.SessionConfigOptions()
	}
	return nil
}
