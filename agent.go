package acp

import (
	"context"
	"encoding/json/jsontext"
	"io"

	"github.com/ironpark/go-acp/internal/jsonrpc"
	schema "github.com/ironpark/go-acp/schema/v1"
)

// AgentSideConnection is the agent's view of an ACP connection.
//
// It serves an [Agent] to the peer and implements [Client] for calls back to
// it, so an agent needs no other handle to stream updates, ask for
// permissions, read files or run terminals.
//
// See protocol docs: [Agent](https://agentclientprotocol.com/protocol/overview#agent)
type AgentSideConnection struct {
	conn  *jsonrpc.Connection
	agent Agent
}

var _ Client = (*AgentSideConnection)(nil)

// NewAgentSideConnection connects an agent to a client.
//
// newAgent receives the connection being built, so the agent can keep it and
// call the client while handling a request:
//
//	conn := acp.NewAgentSideConnection(func(c *acp.AgentSideConnection) acp.Agent {
//		return &myAgent{client: c}
//	}, os.Stdin, os.Stdout)
//	err := conn.Start(ctx)
//
// reader carries messages from the client and writer carries messages to it;
// for a stdio agent those are os.Stdin and os.Stdout.
//
// See protocol docs: [Communication Model](https://agentclientprotocol.com/protocol/overview#communication-model)
func NewAgentSideConnection(newAgent func(*AgentSideConnection) Agent, reader io.Reader, writer io.Writer, opts ...Option) *AgentSideConnection {
	var o options
	o.apply(opts)

	c := &AgentSideConnection{}
	c.agent = newAgent(c)
	c.conn = newConnection(c.handleRequest, c.handleNotification, reader, writer, &o)
	return c
}

// Start processes messages until the peer disconnects or ctx is cancelled.
func (c *AgentSideConnection) Start(ctx context.Context) error { return c.conn.Start(ctx) }

// Close shuts the connection down, waiting for in-flight handlers.
func (c *AgentSideConnection) Close() error { return c.conn.Close() }

// Done is closed once the connection stops.
func (c *AgentSideConnection) Done() <-chan struct{} { return c.conn.Done() }

// Client returns the peer client. The connection itself implements [Client].
func (c *AgentSideConnection) Client() Client { return c }

// --- Outgoing calls to the client ---

// SessionUpdate streams turn progress to the client.
func (c *AgentSideConnection) SessionUpdate(ctx context.Context, params *SessionNotification) error {
	return c.conn.SendNotification(ctx, schema.ClientMethodsSessionUpdate, params)
}

// RequestPermission asks the user to authorize a tool call.
func (c *AgentSideConnection) RequestPermission(ctx context.Context, params *RequestPermissionRequest) (*RequestPermissionResponse, error) {
	return call[RequestPermissionResponse](ctx, c.conn, schema.ClientMethodsSessionRequestPermission, params)
}

// ReadTextFile reads a text file through the client. Requires the client's
// `fs.readTextFile` capability.
func (c *AgentSideConnection) ReadTextFile(ctx context.Context, params *ReadTextFileRequest) (*ReadTextFileResponse, error) {
	return call[ReadTextFileResponse](ctx, c.conn, schema.ClientMethodsFsReadTextFile, params)
}

// WriteTextFile writes a text file through the client. Requires the client's
// `fs.writeTextFile` capability.
func (c *AgentSideConnection) WriteTextFile(ctx context.Context, params *WriteTextFileRequest) (*WriteTextFileResponse, error) {
	return call[WriteTextFileResponse](ctx, c.conn, schema.ClientMethodsFsWriteTextFile, params)
}

// CreateTerminal starts a command in a client-managed terminal. Requires the
// client's `terminal` capability.
func (c *AgentSideConnection) CreateTerminal(ctx context.Context, params *CreateTerminalRequest) (*CreateTerminalResponse, error) {
	return call[CreateTerminalResponse](ctx, c.conn, schema.ClientMethodsTerminalCreate, params)
}

// NewTerminal is CreateTerminal plus a [TerminalHandle] bound to the new
// terminal, which is usually what an agent wants.
func (c *AgentSideConnection) NewTerminal(ctx context.Context, params *CreateTerminalRequest) (*TerminalHandle, error) {
	response, err := c.CreateTerminal(ctx, params)
	if err != nil {
		return nil, err
	}
	return NewTerminalHandle(response.TerminalID, params.SessionID, c), nil
}

// TerminalOutput returns a terminal's output so far without waiting for exit.
func (c *AgentSideConnection) TerminalOutput(ctx context.Context, params *TerminalOutputRequest) (*TerminalOutputResponse, error) {
	return call[TerminalOutputResponse](ctx, c.conn, schema.ClientMethodsTerminalOutput, params)
}

// ReleaseTerminal kills the command if needed and frees the terminal.
func (c *AgentSideConnection) ReleaseTerminal(ctx context.Context, params *ReleaseTerminalRequest) (*ReleaseTerminalResponse, error) {
	return call[ReleaseTerminalResponse](ctx, c.conn, schema.ClientMethodsTerminalRelease, params)
}

// WaitForTerminalExit blocks until the terminal's command exits.
func (c *AgentSideConnection) WaitForTerminalExit(ctx context.Context, params *WaitForTerminalExitRequest) (*WaitForTerminalExitResponse, error) {
	return call[WaitForTerminalExitResponse](ctx, c.conn, schema.ClientMethodsTerminalWaitForExit, params)
}

// KillTerminal kills the command but keeps the terminal id valid.
func (c *AgentSideConnection) KillTerminal(ctx context.Context, params *KillTerminalRequest) (*KillTerminalResponse, error) {
	return call[KillTerminalResponse](ctx, c.conn, schema.ClientMethodsTerminalKill, params)
}

// CreateElicitation asks the client to collect input from the user. Requires
// the client's `elicitation` capability.
func (c *AgentSideConnection) CreateElicitation(ctx context.Context, params *CreateElicitationRequest) (*CreateElicitationResponse, error) {
	return call[CreateElicitationResponse](ctx, c.conn, schema.ClientMethodsElicitationCreate, params)
}

// CompleteElicitation tells the client an elicitation no longer needs an answer.
func (c *AgentSideConnection) CompleteElicitation(ctx context.Context, params *CompleteElicitationNotification) error {
	return c.conn.SendNotification(ctx, schema.ClientMethodsElicitationComplete, params)
}

// ExtMethod sends a request outside the spec and returns its raw result.
func (c *AgentSideConnection) ExtMethod(ctx context.Context, method string, params any) (jsontext.Value, error) {
	return c.conn.SendRequest(ctx, method, params)
}

// ExtNotification sends a notification outside the spec.
func (c *AgentSideConnection) ExtNotification(ctx context.Context, method string, params any) error {
	return c.conn.SendNotification(ctx, method, params)
}

// --- Incoming agent methods ---

func (c *AgentSideConnection) handleRequest(ctx context.Context, method string, params jsontext.Value) (any, error) {
	switch method {
	case schema.AgentMethodsInitialize:
		return request(ctx, params, c.agent.Initialize)
	case schema.AgentMethodsAuthenticate:
		return request(ctx, params, c.agent.Authenticate)
	case schema.AgentMethodsSessionPrompt:
		return request(ctx, params, c.agent.Prompt)

	case schema.AgentMethodsSessionNew:
		return request(ctx, params, c.agent.NewSession)

	case schema.AgentMethodsSessionLoad:
		if loader, ok := c.agent.(SessionLoader); ok {
			return request(ctx, params, loader.LoadSession)
		}
	case schema.AgentMethodsSessionList:
		if lister, ok := c.agent.(SessionLister); ok {
			return request(ctx, params, lister.ListSessions)
		}
	case schema.AgentMethodsSessionDelete:
		if deleter, ok := c.agent.(SessionDeleter); ok {
			return request(ctx, params, deleter.DeleteSession)
		}

	case schema.AgentMethodsSessionFork:
		if forker, ok := c.agent.(SessionForker); ok {
			return request(ctx, params, forker.ForkSession)
		}
	case schema.AgentMethodsSessionResume:
		if resumer, ok := c.agent.(SessionResumer); ok {
			return request(ctx, params, resumer.ResumeSession)
		}
	case schema.AgentMethodsSessionClose:
		if closer, ok := c.agent.(SessionCloser); ok {
			return request(ctx, params, closer.CloseSession)
		}
	case schema.AgentMethodsSessionSetMode:
		if setter, ok := c.agent.(SessionModeSetter); ok {
			return request(ctx, params, setter.SetSessionMode)
		}
	case schema.AgentMethodsSessionSetConfigOption:
		if setter, ok := c.agent.(SessionConfigOptionSetter); ok {
			return request(ctx, params, setter.SetSessionConfigOption)
		}

	case schema.AgentMethodsProvidersList:
		if providers, ok := c.agent.(ProviderManager); ok {
			return request(ctx, params, providers.ListProviders)
		}
	case schema.AgentMethodsProvidersSet:
		if providers, ok := c.agent.(ProviderManager); ok {
			return request(ctx, params, providers.SetProvider)
		}
	case schema.AgentMethodsProvidersDisable:
		if providers, ok := c.agent.(ProviderManager); ok {
			return request(ctx, params, providers.DisableProvider)
		}
	case schema.AgentMethodsLogout:
		if handler, ok := c.agent.(LogoutHandler); ok {
			return request(ctx, params, handler.Logout)
		}

	case schema.AgentMethodsNesStart:
		if nes, ok := c.agent.(NesHandler); ok {
			return request(ctx, params, nes.StartNes)
		}
	case schema.AgentMethodsNesSuggest:
		if nes, ok := c.agent.(NesHandler); ok {
			return request(ctx, params, nes.SuggestNes)
		}
	case schema.AgentMethodsNesClose:
		if nes, ok := c.agent.(NesHandler); ok {
			return request(ctx, params, nes.CloseNes)
		}

	default:
		// Extension methods, including mcp/*, which the reference SDKs also
		// leave to extensions.
		if handler, ok := c.agent.(ExtMethodHandler); ok {
			return handler.ExtMethod(ctx, method, params)
		}
	}
	return nil, jsonrpc.MethodNotFound(method)
}

func (c *AgentSideConnection) handleNotification(ctx context.Context, method string, params jsontext.Value) error {
	switch method {
	case schema.AgentMethodsSessionCancel:
		return notify(ctx, params, c.agent.Cancel)

	case schema.AgentMethodsNesAccept:
		if nes, ok := c.agent.(NesHandler); ok {
			return notify(ctx, params, nes.AcceptNes)
		}
	case schema.AgentMethodsNesReject:
		if nes, ok := c.agent.(NesHandler); ok {
			return notify(ctx, params, nes.RejectNes)
		}

	case schema.AgentMethodsDocumentDidOpen:
		if docs, ok := c.agent.(DocumentHandler); ok {
			return notify(ctx, params, docs.DidOpenDocument)
		}
	case schema.AgentMethodsDocumentDidChange:
		if docs, ok := c.agent.(DocumentHandler); ok {
			return notify(ctx, params, docs.DidChangeDocument)
		}
	case schema.AgentMethodsDocumentDidClose:
		if docs, ok := c.agent.(DocumentHandler); ok {
			return notify(ctx, params, docs.DidCloseDocument)
		}
	case schema.AgentMethodsDocumentDidSave:
		if docs, ok := c.agent.(DocumentHandler); ok {
			return notify(ctx, params, docs.DidSaveDocument)
		}
	case schema.AgentMethodsDocumentDidFocus:
		if docs, ok := c.agent.(DocumentHandler); ok {
			return notify(ctx, params, docs.DidFocusDocument)
		}

	default:
		if handler, ok := c.agent.(ExtNotificationHandler); ok {
			return handler.ExtNotification(ctx, method, params)
		}
	}
	return jsonrpc.MethodNotFound(method)
}
