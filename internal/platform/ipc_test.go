package platform

import (
	"bufio"
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// shortDir returns a short temporary directory: t.TempDir includes the test
// name and can exceed the 103-byte socket path limit on macOS.
func shortDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "cb")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// echo serves one line-based echo per connection until ln closes.
func echo(ln net.Listener) {
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		go func() {
			defer func() { _ = c.Close() }()
			r := bufio.NewReader(c)
			for {
				line, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if _, err := c.Write([]byte(line)); err != nil {
					return
				}
			}
		}()
	}
}

func TestListenDialRoundTrip(t *testing.T) {
	path := filepath.Join(shortDir(t), SocketName)
	ln, err := Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	go echo(ln)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := Dial(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	r := bufio.NewReader(c)
	var times []time.Duration
	for range 100 {
		start := time.Now()
		if _, err := c.Write([]byte("decide src/auth.go\n")); err != nil {
			t.Fatal(err)
		}
		line, err := r.ReadString('\n')
		if err != nil || strings.TrimSpace(line) != "decide src/auth.go" {
			t.Fatalf("echo = %q, %v", line, err)
		}
		times = append(times, time.Since(start))
	}
	sort.Slice(times, func(i, j int) bool { return times[i] < times[j] })
	t.Logf("round trip p50 %v, p95 %v", times[50], times[95])
	// Acceptance target is under 10 ms locally; CI runners get headroom.
	if times[50] > 50*time.Millisecond {
		t.Errorf("round trip p50 %v, want well under 10 ms", times[50])
	}
}

func TestListenReplacesStaleSocket(t *testing.T) {
	path := filepath.Join(shortDir(t), SocketName)
	ln, err := Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	// Leave the socket file behind, as a crashed daemon would.
	ln.(*net.UnixListener).SetUnlinkOnClose(false)
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("stale socket file missing: %v", err)
	}
	ln2, err := Listen(path)
	if err != nil {
		t.Fatalf("Listen over stale socket: %v", err)
	}
	_ = ln2.Close()
}

func TestListenRefusesRunningDaemon(t *testing.T) {
	path := filepath.Join(shortDir(t), SocketName)
	ln, err := Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	go echo(ln)
	if _, err := Listen(path); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("second Listen: err = %v, want ErrAlreadyRunning", err)
	}
}

func TestDialWithoutDaemonFailsFast(t *testing.T) {
	path := filepath.Join(shortDir(t), SocketName)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := Dial(ctx, path); err == nil {
		t.Fatal("Dial succeeded with no daemon")
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("Dial took %v without a daemon, want a fast failure", d)
	}
}

func TestListenRejectsLongPath(t *testing.T) {
	long := filepath.Join(shortDir(t), strings.Repeat("x", 120), SocketName)
	if _, err := Listen(long); !errors.Is(err, ErrSocketPathTooLong) {
		t.Fatalf("err = %v, want ErrSocketPathTooLong", err)
	}
}

func TestSocketPath(t *testing.T) {
	p := Paths{Runtime: filepath.Join("run", "crewbo")}
	if got, want := p.SocketPath(), filepath.Join("run", "crewbo", SocketName); got != want {
		t.Errorf("SocketPath = %q, want %q", got, want)
	}
}
