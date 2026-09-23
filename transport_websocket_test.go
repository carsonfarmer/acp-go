package acp

import (
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/coder/websocket"
)

func dialTest(t *testing.T, url string) *WebSocketTransport {
	t.Helper()
	ws, err := DialWebSocket(t.Context(), "ws"+strings.TrimPrefix(url, "http"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ws.Close() })
	return ws
}

func TestWebSocketConversation(t *testing.T) {
	server := NewHTTPServer(fakeAgent)
	ts := httptest.NewServer(server)
	defer ts.Close()
	defer server.Close()
	ws := dialTest(t, ts.URL)

	for _, step := range []struct{ send, want string }{
		{`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`, `"protocolVersion":1`},
		{`{"jsonrpc":"2.0","id":2,"method":"session/new","params":{}}`, `"sessionId":"s1"`},
		{`{"jsonrpc":"2.0","id":3,"method":"session/prompt","params":{"sessionId":"s1"}}`, `"method":"session/update"`},
	} {
		if err := ws.WriteMessage(t.Context(), []byte(step.send)); err != nil {
			t.Fatal(err)
		}
		got, err := readWithin(t, ws)
		if err != nil || !strings.Contains(string(got), step.want) {
			t.Fatalf("read %s, %v; want %s", got, err, step.want)
		}
	}
	if got, err := readWithin(t, ws); err != nil || !strings.Contains(string(got), `"stopReason"`) {
		t.Fatalf("read %s, %v; want the prompt reply", got, err)
	}
}

func TestWebSocketUpgradeHasConnectionID(t *testing.T) {
	server := NewHTTPServer(fakeAgent)
	ts := httptest.NewServer(server)
	defer ts.Close()
	defer server.Close()

	conn, resp, err := websocket.Dial(t.Context(), ts.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	if resp.Header.Get(ConnectionIDHeader) == "" {
		t.Fatalf("upgrade response has no %s", ConnectionIDHeader)
	}
}

func TestWebSocketEOFWhenServerCloses(t *testing.T) {
	server := NewHTTPServer(fakeAgent)
	ts := httptest.NewServer(server)
	defer ts.Close()
	ws := dialTest(t, ts.URL)
	if err := ws.WriteMessage(t.Context(), []byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := readWithin(t, ws); err != nil {
		t.Fatal(err)
	}

	server.Close()
	if _, err := readWithin(t, ws); !errors.Is(err, io.EOF) {
		t.Fatalf("after the server closed, ReadMessage = %v, want io.EOF", err)
	}
}

func TestWebSocketOrigin(t *testing.T) {
	for _, tt := range []struct {
		name    string
		opts    []HTTPServerOption
		allowed bool
	}{
		{"other origin rejected by default", nil, false},
		{"allowed by pattern", []HTTPServerOption{WithWebSocketOrigins("app.example.com")}, true},
	} {
		server := NewHTTPServer(fakeAgent, tt.opts...)
		ts := httptest.NewServer(server)
		_, err := DialWebSocket(t.Context(), ts.URL, WithHTTPHeader("Origin", "https://app.example.com"))
		if (err == nil) != tt.allowed {
			t.Errorf("%s: dial error %v", tt.name, err)
		}
		if err != nil && !strings.Contains(err.Error(), "403") {
			t.Errorf("%s: want HTTP 403, got %v", tt.name, err)
		}
		server.Close()
		ts.Close()
	}
}
