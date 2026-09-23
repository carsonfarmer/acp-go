package acp2_test

import (
	"context"
	"testing"

	"github.com/ironpark/acp-go/acp2"
)

// answerClient answers every permission request with response.
type answerClient struct {
	response *acp2.RequestPermissionResponse
	offered  []acp2.PermissionOption
}

func (*answerClient) SessionUpdate(context.Context, *acp2.UpdateSessionNotification) error {
	return nil
}

func (c *answerClient) RequestPermission(_ context.Context, params *acp2.RequestPermissionRequest) (*acp2.RequestPermissionResponse, error) {
	c.offered = params.Options
	return c.response, nil
}

func TestSessionStreamRequestPermission(t *testing.T) {
	cases := []struct {
		name     string
		response *acp2.RequestPermissionResponse
		choice   acp2.PermissionOptionID
		allowed  bool
		fails    bool
	}{
		{"allow once", acp2.PermissionSelected("allow_once"), "allow_once", true, false},
		{"allow always", acp2.PermissionSelected("allow_always"), "allow_always", true, false},
		{"reject", acp2.PermissionSelected("reject_once"), "reject_once", false, false},
		{"cancelled", acp2.PermissionCancelled(), "", false, false},
		{"not offered", acp2.PermissionSelected("reject_always"), "", false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			client := &answerClient{response: c.response}
			stream := acp2.NewSessionStream(client, "s1")
			choice, allowed, err := stream.RequestPermission(t.Context(), "Write config.json", acp2.RequestPermissionSubject{})
			if (err != nil) != c.fails || choice.OptionID != c.choice || allowed != c.allowed {
				t.Fatalf("got %q, %v, %v; want %q, %v, fails %v", choice.OptionID, allowed, err, c.choice, c.allowed, c.fails)
			}
			if len(client.offered) != len(acp2.DefaultPermissionOptions()) {
				t.Fatalf("offered %v, want the default options", client.offered)
			}
		})
	}
}
