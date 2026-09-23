package acpv1_test

import (
	"context"
	"testing"

	"github.com/ironpark/go-acp/acpv1"
)

// bareAgent implements only the required methods.
type bareAgent struct {
	*acpv1.SessionManager[struct{}]
}

func (bareAgent) Initialize(context.Context, *acpv1.InitializeRequest) (*acpv1.InitializeResponse, error) {
	return nil, nil
}
func (bareAgent) Prompt(context.Context, *acpv1.PromptRequest) (*acpv1.PromptResponse, error) {
	return nil, nil
}
func (bareAgent) Cancel(context.Context, *acpv1.CancelNotification) error { return nil }

// forkingAgent adds one optional interface on top of the manager's three.
type forkingAgent struct{ bareAgent }

func (forkingAgent) ForkSession(context.Context, *acpv1.ForkSessionRequest) (*acpv1.ForkSessionResponse, error) {
	return nil, nil
}

type logoutAgent struct{ bareAgent }

func (logoutAgent) Logout(context.Context, *acpv1.LogoutRequest) (*acpv1.LogoutResponse, error) {
	return nil, nil
}

func TestCapabilitiesOfFollowsImplementedInterfaces(t *testing.T) {
	manager := acpv1.NewSessionManager(acpv1.NewMemoryStore[struct{}](), nil)

	// The manager supplies session/load, session/list and session/delete.
	caps := acpv1.CapabilitiesOf(bareAgent{manager})
	if caps.LoadSession == nil || !*caps.LoadSession {
		t.Errorf("loadSession = %v, want true", caps.LoadSession)
	}
	if caps.SessionCapabilities == nil || caps.SessionCapabilities.List == nil || caps.SessionCapabilities.Delete == nil {
		t.Errorf("session capabilities = %+v, want list and delete", caps.SessionCapabilities)
	}
	if caps.SessionCapabilities.Fork != nil || caps.Providers != nil || caps.Nes != nil || caps.Auth != nil {
		t.Errorf("advertised unimplemented capabilities: %+v", caps)
	}
	if caps := acpv1.CapabilitiesOf(logoutAgent{bareAgent{manager}}); caps.Auth == nil || caps.Auth.Logout == nil {
		t.Error("auth.logout not advertised for an agent implementing LogoutHandler")
	}

	if caps := acpv1.CapabilitiesOf(forkingAgent{bareAgent{manager}}); caps.SessionCapabilities.Fork == nil {
		t.Error("fork not advertised for an agent implementing SessionForker")
	}
}

type readOnlyClient struct{}

func (readOnlyClient) SessionUpdate(context.Context, *acpv1.SessionNotification) error { return nil }
func (readOnlyClient) RequestPermission(context.Context, *acpv1.RequestPermissionRequest) (*acpv1.RequestPermissionResponse, error) {
	return nil, nil
}
func (readOnlyClient) ReadTextFile(context.Context, *acpv1.ReadTextFileRequest) (*acpv1.ReadTextFileResponse, error) {
	return nil, nil
}

func TestClientCapabilitiesOfFollowsImplementedInterfaces(t *testing.T) {
	caps := acpv1.ClientCapabilitiesOf(readOnlyClient{})
	if caps.Fs == nil || caps.Fs.ReadTextFile == nil || !*caps.Fs.ReadTextFile {
		t.Errorf("fs.readTextFile = %+v, want true", caps.Fs)
	}
	if caps.Fs.WriteTextFile == nil || *caps.Fs.WriteTextFile {
		t.Errorf("fs.writeTextFile = %+v, want explicit false", caps.Fs)
	}
	if caps.Terminal != nil || caps.Elicitation != nil {
		t.Errorf("advertised unimplemented capabilities: %+v", caps)
	}
}
