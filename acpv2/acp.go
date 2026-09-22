// Package acpv2 implements the draft Agent Client Protocol v2 for Go.
//
// ACP v2 is still a draft: its wire protocol and this API may change
// incompatibly in any release. The stable entry point remains the v1 package
// at the module root; import this package to opt in, exactly as the upstream
// TypeScript SDK exposes v2 under an experimental entry point.
//
// The wire types live in [github.com/ironpark/go-acp/schema/v2]; this package
// adds the [AgentSideConnection] and [ClientSideConnection] façades on the
// same JSON-RPC runtime the v1 package uses. Options, transports, middleware
// and [RequestError] are shared types, so one value configures either version.
//
// Compared with v1, v2 moves file system and terminal access behind MCP
// (`mcp/*`), replaces `authenticate` with `auth/login`/`auth/logout`, drops
// `session/load` and `session/set_mode`, and renames the initialize members to
// `info` and `capabilities`. The ProtocolRouter in
// [github.com/ironpark/go-acp/router] serves both versions on one endpoint.
//
// Which methods are required is this package's call: the upstream v2 SDK
// registers handlers per method and enforces nothing. The split below keeps
// the same shape as v1 — the methods every agent or client needs to hold a
// conversation are required, everything gated by a capability is optional.
package acpv2

import (
	"context"
	"encoding/json/jsontext"
)

// Agent is the set of methods every ACP v2 agent must handle. Implement the
// optional interfaces below for the rest; unimplemented methods are answered
// with "method not found".
type Agent interface {
	// Initialize negotiates the protocol version and exchanges capabilities.
	Initialize(ctx context.Context, params *InitializeRequest) (*InitializeResponse, error)

	// NewSession creates a conversation session with its own context.
	NewSession(ctx context.Context, params *NewSessionRequest) (*NewSessionResponse, error)

	// Prompt runs one prompt turn and returns once it stops.
	Prompt(ctx context.Context, params *PromptRequest) (*PromptResponse, error)

	// CancelSession is a notification asking the agent to abort the current
	// turn. The pending Prompt call should return with the cancelled outcome.
	CancelSession(ctx context.Context, params *CancelSessionNotification) error
}

// AuthHandler handles auth/login and auth/logout. Advertise it with the
// `capabilities.auth` agent capability.
type AuthHandler interface {
	Login(ctx context.Context, params *LoginAuthRequest) (*LoginAuthResponse, error)
	Logout(ctx context.Context, params *LogoutAuthRequest) (*LogoutAuthResponse, error)
}

// SessionLister handles session/list.
type SessionLister interface {
	ListSessions(ctx context.Context, params *ListSessionsRequest) (*ListSessionsResponse, error)
}

// SessionDeleter handles session/delete. Advertise it with the
// `capabilities.session.delete` agent capability.
type SessionDeleter interface {
	DeleteSession(ctx context.Context, params *DeleteSessionRequest) (*DeleteSessionResponse, error)
}

// SessionForker handles session/fork. Advertise it with the
// `capabilities.session.fork` agent capability.
//
// **UNSTABLE**: this capability is not part of the spec yet and may change.
type SessionForker interface {
	ForkSession(ctx context.Context, params *ForkSessionRequest) (*ForkSessionResponse, error)
}

// SessionResumer handles session/resume, continuing a session without
// replaying its history.
type SessionResumer interface {
	ResumeSession(ctx context.Context, params *ResumeSessionRequest) (*ResumeSessionResponse, error)
}

// SessionCloser handles session/close.
type SessionCloser interface {
	CloseSession(ctx context.Context, params *CloseSessionRequest) (*CloseSessionResponse, error)
}

// SessionConfigOptionSetter handles session/set_config_option. The response
// carries every option and its current value, since changing one option may
// change the others.
type SessionConfigOptionSetter interface {
	SetSessionConfigOption(ctx context.Context, params *SetSessionConfigOptionRequest) (*SetSessionConfigOptionResponse, error)
}

// ProviderManager handles the providers/* methods. Advertise them with the
// `capabilities.providers` agent capability.
//
// **UNSTABLE**: these methods are not part of the spec yet and may change.
type ProviderManager interface {
	ListProviders(ctx context.Context, params *ListProvidersRequest) (*ListProvidersResponse, error)
	SetProvider(ctx context.Context, params *SetProviderRequest) (*SetProviderResponse, error)
	DisableProvider(ctx context.Context, params *DisableProviderRequest) (*DisableProviderResponse, error)
}

// NesHandler handles the nes/* methods for Next Edit Suggestions. Advertise
// them with the `capabilities.nes` agent capability. AcceptNes and RejectNes
// are notifications.
//
// **UNSTABLE**: these methods are not part of the spec yet and may change.
type NesHandler interface {
	StartNes(ctx context.Context, params *StartNesRequest) (*StartNesResponse, error)
	SuggestNes(ctx context.Context, params *SuggestNesRequest) (*SuggestNesResponse, error)
	CloseNes(ctx context.Context, params *CloseNesRequest) (*CloseNesResponse, error)
	AcceptNes(ctx context.Context, params *AcceptNesNotification) error
	RejectNes(ctx context.Context, params *RejectNesNotification) error
}

// DocumentHandler receives the document/did* notifications that mirror the
// client's open editors.
//
// **UNSTABLE**: these notifications are not part of the spec yet and may change.
type DocumentHandler interface {
	DidOpenDocument(ctx context.Context, params *DidOpenDocumentNotification) error
	DidChangeDocument(ctx context.Context, params *DidChangeDocumentNotification) error
	DidCloseDocument(ctx context.Context, params *DidCloseDocumentNotification) error
	DidSaveDocument(ctx context.Context, params *DidSaveDocumentNotification) error
	DidFocusDocument(ctx context.Context, params *DidFocusDocumentNotification) error
}

// MCPMessageHandler receives MCP traffic the client forwards to the agent over
// mcp/message. The method carries either a request, answered with the MCP
// result, or a notification, which has no response.
//
// **UNSTABLE**: MCP proxying is not part of the spec yet and may change.
type MCPMessageHandler interface {
	MessageMCP(ctx context.Context, params *MessageMCPRequest) (*MessageMCPResponse, error)
	NotifyMCP(ctx context.Context, params *MessageMCPNotification) error
}

// Client is the set of methods every ACP v2 client must handle. MCP and
// elicitation support are optional; implement the matching interface below and
// advertise the capability from `InitializeRequest.Capabilities`.
type Client interface {
	// SessionUpdate is a notification streaming turn progress to the user.
	SessionUpdate(ctx context.Context, params *UpdateSessionNotification) error

	// RequestPermission asks the user to authorize a tool call. When the turn
	// is cancelled the client MUST answer with the cancelled outcome rather
	// than leaving the request pending.
	RequestPermission(ctx context.Context, params *RequestPermissionRequest) (*RequestPermissionResponse, error)
}

// MCPConnector lets the agent reach MCP servers through the client: mcp/connect
// opens a connection, mcp/message carries requests and notifications over it,
// and mcp/disconnect closes it. In v2 this replaces the v1 fs/* and terminal/*
// methods.
//
// **UNSTABLE**: MCP proxying is not part of the spec yet and may change.
type MCPConnector interface {
	ConnectMCP(ctx context.Context, params *ConnectMCPRequest) (*ConnectMCPResponse, error)
	MessageMCP(ctx context.Context, params *MessageMCPRequest) (*MessageMCPResponse, error)
	NotifyMCP(ctx context.Context, params *MessageMCPNotification) error
	DisconnectMCP(ctx context.Context, params *DisconnectMCPRequest) (*DisconnectMCPResponse, error)
}

// ElicitationHandler handles elicitation/create and the elicitation/complete
// notification. Advertise it with the `capabilities.elicitation` client
// capability.
type ElicitationHandler interface {
	CreateElicitation(ctx context.Context, params *CreateElicitationRequest) (*CreateElicitationResponse, error)
	CompleteElicitation(ctx context.Context, params *CompleteElicitationNotification) error
}

// ExtMethodHandler handles methods outside the spec. Prefix custom methods
// with a unique identifier such as a domain name.
type ExtMethodHandler interface {
	ExtMethod(ctx context.Context, method string, params jsontext.Value) (any, error)
}

// ExtNotificationHandler handles notifications outside the spec.
//
// The connection answers $/cancel_request itself, so it never reaches here.
type ExtNotificationHandler interface {
	ExtNotification(ctx context.Context, method string, params jsontext.Value) error
}
