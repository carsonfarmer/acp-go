package acp1_test

import (
	"context"
	"testing"

	acp "github.com/ironpark/go-acp"
	"github.com/ironpark/go-acp/acp1"
)

// versionAgent answers initialize with the protocol version it was sent.
type versionAgent struct{ bareAgent }

func (versionAgent) Initialize(_ context.Context, params *acp1.InitializeRequest) (*acp1.InitializeResponse, error) {
	return &acp1.InitializeResponse{ProtocolVersion: params.ProtocolVersion}, nil
}

func TestInitializeFillsProtocolVersion(t *testing.T) {
	for _, params := range []*acp1.InitializeRequest{nil, {}} {
		_, conn := acp1.Pipe(t.Context(),
			func(*acp1.AgentSideConnection) acp1.Agent { return versionAgent{} },
			func(*acp1.ClientSideConnection) acp1.Client { return acp1.UnimplementedClient{} })
		got, err := conn.Initialize(t.Context(), params)
		if err != nil || got.ProtocolVersion != acp1.ProtocolVersion {
			t.Fatalf("Initialize(%v) = %v, %v; want protocol version %d", params, got, err, acp1.ProtocolVersion)
		}
		if params != nil && params.ProtocolVersion != 0 {
			t.Fatal("Initialize changed the caller's request")
		}
	}
}

func TestUnimplementedClient(t *testing.T) {
	var client acp1.Client = acp1.UnimplementedClient{}
	if err := client.SessionUpdate(t.Context(), &acp1.SessionNotification{}); err != nil {
		t.Fatalf("SessionUpdate: %v", err)
	}
	if _, err := client.RequestPermission(t.Context(), &acp1.RequestPermissionRequest{}); !acp.IsCode(err, acp.ErrorCodeMethodNotFound) {
		t.Fatalf("RequestPermission: %v, want method not found", err)
	}
}
