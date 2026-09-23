//go:build !unix

package acpconn

import "os/exec"

// detach is a no-op where Ctrl-C does not signal a whole process group.
func detach(*exec.Cmd) {}
