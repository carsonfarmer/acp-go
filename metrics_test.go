package acp

import (
	"context"
	"encoding/json/jsontext"
	"testing"
	"time"
)

type observation struct {
	method   string
	kind     RPCKind
	duration time.Duration
	err      error
}

func TestMetricsMiddleware(t *testing.T) {
	var got []observation
	mw := MetricsMiddleware(MetricsFunc(func(_ context.Context, method string, kind RPCKind, d time.Duration, err error) {
		got = append(got, observation{method, kind, d, err})
	}))

	request := mw.Request(func(context.Context, string, jsontext.Value) (any, error) { return nil, nil })
	failing := mw.Request(func(context.Context, string, jsontext.Value) (any, error) {
		return nil, ErrMethodNotFound("x/y")
	})
	notification := mw.Notification(func(context.Context, string, jsontext.Value) error { return nil })

	request(t.Context(), "session/new", nil)
	failing(t.Context(), "x/y", nil)
	notification(t.Context(), "session/update", nil)

	want := []struct {
		method string
		kind   RPCKind
		failed bool
	}{
		{"session/new", RPCRequest, false},
		{"x/y", RPCRequest, true},
		{"session/update", RPCNotification, false},
	}
	if len(got) != len(want) {
		t.Fatalf("observed %d messages, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i].method != w.method || got[i].kind != w.kind {
			t.Errorf("observation %d = %q/%q, want %q/%q", i, got[i].method, got[i].kind, w.method, w.kind)
		}
		if got[i].duration < 0 {
			t.Errorf("observation %d has a negative duration %s", i, got[i].duration)
		}
		if (got[i].err != nil) != w.failed {
			t.Errorf("observation %d err = %v, want failed=%v", i, got[i].err, w.failed)
		}
	}
}

func TestMetricsMiddlewareNilIsNoOp(t *testing.T) {
	// The connection skips nil hooks, so the zero Middleware wraps nothing.
	if mw := MetricsMiddleware(nil); mw.Request != nil || mw.Notification != nil {
		t.Fatal("MetricsMiddleware(nil) should install no hooks")
	}
}
