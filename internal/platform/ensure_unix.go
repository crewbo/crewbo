//go:build unix

package platform

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// ErrUnsafeDir means a directory exists but is a symlink, is not a
// directory, or belongs to another user, so it cannot hold the daemon socket.
var ErrUnsafeDir = errors.New("platform: unsafe directory")

// Ensure creates the directories with owner-only permissions. The runtime
// directory may sit in a shared /tmp, so an existing one must be a real
// directory owned by the current user; its mode is tightened to 0700.
func (p Paths) Ensure() error {
	for _, dir := range []string{p.Data, p.Config} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
	}
	if err := os.MkdirAll(p.Runtime, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", p.Runtime, err)
	}
	fi, err := os.Lstat(p.Runtime)
	if err != nil {
		return fmt.Errorf("check %s: %w", p.Runtime, err)
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !fi.IsDir() || !ok || int(st.Uid) != os.Getuid() {
		return fmt.Errorf("%s: %w", p.Runtime, ErrUnsafeDir)
	}
	if fi.Mode().Perm() != 0o700 {
		if err := os.Chmod(p.Runtime, 0o700); err != nil { //nolint:gosec // a directory needs the x bit; 0700 is owner-only
			return fmt.Errorf("restrict %s: %w", p.Runtime, err)
		}
	}
	return nil
}
