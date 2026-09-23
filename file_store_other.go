//go:build !unix

package acp

// syncDir does nothing where a directory cannot be synced, as on Windows; a
// rename there is durable once the file system commits it.
func syncDir(string) error { return nil }
