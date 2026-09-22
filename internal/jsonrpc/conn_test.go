package jsonrpc

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// pipeTransport is a transport whose peer is a channel of raw messages, so a
// test can inject exactly the bytes it wants and inspect what was written.
type pipeTransport struct {
	in  chan jsontext.Value
	out chan jsontext.Value

	closeOnce sync.Once
}

func newPipeTransport() *pipeTransport {
	return &pipeTransport{
		in:  make(chan jsontext.Value, 16),
		out: make(chan jsontext.Value, 16),
	}
}

func (t *pipeTransport) ReadMessage(ctx context.Context) (jsontext.Value, error) {
	select {
	case msg, ok := <-t.in:
		if !ok {
			return nil, io.EOF
		}
		return msg, nil
	case <-ctx.Done():
		return nil, io.EOF
	}
}

func (t *pipeTransport) WriteMessage(_ context.Context, data jsontext.Value) error {
	t.out <- data.Clone()
	return nil
}

func (t *pipeTransport) Close() error {
	t.closeOnce.Do(func() { close(t.in) })
	return nil
}

// receive returns the next message written by the connection.
func (t *pipeTransport) receive(tb testing.TB) map[string]jsontext.Value {
	tb.Helper()
	select {
	case data := <-t.out:
		var msg map[string]jsontext.Value
		if err := json.Unmarshal(data, &msg); err != nil {
			tb.Fatalf("decode written message %s: %v", data, err)
		}
		return msg
	case <-time.After(2 * time.Second):
		tb.Fatal("timed out waiting for an outgoing message")
		return nil
	}
}

// start runs a connection over the transport until the test ends.
func start(tb testing.TB, request RequestHandler, notification NotificationHandler, transport Transport, opts ...Option) *Connection {
	tb.Helper()
	conn := New(request, notification, transport, opts...)
	ctx, cancel := context.WithCancel(tb.Context())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = conn.Start(ctx)
	}()
	tb.Cleanup(func() {
		cancel()
		<-done
	})
	return conn
}

func TestRequestIDIsEchoedVerbatim(t *testing.T) {
	transport := newPipeTransport()
	start(t, func(context.Context, string, jsontext.Value) (any, error) {
		return map[string]string{"ok": "yes"}, nil
	}, nil, transport)

	for _, id := range []string{`"abc"`, `7`, `null`} {
		transport.in <- jsontext.Value(`{"jsonrpc":"2.0","id":` + id + `,"method":"ping"}`)
		got := transport.receive(t)
		if string(got["id"]) != id {
			t.Errorf("id %s echoed as %s", id, got["id"])
		}
	}
}

func TestVoidHandlerRepliesWithEmptyObject(t *testing.T) {
	transport := newPipeTransport()
	start(t, func(context.Context, string, jsontext.Value) (any, error) {
		return nil, nil
	}, nil, transport)

	transport.in <- jsontext.Value(`{"jsonrpc":"2.0","id":1,"method":"ping"}`)
	if got := string(transport.receive(t)["result"]); got != `{}` {
		t.Errorf("result = %s, want {}", got)
	}
}

func TestPanicBecomesInternalError(t *testing.T) {
	transport := newPipeTransport()
	start(t, func(context.Context, string, jsontext.Value) (any, error) {
		panic("boom")
	}, nil, transport)

	transport.in <- jsontext.Value(`{"jsonrpc":"2.0","id":1,"method":"ping"}`)
	var wire wireError
	if err := json.Unmarshal(transport.receive(t)["error"], &wire); err != nil {
		t.Fatalf("decode error object: %v", err)
	}
	if wire.Code != CodeInternalError {
		t.Errorf("code = %d, want %d", wire.Code, CodeInternalError)
	}
	if !strings.Contains(wire.Message, "boom") {
		t.Errorf("message = %q, want it to mention the panic", wire.Message)
	}
}

func TestMissingHandlerReportsMethodNotFound(t *testing.T) {
	transport := newPipeTransport()
	start(t, nil, nil, transport)

	transport.in <- jsontext.Value(`{"jsonrpc":"2.0","id":1,"method":"nope"}`)
	var wire wireError
	if err := json.Unmarshal(transport.receive(t)["error"], &wire); err != nil {
		t.Fatalf("decode error object: %v", err)
	}
	if wire.Code != CodeMethodNotFound {
		t.Errorf("code = %d, want %d", wire.Code, CodeMethodNotFound)
	}
}

func TestIncomingCancelRequestCancelsHandler(t *testing.T) {
	transport := newPipeTransport()
	started := make(chan struct{})
	start(t, func(ctx context.Context, _ string, _ jsontext.Value) (any, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}, nil, transport)

	transport.in <- jsontext.Value(`{"jsonrpc":"2.0","id":1,"method":"slow"}`)
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("handler never started")
	}
	transport.in <- jsontext.Value(`{"jsonrpc":"2.0","method":"$/cancel_request","params":{"requestId":1}}`)

	var wire wireError
	if err := json.Unmarshal(transport.receive(t)["error"], &wire); err != nil {
		t.Fatalf("decode error object: %v", err)
	}
	if wire.Code != CodeRequestCancelled {
		t.Errorf("code = %d, want %d", wire.Code, CodeRequestCancelled)
	}
}

func TestCancelRequestNeverReachesNotificationHandler(t *testing.T) {
	transport := newPipeTransport()
	seen := make(chan string, 4)
	start(t, nil, func(_ context.Context, method string, _ jsontext.Value) error {
		seen <- method
		return nil
	}, transport)

	transport.in <- jsontext.Value(`{"jsonrpc":"2.0","method":"$/cancel_request","params":{"requestId":1}}`)
	transport.in <- jsontext.Value(`{"jsonrpc":"2.0","method":"after"}`)

	select {
	case method := <-seen:
		if method != "after" {
			t.Errorf("notification handler saw %q", method)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the notification")
	}
}

func TestOutgoingCancellationSendsCancelRequest(t *testing.T) {
	transport := newPipeTransport()
	conn := start(t, nil, nil, transport)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		_, err := conn.SendRequest(ctx, "slow", map[string]string{})
		done <- err
	}()

	request := transport.receive(t)
	if string(request["method"]) != `"slow"` {
		t.Fatalf("first message was %s", request["method"])
	}
	cancel()

	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Errorf("SendRequest err = %v, want context.Canceled", err)
	}
	notification := transport.receive(t)
	if string(notification["method"]) != `"$/cancel_request"` {
		t.Fatalf("second message was %s, want $/cancel_request", notification["method"])
	}
	if want := `{"requestId":` + string(request["id"]) + `}`; string(notification["params"]) != want {
		t.Errorf("params = %s, want %s", notification["params"], want)
	}
}

func TestNullResultResolvesTheCaller(t *testing.T) {
	transport := newPipeTransport()
	conn := start(t, nil, nil, transport)

	done := make(chan jsontext.Value, 1)
	go func() {
		result, err := conn.SendRequest(t.Context(), "ping", nil)
		if err != nil {
			t.Errorf("SendRequest: %v", err)
		}
		done <- result
	}()

	id := transport.receive(t)["id"]
	transport.in <- jsontext.Value(`{"jsonrpc":"2.0","id":` + string(id) + `,"result":null}`)

	select {
	case result := <-done:
		if string(result) != "null" {
			t.Errorf("result = %s, want null", result)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("caller never resolved")
	}
}

func TestResponseIDMatchesCanonically(t *testing.T) {
	transport := newPipeTransport()
	conn := start(t, nil, nil, transport)

	done := make(chan error, 1)
	go func() {
		_, err := conn.SendRequest(t.Context(), "ping", nil)
		done <- err
	}()

	id := strings.TrimSpace(string(transport.receive(t)["id"]))
	// A peer that echoes the id as 1.0 rather than 1 still matches.
	transport.in <- jsontext.Value(`{"jsonrpc":"2.0","id":` + id + `.0,"result":{}}`)

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("SendRequest: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("caller never resolved")
	}
}

func TestNotificationsAreHandledInOrder(t *testing.T) {
	transport := newPipeTransport()
	order := make(chan string, 8)
	start(t, nil, func(_ context.Context, method string, _ jsontext.Value) error {
		order <- method
		return nil
	}, transport)

	for _, method := range []string{"first", "second", "third"} {
		transport.in <- jsontext.Value(`{"jsonrpc":"2.0","method":"` + method + `"}`)
	}
	for _, want := range []string{"first", "second", "third"} {
		select {
		case got := <-order:
			if got != want {
				t.Fatalf("handled %q, want %q", got, want)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for %q", want)
		}
	}
}

func TestInFlightRequestStillRepliesAfterEOF(t *testing.T) {
	transport := newPipeTransport()
	release := make(chan struct{})
	started := make(chan struct{})
	conn := New(func(context.Context, string, jsontext.Value) (any, error) {
		close(started)
		<-release
		return map[string]string{"late": "yes"}, nil
	}, nil, transport)

	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = conn.Start(t.Context())
	}()

	transport.in <- jsontext.Value(`{"jsonrpc":"2.0","id":1,"method":"slow"}`)
	<-started
	transport.Close() // the peer hangs up while the handler is still running
	close(release)

	// Start must not return until the reply has been written: a process that
	// exits when Start returns would otherwise drop it.
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Start did not return")
	}
	select {
	case data := <-transport.out:
		var msg map[string]jsontext.Value
		if err := json.Unmarshal(data, &msg); err != nil {
			t.Fatalf("decode written message %s: %v", data, err)
		}
		if got := string(msg["result"]); got != `{"late":"yes"}` {
			t.Errorf("result = %s", got)
		}
	default:
		t.Fatal("the reply was not written before Start returned")
	}
}

func TestPendingRequestsFailWhenTheConnectionEnds(t *testing.T) {
	transport := newPipeTransport()
	conn := New(nil, nil, transport)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = conn.Start(t.Context())
	}()

	result := make(chan error, 1)
	go func() {
		_, err := conn.SendRequest(t.Context(), "ping", nil)
		result <- err
	}()
	transport.receive(t) // the request reached the wire
	transport.Close()

	select {
	case err := <-result:
		if err == nil {
			t.Error("SendRequest returned nil after the connection closed")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("caller was left waiting")
	}
	<-done
}

func TestMiddlewareWrapsBothDirections(t *testing.T) {
	transport := newPipeTransport()
	var calls []string
	var mu sync.Mutex
	record := func(name string) {
		mu.Lock()
		defer mu.Unlock()
		calls = append(calls, name)
	}

	notified := make(chan struct{})
	start(t,
		func(context.Context, string, jsontext.Value) (any, error) { return nil, nil },
		func(context.Context, string, jsontext.Value) error { close(notified); return nil },
		transport,
		WithMiddleware(Middleware{
			Request: func(next RequestHandler) RequestHandler {
				return func(ctx context.Context, method string, params jsontext.Value) (any, error) {
					record("request:" + method)
					return next(ctx, method, params)
				}
			},
			Notification: func(next NotificationHandler) NotificationHandler {
				return func(ctx context.Context, method string, params jsontext.Value) error {
					record("notification:" + method)
					return next(ctx, method, params)
				}
			},
		}),
	)

	transport.in <- jsontext.Value(`{"jsonrpc":"2.0","id":1,"method":"ask"}`)
	transport.receive(t)
	transport.in <- jsontext.Value(`{"jsonrpc":"2.0","method":"tell"}`)
	<-notified

	mu.Lock()
	defer mu.Unlock()
	want := []string{"request:ask", "notification:tell"}
	if len(calls) != len(want) || calls[0] != want[0] || calls[1] != want[1] {
		t.Errorf("middleware saw %v, want %v", calls, want)
	}
}
