package acp2_test

import (
	"context"
	"strings"
	"testing"

	acp "github.com/ironpark/go-acp"
	"github.com/ironpark/go-acp/acp2"
)

func TestSessionManagerLookup(t *testing.T) {
	manager := acp2.NewSessionManager(acp2.NewMemoryStore[string](),
		func(context.Context, *acp2.NewSessionRequest) (acp2.SessionID, string, error) {
			return "s1", "state", nil
		})
	if _, err := manager.NewSession(t.Context(), &acp2.NewSessionRequest{}); err != nil {
		t.Fatal(err)
	}
	if got, err := manager.Lookup("s1"); err != nil || got != "state" {
		t.Fatalf("Lookup(s1) = %q, %v", got, err)
	}
	if _, err := manager.Lookup("nope"); !acp.IsCode(err, acp.ErrorCodeResourceNotFound) {
		t.Fatalf("Lookup(nope) = %v, want resource not found", err)
	}
}

func TestGeneratedIDs(t *testing.T) {
	a, b := acp2.GenerateToolCallID(), acp2.GenerateToolCallID()
	if a == b || !strings.HasPrefix(string(a), "call_") {
		t.Fatalf("tool call ids %q, %q", a, b)
	}
	if id := acp2.GenerateMessageID(); !strings.HasPrefix(string(id), "message_") {
		t.Fatalf("message id %q", id)
	}
}
