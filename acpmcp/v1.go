package acpmcp

import (
	"context"
	"encoding/json/jsontext"

	"github.com/ironpark/acp-go/acp1"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// HostV1 serves MCP servers from an ACP v1 client to its agent. It implements
// [acp1.MCPConnector]; embed it in the client so the connection routes the
// agent's mcp/* calls to it:
//
//	type myClient struct {
//		*acpmcp.HostV1
//		// ...
//	}
//
//	acp1.SpawnAgent(ctx, cmd, func(conn *acp1.ClientSideConnection) acp1.Client {
//		return &myClient{HostV1: acpmcp.NewHostV1(conn)}
//	})
type HostV1 struct{ h *host }

// NewHostV1 returns a host that answers the agent on conn.
func NewHostV1(conn *acp1.ClientSideConnection) *HostV1 {
	return &HostV1{h: newHost(peer{
		request: func(ctx context.Context, id, method string, params map[string]jsontext.Value) (jsontext.Value, error) {
			result, err := conn.MessageMCP(ctx, &acp1.MessageMCPRequest{ConnectionID: acp1.MCPConnectionID(id), Method: method, Params: params})
			if err != nil {
				return nil, err
			}
			return *result, nil
		},
		notify: func(ctx context.Context, id, method string, params map[string]jsontext.Value) error {
			return conn.NotifyMCP(ctx, &acp1.MessageMCPNotification{ConnectionID: acp1.MCPConnectionID(id), Method: method, Params: params})
		},
	})}
}

// Add registers server under name and returns its entry for the MCPServers
// of session/new. The agent reaches it only if it advertises
// mcpCapabilities.acp.
func (h *HostV1) Add(name string, server *mcp.Server) acp1.MCPServer {
	return acp1.NewMCPServer(acp1.MCPServerACP{Name: name, ServerID: acp1.MCPServerACPID(h.h.add(server))})
}

func (h *HostV1) ConnectMCP(ctx context.Context, params *acp1.ConnectMCPRequest) (*acp1.ConnectMCPResponse, error) {
	id, err := h.h.connect(ctx, string(params.ServerID))
	if err != nil {
		return nil, err
	}
	return &acp1.ConnectMCPResponse{ConnectionID: acp1.MCPConnectionID(id)}, nil
}

func (h *HostV1) MessageMCP(ctx context.Context, params *acp1.MessageMCPRequest) (*acp1.MessageMCPResponse, error) {
	result, err := h.h.message(ctx, string(params.ConnectionID), params.Method, params.Params)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (h *HostV1) NotifyMCP(_ context.Context, params *acp1.MessageMCPNotification) error {
	return h.h.notifyMessage(string(params.ConnectionID), params.Method, params.Params)
}

func (h *HostV1) DisconnectMCP(_ context.Context, params *acp1.DisconnectMCPRequest) (*acp1.DisconnectMCPResponse, error) {
	if err := h.h.disconnect(string(params.ConnectionID)); err != nil {
		return nil, err
	}
	return &acp1.DisconnectMCPResponse{}, nil
}

// DialerV1 connects an ACP v1 agent to the MCP servers its client provides.
// It implements [acp1.MCPMessageHandler], which carries the servers'
// requests and notifications back to the agent; embed it in the agent, and
// [acp1.CapabilitiesOf] then advertises mcpCapabilities.acp:
//
//	type myAgent struct {
//		*acpmcp.DialerV1
//		// ...
//	}
//
//	acp1.NewAgentSideConnection(func(conn *acp1.AgentSideConnection) acp1.Agent {
//		return &myAgent{DialerV1: acpmcp.NewDialerV1(conn)}
//	}, os.Stdin, os.Stdout)
type DialerV1 struct{ d *dialer }

// NewDialerV1 returns a dialer that reaches the client on conn.
func NewDialerV1(conn *acp1.AgentSideConnection) *DialerV1 {
	return &DialerV1{d: newDialer(peer{
		request: func(ctx context.Context, id, method string, params map[string]jsontext.Value) (jsontext.Value, error) {
			result, err := conn.MessageMCP(ctx, &acp1.MessageMCPRequest{ConnectionID: acp1.MCPConnectionID(id), Method: method, Params: params})
			if err != nil {
				return nil, err
			}
			return *result, nil
		},
		notify: func(ctx context.Context, id, method string, params map[string]jsontext.Value) error {
			return conn.NotifyMCP(ctx, &acp1.MessageMCPNotification{ConnectionID: acp1.MCPConnectionID(id), Method: method, Params: params})
		},
	}, func(ctx context.Context, serverID string) (string, error) {
		response, err := conn.ConnectMCP(ctx, &acp1.ConnectMCPRequest{ServerID: acp1.MCPServerACPID(serverID)})
		if err != nil {
			return "", err
		}
		return string(response.ConnectionID), nil
	}, func(ctx context.Context, id string) error {
		_, err := conn.DisconnectMCP(ctx, &acp1.DisconnectMCPRequest{ConnectionID: acp1.MCPConnectionID(id)})
		return err
	})}
}

// Connect opens an MCP session with client to server, an entry of the
// MCPServers in session/new, as [mcp.Client.Connect] does over any other
// transport; opts may be nil. Closing the session sends mcp/disconnect.
func (d *DialerV1) Connect(ctx context.Context, server acp1.MCPServerACP, client *mcp.Client, opts *mcp.ClientSessionOptions) (*mcp.ClientSession, error) {
	return d.d.dial(ctx, string(server.ServerID), client, opts)
}

func (d *DialerV1) MessageMCP(ctx context.Context, params *acp1.MessageMCPRequest) (*acp1.MessageMCPResponse, error) {
	result, err := d.d.message(ctx, string(params.ConnectionID), params.Method, params.Params)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (d *DialerV1) NotifyMCP(_ context.Context, params *acp1.MessageMCPNotification) error {
	return d.d.notifyMessage(string(params.ConnectionID), params.Method, params.Params)
}
