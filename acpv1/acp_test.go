package acpv1_test

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"io"
	"testing"
	"time"

	acp "github.com/ironpark/go-acp"
	"github.com/ironpark/go-acp/acpv1"
	schema "github.com/ironpark/go-acp/schema/v1"
)

// testAgent implements the required Agent methods plus the optional terminal
// and extension hooks the tests exercise. Session lifecycle comes from the
// embedded manager.
type testAgent struct {
	*acpv1.SessionManager[*testSession]
	client acpv1.Client

	cancelled chan acpv1.SessionID
}

type testSession struct{ cwd string }

func newTestAgent() *testAgent {
	return &testAgent{
		SessionManager: acpv1.NewSessionManager(
			acpv1.NewMemoryStore[*testSession](),
			func(_ context.Context, params *acpv1.NewSessionRequest) (acpv1.SessionID, *testSession, error) {
				return acpv1.GenerateSessionID(), &testSession{cwd: params.Cwd}, nil
			},
		),
		cancelled: make(chan acpv1.SessionID, 1),
	}
}

func (a *testAgent) Initialize(context.Context, *acpv1.InitializeRequest) (*acpv1.InitializeResponse, error) {
	return &acpv1.InitializeResponse{ProtocolVersion: acpv1.ProtocolVersion}, nil
}

func (a *testAgent) Authenticate(context.Context, *acpv1.AuthenticateRequest) (*acpv1.AuthenticateResponse, error) {
	return &acpv1.AuthenticateResponse{}, nil
}

func (a *testAgent) Prompt(ctx context.Context, params *acpv1.PromptRequest) (*acpv1.PromptResponse, error) {
	stream := acpv1.NewSessionStream(a.client, params.SessionID)
	if err := stream.SendText(ctx, "hello", acpv1.WithMessageID("msg_1")); err != nil {
		return nil, err
	}
	return &acpv1.PromptResponse{StopReason: schema.StopReasonEndTurn}, nil
}

func (a *testAgent) Cancel(_ context.Context, params *acpv1.CancelNotification) error {
	a.cancelled <- params.SessionID
	return nil
}

func (a *testAgent) ExtMethod(_ context.Context, method string, params jsontext.Value) (any, error) {
	return map[string]string{"method": method, "params": string(params)}, nil
}

// testClient records the updates it receives and always allows tool calls.
type testClient struct {
	updates chan *acpv1.SessionNotification
}

func newTestClient() *testClient {
	return &testClient{updates: make(chan *acpv1.SessionNotification, 16)}
}

func (c *testClient) SessionUpdate(_ context.Context, params *acpv1.SessionNotification) error {
	c.updates <- params
	return nil
}

func (c *testClient) RequestPermission(_ context.Context, params *acpv1.RequestPermissionRequest) (*acpv1.RequestPermissionResponse, error) {
	return &acpv1.RequestPermissionResponse{
		Outcome: schema.NewRequestPermissionOutcome(schema.RequestPermissionOutcomeSelected{
			OptionID: params.Options[0].OptionID,
		}),
	}, nil
}

func (c *testClient) ReadTextFile(_ context.Context, params *acpv1.ReadTextFileRequest) (*acpv1.ReadTextFileResponse, error) {
	return &acpv1.ReadTextFileResponse{Content: "contents of " + params.Path}, nil
}

// connect wires an agent and a client together over in-memory pipes.
func connect(t *testing.T, agent *testAgent, client acpv1.Client) (*acpv1.ClientSideConnection, *acpv1.AgentSideConnection) {
	t.Helper()

	agentIn, clientOut := io.Pipe()
	clientIn, agentOut := io.Pipe()

	agentConn := acpv1.NewAgentSideConnection(func(c *acpv1.AgentSideConnection) acpv1.Agent {
		agent.client = c
		return agent
	}, agentIn, agentOut)
	clientConn := acpv1.NewClientSideConnection(func(*acpv1.ClientSideConnection) acpv1.Client {
		return client
	}, clientIn, clientOut)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{}, 2)
	go func() { _ = agentConn.Start(ctx); done <- struct{}{} }()
	go func() { _ = clientConn.Start(ctx); done <- struct{}{} }()

	t.Cleanup(func() {
		cancel()
		_ = clientOut.Close()
		_ = agentOut.Close()
		for range 2 {
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Error("connection did not stop")
			}
		}
	})
	return clientConn, agentConn
}

func TestPromptTurn(t *testing.T) {
	client := newTestClient()
	conn, _ := connect(t, newTestAgent(), client)
	ctx := t.Context()

	initialized, err := conn.Initialize(ctx, &acpv1.InitializeRequest{ProtocolVersion: acpv1.ProtocolVersion})
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if initialized.ProtocolVersion != acpv1.ProtocolVersion {
		t.Errorf("protocol version = %d, want %d", initialized.ProtocolVersion, acpv1.ProtocolVersion)
	}

	created, err := conn.NewSession(ctx, &acpv1.NewSessionRequest{Cwd: "/tmp", MCPServers: []schema.MCPServer{}})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	result, err := conn.Prompt(ctx, &acpv1.PromptRequest{
		SessionID: created.SessionID,
		Prompt:    []acpv1.ContentBlock{schema.NewContentBlock(schema.ContentBlockText{Text: "hi"})},
	})
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	if result.StopReason != schema.StopReasonEndTurn {
		t.Errorf("stop reason = %q, want %q", result.StopReason, schema.StopReasonEndTurn)
	}

	select {
	case update := <-client.updates:
		chunk, ok := update.Update.Variant().(schema.SessionUpdateAgentMessageChunk)
		if !ok {
			t.Fatalf("update variant = %T, want an agent message chunk", update.Update.Variant())
		}
		text, ok := chunk.Content.Variant().(schema.ContentBlockText)
		if !ok || text.Text != "hello" {
			t.Errorf("chunk content = %#v", chunk.Content.Variant())
		}
		if chunk.MessageID == nil || *chunk.MessageID != "msg_1" {
			t.Errorf("chunk message id = %v, want msg_1", chunk.MessageID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no session update arrived")
	}
}

func TestVoidResponseDecodes(t *testing.T) {
	conn, _ := connect(t, newTestAgent(), newTestClient())
	if _, err := conn.Authenticate(t.Context(), &acpv1.AuthenticateRequest{MethodID: "none"}); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
}

func TestSessionManagerServesLifecycleMethods(t *testing.T) {
	conn, _ := connect(t, newTestAgent(), newTestClient())
	ctx := t.Context()

	created, err := conn.NewSession(ctx, &acpv1.NewSessionRequest{Cwd: "/tmp", MCPServers: []schema.MCPServer{}})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	listed, err := conn.ListSessions(ctx, &acpv1.ListSessionsRequest{})
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(listed.Sessions) != 1 || listed.Sessions[0].SessionID != created.SessionID {
		t.Fatalf("listed sessions = %#v", listed.Sessions)
	}
	if _, err := conn.DeleteSession(ctx, &acpv1.DeleteSessionRequest{SessionID: created.SessionID}); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if _, err := conn.LoadSession(ctx, &acpv1.LoadSessionRequest{
		SessionID:  created.SessionID,
		Cwd:        "/tmp",
		MCPServers: []schema.MCPServer{},
	}); !hasCode(err, acp.ErrorCodeResourceNotFound) {
		t.Errorf("LoadSession after delete: %v, want a resource-not-found error", err)
	}
}

func TestUnimplementedOptionalMethodIsMethodNotFound(t *testing.T) {
	conn, _ := connect(t, newTestAgent(), newTestClient())
	_, err := conn.SetSessionMode(t.Context(), &acpv1.SetSessionModeRequest{
		SessionID: "session_1",
		ModeID:    "ask",
	})
	if !hasCode(err, acp.ErrorCodeMethodNotFound) {
		t.Errorf("SetSessionMode error = %v, want method not found", err)
	}
}

func TestInvalidParamsAreRejectedBeforeTheHandler(t *testing.T) {
	conn, _ := connect(t, newTestAgent(), newTestClient())
	// sessionId is required, so validation fails before Prompt is called.
	_, err := conn.ExtMethod(t.Context(), schema.AgentMethodsSessionPrompt, map[string]any{"prompt": []any{}})
	if !hasCode(err, acp.ErrorCodeInvalidParams) {
		t.Errorf("error = %v, want invalid params", err)
	}
}

func TestExtensionMethodsFallThrough(t *testing.T) {
	conn, _ := connect(t, newTestAgent(), newTestClient())
	raw, err := conn.ExtMethod(t.Context(), "example.com/ping", map[string]string{"hello": "world"})
	if err != nil {
		t.Fatalf("ExtMethod: %v", err)
	}
	var echoed struct {
		Method string `json:"method"`
		Params string `json:"params"`
	}
	if err := json.Unmarshal(raw, &echoed); err != nil {
		t.Fatalf("decode ext response: %v", err)
	}
	if echoed.Method != "example.com/ping" || echoed.Params != `{"hello":"world"}` {
		t.Errorf("ext method echoed %#v", echoed)
	}
}

func TestCancelNotificationReachesTheAgent(t *testing.T) {
	agent := newTestAgent()
	conn, _ := connect(t, agent, newTestClient())

	if err := conn.Cancel(t.Context(), &acpv1.CancelNotification{SessionID: "session_1"}); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	select {
	case id := <-agent.cancelled:
		if id != "session_1" {
			t.Errorf("cancelled session = %q", id)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("agent never saw the cancel notification")
	}
}

func TestAgentCallsBackIntoTheClient(t *testing.T) {
	agent := newTestAgent()
	_, agentConn := connect(t, agent, newTestClient())
	ctx := t.Context()

	read, err := agentConn.ReadTextFile(ctx, &acpv1.ReadTextFileRequest{SessionID: "session_1", Path: "/etc/hosts"})
	if err != nil {
		t.Fatalf("ReadTextFile: %v", err)
	}
	if read.Content != "contents of /etc/hosts" {
		t.Errorf("content = %q", read.Content)
	}

	granted, err := agentConn.RequestPermission(ctx, &acpv1.RequestPermissionRequest{
		SessionID: "session_1",
		ToolCall:  schema.ToolCallUpdate{ToolCallID: "call_1"},
		Options: []schema.PermissionOption{
			{OptionID: "allow", Name: "Allow", Kind: schema.PermissionOptionKindAllowOnce},
		},
	})
	if err != nil {
		t.Fatalf("RequestPermission: %v", err)
	}
	selected, ok := granted.Outcome.Variant().(schema.RequestPermissionOutcomeSelected)
	if !ok || selected.OptionID != "allow" {
		t.Errorf("outcome = %#v", granted.Outcome.Variant())
	}
}

func TestUnsupportedClientMethodIsMethodNotFound(t *testing.T) {
	// testClient implements neither FileWriter nor TerminalHandler.
	_, agentConn := connect(t, newTestAgent(), newTestClient())
	_, err := agentConn.WriteTextFile(t.Context(), &acpv1.WriteTextFileRequest{
		SessionID: "session_1",
		Path:      "/tmp/file",
		Content:   "data",
	})
	if !hasCode(err, acp.ErrorCodeMethodNotFound) {
		t.Errorf("WriteTextFile error = %v, want method not found", err)
	}
}

// hasCode reports whether err is a RequestError with the given code.
func hasCode(err error, code acp.ErrorCode) bool {
	var reqErr *acp.RequestError
	return errors.As(err, &reqErr) && reqErr.Code == code
}
