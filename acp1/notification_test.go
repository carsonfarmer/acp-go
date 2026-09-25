package acp1_test

import (
	"testing"
	"time"

	acp "github.com/ironpark/acp-go"
	"github.com/ironpark/acp-go/acp1"
	schema "github.com/ironpark/acp-go/schema/v1"
)

// Notifications nobody handles are ignored, as the protocol asks, instead of
// reaching the error handler as "method not found" each time one arrives.
func TestUnhandledNotificationsAreIgnored(t *testing.T) {
	ctx := t.Context()
	reported := make(chan error, 8)
	agentConn, clientConn := acp1.Pipe(ctx, func(*acp1.AgentSideConnection) acp1.Agent { return bareAgent{} },
		func(*acp1.ClientSideConnection) acp1.Client { return newTestClient() },
		acp.WithErrorHandler(func(err error) { reported <- err }))

	// bareAgent handles neither documents nor extensions, and testClient
	// neither elicitations nor extensions.
	for name, send := range map[string]func() error{
		"document/didOpen": func() error {
			return clientConn.DidOpenDocument(ctx, &acp1.DidOpenDocumentNotification{
				SessionID: "session_1", URI: "file:///main.go", LanguageID: "go", Version: 1,
			})
		},
		"agent extension": func() error { return clientConn.ExtNotification(ctx, "_example.com/progress", nil) },
		"elicitation/complete": func() error {
			return agentConn.CompleteElicitation(ctx, &acp1.CompleteElicitationNotification{ElicitationID: "elicitation_1"})
		},
		"client extension": func() error { return agentConn.ExtNotification(ctx, "_example.com/progress", nil) },
	} {
		if err := send(); err != nil {
			t.Fatalf("send %s: %v", name, err)
		}
	}
	// Notifications are handled in order on each read loop, so once a
	// request sent after them is answered, even with an error, they have been
	// handled.
	_, _ = clientConn.ExtMethod(ctx, "_example.com/sync", nil)
	_, _ = agentConn.ExtMethod(ctx, "_example.com/sync", nil)
	select {
	case err := <-reported:
		t.Fatalf("an unhandled notification was reported: %v", err)
	default:
	}

	// A handled notification that fails is still reported.
	if err := clientConn.ExtNotification(ctx, schema.AgentMethodsSessionCancel, map[string]any{}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-reported:
		if !acp.IsCode(err, acp.ErrorCodeInvalidParams) {
			t.Errorf("reported %v, want invalid params", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("session/cancel without a session id was not reported")
	}
}
