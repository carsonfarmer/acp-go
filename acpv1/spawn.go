package acpv1

import (
	"context"
	"fmt"
	acp "github.com/ironpark/go-acp"
	"os/exec"
)

// SpawnAgent starts an agent process and connects to it over its stdio.
//
// The process is killed when ctx is cancelled, and the connection is closed
// once the process exits. Call [ClientSideConnection.Start] to begin
// processing messages.
//
// Nothing is done with the agent's stderr; use [SpawnAgentCmd] to capture it
// or to set the working directory or environment.
func SpawnAgent(ctx context.Context, newClient func(*ClientSideConnection) Client, command string, args ...string) (*ClientSideConnection, error) {
	return SpawnAgentCmd(ctx, newClient, exec.CommandContext(ctx, command, args...))
}

// SpawnAgentCmd connects to an agent process the caller has configured but not
// yet started. Its Stdin and Stdout are replaced by the connection's pipes, and
// the process is killed when ctx is done.
func SpawnAgentCmd(ctx context.Context, newClient func(*ClientSideConnection) Client, cmd *exec.Cmd, opts ...acp.Option) (*ClientSideConnection, error) {
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("agent stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("agent stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start agent process: %w", err)
	}

	conn := NewClientSideConnection(newClient, stdout, stdin, opts...)

	stopKill := context.AfterFunc(ctx, func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	})
	// cmd.Wait returns once the process exits, so this goroutine always ends.
	go func() {
		_ = cmd.Wait()
		stopKill()
		_ = conn.Close()
	}()
	return conn, nil
}
