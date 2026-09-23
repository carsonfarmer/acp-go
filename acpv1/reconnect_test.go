package acpv1_test

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	acp "github.com/ironpark/go-acp"
	"github.com/ironpark/go-acp/acpv1"
)

// replayAgent replays a loaded session's history before answering the load,
// as a real agent would.
type replayAgent struct{ *permissionAgent }

func (a *replayAgent) Initialize(context.Context, *acpv1.InitializeRequest) (*acpv1.InitializeResponse, error) {
	return &acpv1.InitializeResponse{ProtocolVersion: acpv1.ProtocolVersion, AgentCapabilities: acpv1.CapabilitiesOf(a)}, nil
}

func (a *replayAgent) LoadSession(ctx context.Context, params *acpv1.LoadSessionRequest) (*acpv1.LoadSessionResponse, error) {
	if _, err := a.SessionManager.LoadSession(ctx, params); err != nil {
		return nil, err
	}
	if err := acpv1.NewSessionStream(a.client, params.SessionID).SendUserMessage(ctx, "earlier"); err != nil {
		return nil, err
	}
	return &acpv1.LoadSessionResponse{}, nil
}

// TestReconnectLoadsSession follows the ACP v1 reconnect flow over
// Streamable HTTP: a new connection with the same cookie jar, initialize,
// then session/load of the saved session.
func TestReconnectLoadsSession(t *testing.T) {
	sessions := newTestAgent().SessionManager // outlives any one connection
	server := acp.NewHTTPServer(func(ctx context.Context, tr acp.Transport) error {
		conn := acpv1.NewAgentSideConnection(func(c *acpv1.AgentSideConnection) acpv1.Agent {
			a := &replayAgent{&permissionAgent{newTestAgent()}}
			a.SessionManager = sessions
			a.client = c
			return a
		}, nil, nil, acp.WithTransport(tr))
		return conn.Start(ctx)
	})
	defer server.Close()
	// Stand in for a load balancer: set an affinity cookie once, and count
	// requests that bring it back.
	var withCookie atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := r.Cookie("affinity"); err == nil {
			withCookie.Add(1)
		} else {
			http.SetCookie(w, &http.Cookie{Name: "affinity", Value: "backend-1"})
		}
		server.ServeHTTP(w, r)
	}))
	defer ts.Close()

	jar, _ := cookiejar.New(nil)
	connect := func(client *testClient) *acpv1.RemoteAgent {
		agent := acpv1.ConnectAgent(t.Context(), acp.NewHTTPClientTransport(ts.URL, acp.WithCookieJar(jar)),
			func(*acpv1.ClientSideConnection) acpv1.Client { return client })
		init, err := agent.Initialize(t.Context(), &acpv1.InitializeRequest{ProtocolVersion: acpv1.ProtocolVersion})
		if err != nil {
			t.Fatal(err)
		}
		if l := init.AgentCapabilities.LoadSession; l == nil || !*l {
			t.Fatal("agent cannot load sessions")
		}
		return agent
	}

	first := connect(newTestClient())
	session, err := first.StartSession(t.Context(), &acpv1.NewSessionRequest{Cwd: "/tmp"})
	if err != nil {
		t.Fatal(err)
	}
	saved := session.ID
	first.Close() // the connection drops; the session stays on the server

	client := newTestClient()
	second := connect(client)
	defer second.Close()
	if withCookie.Load() == 0 {
		t.Fatal("the reconnect did not send the affinity cookie")
	}
	if _, err := second.LoadSession(t.Context(), &acpv1.LoadSessionRequest{SessionID: saved, Cwd: "/tmp"}); err != nil {
		t.Fatal(err)
	}
	select {
	case replay := <-client.updates:
		if replay.SessionID != saved || replay.Update.Tag() != "user_message_chunk" {
			t.Fatalf("replayed %+v", replay)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no replay after session/load")
	}

	// The loaded session works like a new one, on its own stream.
	turn, err := second.Session(saved).Prompt(t.Context(), acpv1.TextBlock("again"))
	if err != nil {
		t.Fatal(err)
	}
	if text, _, err := turn.Text(); err != nil || text != "allowed allow" {
		t.Fatalf("turn after reconnect: %q %v", text, err)
	}

	// Loading a session the agent does not know fails cleanly.
	if _, err := second.LoadSession(t.Context(), &acpv1.LoadSessionRequest{SessionID: "missing", Cwd: "/tmp"}); !acp.IsCode(err, acp.ErrorCodeResourceNotFound) {
		t.Fatalf("loading an unknown session: %v", err)
	}
}
