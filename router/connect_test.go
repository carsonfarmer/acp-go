package router_test

import (
	"context"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	acp "github.com/ironpark/acp-go"
	"github.com/ironpark/acp-go/acp1"
	"github.com/ironpark/acp-go/acp2"
	"github.com/ironpark/acp-go/acphttp"
	"github.com/ironpark/acp-go/router"
)

// remoteAgent runs serve behind an acphttp.Server, for Streamable HTTP and
// WebSocket clients, counting the connections they open.
func remoteAgent(t *testing.T, serve func(ctx context.Context, tr acp.Transport) error) (url string, connections *atomic.Int32) {
	connections = new(atomic.Int32)
	server := acphttp.NewServer(func(ctx context.Context, tr acp.Transport) error {
		connections.Add(1)
		return serve(ctx, tr)
	})
	ts := httptest.NewServer(server)
	t.Cleanup(func() {
		server.Close()
		ts.Close()
	})
	return ts.URL, connections
}

func dialers(url string) map[string]func(context.Context) (acp.Transport, error) {
	return map[string]func(context.Context) (acp.Transport, error){
		"streamable HTTP": func(context.Context) (acp.Transport, error) { return acphttp.NewClientTransport(url), nil },
		"WebSocket": func(ctx context.Context) (acp.Transport, error) {
			return acphttp.DialWebSocket(ctx, "ws"+strings.TrimPrefix(url, "http"))
		},
	}
}

// connectEach connects over each transport, checks the agent, and checks how
// many connections the negotiation opened.
func connectEach(t *testing.T, url string, connections *atomic.Int32, wantConns int32, check func(*testing.T, *router.Agent)) {
	for name, dial := range dialers(url) {
		t.Run(name, func(t *testing.T) {
			before := connections.Load()
			agent, err := connector().Connect(t.Context(), dial)
			if err != nil {
				t.Fatal(err)
			}
			defer agent.Close()
			check(t, agent)
			if n := connections.Load() - before; n != wantConns {
				t.Fatalf("opened %d connections, want %d", n, wantConns)
			}
		})
	}
}

func TestConnectPrefersV2(t *testing.T) {
	r := router.New().
		WithV1(func(*acp1.AgentSideConnection) acp1.Agent {
			return &v1Agent{initialized: make(chan *acp1.InitializeRequest, 1)}
		}).
		WithV2(func(*acp2.AgentSideConnection) acp2.Agent {
			return &v2Agent{initialized: make(chan *acp2.InitializeRequest, 1)}
		})
	url, connections := remoteAgent(t, func(ctx context.Context, tr acp.Transport) error { return r.Serve(ctx, tr) })

	connectEach(t, url, connections, 1, func(t *testing.T, agent *router.Agent) {
		if agent.V2 == nil || agent.V2Init.ProtocolVersion != 2 {
			t.Fatalf("got %+v", agent)
		}
		session, err := agent.V2.StartSession(t.Context(), &acp2.NewSessionRequest{Cwd: "/tmp"})
		if err != nil || session.ID != "v2-session" {
			t.Fatalf("got %+v %v", session, err)
		}
	})
}

func TestConnectFallsBackToV1(t *testing.T) {
	// A plain v1 agent with no router receives the v2 initialize as is.
	url, connections := remoteAgent(t, func(ctx context.Context, tr acp.Transport) error {
		conn := acp1.NewAgentSideConnection(func(*acp1.AgentSideConnection) acp1.Agent {
			return &v1Agent{initialized: make(chan *acp1.InitializeRequest, 1)}
		}, tr)
		return conn.Start(ctx)
	})

	// The v2 attempt and the v1 connection.
	connectEach(t, url, connections, 2, func(t *testing.T, agent *router.Agent) {
		if agent.V1 == nil || agent.V2 != nil || agent.V1Init.ProtocolVersion != 1 {
			t.Fatalf("got %+v", agent)
		}
		if _, err := agent.V1.NewSession(t.Context(), &acp1.NewSessionRequest{Cwd: "/tmp"}); err != nil {
			t.Fatal(err)
		}
	})
}
