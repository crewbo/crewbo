//go:build unix

package platform

import (
	"fmt"
	"os"
)

// restrictSocket makes the socket file owner-only, on top of the 0700
// runtime directory.
func restrictSocket(path string) error {
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("restrict %s: %w", path, err)
	}
	return nil
}
