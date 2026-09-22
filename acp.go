// Package acp implements the Agent Client Protocol (ACP) v1 for Go.
//
// The wire types are generated from the upstream TypeScript SDK and live in
// [github.com/ironpark/go-acp/schema/v1]; this package adds the JSON-RPC
// runtime and the two connection façades:
//
//   - [AgentSideConnection] serves an [Agent] and calls the peer [Client].
//   - [ClientSideConnection] serves a [Client] and calls the peer [Agent].
//
// Incoming parameters are validated with the SDK's Zod rules before a handler
// sees them, so handlers receive normalized values.
//
// See the protocol docs: https://agentclientprotocol.com
package acp

//go:generate sh -c "cd internal/cmd/schema && go run . -source ../../../schema/typescript -out ../../../schema"

import (
	"context"
	"encoding/json/jsontext"
)

// Agent is the set of methods every ACP agent must handle.
//
// Everything beyond these five methods is optional and gated by a capability
// the agent advertises from [Agent.Initialize]. Implement the matching optional
// interface below and the connection routes the method to it; when it is not
// implemented the peer receives "method not found".
//
// See protocol docs: [Agent](https://agentclientprotocol.com/protocol/overview#agent)
type Agent interface {
	// Initialize negotiates the protocol version and exchanges capabilities.
	//
	// See protocol docs: [Initialization](https://agentclientprotocol.com/protocol/initialization)
	Initialize(ctx context.Context, params *InitializeRequest) (*InitializeResponse, error)

	// Authenticate authenticates the client with one of the advertised methods.
	//
	// See protocol docs: [Authentication](https://agentclientprotocol.com/protocol/authentication)
	Authenticate(ctx context.Context, params *AuthenticateRequest) (*AuthenticateResponse, error)

	// NewSession creates a conversation session with its own context.
	//
	// See protocol docs: [Session Setup](https://agentclientprotocol.com/protocol/session-setup)
	NewSession(ctx context.Context, params *NewSessionRequest) (*NewSessionResponse, error)

	// Prompt runs one prompt turn and returns once it stops.
	//
	// See protocol docs: [Prompt Turn](https://agentclientprotocol.com/protocol/prompt-turn)
	Prompt(ctx context.Context, params *PromptRequest) (*PromptResponse, error)

	// Cancel is a notification asking the agent to abort the current turn.
	// The pending Prompt call should return with StopReasonCancelled.
	//
	// See protocol docs: [Cancellation](https://agentclientprotocol.com/protocol/prompt-turn#cancellation)
	Cancel(ctx context.Context, params *CancelNotification) error
}

// SessionLoader handles session/load. Advertise it with the `loadSession`
// agent capability. When a [SessionStore] is configured and the agent does not
// implement this interface, the store answers the method instead.
type SessionLoader interface {
	LoadSession(ctx context.Context, params *LoadSessionRequest) (*LoadSessionResponse, error)
}

// SessionLister handles session/list. Advertise it with the
// `sessionCapabilities.list` agent capability.
type SessionLister interface {
	ListSessions(ctx context.Context, params *ListSessionsRequest) (*ListSessionsResponse, error)
}

// SessionDeleter handles session/delete. Advertise it with the
// `sessionCapabilities.delete` agent capability.
type SessionDeleter interface {
	DeleteSession(ctx context.Context, params *DeleteSessionRequest) (*DeleteSessionResponse, error)
}

// SessionForker handles session/fork. Advertise it with the
// `sessionCapabilities.fork` agent capability.
//
// **UNSTABLE**: this capability is not part of the spec yet and may change.
type SessionForker interface {
	ForkSession(ctx context.Context, params *ForkSessionRequest) (*ForkSessionResponse, error)
}

// SessionResumer handles session/resume, continuing a session without
// replaying its history. Advertise it with the `sessionCapabilities.resume`
// agent capability.
//
// **UNSTABLE**: this capability is not part of the spec yet and may change.
type SessionResumer interface {
	ResumeSession(ctx context.Context, params *ResumeSessionRequest) (*ResumeSessionResponse, error)
}

// SessionCloser handles session/close. Advertise it with the
// `sessionCapabilities.close` agent capability.
//
// **UNSTABLE**: this capability is not part of the spec yet and may change.
type SessionCloser interface {
	CloseSession(ctx context.Context, params *CloseSessionRequest) (*CloseSessionResponse, error)
}

// SessionModeSetter handles session/set_mode.
//
// See protocol docs: [Session Modes](https://agentclientprotocol.com/protocol/session-modes)
type SessionModeSetter interface {
	SetSessionMode(ctx context.Context, params *SetSessionModeRequest) (*SetSessionModeResponse, error)
}

// SessionConfigOptionSetter handles session/set_config_option. The response
// carries every option and its current value, since changing one option may
// change the others.
type SessionConfigOptionSetter interface {
	SetSessionConfigOption(ctx context.Context, params *SetSessionConfigOptionRequest) (*SetSessionConfigOptionResponse, error)
}

// ProviderManager handles the providers/* methods. Advertise them with the
// `providers` agent capability.
//
// **UNSTABLE**: these methods are not part of the spec yet and may change.
type ProviderManager interface {
	ListProviders(ctx context.Context, params *ListProvidersRequest) (*ListProvidersResponse, error)
	SetProvider(ctx context.Context, params *SetProviderRequest) (*SetProviderResponse, error)
	DisableProvider(ctx context.Context, params *DisableProviderRequest) (*DisableProviderResponse, error)
}

// LogoutHandler handles the logout method, clearing stored credentials.
type LogoutHandler interface {
	Logout(ctx context.Context, params *LogoutRequest) (*LogoutResponse, error)
}

// NesHandler handles the nes/* methods for Next Edit Suggestions. Advertise
// them with the `nes` agent capability. AcceptNes and RejectNes are
// notifications.
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

// Client is the set of methods every ACP client must handle.
//
// File system, terminal and elicitation support are optional; implement the
// matching interface below and advertise the capability from
// `InitializeRequest.ClientCapabilities`.
//
// See protocol docs: [Client](https://agentclientprotocol.com/protocol/overview#client)
type Client interface {
	// SessionUpdate is a notification streaming turn progress to the user.
	//
	// See protocol docs: [Agent Reports Output](https://agentclientprotocol.com/protocol/prompt-turn#3-agent-reports-output)
	SessionUpdate(ctx context.Context, params *SessionNotification) error

	// RequestPermission asks the user to authorize a tool call.
	//
	// When the turn is cancelled the client MUST answer with the cancelled
	// outcome rather than leaving the request pending.
	//
	// See protocol docs: [Requesting Permission](https://agentclientprotocol.com/protocol/tool-calls#requesting-permission)
	RequestPermission(ctx context.Context, params *RequestPermissionRequest) (*RequestPermissionResponse, error)
}

// FileReader handles fs/read_text_file. Advertise it with the
// `fs.readTextFile` client capability.
type FileReader interface {
	ReadTextFile(ctx context.Context, params *ReadTextFileRequest) (*ReadTextFileResponse, error)
}

// FileWriter handles fs/write_text_file. Advertise it with the
// `fs.writeTextFile` client capability.
type FileWriter interface {
	WriteTextFile(ctx context.Context, params *WriteTextFileRequest) (*WriteTextFileResponse, error)
}

// TerminalHandler handles every terminal/* method. Advertise it with the
// `terminal` client capability, which covers all five methods at once.
//
// See protocol docs: [Terminals](https://agentclientprotocol.com/protocol/terminals)
type TerminalHandler interface {
	CreateTerminal(ctx context.Context, params *CreateTerminalRequest) (*CreateTerminalResponse, error)
	TerminalOutput(ctx context.Context, params *TerminalOutputRequest) (*TerminalOutputResponse, error)
	ReleaseTerminal(ctx context.Context, params *ReleaseTerminalRequest) (*ReleaseTerminalResponse, error)
	WaitForTerminalExit(ctx context.Context, params *WaitForTerminalExitRequest) (*WaitForTerminalExitResponse, error)
	KillTerminal(ctx context.Context, params *KillTerminalRequest) (*KillTerminalResponse, error)
}

// ElicitationHandler handles elicitation/create and the elicitation/complete
// notification. Advertise it with the `elicitation` client capability.
type ElicitationHandler interface {
	CreateElicitation(ctx context.Context, params *CreateElicitationRequest) (*CreateElicitationResponse, error)
	CompleteElicitation(ctx context.Context, params *CompleteElicitationNotification) error
}

// ExtMethodHandler handles methods outside the spec, including the mcp/*
// methods, which the reference SDKs also leave to extensions. Prefix custom
// methods with a unique identifier such as a domain name.
//
// See protocol docs: [Extensibility](https://agentclientprotocol.com/protocol/extensibility)
type ExtMethodHandler interface {
	ExtMethod(ctx context.Context, method string, params jsontext.Value) (any, error)
}

// ExtNotificationHandler handles notifications outside the spec.
//
// The connection answers $/cancel_request itself, so it never reaches here.
type ExtNotificationHandler interface {
	ExtNotification(ctx context.Context, method string, params jsontext.Value) error
}
