package daemon

import (
	"bufio"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/crewbo/crewbo/internal/claims"
	"github.com/crewbo/crewbo/internal/platform"
)

// testPaths returns short temporary directories (macOS limits socket paths
// to 103 bytes, and t.TempDir includes the test name).
func testPaths(t *testing.T) platform.Paths {
	t.Helper()
	root, err := os.MkdirTemp("", "cbd")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	return platform.Paths{Data: filepath.Join(root, "d"), Config: filepath.Join(root, "c"), Runtime: filepath.Join(root, "r")}
}

// running is a daemon started by a test. done is closed when Run returns,
// so it can be waited on any number of times.
type running struct {
	cancel context.CancelFunc
	done   chan struct{}
	err    error
}

// start runs a daemon until the test ends and waits until it answers.
func start(t *testing.T, cfg Config) *running {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	r := &running{cancel: cancel, done: make(chan struct{})}
	go func() { r.err = New(cfg).Run(ctx); close(r.done) }()
	t.Cleanup(func() { cancel(); <-r.done })
	deadline := time.Now().Add(5 * time.Second)
	for {
		c, err := dial(t, cfg.Paths)
		if err == nil {
			_, err = c.Call(context.Background(), Request{Method: MethodStatus})
			_ = c.Close()
			if err == nil {
				return r
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("daemon did not start: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func dial(t *testing.T, p platform.Paths) (*Client, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	return Dial(ctx, p)
}

func claimSet(t *testing.T) *claims.Set {
	t.Helper()
	s := claims.NewSet()
	c := claims.Claim{ID: "c1", Pattern: "src/auth.go", Holder: claims.Holder{SessionID: "alice"}, LeaseExpires: time.Now().Add(time.Hour)}
	if err := s.Add(c, time.Now()); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestStatusAndDecide(t *testing.T) {
	p := testPaths(t)
	start(t, Config{Paths: p, Version: "v0.0.0-test", Claims: map[string]*claims.Set{"": claimSet(t)}})
	c, err := dial(t, p)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	ctx := context.Background()

	resp, err := c.Call(ctx, Request{Method: MethodStatus})
	if err != nil || resp.Status.Version != "v0.0.0-test" || resp.Status.ActiveClaims != 1 || resp.Status.PID != os.Getpid() {
		t.Fatalf("status = %+v, %v", resp.Status, err)
	}
	tests := []struct {
		session, path string
		allow         bool
	}{
		{"bob", "src/auth.go", false},
		{"bob", "SRC/AUTH.go", false},
		{"alice", "src/auth.go", true},
		{"bob", "src/limits.go", true},
	}
	for _, tt := range tests {
		resp, err := c.Call(ctx, Request{Method: MethodDecide, Session: tt.session, Paths: []string{tt.path}})
		if err != nil || resp.Allow != tt.allow {
			t.Errorf("decide %s %s = %+v, %v; want allow=%v", tt.session, tt.path, resp, err, tt.allow)
		}
		if !tt.allow && (resp.BlockedBy == nil || resp.BlockedBy.Holder.SessionID != "alice") {
			t.Errorf("decide %s %s: blocked_by = %+v, want alice's claim", tt.session, tt.path, resp.BlockedBy)
		}
	}
	if _, err := c.Call(ctx, Request{Method: MethodDecide, Paths: []string{"a"}}); err == nil {
		t.Error("decide without session succeeded")
	}
	if _, err := c.Call(ctx, Request{Method: "frobnicate"}); err == nil {
		t.Error("unknown method succeeded")
	}
}

func TestWrongTokenGetsNoAnswer(t *testing.T) {
	p := testPaths(t)
	start(t, Config{Paths: p})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, err := platform.Dial(ctx, p.SocketPath())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err := conn.Write([]byte(`{"token":"wrong"}` + "\n" + `{"id":1,"method":"status"}` + "\n")); err != nil {
		t.Fatal(err)
	}
	if line, err := bufio.NewReader(conn).ReadString('\n'); err == nil {
		t.Fatalf("daemon answered an unauthenticated connection: %q", line)
	}
}

func TestSecondDaemonRefusedAndTokenKept(t *testing.T) {
	p := testPaths(t)
	start(t, Config{Paths: p})
	tokenPath := filepath.Join(p.Runtime, TokenName)
	before, err := os.ReadFile(tokenPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := New(Config{Paths: p}).Run(context.Background()); !errors.Is(err, platform.ErrAlreadyRunning) {
		t.Fatalf("second daemon: err = %v, want ErrAlreadyRunning", err)
	}
	after, _ := os.ReadFile(tokenPath)
	if string(before) != string(after) {
		t.Fatal("second daemon replaced the running daemon's token")
	}
	c, err := dial(t, p)
	if err != nil {
		t.Fatalf("first daemon unreachable after refused start: %v", err)
	}
	_ = c.Close()
}

func TestShutdownCleansUp(t *testing.T) {
	p := testPaths(t)
	r := start(t, Config{Paths: p})
	c, err := dial(t, p)
	if err != nil {
		t.Fatal(err)
	}
	r.cancel()
	select {
	case <-r.done:
		if r.err != nil {
			t.Fatalf("Run returned %v", r.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("daemon did not stop within 5 s with a client connected")
	}
	_ = c.Close()
	if _, err := os.Stat(filepath.Join(p.Runtime, TokenName)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("token file left behind: %v", err)
	}
	if _, err := dial(t, p); !errors.Is(err, ErrNotRunning) {
		t.Errorf("Dial after shutdown: err = %v, want ErrNotRunning", err)
	}
}

func TestConnectionsDoNotLeakGoroutines(t *testing.T) {
	p := testPaths(t)
	start(t, Config{Paths: p})
	run := func(n int) {
		for range n {
			c, err := dial(t, p)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := c.Call(context.Background(), Request{Method: MethodStatus}); err != nil {
				t.Fatal(err)
			}
			_ = c.Close()
		}
	}
	run(20)
	time.Sleep(100 * time.Millisecond)
	base := runtime.NumGoroutine()
	run(200)
	time.Sleep(200 * time.Millisecond)
	if grown := runtime.NumGoroutine() - base; grown > 10 {
		t.Errorf("goroutines grew by %d after 200 connections", grown)
	}
}
