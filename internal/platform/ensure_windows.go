package platform

import (
	"fmt"
	"os"
)

// Ensure creates the directories. On Windows the mode argument is ignored;
// the folders inherit the user-only ACL of %LOCALAPPDATA% and %APPDATA%.
func (p Paths) Ensure() error {
	for _, dir := range []string{p.Data, p.Config, p.Runtime} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
	}
	return nil
}
