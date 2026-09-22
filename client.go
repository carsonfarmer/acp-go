package acp

import (
	"context"
	"encoding/json/jsontext"
	"io"

	"github.com/ironpark/go-acp/internal/jsonrpc"
	schema "github.com/ironpark/go-acp/schema/v1"
)

// ClientSideConnection is the client's view of an ACP connection.
//
// It serves a [Client] to the peer agent and exposes every agent method for
// outgoing calls, so an editor drives a session entirely through this type.
//
// See protocol docs: [Client](https://agentclientprotocol.com/protocol/overview#client)
type ClientSideConnection struct {
	conn   *jsonrpc.Connection
	client Client
}

var _ Agent = (*ClientSideConnection)(nil)

// NewClientSideConnection connects a client to an agent.
//
// newClient receives the connection being built, so the client can call the
// agent while handling one of its requests:
//
//	conn := acp.NewClientSideConnection(func(c *acp.ClientSideConnection) acp.Client {
//		return &myClient{agent: c}
//	}, agentStdout, agentStdin)
//	go conn.Start(ctx)
//
// reader carries messages from the agent and writer carries messages to it;
// when spawning an agent process those are its stdout and stdin.
//
// See protocol docs: [Communication Model](https://agentclientprotocol.com/protocol/overview#communication-model)
func NewClientSideConnection(newClient func(*ClientSideConnection) Client, reader io.Reader, writer io.Writer, opts ...Option) *ClientSideConnection {
	var o options
	o.apply(opts)

	c := &ClientSideConnection{}
	c.client = newClient(c)
	c.conn = newConnection(c.handleRequest, c.handleNotification, reader, writer, &o)
	return c
}

// Start processes messages until the peer disconnects or ctx is cancelled.
func (c *ClientSideConnection) Start(ctx context.Context) error { return c.conn.Start(ctx) }

// Close shuts the connection down, waiting for in-flight handlers.
func (c *ClientSideConnection) Close() error { return c.conn.Close() }

// Done is closed once the connection stops.
func (c *ClientSideConnection) Done() <-chan struct{} { return c.conn.Done() }

// --- Outgoing calls to the agent ---

// Initialize negotiates the protocol version and exchanges capabilities. It is
// the first call on every connection.
func (c *ClientSideConnection) Initialize(ctx context.Context, params *InitializeRequest) (*InitializeResponse, error) {
	return call[InitializeResponse](ctx, c.conn, schema.AgentMethodsInitialize, params)
}

// Authenticate authenticates with one of the methods the agent advertised.
func (c *ClientSideConnection) Authenticate(ctx context.Context, params *AuthenticateRequest) (*AuthenticateResponse, error) {
	return call[AuthenticateResponse](ctx, c.conn, schema.AgentMethodsAuthenticate, params)
}

// Logout clears the credentials the agent holds.
func (c *ClientSideConnection) Logout(ctx context.Context, params *LogoutRequest) (*LogoutResponse, error) {
	return call[LogoutResponse](ctx, c.conn, schema.AgentMethodsLogout, params)
}

// NewSession creates a session. It may fail with an auth-required error.
func (c *ClientSideConnection) NewSession(ctx context.Context, params *NewSessionRequest) (*NewSessionResponse, error) {
	return call[NewSessionResponse](ctx, c.conn, schema.AgentMethodsSessionNew, params)
}

// LoadSession resumes a session and replays its history as notifications.
// Requires the agent's `loadSession` capability.
func (c *ClientSideConnection) LoadSession(ctx context.Context, params *LoadSessionRequest) (*LoadSessionResponse, error) {
	return call[LoadSessionResponse](ctx, c.conn, schema.AgentMethodsSessionLoad, params)
}

// ListSessions lists sessions, optionally filtered and paginated.
func (c *ClientSideConnection) ListSessions(ctx context.Context, params *ListSessionsRequest) (*ListSessionsResponse, error) {
	return call[ListSessionsResponse](ctx, c.conn, schema.AgentMethodsSessionList, params)
}

// DeleteSession deletes a session and its stored history.
func (c *ClientSideConnection) DeleteSession(ctx context.Context, params *DeleteSessionRequest) (*DeleteSessionResponse, error) {
	return call[DeleteSessionResponse](ctx, c.conn, schema.AgentMethodsSessionDelete, params)
}

// ForkSession branches a session so work continues without touching the
// original history.
//
// **UNSTABLE**: this capability is not part of the spec yet and may change.
func (c *ClientSideConnection) ForkSession(ctx context.Context, params *ForkSessionRequest) (*ForkSessionResponse, error) {
	return call[ForkSessionResponse](ctx, c.conn, schema.AgentMethodsSessionFork, params)
}

// ResumeSession continues a session without replaying its history.
//
// **UNSTABLE**: this capability is not part of the spec yet and may change.
func (c *ClientSideConnection) ResumeSession(ctx context.Context, params *ResumeSessionRequest) (*ResumeSessionResponse, error) {
	return call[ResumeSessionResponse](ctx, c.conn, schema.AgentMethodsSessionResume, params)
}

// CloseSession cancels any ongoing work and frees the session's resources.
//
// **UNSTABLE**: this capability is not part of the spec yet and may change.
func (c *ClientSideConnection) CloseSession(ctx context.Context, params *CloseSessionRequest) (*CloseSessionResponse, error) {
	return call[CloseSessionResponse](ctx, c.conn, schema.AgentMethodsSessionClose, params)
}

// SetSessionMode switches the session between the agent's advertised modes.
func (c *ClientSideConnection) SetSessionMode(ctx context.Context, params *SetSessionModeRequest) (*SetSessionModeResponse, error) {
	return call[SetSessionModeResponse](ctx, c.conn, schema.AgentMethodsSessionSetMode, params)
}

// SetSessionConfigOption sets one configuration option. The response returns
// every option, since one change may affect the others.
func (c *ClientSideConnection) SetSessionConfigOption(ctx context.Context, params *SetSessionConfigOptionRequest) (*SetSessionConfigOptionResponse, error) {
	return call[SetSessionConfigOptionResponse](ctx, c.conn, schema.AgentMethodsSessionSetConfigOption, params)
}

// ListProviders lists the model providers the agent can use.
//
// **UNSTABLE**: this capability is not part of the spec yet and may change.
func (c *ClientSideConnection) ListProviders(ctx context.Context, params *ListProvidersRequest) (*ListProvidersResponse, error) {
	return call[ListProvidersResponse](ctx, c.conn, schema.AgentMethodsProvidersList, params)
}

// SetProvider configures one provider.
//
// **UNSTABLE**: this capability is not part of the spec yet and may change.
func (c *ClientSideConnection) SetProvider(ctx context.Context, params *SetProviderRequest) (*SetProviderResponse, error) {
	return call[SetProviderResponse](ctx, c.conn, schema.AgentMethodsProvidersSet, params)
}

// DisableProvider turns one provider off.
//
// **UNSTABLE**: this capability is not part of the spec yet and may change.
func (c *ClientSideConnection) DisableProvider(ctx context.Context, params *DisableProviderRequest) (*DisableProviderResponse, error) {
	return call[DisableProviderResponse](ctx, c.conn, schema.AgentMethodsProvidersDisable, params)
}

// Prompt runs one prompt turn and returns once the agent stops.
//
// Cancelling ctx cancels the JSON-RPC request; to cancel the turn itself with
// the protocol's own semantics, send [ClientSideConnection.Cancel].
//
// See protocol docs: [Prompt Turn](https://agentclientprotocol.com/protocol/prompt-turn)
func (c *ClientSideConnection) Prompt(ctx context.Context, params *PromptRequest) (*PromptResponse, error) {
	return call[PromptResponse](ctx, c.conn, schema.AgentMethodsSessionPrompt, params)
}

// Cancel asks the agent to end the current turn. The pending Prompt call
// returns with the cancelled stop reason.
//
// See protocol docs: [Cancellation](https://agentclientprotocol.com/protocol/prompt-turn#cancellation)
func (c *ClientSideConnection) Cancel(ctx context.Context, params *CancelNotification) error {
	return c.conn.SendNotification(ctx, schema.AgentMethodsSessionCancel, params)
}

// StartNes starts a Next Edit Suggestions stream.
//
// **UNSTABLE**: this capability is not part of the spec yet and may change.
func (c *ClientSideConnection) StartNes(ctx context.Context, params *StartNesRequest) (*StartNesResponse, error) {
	return call[StartNesResponse](ctx, c.conn, schema.AgentMethodsNesStart, params)
}

// SuggestNes asks for the next edit suggestion.
//
// **UNSTABLE**: this capability is not part of the spec yet and may change.
func (c *ClientSideConnection) SuggestNes(ctx context.Context, params *SuggestNesRequest) (*SuggestNesResponse, error) {
	return call[SuggestNesResponse](ctx, c.conn, schema.AgentMethodsNesSuggest, params)
}

// CloseNes ends a Next Edit Suggestions stream.
//
// **UNSTABLE**: this capability is not part of the spec yet and may change.
func (c *ClientSideConnection) CloseNes(ctx context.Context, params *CloseNesRequest) (*CloseNesResponse, error) {
	return call[CloseNesResponse](ctx, c.conn, schema.AgentMethodsNesClose, params)
}

// AcceptNes reports that the user accepted a suggestion.
//
// **UNSTABLE**: this capability is not part of the spec yet and may change.
func (c *ClientSideConnection) AcceptNes(ctx context.Context, params *AcceptNesNotification) error {
	return c.conn.SendNotification(ctx, schema.AgentMethodsNesAccept, params)
}

// RejectNes reports that the user rejected a suggestion.
//
// **UNSTABLE**: this capability is not part of the spec yet and may change.
func (c *ClientSideConnection) RejectNes(ctx context.Context, params *RejectNesNotification) error {
	return c.conn.SendNotification(ctx, schema.AgentMethodsNesReject, params)
}

// DidOpenDocument tells the agent a document was opened.
//
// **UNSTABLE**: this notification is not part of the spec yet and may change.
func (c *ClientSideConnection) DidOpenDocument(ctx context.Context, params *DidOpenDocumentNotification) error {
	return c.conn.SendNotification(ctx, schema.AgentMethodsDocumentDidOpen, params)
}

// DidChangeDocument tells the agent a document changed.
//
// **UNSTABLE**: this notification is not part of the spec yet and may change.
func (c *ClientSideConnection) DidChangeDocument(ctx context.Context, params *DidChangeDocumentNotification) error {
	return c.conn.SendNotification(ctx, schema.AgentMethodsDocumentDidChange, params)
}

// DidCloseDocument tells the agent a document was closed.
//
// **UNSTABLE**: this notification is not part of the spec yet and may change.
func (c *ClientSideConnection) DidCloseDocument(ctx context.Context, params *DidCloseDocumentNotification) error {
	return c.conn.SendNotification(ctx, schema.AgentMethodsDocumentDidClose, params)
}

// DidSaveDocument tells the agent a document was saved.
//
// **UNSTABLE**: this notification is not part of the spec yet and may change.
func (c *ClientSideConnection) DidSaveDocument(ctx context.Context, params *DidSaveDocumentNotification) error {
	return c.conn.SendNotification(ctx, schema.AgentMethodsDocumentDidSave, params)
}

// DidFocusDocument tells the agent a document was focused.
//
// **UNSTABLE**: this notification is not part of the spec yet and may change.
func (c *ClientSideConnection) DidFocusDocument(ctx context.Context, params *DidFocusDocumentNotification) error {
	return c.conn.SendNotification(ctx, schema.AgentMethodsDocumentDidFocus, params)
}

// ExtMethod sends a request outside the spec and returns its raw result.
func (c *ClientSideConnection) ExtMethod(ctx context.Context, method string, params any) (jsontext.Value, error) {
	return c.conn.SendRequest(ctx, method, params)
}

// ExtNotification sends a notification outside the spec.
func (c *ClientSideConnection) ExtNotification(ctx context.Context, method string, params any) error {
	return c.conn.SendNotification(ctx, method, params)
}

// --- Incoming client methods ---

func (c *ClientSideConnection) handleRequest(ctx context.Context, method string, params jsontext.Value) (any, error) {
	switch method {
	case schema.ClientMethodsSessionRequestPermission:
		return request(ctx, params, c.client.RequestPermission)

	case schema.ClientMethodsFsReadTextFile:
		if reader, ok := c.client.(FileReader); ok {
			return request(ctx, params, reader.ReadTextFile)
		}
	case schema.ClientMethodsFsWriteTextFile:
		if writer, ok := c.client.(FileWriter); ok {
			return request(ctx, params, writer.WriteTextFile)
		}

	case schema.ClientMethodsTerminalCreate:
		if terminals, ok := c.client.(TerminalHandler); ok {
			return request(ctx, params, terminals.CreateTerminal)
		}
	case schema.ClientMethodsTerminalOutput:
		if terminals, ok := c.client.(TerminalHandler); ok {
			return request(ctx, params, terminals.TerminalOutput)
		}
	case schema.ClientMethodsTerminalRelease:
		if terminals, ok := c.client.(TerminalHandler); ok {
			return request(ctx, params, terminals.ReleaseTerminal)
		}
	case schema.ClientMethodsTerminalWaitForExit:
		if terminals, ok := c.client.(TerminalHandler); ok {
			return request(ctx, params, terminals.WaitForTerminalExit)
		}
	case schema.ClientMethodsTerminalKill:
		if terminals, ok := c.client.(TerminalHandler); ok {
			return request(ctx, params, terminals.KillTerminal)
		}

	case schema.ClientMethodsElicitationCreate:
		if elicit, ok := c.client.(ElicitationHandler); ok {
			return request(ctx, params, elicit.CreateElicitation)
		}

	default:
		// Extension methods, including mcp/*, which the reference SDKs also
		// leave to extensions.
		if handler, ok := c.client.(ExtMethodHandler); ok {
			return handler.ExtMethod(ctx, method, params)
		}
	}
	return nil, jsonrpc.MethodNotFound(method)
}

func (c *ClientSideConnection) handleNotification(ctx context.Context, method string, params jsontext.Value) error {
	switch method {
	case schema.ClientMethodsSessionUpdate:
		return notify(ctx, params, c.client.SessionUpdate)

	case schema.ClientMethodsElicitationComplete:
		if elicit, ok := c.client.(ElicitationHandler); ok {
			return notify(ctx, params, elicit.CompleteElicitation)
		}

	default:
		if handler, ok := c.client.(ExtNotificationHandler); ok {
			return handler.ExtNotification(ctx, method, params)
		}
	}
	return jsonrpc.MethodNotFound(method)
}
