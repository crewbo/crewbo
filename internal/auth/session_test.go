package auth

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSessionSaveLoad(t *testing.T) {
	path := SessionPath(filepath.Join(t.TempDir(), "config"))
	if _, err := LoadSession(path); !errors.Is(err, ErrNoSession) {
		t.Fatalf("no file: %v", err)
	}
	s := Session{Server: "https://x", SessionToken: "cbs_1", GitHubLogin: "RM1338", ExpiresUnixMs: 5}
	if err := SaveSession(path, s); err != nil {
		t.Fatal(err)
	}
	s.SessionToken = "cbs_2" // a second login replaces the first
	if err := SaveSession(path, s); err != nil {
		t.Fatal(err)
	}
	if got, err := LoadSession(path); err != nil || got != s {
		t.Errorf("LoadSession = %+v %v", got, err)
	}
	if runtime.GOOS != "windows" {
		if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
			t.Errorf("mode = %v %v, want 0600", fi.Mode().Perm(), err)
		}
	}
	if err := os.WriteFile(path, []byte(`{"server":"x"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSession(path); err == nil || errors.Is(err, ErrNoSession) {
		t.Errorf("damaged file: %v", err)
	}
}
