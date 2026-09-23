//go:build unix

package main

import (
	"os/exec"
	"syscall"
)

// detach starts cmd in its own process group, so the SIGINT a terminal sends
// on Ctrl-C reaches only the client, which turns it into session/cancel,
// instead of also killing the agent.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}
