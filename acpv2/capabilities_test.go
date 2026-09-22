package acpv2_test

import (
	"context"
	"testing"

	"github.com/ironpark/go-acp/acpv2"
)

type bareAgent struct {
	*acpv2.SessionManager[struct{}]
}

func (bareAgent) Initialize(context.Context, *acpv2.InitializeRequest) (*acpv2.InitializeResponse, error) {
	return nil, nil
}
func (bareAgent) Prompt(context.Context, *acpv2.PromptRequest) (*acpv2.PromptResponse, error) {
	return nil, nil
}
func (bareAgent) CancelSession(context.Context, *acpv2.CancelSessionNotification) error { return nil }

type authAgent struct{ bareAgent }

func (authAgent) Login(context.Context, *acpv2.LoginAuthRequest) (*acpv2.LoginAuthResponse, error) {
	return nil, nil
}
func (authAgent) Logout(context.Context, *acpv2.LogoutAuthRequest) (*acpv2.LogoutAuthResponse, error) {
	return nil, nil
}

func TestCapabilitiesOfFollowsImplementedInterfaces(t *testing.T) {
	manager := acpv2.NewSessionManager(acpv2.NewMemoryStore[struct{}](), nil)

	caps := acpv2.CapabilitiesOf(bareAgent{manager})
	if caps.Session == nil || caps.Session.Delete == nil {
		t.Errorf("session capabilities = %+v, want delete", caps.Session)
	}
	if caps.Auth != nil || caps.Providers != nil || caps.Nes != nil {
		t.Errorf("advertised unimplemented capabilities: %+v", caps)
	}
	if caps := acpv2.CapabilitiesOf(authAgent{bareAgent{manager}}); caps.Auth == nil {
		t.Error("auth not advertised for an agent implementing AuthHandler")
	}
}
