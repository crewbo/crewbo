//go:build unix

package daemon

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTokenFileIsOwnerOnly(t *testing.T) {
	p := testPaths(t)
	start(t, Config{Paths: p})
	fi, err := os.Stat(filepath.Join(p.Runtime, TokenName))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("token mode %v, want 0600", fi.Mode().Perm())
	}
}
