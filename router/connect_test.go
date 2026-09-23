package router_test

import (
	"context"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	acp "github.com/ironpark/go-acp"
	"github.com/ironpark/go-acp/acpv1"
	"github.com/ironpark/go-acp/acpv2"
	"github.com/ironpark/go-acp/router"
)

// remoteAgent runs serve behind an HTTPServer, for Streamable HTTP and
// WebSocket clients, counting the connections they open.
func remoteAgent(t *testing.T, serve func(ctx context.Context, tr acp.Transport) error) (url string, connections *atomic.Int32) {
	connections = new(atomic.Int32)
	server := acp.NewHTTPServer(func(ctx context.Context, tr acp.Transport) error {
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
		"streamable HTTP": func(context.Context) (acp.Transport, error) { return acp.NewHTTPClientTransport(url), nil },
		"WebSocket": func(ctx context.Context) (acp.Transport, error) {
			return acp.DialWebSocket(ctx, "ws"+strings.TrimPrefix(url, "http"))
		},
	}
}

func TestConnectPrefersV2(t *testing.T) {
	r := router.New().
		WithV1(func(*acpv1.AgentSideConnection) acpv1.Agent {
			return &v1Agent{initialized: make(chan *acpv1.InitializeRequest, 1)}
		}).
		WithV2(func(*acpv2.AgentSideConnection) acpv2.Agent {
			return &v2Agent{initialized: make(chan *acpv2.InitializeRequest, 1)}
		})
	url, connections := remoteAgent(t, func(ctx context.Context, tr acp.Transport) error { return r.Serve(ctx, tr) })

	for name, dial := range dialers(url) {
		t.Run(name, func(t *testing.T) {
			before := connections.Load()
			agent, err := connector().Connect(t.Context(), dial)
			if err != nil {
				t.Fatal(err)
			}
			defer agent.Close()
			if agent.V2 == nil || agent.V2Init.ProtocolVersion != 2 {
				t.Fatalf("got %+v", agent)
			}
			session, err := agent.V2.StartSession(t.Context(), &acpv2.NewSessionRequest{Cwd: "/tmp"})
			if err != nil || session.ID != "v2-session" {
				t.Fatalf("got %+v %v", session, err)
			}
			if n := connections.Load() - before; n != 1 {
				t.Fatalf("opened %d connections, want 1", n)
			}
		})
	}
}

func TestConnectFallsBackToV1(t *testing.T) {
	// A plain v1 agent with no router receives the v2 initialize as is.
	url, connections := remoteAgent(t, func(ctx context.Context, tr acp.Transport) error {
		conn := acpv1.NewAgentSideConnection(func(*acpv1.AgentSideConnection) acpv1.Agent {
			return &v1Agent{initialized: make(chan *acpv1.InitializeRequest, 1)}
		}, nil, nil, acp.WithTransport(tr))
		return conn.Start(ctx)
	})

	for name, dial := range dialers(url) {
		t.Run(name, func(t *testing.T) {
			before := connections.Load()
			agent, err := connector().Connect(t.Context(), dial)
			if err != nil {
				t.Fatal(err)
			}
			defer agent.Close()
			if agent.V1 == nil || agent.V2 != nil || agent.V1Init.ProtocolVersion != 1 {
				t.Fatalf("got %+v", agent)
			}
			if _, err := agent.V1.NewSession(t.Context(), &acpv1.NewSessionRequest{Cwd: "/tmp"}); err != nil {
				t.Fatal(err)
			}
			// The v2 attempt and the v1 connection.
			if n := connections.Load() - before; n != 2 {
				t.Fatalf("opened %d connections, want 2", n)
			}
		})
	}
}
