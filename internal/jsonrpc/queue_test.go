package jsonrpc

import (
	"context"
	"encoding/json/jsontext"
	"errors"
	"io"
	"sync"
	"testing"
	"time"
)

// stalledTransport is a peer that stopped reading: the first write blocks
// until the connection ends, so the write queue behind it fills up.
type stalledTransport struct {
	writing chan struct{} // closed once the first write is blocked
	once    sync.Once
}

func newStalledTransport() *stalledTransport {
	return &stalledTransport{writing: make(chan struct{})}
}

func (t *stalledTransport) ReadMessage(ctx context.Context) (jsontext.Value, error) {
	<-ctx.Done()
	return nil, io.EOF
}

func (t *stalledTransport) WriteMessage(ctx context.Context, _ jsontext.Value) error {
	t.once.Do(func() { close(t.writing) })
	<-ctx.Done()
	return ctx.Err()
}

func (*stalledTransport) Close() error { return nil }

// stall starts a connection whose one-slot write queue is full behind a
// write the peer never takes.
func stall(t *testing.T, fill bool) *Connection {
	t.Helper()
	transport := newStalledTransport()
	conn := start(t, nil, nil, transport, WithWriteQueueSize(1))
	if err := conn.SendNotification(t.Context(), "first", nil); err != nil {
		t.Fatal(err)
	}
	select {
	case <-transport.writing:
	case <-time.After(2 * time.Second):
		t.Fatal("the write loop never took the first message")
	}
	if fill {
		if err := conn.SendNotification(t.Context(), "second", nil); err != nil {
			t.Fatal(err)
		}
	}
	return conn
}

// returnsWithin fails the test unless send returns ctx's deadline error
// promptly after the deadline passes.
func returnsWithin(t *testing.T, send func(ctx context.Context) error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- send(ctx) }()
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("got %v, want the caller's deadline", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("still blocked on the full write queue after the caller's deadline")
	}
}

// A peer that stops reading must not hold a caller past its own context,
// such as a cancelled turn streaming its updates.
func TestFullWriteQueueRespectsCallerContext(t *testing.T) {
	t.Run("notification", func(t *testing.T) {
		conn := stall(t, true)
		returnsWithin(t, func(ctx context.Context) error {
			return conn.SendNotification(ctx, "third", nil)
		})
	})
	t.Run("request", func(t *testing.T) {
		conn := stall(t, true)
		returnsWithin(t, func(ctx context.Context) error {
			_, err := conn.SendRequest(ctx, "third", nil)
			return err
		})
	})
}

// A request that was queued, but whose caller gave up while the queue is
// full, returns without waiting to queue its $/cancel_request.
func TestAbandonedRequestReturnsWhileQueueIsFull(t *testing.T) {
	conn := stall(t, false)
	returnsWithin(t, func(ctx context.Context) error {
		_, err := conn.SendRequest(ctx, "takes the last slot", nil)
		return err
	})
}
