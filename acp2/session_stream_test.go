package acp2_test

import (
	"testing"

	"github.com/ironpark/acp-go/acp2"
)

func TestSessionStreamWithMeta(t *testing.T) {
	client := newTestClient()
	stream := acp2.NewSessionStream(client, "s1")
	var meta acp2.Meta
	if err := meta.Set("trace", "abc"); err != nil {
		t.Fatal(err)
	}
	traced := stream.WithMeta(meta)
	if err := traced.SendText(t.Context(), "m1", "hi"); err != nil {
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
	if err := stream.SendText(t.Context(), "m1", "hi"); err != nil {
		t.Fatal(err)
	}
	if got := <-client.updates; got.Meta != nil {
		t.Fatalf("plain stream sent _meta %v", got.Meta)
	}
}

func TestSessionStreamPlanAndToolContent(t *testing.T) {
	client := newTestClient()
	stream := acp2.NewSessionStream(client, "s1")
	entries := []acp2.PlanEntry{{Content: "Read the file", Priority: acp2.PlanEntryPriorityHigh, Status: acp2.PlanEntryStatusPending}}
	if err := stream.SendPlan(t.Context(), "plan_1", entries); err != nil {
		t.Fatal(err)
	}
	update, ok := (<-client.updates).Update.As[acp2.SessionUpdatePlanUpdate]()
	if !ok {
		t.Fatal("SendPlan sent another update")
	}
	items, ok := update.Plan.As[acp2.PlanUpdateContentItems]()
	if !ok || items.PlanID != "plan_1" || len(items.Entries) != 1 {
		t.Fatalf("plan = %+v", update.Plan)
	}
	if tag := acp2.ToolDiff(acp2.NewDiffChange(acp2.DiffChangeDelete{Path: "/a"})).Tag(); tag != "diff" {
		t.Errorf("ToolDiff tag %q", tag)
	}
	if tag := acp2.ToolTerminal("term_1").Tag(); tag != "terminal" {
		t.Errorf("ToolTerminal tag %q", tag)
	}
}
