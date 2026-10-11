//go:build unix

package platform

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureUnix(t *testing.T) {
	root := t.TempDir()
	p := Paths{Data: filepath.Join(root, "data"), Config: filepath.Join(root, "config"), Runtime: filepath.Join(root, "run")}
	if err := p.Ensure(); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{p.Data, p.Config, p.Runtime} {
		fi, err := os.Stat(dir)
		if err != nil || fi.Mode().Perm() != 0o700 {
			t.Errorf("%s: mode %v, err %v; want 0700", dir, fi.Mode().Perm(), err)
		}
	}
	// A runtime dir left with loose permissions is tightened.
	if err := os.Chmod(p.Runtime, 0o755); err != nil { //nolint:gosec // deliberately loosened for the test
		t.Fatal(err)
	}
	if err := p.Ensure(); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(p.Runtime); fi.Mode().Perm() != 0o700 {
		t.Errorf("runtime mode %v, want 0700", fi.Mode().Perm())
	}
}

func TestEnsureUnixRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "elsewhere")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "run")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	p := Paths{Data: filepath.Join(root, "d"), Config: filepath.Join(root, "c"), Runtime: link}
	if err := p.Ensure(); !errors.Is(err, ErrUnsafeDir) {
		t.Errorf("err = %v, want ErrUnsafeDir", err)
	}
}

func TestEnsureUnixRejectsFile(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "run")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	p := Paths{Data: filepath.Join(root, "d"), Config: filepath.Join(root, "c"), Runtime: file}
	if err := p.Ensure(); err == nil {
		t.Error("Ensure accepted a regular file as the runtime directory")
	}
}

func TestDefaultEnv(t *testing.T) {
	env, err := DefaultEnv()
	if err != nil {
		t.Fatal(err)
	}
	if env.Home == "" || env.UID != os.Getuid() {
		t.Errorf("DefaultEnv = %+v", env)
	}
	if _, err := Resolve(env); err != nil {
		t.Errorf("Resolve(DefaultEnv()): %v", err)
	}
}
