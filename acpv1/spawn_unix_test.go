//go:build unix

package acpv1_test

import (
	"syscall"
	"testing"

	"github.com/ironpark/go-acp/acpv1"
)

// A terminal's Ctrl-C signals the foreground process group; the agent must
// be outside it so the client can answer with session/cancel instead.
func TestSpawnAgentOwnProcessGroup(t *testing.T) {
	cmd := agentCmd("serve")
	agent, err := acpv1.SpawnAgent(t.Context(), cmd, func(*acpv1.ClientSideConnection) acpv1.Client {
		return newTestClient()
	})
	if err != nil {
		t.Fatal(err)
	}
	defer agent.Close()

	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	if pgid == syscall.Getpgrp() {
		t.Fatalf("agent shares the client's process group %d", pgid)
	}
	if pgid != cmd.Process.Pid {
		t.Fatalf("agent process group = %d, want its own (%d)", pgid, cmd.Process.Pid)
	}
}
