package acpv1_test

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	acp "github.com/ironpark/go-acp"
	"github.com/ironpark/go-acp/acpv1"
)

// permissionAgent asks the client for permission mid-turn, a request that
// travels on the session stream and is answered by a POST.
type permissionAgent struct{ *testAgent }

func (a *permissionAgent) Prompt(ctx context.Context, params *acpv1.PromptRequest) (*acpv1.PromptResponse, error) {
	permission, err := a.client.RequestPermission(ctx, &acpv1.RequestPermissionRequest{
		SessionID: params.SessionID,
		ToolCall:  acpv1.ToolCallUpdate{ToolCallID: "call_1"},
		Options:   []acpv1.PermissionOption{{OptionID: "allow", Name: "Allow", Kind: acpv1.PermissionOptionKindAllowOnce}},
	})
	if err != nil {
		return nil, err
	}
	text := "denied"
	if selected, ok := permission.Outcome.As[acpv1.RequestPermissionOutcomeSelected](); ok {
		text = "allowed " + string(selected.OptionID)
	}
	if err := acpv1.NewSessionStream(a.client, params.SessionID).SendText(ctx, text); err != nil {
		return nil, err
	}
	return &acpv1.PromptResponse{StopReason: acpv1.StopReasonEndTurn}, nil
}

func TestConnectAgentOverHTTP(t *testing.T) {
	server := acp.NewHTTPServer(func(ctx context.Context, tr acp.Transport) error {
		conn := acpv1.NewAgentSideConnection(func(c *acpv1.AgentSideConnection) acpv1.Agent {
			a := &permissionAgent{newTestAgent()}
			a.client = c
			return a
		}, nil, nil, acp.WithTransport(tr))
		return conn.Start(ctx)
	})
	ts := httptest.NewServer(server)
	defer ts.Close()
	defer server.Close()

	transports := map[string]func() acp.Transport{
		"streamable HTTP": func() acp.Transport { return acp.NewHTTPClientTransport(ts.URL) },
		"WebSocket": func() acp.Transport {
			ws, err := acp.DialWebSocket(t.Context(), "ws"+strings.TrimPrefix(ts.URL, "http"))
			if err != nil {
				t.Fatal(err)
			}
			return ws
		},
	}
	for name, dial := range transports {
		t.Run(name, func(t *testing.T) {
			agent := acpv1.ConnectAgent(t.Context(), dial(), func(*acpv1.ClientSideConnection) acpv1.Client {
				return newTestClient()
			})
			if _, err := agent.Initialize(t.Context(), &acpv1.InitializeRequest{ProtocolVersion: acpv1.ProtocolVersion}); err != nil {
				t.Fatal(err)
			}
			// Two sessions on one connection.
			for range 2 {
				session, err := agent.StartSession(t.Context(), &acpv1.NewSessionRequest{Cwd: "/tmp"})
				if err != nil {
					t.Fatal(err)
				}
				turn, err := session.Prompt(t.Context(), acpv1.TextBlock("hi"))
				if err != nil {
					t.Fatal(err)
				}
				text, response, err := turn.Text()
				if err != nil || text != "allowed allow" || response.StopReason != acpv1.StopReasonEndTurn {
					t.Fatalf("turn: %q %+v %v", text, response, err)
				}
			}
			if _, err := acp.CallExt[map[string]string](t.Context(), agent, "_test/echo", map[string]int{"n": 1}); err != nil {
				t.Fatal(err)
			}
			if err := agent.Close(); err != nil {
				t.Fatal(err)
			}
			if err := agent.Wait(); err != nil {
				t.Fatalf("Wait after Close = %v", err)
			}
		})
	}
}
