package install

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Apply reads path (absent counts as empty), transforms it, and when the
// result differs replaces path atomically, or removes it when transform
// returns nil output. It returns whether the file changed.
//
// The backup at path+".crewbo.bak" is written only from a config without
// Crewbo hooks for agent, so uninstalling or reinstalling never replaces
// the user's original with a hooked copy.
func Apply(path, agent string, transform func([]byte) ([]byte, bool, error)) (bool, error) {
	orig, err := os.ReadFile(path) //nolint:gosec // the agent's own config file
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	out, changed, err := transform(orig)
	if err != nil || !changed {
		return false, err
	}
	if out == nil {
		return true, os.Remove(path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return false, err
	}
	mode := os.FileMode(0o600)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}
	if orig != nil && !installed(agent, orig) {
		if err := os.WriteFile(path+".crewbo.bak", orig, mode); err != nil { //nolint:gosec // the agent config path the user chose (CLAUDE_CONFIG_DIR, CODEX_HOME or home)
			return false, fmt.Errorf("back up %s: %w", path, err)
		}
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".crewbo-*")
	if err != nil {
		return false, err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(out); err != nil {
		_ = tmp.Close()
		return false, err
	}
	if err := tmp.Close(); err != nil {
		return false, err
	}
	if err := os.Chmod(tmp.Name(), mode); err != nil {
		return false, err
	}
	return true, os.Rename(tmp.Name(), path)
}
