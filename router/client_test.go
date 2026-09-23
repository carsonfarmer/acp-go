package router_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"testing"

	acp "github.com/ironpark/go-acp"
	"github.com/ironpark/go-acp/acpv1"
	"github.com/ironpark/go-acp/acpv2"
	"github.com/ironpark/go-acp/router"
)

// TestMain doubles as the agent process for the ClientConnector tests.
func TestMain(m *testing.M) {
	ctx := context.Background()
	newV1 := func(*acpv1.AgentSideConnection) acpv1.Agent {
		return &v1Agent{initialized: make(chan *acpv1.InitializeRequest, 1)}
	}
	newV2 := func(*acpv2.AgentSideConnection) acpv2.Agent {
		return &v2Agent{initialized: make(chan *acpv2.InitializeRequest, 1)}
	}
	switch os.Getenv("ROUTER_TEST_AGENT") {
	case "v1":
		// A plain v1 agent with no router: it receives the v2 initialize
		// request as is.
		_ = acpv1.NewAgentSideConnection(newV1, os.Stdin, os.Stdout).Start(ctx)
		os.Exit(0)
	case "both":
		_ = router.New().WithV1(newV1).WithV2(newV2).ServeStdio(ctx, os.Stdin, os.Stdout)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func agentCommand(mode string) func() *exec.Cmd {
	return func() *exec.Cmd {
		cmd := exec.Command(os.Args[0], "-test.run=^$")
		cmd.Env = append(os.Environ(), "ROUTER_TEST_AGENT="+mode)
		return cmd
	}
}

type v1Client struct{}

func (v1Client) SessionUpdate(context.Context, *acpv1.SessionNotification) error { return nil }
func (v1Client) RequestPermission(context.Context, *acpv1.RequestPermissionRequest) (*acpv1.RequestPermissionResponse, error) {
	return nil, nil
}

type v2Client struct{}

func (v2Client) SessionUpdate(context.Context, *acpv2.UpdateSessionNotification) error { return nil }
func (v2Client) RequestPermission(context.Context, *acpv2.RequestPermissionRequest) (*acpv2.RequestPermissionResponse, error) {
	return nil, nil
}

func connector() *router.ClientConnector {
	return router.NewClient().
		WithV1(func(*acpv1.ClientSideConnection) acpv1.Client { return v1Client{} }, nil).
		WithV2(func(*acpv2.ClientSideConnection) acpv2.Client { return v2Client{} }, nil)
}

func TestClientPrefersV2(t *testing.T) {
	agent, err := connector().Spawn(t.Context(), agentCommand("both"))
	if err != nil {
		t.Fatal(err)
	}
	defer agent.Close()
	if agent.V2 == nil || agent.V1 != nil || agent.V2Init.ProtocolVersion != 2 {
		t.Fatalf("got %+v", agent)
	}
	session, err := agent.V2.StartSession(t.Context(), &acpv2.NewSessionRequest{Cwd: "/tmp"})
	if err != nil || session.ID != "v2-session" {
		t.Fatalf("got %+v %v", session, err)
	}
}

func TestClientFallsBackToV1(t *testing.T) {
	agent, err := connector().Spawn(t.Context(), agentCommand("v1"))
	if err != nil {
		t.Fatal(err)
	}
	defer agent.Close()
	if agent.V1 == nil || agent.V2 != nil || agent.V1Init.ProtocolVersion != 1 {
		t.Fatalf("got %+v", agent)
	}
	session, err := agent.V1.StartSession(t.Context(), &acpv1.NewSessionRequest{Cwd: "/tmp"})
	if err != nil || session.ID != "v1-session" {
		t.Fatalf("got %+v %v", session, err)
	}
	// Extension calls go through Agent without knowing the version.
	if _, err := acp.CallExt[struct{}](t.Context(), agent, "_test/none", nil); !acp.IsCode(err, acp.ErrorCodeMethodNotFound) {
		t.Fatalf("ext call: %v", err)
	}
}

func TestClientWithoutV1RejectsV1Agent(t *testing.T) {
	_, err := router.NewClient().
		WithV2(func(*acpv2.ClientSideConnection) acpv2.Client { return v2Client{} }, nil).
		Spawn(t.Context(), agentCommand("v1"))
	if !errors.Is(err, router.ErrNoCommonVersion) {
		t.Fatalf("got %v", err)
	}
}
