package acpmcp

import (
	"context"
	"encoding/json/jsontext"

	"github.com/ironpark/go-acp/acp2"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// HostV2 serves MCP servers from an ACP v2 client to its agent. It implements
// [acp2.MCPConnector]; embed it in the client so the connection routes the
// agent's mcp/* calls to it:
//
//	type myClient struct {
//		*acpmcp.HostV2
//		// ...
//	}
//
//	acp2.SpawnAgent(ctx, cmd, func(conn *acp2.ClientSideConnection) acp2.Client {
//		return &myClient{HostV2: acpmcp.NewHostV2(conn)}
//	})
type HostV2 struct{ h *host }

// NewHostV2 returns a host that answers the agent on conn.
func NewHostV2(conn *acp2.ClientSideConnection) *HostV2 {
	return &HostV2{h: newHost(peer{
		request: func(ctx context.Context, id, method string, params map[string]jsontext.Value) (jsontext.Value, error) {
			result, err := conn.MessageMCP(ctx, &acp2.MessageMCPRequest{ConnectionID: acp2.MCPConnectionID(id), Method: method, Params: params})
			if err != nil {
				return nil, err
			}
			return *result, nil
		},
		notify: func(ctx context.Context, id, method string, params map[string]jsontext.Value) error {
			return conn.NotifyMCP(ctx, &acp2.MessageMCPNotification{ConnectionID: acp2.MCPConnectionID(id), Method: method, Params: params})
		},
	})}
}

// Add registers server under name and returns its entry for the MCPServers
// of session/new.
func (h *HostV2) Add(name string, server *mcp.Server) acp2.MCPServer {
	return acp2.NewMCPServer(acp2.MCPServerACP{Name: name, ServerID: acp2.MCPServerACPID(h.h.add(server))})
}

func (h *HostV2) ConnectMCP(ctx context.Context, params *acp2.ConnectMCPRequest) (*acp2.ConnectMCPResponse, error) {
	id, err := h.h.connect(ctx, string(params.ServerID))
	if err != nil {
		return nil, err
	}
	return &acp2.ConnectMCPResponse{ConnectionID: acp2.MCPConnectionID(id)}, nil
}

func (h *HostV2) MessageMCP(ctx context.Context, params *acp2.MessageMCPRequest) (*acp2.MessageMCPResponse, error) {
	result, err := h.h.message(ctx, string(params.ConnectionID), params.Method, params.Params)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (h *HostV2) NotifyMCP(_ context.Context, params *acp2.MessageMCPNotification) error {
	return h.h.notifyMessage(string(params.ConnectionID), params.Method, params.Params)
}

func (h *HostV2) DisconnectMCP(_ context.Context, params *acp2.DisconnectMCPRequest) (*acp2.DisconnectMCPResponse, error) {
	if err := h.h.disconnect(string(params.ConnectionID)); err != nil {
		return nil, err
	}
	return &acp2.DisconnectMCPResponse{}, nil
}

// DialerV2 connects an ACP v2 agent to the MCP servers its client provides.
// It implements [acp2.MCPMessageHandler], which carries the servers'
// requests and notifications back to the agent; embed it in the agent:
//
//	type myAgent struct {
//		*acpmcp.DialerV2
//		// ...
//	}
//
//	acp2.NewAgentSideConnection(func(conn *acp2.AgentSideConnection) acp2.Agent {
//		return &myAgent{DialerV2: acpmcp.NewDialerV2(conn)}
//	}, os.Stdin, os.Stdout)
type DialerV2 struct{ d *dialer }

// NewDialerV2 returns a dialer that reaches the client on conn.
func NewDialerV2(conn *acp2.AgentSideConnection) *DialerV2 {
	return &DialerV2{d: newDialer(peer{
		request: func(ctx context.Context, id, method string, params map[string]jsontext.Value) (jsontext.Value, error) {
			result, err := conn.MessageMCP(ctx, &acp2.MessageMCPRequest{ConnectionID: acp2.MCPConnectionID(id), Method: method, Params: params})
			if err != nil {
				return nil, err
			}
			return *result, nil
		},
		notify: func(ctx context.Context, id, method string, params map[string]jsontext.Value) error {
			return conn.NotifyMCP(ctx, &acp2.MessageMCPNotification{ConnectionID: acp2.MCPConnectionID(id), Method: method, Params: params})
		},
	}, func(ctx context.Context, serverID string) (string, error) {
		response, err := conn.ConnectMCP(ctx, &acp2.ConnectMCPRequest{ServerID: acp2.MCPServerACPID(serverID)})
		if err != nil {
			return "", err
		}
		return string(response.ConnectionID), nil
	}, func(ctx context.Context, id string) error {
		_, err := conn.DisconnectMCP(ctx, &acp2.DisconnectMCPRequest{ConnectionID: acp2.MCPConnectionID(id)})
		return err
	})}
}

// Connect opens an MCP session with client to server, an entry of the
// MCPServers in session/new, as [mcp.Client.Connect] does over any other
// transport; opts may be nil. Closing the session sends mcp/disconnect.
func (d *DialerV2) Connect(ctx context.Context, server acp2.MCPServerACP, client *mcp.Client, opts *mcp.ClientSessionOptions) (*mcp.ClientSession, error) {
	return d.d.dial(ctx, string(server.ServerID), client, opts)
}

func (d *DialerV2) MessageMCP(ctx context.Context, params *acp2.MessageMCPRequest) (*acp2.MessageMCPResponse, error) {
	result, err := d.d.message(ctx, string(params.ConnectionID), params.Method, params.Params)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (d *DialerV2) NotifyMCP(_ context.Context, params *acp2.MessageMCPNotification) error {
	return d.d.notifyMessage(string(params.ConnectionID), params.Method, params.Params)
}
