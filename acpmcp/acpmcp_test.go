package acpmcp_test

import (
	"context"
	"testing"
	"time"

	acp "github.com/ironpark/acp-go"
	"github.com/ironpark/acp-go/acp1"
	"github.com/ironpark/acp-go/acp2"
	"github.com/ironpark/acp-go/acpmcp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type echoInput struct {
	Message string `json:"message"`
}

type echoOutput struct {
	Reply string `json:"reply"`
}

// newServer is an MCP server with an echo tool. It reports each stateful
// session once initialized, so the test can send requests the other way.
func newServer(sessions chan<- *mcp.ServerSession) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "tools", Version: "1.0.0"}, &mcp.ServerOptions{
		InitializedHandler: func(_ context.Context, req *mcp.InitializedRequest) { sessions <- req.Session },
	})
	mcp.AddTool(server, &mcp.Tool{Name: "echo", Description: "Echoes the message"},
		func(_ context.Context, _ *mcp.CallToolRequest, in echoInput) (*mcp.CallToolResult, echoOutput, error) {
			return nil, echoOutput{Reply: "echo: " + in.Message}, nil
		})
	return server
}

// newClient is an MCP client with one root, reporting tool list changes.
func newClient(changed chan<- struct{}) *mcp.Client {
	client := mcp.NewClient(&mcp.Implementation{Name: "agent", Version: "1.0.0"}, &mcp.ClientOptions{
		ToolListChangedHandler: func(context.Context, *mcp.ToolListChangedRequest) {
			select {
			case changed <- struct{}{}:
			default:
			}
		},
	})
	client.AddRoots(&mcp.Root{URI: "file:///project", Name: "project"})
	return client
}

// sessionModes are the two ways an MCP client opens a session: the
// stateless discovery of MCP 2026-07-28, and the initialize handshake before
// it, which also lets the server send requests to the client.
var sessionModes = map[string]*mcp.ClientSessionOptions{
	"stateless": nil,
	"stateful":  {ProtocolVersion: "2025-11-25"},
}

// connectFunc opens an MCP session to a server the client side provides.
type connectFunc func(ctx context.Context, server *mcp.Server, client *mcp.Client, opts *mcp.ClientSessionOptions) (*mcp.ClientSession, error)

// exercise runs an MCP session over ACP in each mode: requests, an error, a
// notification from the server and, in a stateful session, a request from
// the server to the agent's MCP client and the disconnect on close.
func exercise(t *testing.T, connect connectFunc) {
	for name, opts := range sessionModes {
		t.Run(name, func(t *testing.T) {
			ctx := t.Context()
			serverSessions := make(chan *mcp.ServerSession, 1)
			server := newServer(serverSessions)
			changed := make(chan struct{}, 1)
			session, err := connect(ctx, server, newClient(changed), opts)
			if err != nil {
				t.Fatalf("Connect: %v", err)
			}
			defer session.Close()

			tools, err := session.ListTools(ctx, nil)
			if err != nil || len(tools.Tools) != 1 {
				t.Fatalf("ListTools: %+v %v", tools, err)
			}
			result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "echo", Arguments: map[string]any{"message": "hi"}})
			if err != nil || result.IsError {
				t.Fatalf("CallTool: %+v %v", result, err)
			}
			if got := result.StructuredContent.(map[string]any)["reply"]; got != "echo: hi" {
				t.Errorf("CallTool = %v, want %q", got, "echo: hi")
			}
			// A request the server rejects keeps its MCP error through ACP.
			if _, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "missing"}); err == nil {
				t.Error("calling an unknown tool succeeded")
			}

			// A notification from the server: adding a tool changes the list.
			server.AddTool(&mcp.Tool{Name: "late", InputSchema: map[string]any{"type": "object"}},
				func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
					return &mcp.CallToolResult{}, nil
				})
			select {
			case <-changed:
			case <-time.After(2 * time.Second):
				t.Fatal("no tools/list_changed notification reached the agent")
			}

			if opts == nil {
				return
			}
			var serverSession *mcp.ServerSession
			select {
			case serverSession = <-serverSessions:
			case <-time.After(2 * time.Second):
				t.Fatal("the server session never initialized")
			}
			// A request from the server to the agent's MCP client.
			roots, err := serverSession.ListRoots(ctx, nil)
			if err != nil {
				t.Fatalf("ListRoots: %v", err)
			}
			if len(roots.Roots) != 1 || roots.Roots[0].URI != "file:///project" {
				t.Errorf("roots = %+v", roots.Roots)
			}
			// Closing the session sends mcp/disconnect, which ends the
			// server's session on the client side.
			session.Close()
			ended := make(chan struct{})
			go func() { serverSession.Wait(); close(ended) }()
			select {
			case <-ended:
			case <-time.After(2 * time.Second):
				t.Fatal("the server session outlived the disconnect")
			}
		})
	}
}

type v1Agent struct {
	*acpmcp.DialerV1
}

func (v1Agent) Initialize(context.Context, *acp1.InitializeRequest) (*acp1.InitializeResponse, error) {
	return &acp1.InitializeResponse{ProtocolVersion: acp1.ProtocolVersion}, nil
}
func (v1Agent) NewSession(context.Context, *acp1.NewSessionRequest) (*acp1.NewSessionResponse, error) {
	return &acp1.NewSessionResponse{SessionID: "s"}, nil
}
func (v1Agent) Prompt(context.Context, *acp1.PromptRequest) (*acp1.PromptResponse, error) {
	return &acp1.PromptResponse{StopReason: acp1.StopReasonEndTurn}, nil
}
func (v1Agent) Cancel(context.Context, *acp1.CancelNotification) error { return nil }

type v1Client struct {
	acp1.UnimplementedClient
	*acpmcp.HostV1
}

func TestMCPOverACPv1(t *testing.T) {
	var agent *v1Agent
	var client *v1Client
	acp1.Pipe(t.Context(),
		func(c *acp1.AgentSideConnection) acp1.Agent { agent = &v1Agent{acpmcp.NewDialerV1(c)}; return agent },
		func(c *acp1.ClientSideConnection) acp1.Client {
			client = &v1Client{HostV1: acpmcp.NewHostV1(c)}
			return client
		})

	if !acp1.CapabilitiesOf(agent).GetMCPCapabilities().GetACP() {
		t.Error("an agent embedding DialerV1 does not advertise mcpCapabilities.acp")
	}
	exercise(t, func(ctx context.Context, server *mcp.Server, mcpClient *mcp.Client, opts *mcp.ClientSessionOptions) (*mcp.ClientSession, error) {
		entry, ok := client.Add("tools", server).As[acp1.MCPServerACP]() // listed in session/new
		if !ok {
			t.Fatal("Add did not return an acp-transport server")
		}
		return agent.Connect(ctx, entry, mcpClient, opts)
	})

	unknown := acp1.MCPServerACP{Name: "x", ServerID: "unknown"}
	if _, err := agent.Connect(t.Context(), unknown, newClient(nil), nil); !acp.IsCode(err, acp.ErrorCodeResourceNotFound) {
		t.Errorf("connecting to an unknown server: %v", err)
	}
}

type v2Agent struct {
	*acpmcp.DialerV2
}

func (v2Agent) Initialize(context.Context, *acp2.InitializeRequest) (*acp2.InitializeResponse, error) {
	return &acp2.InitializeResponse{ProtocolVersion: acp2.ProtocolVersion}, nil
}
func (v2Agent) NewSession(context.Context, *acp2.NewSessionRequest) (*acp2.NewSessionResponse, error) {
	return &acp2.NewSessionResponse{SessionID: "s"}, nil
}
func (v2Agent) Prompt(context.Context, *acp2.PromptRequest) (*acp2.PromptResponse, error) {
	return &acp2.PromptResponse{MessageID: "m"}, nil
}
func (v2Agent) CancelSession(context.Context, *acp2.CancelSessionNotification) error { return nil }

type v2Client struct {
	acp2.UnimplementedClient
	*acpmcp.HostV2
}

func TestMCPOverACPv2(t *testing.T) {
	var agent *v2Agent
	var client *v2Client
	acp2.Pipe(t.Context(),
		func(c *acp2.AgentSideConnection) acp2.Agent { agent = &v2Agent{acpmcp.NewDialerV2(c)}; return agent },
		func(c *acp2.ClientSideConnection) acp2.Client {
			client = &v2Client{HostV2: acpmcp.NewHostV2(c)}
			return client
		})

	exercise(t, func(ctx context.Context, server *mcp.Server, mcpClient *mcp.Client, opts *mcp.ClientSessionOptions) (*mcp.ClientSession, error) {
		entry, _ := client.Add("tools", server).As[acp2.MCPServerACP]()
		return agent.Connect(ctx, entry, mcpClient, opts)
	})
}
