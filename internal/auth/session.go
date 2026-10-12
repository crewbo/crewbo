package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Session is a member's sign-in to one sync server, saved by `crewbo
// login` and read by the daemon.
type Session struct {
	Server        string `json:"server"`
	SessionToken  string `json:"session_token"`
	GitHubLogin   string `json:"github_login"`
	ExpiresUnixMs int64  `json:"expires_unix_ms"`
}

// ErrNoSession means nobody has signed in on this machine.
var ErrNoSession = errors.New("auth: not signed in; run crewbo login")

// SessionPath is where the session is saved inside the config directory.
func SessionPath(configDir string) string { return filepath.Join(configDir, "session.json") }

// SaveSession writes s to path, readable only by the owner on Unix (on
// Windows the per-user config directory's ACL protects it), replacing any
// earlier session atomically.
func SaveSession(path string, s Session) error {
	b, err := json.MarshalIndent(s, "", "  ") //nolint:gosec // G117: saving the session token is the point; the file is 0600
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".session-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// LoadSession reads the session saved at path.
func LoadSession(path string) (Session, error) {
	b, err := os.ReadFile(path) //nolint:gosec // path is the user's own config dir
	if errors.Is(err, os.ErrNotExist) {
		return Session{}, ErrNoSession
	}
	if err != nil {
		return Session{}, err
	}
	var s Session
	if err := json.Unmarshal(b, &s); err != nil || s.SessionToken == "" {
		return Session{}, fmt.Errorf("auth: %s is damaged; run crewbo login again", path)
	}
	return s, nil
}
