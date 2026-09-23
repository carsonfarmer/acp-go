package acpv1_test

import (
	"testing"

	"github.com/ironpark/go-acp/acpv1"
)

func TestSessionStreamWithMeta(t *testing.T) {
	client := newTestClient()
	stream := acpv1.NewSessionStream(client, "s1")
	var meta acpv1.Meta
	if err := meta.Set("trace", "abc"); err != nil {
		t.Fatal(err)
	}
	traced := stream.WithMeta(meta)
	if err := traced.SendText(t.Context(), "hi"); err != nil {
		t.Fatal(err)
	}
	got := <-client.updates
	if trace, ok, err := got.Meta.Get[string]("trace"); err != nil || !ok || trace != "abc" {
		t.Fatalf("_meta trace = %q %v %v, want abc", trace, ok, err)
	}
	if got.SessionID != "s1" {
		t.Fatalf("session %q, want s1", got.SessionID)
	}

	// The original stream is unchanged.
	if err := stream.SendText(t.Context(), "hi"); err != nil {
		t.Fatal(err)
	}
	if got := <-client.updates; got.Meta != nil {
		t.Fatalf("plain stream sent _meta %v", got.Meta)
	}
}
