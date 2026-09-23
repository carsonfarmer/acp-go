package acphttp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	acp "github.com/ironpark/acp-go"
)

type userKey struct{}

// withUser stands in for an authentication middleware.
func withUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userKey{}, "alice")))
	})
}

func TestServeContextCarriesRequestValues(t *testing.T) {
	users := make(chan any, 2)
	server := NewServer(func(ctx context.Context, tr acp.Transport) error {
		users <- ctx.Value(userKey{})
		return fakeAgent(ctx, tr)
	})
	ts := httptest.NewServer(withUser(server))
	defer ts.Close()
	defer server.Close()

	client := NewClientTransport(ts.URL)
	defer client.Close()
	call(t, client, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	expect(t, client, `"result"`)
	if user := <-users; user != "alice" {
		t.Errorf("Streamable HTTP serve saw user %v", user)
	}
	dialTest(t, ts.URL)
	if user := <-users; user != "alice" {
		t.Errorf("WebSocket serve saw user %v", user)
	}
}

func TestShutdownWaitsForConnections(t *testing.T) {
	server, client, ts := newHTTPPair(t)
	call(t, client, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	expect(t, client, `"result"`)

	shut := make(chan error, 1)
	go func() { shut <- server.Shutdown(t.Context()) }()
	// A new connection is refused while the open one drains.
	for deadline := time.Now().Add(2 * time.Second); ; time.Sleep(time.Millisecond) {
		resp, err := http.Post(ts.URL, "application/json", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusServiceUnavailable {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("initialize during shutdown got %s", resp.Status)
		}
	}
	select {
	case err := <-shut:
		t.Fatalf("Shutdown returned %v with a connection open", err)
	case <-time.After(50 * time.Millisecond):
	}
	client.Close() // deletes the connection
	select {
	case err := <-shut:
		if err != nil {
			t.Fatalf("Shutdown = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Shutdown did not return once the connection ended")
	}
}

func TestShutdownGivesUpWithItsContext(t *testing.T) {
	server, client, _ := newHTTPPair(t)
	call(t, client, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	expect(t, client, `"result"`)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if err := server.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Shutdown = %v, want the deadline", err)
	}
}

func TestServeErrorsReachErrorHandler(t *testing.T) {
	failure := errors.New("agent failed to start")
	reported := make(chan error, 1)
	server := NewServer(func(context.Context, acp.Transport) error { return failure },
		WithErrorHandler(func(err error) { reported <- err }))
	ts := httptest.NewServer(server)
	defer ts.Close()
	defer server.Close()
	dialTest(t, ts.URL)
	select {
	case err := <-reported:
		if !errors.Is(err, failure) {
			t.Fatalf("reported %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the error was not reported")
	}
}

func TestStatusErrors(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "token expired", http.StatusUnauthorized)
	}))
	defer ts.Close()

	client := NewClientTransport(ts.URL)
	defer client.Close()
	err := client.WriteMessage(t.Context(), []byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`))
	if status, ok := errors.AsType[*StatusError](err); !ok || status.StatusCode != http.StatusUnauthorized || string(status.Body) != "token expired" {
		t.Errorf("Streamable HTTP error = %#v", err)
	}
	_, err = DialWebSocket(t.Context(), "ws"+strings.TrimPrefix(ts.URL, "http"))
	if status, ok := errors.AsType[*StatusError](err); !ok || status.StatusCode != http.StatusUnauthorized {
		t.Errorf("WebSocket error = %#v", err)
	}
}
