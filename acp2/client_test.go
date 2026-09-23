package acp2_test

import (
	"context"
	"testing"

	acp "github.com/ironpark/go-acp"
	"github.com/ironpark/go-acp/acp2"
)

// versionAgent answers initialize with the protocol version it was sent.
type versionAgent struct{ bareAgent }

func (versionAgent) Initialize(_ context.Context, params *acp2.InitializeRequest) (*acp2.InitializeResponse, error) {
	return &acp2.InitializeResponse{ProtocolVersion: params.ProtocolVersion}, nil
}

func TestInitializeFillsProtocolVersion(t *testing.T) {
	for _, params := range []*acp2.InitializeRequest{nil, {}} {
		_, conn := acp2.Pipe(t.Context(),
			func(*acp2.AgentSideConnection) acp2.Agent { return versionAgent{} },
			func(*acp2.ClientSideConnection) acp2.Client { return acp2.UnimplementedClient{} })
		got, err := conn.Initialize(t.Context(), params)
		if err != nil || got.ProtocolVersion != acp2.ProtocolVersion {
			t.Fatalf("Initialize(%v) = %v, %v; want protocol version %d", params, got, err, acp2.ProtocolVersion)
		}
		if params != nil && params.ProtocolVersion != 0 {
			t.Fatal("Initialize changed the caller's request")
		}
	}
}

func TestUnimplementedClient(t *testing.T) {
	var client acp2.Client = acp2.UnimplementedClient{}
	if err := client.SessionUpdate(t.Context(), &acp2.UpdateSessionNotification{}); err != nil {
		t.Fatalf("SessionUpdate: %v", err)
	}
	if _, err := client.RequestPermission(t.Context(), &acp2.RequestPermissionRequest{}); !acp.IsCode(err, acp.ErrorCodeMethodNotFound) {
		t.Fatalf("RequestPermission: %v, want method not found", err)
	}
}
