// Command dual-client connects to an agent with ACP v2 when the agent
// supports it and falls back to v1 when it does not, then runs one prompt.
//
//	go run ./docs/example/dual-client                     # dual-agent: speaks v2
//	go build -o /tmp/echo ./docs/example/echo
//	go run ./docs/example/dual-client /tmp/echo           # v1 only: falls back
//
// router.ClientConnector spawns the agent and initializes with v2 first. An
// agent that answers protocolVersion 1 is restarted and initialized with v1,
// so each version sends its own initialize request. The session code below
// then branches once on the version it got.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	acp "github.com/ironpark/go-acp"
	"github.com/ironpark/go-acp/acpv1"
	"github.com/ironpark/go-acp/acpv2"
	"github.com/ironpark/go-acp/router"
)

// The clients only receive updates; each Turn collects its own.
type v1Client struct{}

func (v1Client) SessionUpdate(context.Context, *acpv1.SessionNotification) error { return nil }

func (v1Client) RequestPermission(context.Context, *acpv1.RequestPermissionRequest) (*acpv1.RequestPermissionResponse, error) {
	return nil, acp.ErrMethodNotFound("session/request_permission")
}

type v2Client struct{}

func (v2Client) SessionUpdate(context.Context, *acpv2.UpdateSessionNotification) error { return nil }

func (v2Client) RequestPermission(context.Context, *acpv2.RequestPermissionRequest) (*acpv2.RequestPermissionResponse, error) {
	return nil, acp.ErrMethodNotFound("session/request_permission")
}

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, command []string) error {
	if len(command) == 0 {
		binary, cleanup, err := buildDualAgent()
		if err != nil {
			return err
		}
		defer cleanup()
		command = []string{binary}
	}

	clientInfo := &acpv1.Implementation{Name: "dual-client", Version: "0.1.0"}
	agent, err := router.NewClient().
		WithV1(func(*acpv1.ClientSideConnection) acpv1.Client { return v1Client{} },
			&acpv1.InitializeRequest{ClientInfo: clientInfo}).
		WithV2(func(*acpv2.ClientSideConnection) acpv2.Client { return v2Client{} },
			&acpv2.InitializeRequest{Info: acpv2.Implementation{Name: clientInfo.Name, Version: clientInfo.Version}}).
		// Spawn may start the agent twice, so it takes a command factory.
		Spawn(ctx, func() *exec.Cmd { return exec.Command(command[0], command[1:]...) })
	if err != nil {
		return err
	}
	defer agent.Close() // Close, Wait, Done and extension calls work on either version

	cwd, _ := os.Getwd()
	const prompt = "hello from dual-client"
	switch {
	case agent.V2 != nil:
		fmt.Printf("negotiated v2 with %s\n", agent.V2Init.Info.Name)
		session, err := agent.V2.StartSession(ctx, &acpv2.NewSessionRequest{Cwd: acpv2.AbsolutePath(cwd)})
		if err != nil {
			return err
		}
		turn, _, err := session.Prompt(ctx, acpv2.TextBlock(prompt))
		if err != nil {
			return err
		}
		text, reason, err := turn.Text()
		if err != nil {
			return err
		}
		fmt.Printf("<< %s\nstop reason: %s\n", text, *reason)
	case agent.V1 != nil:
		fmt.Println("negotiated v1")
		session, err := agent.V1.StartSession(ctx, &acpv1.NewSessionRequest{Cwd: cwd})
		if err != nil {
			return err
		}
		turn, err := session.Prompt(ctx, acpv1.TextBlock(prompt))
		if err != nil {
			return err
		}
		text, response, err := turn.Text()
		if err != nil {
			return err
		}
		fmt.Printf("<< %s\nstop reason: %s\n", text, response.StopReason)
	}
	return nil
}

// buildDualAgent compiles the dual-agent example into a temporary directory.
func buildDualAgent() (binary string, cleanup func(), err error) {
	_, currentFile, _, _ := runtime.Caller(0)
	dir, err := os.MkdirTemp("", "acp-dual-agent")
	if err != nil {
		return "", nil, err
	}
	cleanup = func() { os.RemoveAll(dir) }
	binary = filepath.Join(dir, "dual-agent")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	build := exec.Command("go", "build", "-o", binary, ".")
	build.Dir = filepath.Join(filepath.Dir(filepath.Dir(currentFile)), "dual-agent")
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("build dual-agent: %w", err)
	}
	return binary, cleanup, nil
}
