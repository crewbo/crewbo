package platform

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveWindows(t *testing.T) {
	vars := map[string]string{`LOCALAPPDATA`: `C:\Users\u\AppData\Local`, `APPDATA`: `C:\Users\u\AppData\Roaming`}
	got, err := Resolve(Env{Getenv: func(k string) string { return vars[k] }})
	if err != nil {
		t.Fatal(err)
	}
	want := Paths{
		Data:    `C:\Users\u\AppData\Local\crewbo`,
		Config:  `C:\Users\u\AppData\Roaming\crewbo`,
		Runtime: `C:\Users\u\AppData\Local\crewbo\run`,
	}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
	if _, err := Resolve(Env{}); !errors.Is(err, ErrNoBaseDir) {
		t.Errorf("no LOCALAPPDATA: err = %v, want ErrNoBaseDir", err)
	}
}

func TestEnsureWindows(t *testing.T) {
	root := t.TempDir()
	p := Paths{Data: filepath.Join(root, "d"), Config: filepath.Join(root, "c"), Runtime: filepath.Join(root, "d", "run")}
	if err := p.Ensure(); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{p.Data, p.Config, p.Runtime} {
		if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
			t.Errorf("%s not created: %v", dir, err)
		}
	}
}
