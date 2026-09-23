package acphttp

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	acp "github.com/ironpark/acp-go"
)

func TestHTTPServerEndsIdleConnection(t *testing.T) {
	ended := make(chan struct{})
	server := NewServer(func(ctx context.Context, tr acp.Transport) error {
		defer close(ended)
		return fakeAgent(ctx, tr)
	}, WithIdleTimeout(50*time.Millisecond))
	ts := httptest.NewServer(server)
	defer ts.Close()
	defer server.Close()

	// Initialize, then vanish without opening the stream or deleting.
	post := func(body string, header http.Header) *http.Response {
		req, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, ts.URL, strings.NewReader(body))
		req.Header = header
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp
	}
	resp := post(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`, http.Header{})
	conn := resp.Header.Get(ConnectionIDHeader)

	select {
	case <-ended:
	case <-time.After(2 * time.Second):
		t.Fatal("the idle connection was not ended")
	}
	resp = post(`{"jsonrpc":"2.0","id":2,"method":"session/new","params":{}}`, http.Header{ConnectionIDHeader: {conn}})
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("POST to the ended connection: status %d, want 404", resp.StatusCode)
	}
}

func TestHTTPServerKeepsConnectionWithStream(t *testing.T) {
	server, client, _ := newHTTPPair(t, WithIdleTimeout(50*time.Millisecond))
	call(t, client, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	expect(t, client, `"result"`)
	waitAttached(t, server)

	time.Sleep(200 * time.Millisecond)
	call(t, client, `{"jsonrpc":"2.0","id":2,"method":"session/new","params":{}}`)
	expect(t, client, `"sessionId":"s1"`)
}

func TestOutboundNewerReaderTakesOver(t *testing.T) {
	o := newOutbound()
	replaced, ok := o.attach(t.Context())
	if !ok {
		t.Fatal("first attach failed")
	}
	second := make(chan bool)
	go func() {
		_, ok := o.attach(t.Context())
		second <- ok
	}()
	select {
	case <-replaced:
	case <-time.After(2 * time.Second):
		t.Fatal("the first reader was not asked to give way")
	}
	select {
	case <-second:
		t.Fatal("the second reader attached while the first still read")
	case <-time.After(20 * time.Millisecond):
	}
	o.detach()
	if !<-second {
		t.Fatal("second attach failed")
	}
}

// dropFirstEvent fails the first SSE event written, as a network drop would.
type dropFirstEvent struct {
	http.ResponseWriter
	dropped *atomic.Bool
}

func (w dropFirstEvent) Write(b []byte) (int, error) {
	if bytes.HasPrefix(b, []byte("data:")) && w.dropped.CompareAndSwap(false, true) {
		return 0, errors.New("connection reset")
	}
	return w.ResponseWriter.Write(b)
}

func (w dropFirstEvent) Flush() { w.ResponseWriter.(http.Flusher).Flush() }

// A reply whose write fails is sent again when the client reopens the stream.
func TestHTTPStreamDropRedeliversReply(t *testing.T) {
	server := NewServer(fakeAgent)
	var dropped atomic.Bool
	var streams atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			streams.Add(1)
			w = dropFirstEvent{w, &dropped}
		}
		server.ServeHTTP(w, r)
	}))
	defer ts.Close()
	defer server.Close()
	client := NewClientTransport(ts.URL)
	defer client.Close()

	call(t, client, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	expect(t, client, `"result"`)
	// The reply is the first event on the connection stream.
	call(t, client, `{"jsonrpc":"2.0","id":2,"method":"session/new","params":{}}`)
	expect(t, client, `"sessionId":"s1"`)
	if !dropped.Load() {
		t.Fatal("no event was dropped")
	}
	if n := streams.Load(); n < 2 {
		t.Fatalf("%d stream GETs, want the stream reopened", n)
	}
	call(t, client, `{"jsonrpc":"2.0","id":3,"method":"session/prompt","params":{"sessionId":"s1","prompt":[]}}`)
	expect(t, client, `"method":"session/update"`)
	expect(t, client, `"stopReason":"end_turn"`)
}

func TestWebSocketServerDropsSilentClient(t *testing.T) {
	readErr := make(chan error, 1)
	server := NewServer(func(ctx context.Context, tr acp.Transport) error {
		_, err := tr.ReadMessage(ctx)
		readErr <- err
		return err
	}, WithWebSocketPing(50*time.Millisecond))
	ts := httptest.NewServer(server)
	defer ts.Close()
	defer server.Close()

	// A client that never reads never answers a ping.
	conn, _, err := websocket.Dial(t.Context(), ts.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()

	select {
	case err := <-readErr:
		if err == nil || errors.Is(err, io.EOF) || !strings.Contains(err.Error(), "not responding") {
			t.Fatalf("ReadMessage = %v, want the peer reported as not responding", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the silent client was not dropped")
	}
}

func TestWebSocketClientDropsSilentServer(t *testing.T) {
	// The agent never reads, so the server never answers a ping.
	server := NewServer(func(ctx context.Context, tr acp.Transport) error {
		<-ctx.Done()
		return nil
	})
	ts := httptest.NewServer(server)
	defer ts.Close()
	defer server.Close()

	ws, err := DialWebSocket(t.Context(), "ws"+strings.TrimPrefix(ts.URL, "http"),
		WithPingInterval(50*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	if _, err := readWithin(t, ws); err == nil || !strings.Contains(err.Error(), "not responding") {
		t.Fatalf("ReadMessage = %v, want the peer reported as not responding", err)
	}
}

// A stream the server refuses for good ends the transport with that error,
// not a clean EOF.
func TestHTTPStreamRefusedIsReported(t *testing.T) {
	server := NewServer(fakeAgent)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			http.Error(w, "token expired", http.StatusUnauthorized)
			return
		}
		server.ServeHTTP(w, r)
	}))
	defer ts.Close()
	defer server.Close()
	client := NewClientTransport(ts.URL)
	defer client.Close()

	call(t, client, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	expect(t, client, `"result"`)
	if _, err := readWithin(t, client); err == nil || errors.Is(err, io.EOF) || !strings.Contains(err.Error(), "401") {
		t.Fatalf("ReadMessage = %v, want the 401", err)
	}
}

func TestWebSocketPingDisabled(t *testing.T) {
	readErr := make(chan error, 1)
	server := NewServer(func(ctx context.Context, tr acp.Transport) error {
		_, err := tr.ReadMessage(ctx)
		readErr <- err
		return err
	}, WithWebSocketPing(0))
	ts := httptest.NewServer(server)
	defer ts.Close()
	defer server.Close()

	// A client that never reads keeps its connection when pings are off.
	conn, _, err := websocket.Dial(t.Context(), ts.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	select {
	case err := <-readErr:
		t.Fatalf("the connection ended: %v", err)
	case <-time.After(200 * time.Millisecond):
	}
}

// The idle wait starts once initialize has answered, however long the agent
// took to answer it.
func TestHTTPServerIdleWaitStartsAfterInitialize(t *testing.T) {
	server, client, _ := newHTTPPair(t, WithIdleTimeout(100*time.Millisecond))
	server.serve = func(ctx context.Context, tr acp.Transport) error {
		return fakeAgent(ctx, slowInitialize{tr})
	}
	call(t, client, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	expect(t, client, `"result"`)
	call(t, client, `{"jsonrpc":"2.0","id":2,"method":"session/new","params":{}}`)
	expect(t, client, `"sessionId":"s1"`)
}

// slowInitialize delays the agent's first message, its initialize reply,
// past the idle timeout.
type slowInitialize struct{ acp.Transport }

func (t slowInitialize) WriteMessage(ctx context.Context, msg jsontext.Value) error {
	if strings.Contains(string(msg), `"protocolVersion"`) {
		time.Sleep(300 * time.Millisecond)
	}
	return t.Transport.WriteMessage(ctx, msg)
}
