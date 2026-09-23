//go:build !unix

package main

import "os/exec"

// detach is a no-op where Ctrl-C does not signal the whole process group.
func detach(*exec.Cmd) {}
