//go:build unix

package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestDaemonStopsOnSIGINT builds the real binary, starts `crewbo daemon`,
// waits for `crewbo status`, sends SIGINT and expects a clean exit with the
// token file removed.
func TestDaemonStopsOnSIGINT(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs the binary")
	}
	root, err := os.MkdirTemp("", "cbm")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	bin := filepath.Join(root, "crewbo")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	run := filepath.Join(root, "run")
	env := append(os.Environ(), "HOME="+root, "TMPDIR="+root,
		"XDG_DATA_HOME="+root+"/data", "XDG_CONFIG_HOME="+root+"/config", "XDG_RUNTIME_DIR="+run)

	d := exec.Command(bin, "daemon")
	d.Env = env
	var logs bytes.Buffer
	d.Stderr = &logs
	if err := d.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Process.Kill() })

	deadline := time.Now().Add(10 * time.Second)
	for {
		s := exec.Command(bin, "status")
		s.Env = env
		if out, err := s.Output(); err == nil && strings.Contains(string(out), "running") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("status never reported running; daemon log:\n%s", logs.String())
		}
		time.Sleep(50 * time.Millisecond)
	}

	if err := d.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	exited := make(chan error, 1)
	go func() { exited <- d.Wait() }()
	select {
	case err := <-exited:
		if err != nil {
			t.Fatalf("daemon exit: %v\n%s", err, logs.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("daemon did not stop within 10 s of SIGINT")
	}
	if !strings.Contains(logs.String(), "daemon stopped") {
		t.Errorf("no clean-stop log line:\n%s", logs.String())
	}
	matches, _ := filepath.Glob(filepath.Join(run, "*", "daemon.token"))
	if _, err := os.Stat(filepath.Join(run, "crewbo", "daemon.token")); !errors.Is(err, os.ErrNotExist) || len(matches) != 0 {
		t.Errorf("token file left behind after SIGINT: %v %v", err, matches)
	}
}
