package cli

import (
	"bytes"
	"context"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/crewbo/crewbo/internal/claims"
	"github.com/crewbo/crewbo/internal/daemon"
	"github.com/crewbo/crewbo/internal/hooks"
)

func TestToPattern(t *testing.T) {
	root := filepath.Join(string(filepath.Separator)+"r", "repo")
	sub := filepath.Join(root, "src")
	tests := []struct{ cwd, arg, want string }{
		{root, "src/auth.go", "src/auth.go"},
		{sub, "auth.go", "src/auth.go"},
		{sub, "*.go", "src/*.go"},
		{root, "web/**", "web/**"},
		{sub, "../docs/*.md", "docs/*.md"},
	}
	for _, tt := range tests {
		got, err := toPattern(root, tt.cwd, tt.arg)
		if err != nil || got != tt.want {
			t.Errorf("toPattern(%q, %q) = %q, %v; want %q", tt.cwd, tt.arg, got, err, tt.want)
		}
	}
	for _, bad := range []string{"../../outside.go", "../../*.go"} {
		if _, err := toPattern(root, sub, bad); err == nil {
			t.Errorf("toPattern(%q) accepted a path outside the repository", bad)
		}
	}
}

// TestClaimCommands runs add, list, a conflicting add and release against a
// daemon, from inside a temporary repository.
func TestClaimCommands(t *testing.T) {
	p := isolate(t)
	repo := filepath.Join(p.Data, "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, "web"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(repo)
	agent := claims.NewSet()
	if err := agent.Add(claims.Claim{ID: "a1", Pattern: "web/app.ts", Holder: claims.Holder{SessionID: "codex-1", MemberID: "priya"}, LeaseExpires: time.Now().Add(time.Hour)}, time.Now()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = daemon.New(daemon.Config{Paths: p, Claims: map[string]*claims.Set{repo: agent}}).Run(ctx)
	}()
	t.Cleanup(func() { cancel(); <-done })

	run := func(args ...string) (int, string, string) {
		var stdout, stderr bytes.Buffer
		code := Run(context.Background(), append([]string{"claim"}, args...), nil, &stdout, &stderr, BuildInfo{})
		return code, stdout.String(), stderr.String()
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if code, _, _ := run("list"); code == ExitOK {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("daemon not reachable")
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Flags after the path must not become claims (found live in #42).
	code, out, errOut := run("add", "src/auth.go", "--for", "45m", "--task", "12")
	if code != ExitOK || !strings.Contains(out, "src/auth.go") || !strings.Contains(out, "(terminal)") || strings.Contains(out, "--task") || strings.Count(out, "\n") != 2 {
		t.Fatalf("add: %d %q %q", code, out, errOut)
	}
	code, _, errOut = run("add", "lib.go", "web/app.ts")
	if code != ExitError || !strings.Contains(errOut, "web/app.ts is already claimed by priya") || !strings.Contains(errOut, "nothing was claimed") {
		t.Fatalf("conflicting add: %d %q", code, errOut)
	}
	code, out, _ = run("list")
	if code != ExitOK || !strings.Contains(out, "src/auth.go") || strings.Contains(out, "lib.go") || !strings.Contains(out, "web/app.ts") {
		t.Fatalf("list: %d %q", code, out)
	}
	id := strings.Fields(strings.Split(strings.TrimSpace(out), "\n")[1])[0]
	if code, out, _ = run("release", id); code != ExitOK || !strings.Contains(out, "released 1") {
		t.Fatalf("release %s: %d %q", id, code, out)
	}
	if code, _, _ := run("frobnicate"); code != ExitUsage {
		t.Errorf("unknown subcommand exit %d", code)
	}
	if code, _, _ := run("add"); code != ExitUsage {
		t.Errorf("add without paths exit %d", code)
	}
}

func TestClaimOutsideRepoAndWithoutDaemon(t *testing.T) {
	isolate(t)
	t.Chdir(t.TempDir()) // not inside any git repository
	var stdout, stderr bytes.Buffer
	if code := Run(context.Background(), []string{"claim", "list"}, nil, &stdout, &stderr, BuildInfo{}); code != ExitError || !strings.Contains(stderr.String(), "not inside a git repository") {
		t.Errorf("outside repo: %d %q", code, stderr.String())
	}
	repo := filepath.Join(t.TempDir(), "r")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(repo)
	stderr.Reset()
	if code := Run(context.Background(), []string{"claim", "list"}, nil, &stdout, &stderr, BuildInfo{}); code != ExitError || !strings.Contains(stderr.String(), "daemon is not running") {
		t.Errorf("no daemon: %d %q", code, stderr.String())
	}
}

func TestParseShellPicksDialect(t *testing.T) {
	if got, opaque := parseShell("Add-Content -Path a.txt -Value x", hooks.PowerShell); len(got) != 1 || got[0] != "a.txt" || opaque {
		t.Errorf("PowerShell: %q %v", got, opaque)
	}
	if got, _ := parseShell("echo x > b.txt", hooks.POSIX); len(got) != 1 || got[0] != "b.txt" {
		t.Errorf("POSIX: %q", got)
	}
}

func TestParseInterspersed(t *testing.T) {
	tests := []struct {
		args       []string
		positional []string
		task, dur  string
	}{
		{[]string{"src/a.go", "--task", "12"}, []string{"src/a.go"}, "12", "30m0s"},
		{[]string{"--for", "1h", "a.go", "b.go", "--task=7"}, []string{"a.go", "b.go"}, "7", "1h0m0s"},
		{[]string{"a.go", "--", "--task", "x"}, []string{"a.go", "--task", "x"}, "", "30m0s"},
	}
	for _, tt := range tests {
		fs := flag.NewFlagSet("t", flag.ContinueOnError)
		task := fs.String("task", "", "")
		dur := fs.Duration("for", 30*time.Minute, "")
		got, err := parseInterspersed(fs, tt.args)
		if err != nil || !reflect.DeepEqual(got, tt.positional) || *task != tt.task || dur.String() != tt.dur {
			t.Errorf("%q: %q task=%q for=%s err=%v; want %q task=%q for=%s", tt.args, got, *task, dur, err, tt.positional, tt.task, tt.dur)
		}
	}
}
