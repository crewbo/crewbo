package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/crewbo/crewbo/internal/claims"
	"github.com/crewbo/crewbo/internal/daemon"
	"github.com/crewbo/crewbo/internal/platform"
)

// fakeAdapter reads Event fields straight from JSON and prints Result as
// JSON, so tests exercise Run without an agent format.
type fakeAdapter struct{}

func (fakeAdapter) Name() string { return "fake" }

func (fakeAdapter) Decode(k Kind, b []byte) (Event, error) {
	var e Event
	if err := json.Unmarshal(b, &e); err != nil {
		return Event{}, err
	}
	e.Kind, e.Agent = k, "fake"
	return e, nil
}

func (fakeAdapter) Encode(_ Kind, r Result) ([]byte, int) {
	if r == (Result{}) {
		return nil, 0
	}
	b, _ := json.Marshal(r)
	return b, 0
}

type env struct {
	paths platform.Paths
	repo  string
}

// setup creates a git-looking repository and, when withDaemon is set, a
// running daemon in which alice holds src/auth.go.
func setup(t *testing.T, withDaemon bool) env {
	t.Helper()
	root, err := os.MkdirTemp("", "cbh")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	repo := filepath.Join(root, "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	e := env{paths: platform.Paths{Data: filepath.Join(root, "d"), Config: filepath.Join(root, "c"), Runtime: filepath.Join(root, "r")}, repo: repo}
	if !withDaemon {
		return e
	}
	set := claims.NewSet()
	if err := set.Add(claims.Claim{ID: "c1", Pattern: "src/auth.go", Holder: claims.Holder{SessionID: "alice", MemberID: "priya"}, TaskID: "12", LeaseExpires: time.Now().Add(time.Hour)}, time.Now()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = daemon.New(daemon.Config{Paths: e.paths, Claims: map[string]*claims.Set{repo: set}}).Run(ctx)
	}()
	t.Cleanup(func() { cancel(); <-done })
	deadline := time.Now().Add(5 * time.Second)
	for {
		dctx, dcancel := context.WithTimeout(context.Background(), time.Second)
		c, err := daemon.Dial(dctx, e.paths)
		dcancel()
		if err == nil {
			_ = c.Close()
			return e
		}
		if time.Now().After(deadline) {
			t.Fatalf("daemon did not start: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func run(t *testing.T, e env, kind Kind, payload string) Result {
	t.Helper()
	var out bytes.Buffer
	code := Run(context.Background(), fakeAdapter{}, kind, strings.NewReader(payload), &out, Deps{Paths: e.paths, Timeout: testTimeout})
	if code != 0 {
		t.Fatalf("exit code %d", code)
	}
	var r Result
	if out.Len() > 0 {
		if err := json.Unmarshal(out.Bytes(), &r); err != nil {
			t.Fatalf("output %q: %v", out.String(), err)
		}
	}
	return r
}

func payload(t *testing.T, e Event) string {
	t.Helper()
	b, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestDenyClaimedFile(t *testing.T) {
	e := setup(t, true)
	r := run(t, e, PreEdit, payload(t, Event{Session: "bob", Cwd: e.repo, Paths: []string{filepath.Join(e.repo, "src", "auth.go")}}))
	if !r.Deny || !strings.Contains(r.Reason, "src/auth.go is claimed by priya's agent for task 12") {
		t.Fatalf("result = %+v", r)
	}
	if strings.Contains(r.Reason, "handoff") {
		t.Error("reason suggests a handoff command that does not exist yet")
	}
}

func TestAllowOwnClaimAndFreeFiles(t *testing.T) {
	e := setup(t, true)
	for _, ev := range []Event{
		{Session: "alice", Cwd: e.repo, Paths: []string{"src/auth.go"}},
		{Session: "bob", Cwd: e.repo, Paths: []string{"src/limits.go"}},
		{Session: "bob", Cwd: e.repo, Paths: []string{"/elsewhere/src/auth.go"}}, // outside the repo
	} {
		if r := run(t, e, PreEdit, payload(t, ev)); r != (Result{}) {
			t.Errorf("%+v: result = %+v, want silent allow", ev, r)
		}
	}
}

func TestRelativeAndCaseDifferentPaths(t *testing.T) {
	e := setup(t, true)
	sub := filepath.Join(e.repo, "src")
	r := run(t, e, PreEdit, payload(t, Event{Session: "bob", Cwd: sub, Paths: []string{"auth.go"}}))
	if !r.Deny {
		t.Errorf("relative path from a subdirectory not matched: %+v", r)
	}
	// Backslash forms are covered by claims.TestNormalize; here the
	// absolute path uses this OS's separator but different case.
	r = run(t, e, PreEdit, payload(t, Event{Session: "bob", Cwd: e.repo, Paths: []string{filepath.Join(e.repo, "SRC", "Auth.go")}}))
	if !r.Deny {
		t.Errorf("case-different absolute path not matched: %+v", r)
	}
}

func TestByteOrderMarkIsStripped(t *testing.T) {
	e := setup(t, true)
	r := run(t, e, PreEdit, "\xef\xbb\xbf"+payload(t, Event{Session: "bob", Cwd: e.repo, Paths: []string{"src/auth.go"}}))
	if !r.Deny {
		t.Fatalf("BOM-prefixed input not decoded: %+v", r)
	}
}

func TestFailsOpenWithWarning(t *testing.T) {
	e := setup(t, false) // no daemon
	start := time.Now()
	r := run(t, e, PreEdit, payload(t, Event{Session: "bob", Cwd: e.repo, Paths: []string{"src/auth.go"}}))
	if r.Deny || !strings.HasPrefix(r.Warning, warnPrefix) || !strings.Contains(r.Warning, "not running") {
		t.Errorf("no daemon: result = %+v", r)
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("hook took %v with no daemon", d)
	}
	if r := run(t, e, PreEdit, "{not json"); r.Deny || !strings.Contains(r.Warning, "could not read the fake hook input") {
		t.Errorf("bad JSON: result = %+v", r)
	}
	big := `{"Session":"bob","Command":"` + strings.Repeat("x", MaxPayload) + `"}`
	if r := run(t, e, PreShell, big); r.Deny || !strings.Contains(r.Warning, "larger than 1 MiB") {
		t.Errorf("oversized input: result = %+v", r)
	}
}

func TestOutsideRepositoryIsSilent(t *testing.T) {
	e := setup(t, false)
	r := run(t, e, PreEdit, payload(t, Event{Session: "bob", Cwd: os.TempDir(), Paths: []string{"x.go"}}))
	if r != (Result{}) {
		t.Errorf("outside a repository: result = %+v, want silent allow", r)
	}
}

func TestEventsRegisterSession(t *testing.T) {
	e := setup(t, true)
	if r := run(t, e, Prompt, payload(t, Event{Session: "carol", Cwd: e.repo})); r != (Result{}) {
		t.Fatalf("prompt result = %+v", r)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	c, err := daemon.Dial(ctx, e.paths)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	resp, err := c.Call(ctx, daemon.Request{Method: daemon.MethodStatus})
	if err != nil || resp.Status.Sessions != 1 {
		t.Fatalf("status = %+v, %v; want 1 session", resp.Status, err)
	}
}

func TestShellTargetsAreChecked(t *testing.T) {
	e := setup(t, true)
	var out bytes.Buffer
	d := Deps{Paths: e.paths, Timeout: testTimeout, ParseShell: func(cmd string, _ Shell) ([]string, bool) {
		switch cmd {
		case "echo x >> src/auth.go":
			return []string{"src/auth.go"}, false
		case "python3 fix.py src/auth.go":
			return nil, true
		}
		return nil, false
	}}
	for cmd, deny := range map[string]bool{"echo x >> src/auth.go": true, "python3 fix.py src/auth.go": true, "ls src": false} {
		out.Reset()
		in := payload(t, Event{Session: "bob", Cwd: e.repo, Command: cmd})
		Run(context.Background(), fakeAdapter{}, PreShell, strings.NewReader(in), &out, d)
		if got := strings.Contains(out.String(), `"Deny":true`); got != deny {
			t.Errorf("%q: deny = %v, want %v (%s)", cmd, got, deny, out.String())
		}
	}
}

func TestRepoRootFindsWorktreeGitFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".git"), []byte("gitdir: /x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(dir, "a", "b")
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	if got, ok := RepoRoot(sub); !ok || got != dir {
		t.Errorf("repoRoot = %q, %v; want %q", got, ok, dir)
	}
	if _, ok := RepoRoot(""); ok {
		t.Error("empty dir reported a repository")
	}
}

func TestDenyReasonWithoutDetails(t *testing.T) {
	got := denyReason(daemon.Response{Path: "a.go"})
	if !strings.Contains(got, "a.go is claimed by another agent session") {
		t.Errorf("denyReason = %q", got)
	}
	got = denyReason(daemon.Response{Path: "a.go", BlockedBy: &claims.Claim{Holder: claims.Holder{SessionID: "s9"}}})
	if !strings.Contains(got, "agent session s9") {
		t.Errorf("denyReason = %q", got)
	}
	if !errors.Is(ErrUnknownEvent, ErrUnknownEvent) {
		t.Fatal("unreachable")
	}
}

func TestIsWindowsAbs(t *testing.T) {
	for p, want := range map[string]bool{`C:\work\a.go`: true, "c:/work/a.go": true, `work\a.go`: false, "/work/a.go": false, "C:": false} {
		if got := isWindowsAbs(p); got != want {
			t.Errorf("isWindowsAbs(%q) = %v, want %v", p, got, want)
		}
	}
}

func TestDenyReasonForTerminalClaim(t *testing.T) {
	got := denyReason(daemon.Response{Path: "a.go", BlockedBy: &claims.Claim{Holder: claims.Holder{SessionID: daemon.TerminalSession, MemberID: "ronel"}}})
	if !strings.Contains(got, "a.go is claimed by ronel (from a terminal)") {
		t.Errorf("denyReason = %q", got)
	}
}

func TestPostShellViolationReachesAgent(t *testing.T) {
	e := setup(t, true) // alice (member priya) holds src/auth.go
	if err := os.MkdirAll(filepath.Join(e.repo, "src"), 0o700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(e.repo, "src", "auth.go")
	if err := os.WriteFile(file, []byte("package auth\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	d := Deps{Paths: e.paths, Timeout: testTimeout, ParseShell: func(string, Shell) ([]string, bool) { return nil, false }}
	run := func(kind Kind, ev Event) Result {
		t.Helper()
		var out bytes.Buffer
		Run(context.Background(), fakeAdapter{}, kind, strings.NewReader(payload(t, ev)), &out, d)
		var r Result
		if out.Len() > 0 {
			if err := json.Unmarshal(out.Bytes(), &r); err != nil {
				t.Fatal(err)
			}
		}
		return r
	}
	if r := run(PreShell, Event{Session: "bob", Cwd: e.repo, Command: "npm run format", ToolUseID: "t9"}); r != (Result{}) {
		t.Fatalf("pre_shell = %+v, want silent allow", r)
	}
	if err := os.WriteFile(file, []byte("package auth // formatted\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := run(PostEdit, Event{Session: "bob", Cwd: e.repo, ToolUseID: "t9"})
	if !strings.Contains(r.Context, "Your last command changed src/auth.go, which is claimed by priya") || r.Deny {
		t.Errorf("post_edit = %+v", r)
	}
}

// testTimeout replaces the 300 ms production deadline in tests that assert
// a decision: a daemon just started on a busy CI runner can miss it, and
// the hook then fails open by design (TestByteOrderMarkIsStripped on
// ubuntu-latest, PR #87; the adapter tests had the same fix in #71).
const testTimeout = 5 * time.Second
