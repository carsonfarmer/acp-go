package acp

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"log/slog"
	"strings"
	"testing"
)

func TestLoggingMiddleware(t *testing.T) {
	var out bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&out, &slog.HandlerOptions{Level: slog.LevelDebug}))
	mw := LoggingMiddleware(logger)

	request := mw.Request(func(context.Context, string, jsontext.Value) (any, error) { return nil, nil })
	failing := mw.Request(func(context.Context, string, jsontext.Value) (any, error) {
		return nil, ErrMethodNotFound("x/y")
	})
	notification := mw.Notification(func(context.Context, string, jsontext.Value) error { return nil })
	request(t.Context(), "session/new", nil)
	failing(t.Context(), "x/y", nil)
	notification(t.Context(), "session/update", nil)

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	want := []string{
		`level=INFO msg="acp request" method=session/new duration=`,
		`level=WARN msg="acp request" method=x/y duration=`,
		`level=DEBUG msg="acp notification" method=session/update duration=`,
	}
	if len(lines) != len(want) {
		t.Fatalf("logged %d lines, want %d:\n%s", len(lines), len(want), out.String())
	}
	for i, w := range want {
		if !strings.Contains(lines[i], w) {
			t.Errorf("line %d = %s, want it to contain %s", i, lines[i], w)
		}
	}
	if !strings.Contains(lines[1], "code=-32601") || !strings.Contains(lines[1], "error=") {
		t.Errorf("failure line lacks the error and its code: %s", lines[1])
	}
}
