package acp1_test

import (
	"encoding/json/jsontext"
	"testing"

	"github.com/ironpark/acp-go/acp1"
	schema "github.com/ironpark/acp-go/schema/v1"
)

func TestSessionStreamWithMeta(t *testing.T) {
	client := newTestClient()
	stream := acp1.NewSessionStream(client, "s1")
	var meta acp1.Meta
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

func TestSessionStreamToolCallOptions(t *testing.T) {
	client := newTestClient()
	stream := acp1.NewSessionStream(client, "s1")
	ctx := t.Context()

	err := stream.StartToolCall(ctx, "call_1", "Edit", acp1.ToolKindEdit,
		acp1.WithLocations(acp1.ToolCallLocation{Path: "/a.go"}),
		acp1.WithRawInput(jsontext.Value(`{"path":"/a.go"}`)))
	if err != nil {
		t.Fatal(err)
	}
	start, ok := (<-client.updates).Update.As[schema.SessionUpdateToolCall]()
	if !ok || len(start.Locations) != 1 || start.Locations[0].Path != "/a.go" || string(start.RawInput) != `{"path":"/a.go"}` {
		t.Fatalf("start = %+v", start)
	}

	err = stream.CompleteToolCall(ctx, "call_1",
		acp1.WithToolContent(acp1.ToolText("done")),
		acp1.WithRawOutput(jsontext.Value(`{"ok":true}`)))
	if err != nil {
		t.Fatal(err)
	}
	done, ok := (<-client.updates).Update.As[schema.SessionUpdateToolCallUpdate]()
	if !ok || done.GetStatus() != schema.ToolCallStatusCompleted || len(done.Content) != 1 || string(done.RawOutput) != `{"ok":true}` {
		t.Fatalf("complete = %+v", done)
	}
}
