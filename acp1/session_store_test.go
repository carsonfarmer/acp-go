package acp1_test

import (
	"context"
	"strings"
	"testing"

	acp "github.com/ironpark/acp-go"
	"github.com/ironpark/acp-go/acp1"
)

func TestSessionManagerLookup(t *testing.T) {
	manager := acp1.NewSessionManager(acp1.NewMemoryStore[string](),
		func(context.Context, *acp1.NewSessionRequest) (acp1.SessionID, string, error) {
			return "s1", "state", nil
		})
	if _, err := manager.NewSession(t.Context(), &acp1.NewSessionRequest{}); err != nil {
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
	a, b := acp1.GenerateToolCallID(), acp1.GenerateToolCallID()
	if a == b || !strings.HasPrefix(string(a), "call_") {
		t.Fatalf("tool call ids %q, %q", a, b)
	}
	if id := acp1.GenerateMessageID(); !strings.HasPrefix(string(id), "message_") {
		t.Fatalf("message id %q", id)
	}
}
