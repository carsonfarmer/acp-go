package acp_test

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"io"
	"testing"
	"time"

	acp "github.com/ironpark/go-acp"
	schema "github.com/ironpark/go-acp/schema/v1"
)

// testAgent implements the required Agent methods plus the optional terminal
// and extension hooks the tests exercise. Session lifecycle comes from the
// embedded manager.
type testAgent struct {
	*acp.SessionManager[*testSession]
	client acp.Client

	cancelled chan acp.SessionID
}

type testSession struct{ cwd string }

func newTestAgent() *testAgent {
	return &testAgent{
		SessionManager: acp.NewSessionManager(
			acp.NewMemoryStore[*testSession](),
			func(_ context.Context, params *acp.NewSessionRequest) (acp.SessionID, *testSession, error) {
				return acp.GenerateSessionID(), &testSession{cwd: params.Cwd}, nil
			},
		),
		cancelled: make(chan acp.SessionID, 1),
	}
}

func (a *testAgent) Initialize(context.Context, *acp.InitializeRequest) (*acp.InitializeResponse, error) {
	return &acp.InitializeResponse{ProtocolVersion: acp.ProtocolVersion}, nil
}

func (a *testAgent) Authenticate(context.Context, *acp.AuthenticateRequest) (*acp.AuthenticateResponse, error) {
	return &acp.AuthenticateResponse{}, nil
}

func (a *testAgent) Prompt(ctx context.Context, params *acp.PromptRequest) (*acp.PromptResponse, error) {
	stream := acp.NewSessionStream(a.client, params.SessionID)
	if err := stream.SendText(ctx, "hello", acp.WithMessageID("msg_1")); err != nil {
		return nil, err
	}
	return &acp.PromptResponse{StopReason: schema.StopReasonEndTurn}, nil
}

func (a *testAgent) Cancel(_ context.Context, params *acp.CancelNotification) error {
	a.cancelled <- params.SessionID
	return nil
}

func (a *testAgent) ExtMethod(_ context.Context, method string, params jsontext.Value) (any, error) {
	return map[string]string{"method": method, "params": string(params)}, nil
}

// testClient records the updates it receives and always allows tool calls.
type testClient struct {
	updates chan *acp.SessionNotification
}

func newTestClient() *testClient {
	return &testClient{updates: make(chan *acp.SessionNotification, 16)}
}

func (c *testClient) SessionUpdate(_ context.Context, params *acp.SessionNotification) error {
	c.updates <- params
	return nil
}

func (c *testClient) RequestPermission(_ context.Context, params *acp.RequestPermissionRequest) (*acp.RequestPermissionResponse, error) {
	return &acp.RequestPermissionResponse{
		Outcome: schema.NewRequestPermissionOutcome(schema.RequestPermissionOutcomeSelected{
			OptionID: params.Options[0].OptionID,
		}),
	}, nil
}

func (c *testClient) ReadTextFile(_ context.Context, params *acp.ReadTextFileRequest) (*acp.ReadTextFileResponse, error) {
	return &acp.ReadTextFileResponse{Content: "contents of " + params.Path}, nil
}

// connect wires an agent and a client together over in-memory pipes.
func connect(t *testing.T, agent *testAgent, client acp.Client) (*acp.ClientSideConnection, *acp.AgentSideConnection) {
	t.Helper()

	agentIn, clientOut := io.Pipe()
	clientIn, agentOut := io.Pipe()

	agentConn := acp.NewAgentSideConnection(func(c *acp.AgentSideConnection) acp.Agent {
		agent.client = c
		return agent
	}, agentIn, agentOut)
	clientConn := acp.NewClientSideConnection(func(*acp.ClientSideConnection) acp.Client {
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

	initialized, err := conn.Initialize(ctx, &acp.InitializeRequest{ProtocolVersion: acp.ProtocolVersion})
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if initialized.ProtocolVersion != acp.ProtocolVersion {
		t.Errorf("protocol version = %d, want %d", initialized.ProtocolVersion, acp.ProtocolVersion)
	}

	created, err := conn.NewSession(ctx, &acp.NewSessionRequest{Cwd: "/tmp", MCPServers: []schema.MCPServer{}})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	result, err := conn.Prompt(ctx, &acp.PromptRequest{
		SessionID: created.SessionID,
		Prompt:    []acp.ContentBlock{schema.NewContentBlock(schema.ContentBlockText{Text: "hi"})},
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
	if _, err := conn.Authenticate(t.Context(), &acp.AuthenticateRequest{MethodID: "none"}); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
}

func TestSessionManagerServesLifecycleMethods(t *testing.T) {
	conn, _ := connect(t, newTestAgent(), newTestClient())
	ctx := t.Context()

	created, err := conn.NewSession(ctx, &acp.NewSessionRequest{Cwd: "/tmp", MCPServers: []schema.MCPServer{}})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	listed, err := conn.ListSessions(ctx, &acp.ListSessionsRequest{})
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(listed.Sessions) != 1 || listed.Sessions[0].SessionID != created.SessionID {
		t.Fatalf("listed sessions = %#v", listed.Sessions)
	}
	if _, err := conn.DeleteSession(ctx, &acp.DeleteSessionRequest{SessionID: created.SessionID}); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if _, err := conn.LoadSession(ctx, &acp.LoadSessionRequest{
		SessionID:  created.SessionID,
		Cwd:        "/tmp",
		MCPServers: []schema.MCPServer{},
	}); !hasCode(err, acp.ErrorCodeResourceNotFound) {
		t.Errorf("LoadSession after delete: %v, want a resource-not-found error", err)
	}
}

func TestUnimplementedOptionalMethodIsMethodNotFound(t *testing.T) {
	conn, _ := connect(t, newTestAgent(), newTestClient())
	_, err := conn.SetSessionMode(t.Context(), &acp.SetSessionModeRequest{
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

	if err := conn.Cancel(t.Context(), &acp.CancelNotification{SessionID: "session_1"}); err != nil {
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

	read, err := agentConn.ReadTextFile(ctx, &acp.ReadTextFileRequest{SessionID: "session_1", Path: "/etc/hosts"})
	if err != nil {
		t.Fatalf("ReadTextFile: %v", err)
	}
	if read.Content != "contents of /etc/hosts" {
		t.Errorf("content = %q", read.Content)
	}

	granted, err := agentConn.RequestPermission(ctx, &acp.RequestPermissionRequest{
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
	_, err := agentConn.WriteTextFile(t.Context(), &acp.WriteTextFileRequest{
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
