package acp1_test

import (
	"context"
	"testing"

	"github.com/ironpark/acp-go/acp1"
)

// answerClient answers every permission request with response.
type answerClient struct {
	response *acp1.RequestPermissionResponse
	offered  []acp1.PermissionOption
}

func (*answerClient) SessionUpdate(context.Context, *acp1.SessionNotification) error { return nil }

func (c *answerClient) RequestPermission(_ context.Context, params *acp1.RequestPermissionRequest) (*acp1.RequestPermissionResponse, error) {
	c.offered = params.Options
	return c.response, nil
}

func TestSessionStreamRequestPermission(t *testing.T) {
	cases := []struct {
		name     string
		response *acp1.RequestPermissionResponse
		choice   acp1.PermissionOptionID
		allowed  bool
		fails    bool
	}{
		{"allow once", acp1.PermissionSelected("allow_once"), "allow_once", true, false},
		{"allow always", acp1.PermissionSelected("allow_always"), "allow_always", true, false},
		{"reject", acp1.PermissionSelected("reject_once"), "reject_once", false, false},
		{"cancelled", acp1.PermissionCancelled(), "", false, false},
		{"not offered", acp1.PermissionSelected("reject_always"), "", false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			client := &answerClient{response: c.response}
			stream := acp1.NewSessionStream(client, "s1")
			choice, allowed, err := stream.RequestPermission(t.Context(), acp1.ToolCallUpdate{ToolCallID: "call_1"})
			if (err != nil) != c.fails || choice.OptionID != c.choice || allowed != c.allowed {
				t.Fatalf("got %q, %v, %v; want %q, %v, fails %v", choice.OptionID, allowed, err, c.choice, c.allowed, c.fails)
			}
			if len(client.offered) != len(acp1.DefaultPermissionOptions()) {
				t.Fatalf("offered %v, want the default options", client.offered)
			}
		})
	}
}

func TestSessionStreamFiles(t *testing.T) {
	stream := acp1.NewSessionStream(newTestClient(), "s1")
	if got, err := stream.ReadTextFile(t.Context(), "/a.txt"); err != nil || got != "contents of /a.txt" {
		t.Fatalf("ReadTextFile = %q, %v", got, err)
	}
	// testClient serves no fs/write_text_file.
	if err := stream.WriteTextFile(t.Context(), "/a.txt", "x"); err == nil {
		t.Fatal("WriteTextFile through a client that cannot write succeeded")
	}
}
