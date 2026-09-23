package acp1_test

import (
	"context"
	"errors"
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
	if got, err := manager.Lookup(t.Context(), "s1"); err != nil || got != "state" {
		t.Fatalf("Lookup(s1) = %q, %v", got, err)
	}
	if _, err := manager.Lookup(t.Context(), "nope"); !acp.IsCode(err, acp.ErrorCodeResourceNotFound) {
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

// failingStore fails every operation, as a store whose backend is down would.
type failingStore struct{ err error }

func (s failingStore) Get(context.Context, acp1.SessionID) (string, bool, error) {
	return "", false, s.err
}
func (s failingStore) Set(context.Context, acp1.SessionID, string) error { return s.err }
func (s failingStore) Delete(context.Context, acp1.SessionID) error      { return s.err }
func (s failingStore) List(context.Context) ([]acp1.SessionID, error)    { return nil, s.err }

func TestSessionManagerReturnsStoreErrors(t *testing.T) {
	storeErr := errors.New("store unavailable")
	manager := acp1.NewSessionManager[string](failingStore{storeErr},
		func(context.Context, *acp1.NewSessionRequest) (acp1.SessionID, string, error) {
			return "s1", "state", nil
		})
	ctx := t.Context()
	if _, err := manager.NewSession(ctx, &acp1.NewSessionRequest{}); !errors.Is(err, storeErr) {
		t.Errorf("NewSession = %v, want the store's error", err)
	}
	if _, err := manager.Lookup(ctx, "s1"); !errors.Is(err, storeErr) {
		t.Errorf("Lookup = %v, want the store's error", err)
	}
	if _, err := manager.List(ctx, &acp1.ListSessionsRequest{}); !errors.Is(err, storeErr) {
		t.Errorf("List = %v, want the store's error", err)
	}
}
