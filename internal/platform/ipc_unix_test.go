//go:build unix

package platform

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSocketFileIsOwnerOnly(t *testing.T) {
	path := filepath.Join(shortDir(t), SocketName)
	ln, err := Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	fi, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode()&os.ModeSocket == 0 || fi.Mode().Perm() != 0o600 {
		t.Errorf("socket mode = %v, want socket with 0600", fi.Mode())
	}
}
