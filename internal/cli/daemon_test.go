package cli

import (
	"bytes"
	"context"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/crewbo/crewbo/internal/daemon"
	"github.com/crewbo/crewbo/internal/platform"
)

// isolate points platform.DefaultEnv at short temporary directories for this
// test, using each OS's variables (internal/platform/paths_*.go).
func isolate(t *testing.T) platform.Paths {
	t.Helper()
	root, err := os.MkdirTemp("", "cbc")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	switch runtime.GOOS {
	case "windows":
		t.Setenv("LOCALAPPDATA", root+`\local`)
		t.Setenv("APPDATA", root+`\roaming`)
	case "darwin":
		t.Setenv("HOME", root)
		t.Setenv("TMPDIR", root)
	default:
		t.Setenv("HOME", root)
		t.Setenv("XDG_DATA_HOME", root+"/data")
		t.Setenv("XDG_CONFIG_HOME", root+"/config")
		t.Setenv("XDG_RUNTIME_DIR", root+"/run")
	}
	p, err := paths()
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestStatusNotRunning(t *testing.T) {
	isolate(t)
	var stdout, stderr bytes.Buffer
	if code := Run(context.Background(), []string{"status"}, &stdout, &stderr, BuildInfo{}); code != ExitError {
		t.Errorf("exit = %d, want %d", code, ExitError)
	}
	if !strings.Contains(stdout.String(), "not running") {
		t.Errorf("stdout = %q", stdout.String())
	}
}

func TestStatusRunning(t *testing.T) {
	p := isolate(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = daemon.New(daemon.Config{Paths: p, Version: "v9.9.9"}).Run(ctx)
	}()
	t.Cleanup(func() { cancel(); <-done })

	var stdout, stderr bytes.Buffer
	deadline := time.Now().Add(5 * time.Second)
	for {
		stdout.Reset()
		if Run(context.Background(), []string{"status"}, &stdout, &stderr, BuildInfo{}) == ExitOK {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("status never succeeded: %q %q", stdout.String(), stderr.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !strings.Contains(stdout.String(), "running: version v9.9.9") {
		t.Errorf("stdout = %q", stdout.String())
	}
}
