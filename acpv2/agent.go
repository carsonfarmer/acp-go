package acpv2

import (
	"context"
	"encoding/json/jsontext"
	"io"

	"github.com/ironpark/go-acp/internal/acpconn"
	"github.com/ironpark/go-acp/internal/jsonrpc"
	schema "github.com/ironpark/go-acp/schema/v2"
)

// AgentSideConnection is the agent's view of an ACP v2 connection. It serves
// an [Agent] to the peer and implements [Client] for calls back to it.
type AgentSideConnection struct {
	conn  *jsonrpc.Connection
	agent Agent
}

var _ Client = (*AgentSideConnection)(nil)

// NewAgentSideConnection connects an agent to a client. newAgent receives the
// connection being built so the agent can keep it as its [Client]. reader
// carries messages from the client and writer carries messages to it.
func NewAgentSideConnection(newAgent func(*AgentSideConnection) Agent, reader io.Reader, writer io.Writer, opts ...Option) *AgentSideConnection {
	c := &AgentSideConnection{}
	c.agent = newAgent(c)
	c.conn = acpconn.NewConnection(c.handleRequest, c.handleNotification, reader, writer, opts)
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
func (c *AgentSideConnection) SessionUpdate(ctx context.Context, params *UpdateSessionNotification) error {
	return c.conn.SendNotification(ctx, schema.ClientMethodsSessionUpdate, params)
}

// RequestPermission asks the user to authorize a tool call.
func (c *AgentSideConnection) RequestPermission(ctx context.Context, params *RequestPermissionRequest) (*RequestPermissionResponse, error) {
	return call[RequestPermissionResponse](ctx, c.conn, schema.ClientMethodsSessionRequestPermission, params)
}

// ConnectMCP opens an MCP connection through the client.
func (c *AgentSideConnection) ConnectMCP(ctx context.Context, params *ConnectMCPRequest) (*ConnectMCPResponse, error) {
	return call[ConnectMCPResponse](ctx, c.conn, schema.ClientMethodsMCPConnect, params)
}

// MessageMCP sends an MCP request over a connection and returns its result.
func (c *AgentSideConnection) MessageMCP(ctx context.Context, params *MessageMCPRequest) (*MessageMCPResponse, error) {
	return call[MessageMCPResponse](ctx, c.conn, schema.ClientMethodsMCPMessage, params)
}

// NotifyMCP sends an MCP notification over a connection.
func (c *AgentSideConnection) NotifyMCP(ctx context.Context, params *MessageMCPNotification) error {
	return c.conn.SendNotification(ctx, schema.ClientMethodsMCPMessage, params)
}

// DisconnectMCP closes an MCP connection.
func (c *AgentSideConnection) DisconnectMCP(ctx context.Context, params *DisconnectMCPRequest) (*DisconnectMCPResponse, error) {
	return call[DisconnectMCPResponse](ctx, c.conn, schema.ClientMethodsMCPDisconnect, params)
}

// CreateElicitation asks the client to collect input from the user.
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
	case schema.AgentMethodsSessionNew:
		return request(ctx, params, c.agent.NewSession)
	case schema.AgentMethodsSessionPrompt:
		return request(ctx, params, c.agent.Prompt)

	case schema.AgentMethodsAuthLogin:
		if auth, ok := c.agent.(AuthHandler); ok {
			return request(ctx, params, auth.Login)
		}
	case schema.AgentMethodsAuthLogout:
		if auth, ok := c.agent.(AuthHandler); ok {
			return request(ctx, params, auth.Logout)
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

	case schema.AgentMethodsMCPMessage:
		if mcp, ok := c.agent.(MCPMessageHandler); ok {
			return request(ctx, params, mcp.MessageMCP)
		}

	default:
		if handler, ok := c.agent.(ExtMethodHandler); ok {
			return handler.ExtMethod(ctx, method, params)
		}
	}
	return nil, jsonrpc.MethodNotFound(method)
}

func (c *AgentSideConnection) handleNotification(ctx context.Context, method string, params jsontext.Value) error {
	switch method {
	case schema.AgentMethodsSessionCancel:
		return notify(ctx, params, c.agent.CancelSession)

	case schema.AgentMethodsMCPMessage:
		if mcp, ok := c.agent.(MCPMessageHandler); ok {
			return notify(ctx, params, mcp.NotifyMCP)
		}

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
