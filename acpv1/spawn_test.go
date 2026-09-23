package acpv1_test

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"

	acp "github.com/ironpark/go-acp"
	"github.com/ironpark/go-acp/acpv1"
)

// TestMain doubles as the agent process for the SpawnAgent tests.
func TestMain(m *testing.M) {
	switch os.Getenv("ACPV1_TEST_AGENT") {
	case "serve":
		conn := acpv1.NewAgentSideConnection(func(c *acpv1.AgentSideConnection) acpv1.Agent {
			a := newTestAgent()
			a.client = c
			return a
		}, os.Stdin, os.Stdout)
		_ = conn.Start(context.Background())
		os.Exit(0)
	case "fail":
		os.Stderr.WriteString("agent: missing credentials\n")
		os.Exit(3)
	}
	os.Exit(m.Run())
}

func agentCmd(mode string) *exec.Cmd {
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), "ACPV1_TEST_AGENT="+mode)
	return cmd
}

func TestSpawnAgentStartsReading(t *testing.T) {
	agent, err := acpv1.SpawnAgent(t.Context(), agentCmd("serve"), func(*acpv1.ClientSideConnection) acpv1.Client {
		return newTestClient()
	})
	if err != nil {
		t.Fatal(err)
	}
	// No Start call: SpawnAgent already runs the read loop.
	if _, err := agent.Initialize(t.Context(), &acpv1.InitializeRequest{ProtocolVersion: acpv1.ProtocolVersion}); err != nil {
		t.Fatal(err)
	}
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	if err := agent.Wait(); err != nil {
		t.Fatalf("clean shutdown reported %v", err)
	}
}

func TestSpawnAgentReportsProcessFailure(t *testing.T) {
	cmd := agentCmd("fail")
	var stderr strings.Builder
	cmd.Stderr = &stderr
	agent, err := acpv1.SpawnAgent(t.Context(), cmd, func(*acpv1.ClientSideConnection) acpv1.Client {
		return newTestClient()
	})
	if err != nil {
		t.Fatal(err)
	}
	err = agent.Wait()
	if err == nil || !strings.Contains(err.Error(), "exit status 3") {
		t.Fatalf("got %v", err)
	}
	if !strings.Contains(stderr.String(), "missing credentials") {
		t.Fatalf("stderr not forwarded: %q", stderr.String())
	}
}

type extParams struct {
	Path string `json:"path"`
}

type extResult struct {
	Files int `json:"files"`
}

// extAgent serves an extension method through an embedded ExtRouter.
type extAgent struct {
	bareAgent
	acp.ExtRouter
}

func TestExtRouterOverAConnection(t *testing.T) {
	agent := &extAgent{}
	acp.HandleExt(&agent.ExtRouter, "_test/index", func(_ context.Context, p *extParams) (*extResult, error) {
		return &extResult{Files: len(p.Path)}, nil
	})
	_, client := acpv1.Pipe(t.Context(), func(*acpv1.AgentSideConnection) acpv1.Agent { return agent },
		func(*acpv1.ClientSideConnection) acpv1.Client { return newTestClient() })

	got, err := acp.CallExt[extResult](t.Context(), client, "_test/index", extParams{Path: "abcd"})
	if err != nil || got.Files != 4 {
		t.Fatalf("got %+v %v", got, err)
	}
	if _, err := acp.CallExt[extResult](t.Context(), client, "_test/other", nil); !acp.IsCode(err, acp.ErrorCodeMethodNotFound) {
		t.Fatalf("unregistered method: %v", err)
	}
}
