//go:build unix

package acpconn

import (
	"os/exec"
	"syscall"
)

// detach starts cmd in its own process group unless the caller configured
// process attributes. A terminal's Ctrl-C signals the whole foreground group;
// the agent must not die with it, since the client may want to turn Ctrl-C
// into session/cancel. The agent still exits when the client goes away,
// through EOF on its stdin.
func detach(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	}
}
