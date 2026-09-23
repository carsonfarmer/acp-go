//go:build unix

package acp

import "os"

// syncDir flushes dir's entries to disk, so a file created, renamed or
// removed in it stays that way after a power loss.
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	err = d.Sync()
	if closeErr := d.Close(); err == nil {
		err = closeErr
	}
	return err
}
