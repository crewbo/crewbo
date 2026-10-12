package dispatch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The test binary doubles as a fake agent CLI when CREWBO_FAKE_AGENT names
// a file: it records how it was started there, and answers auth status
// with CREWBO_FAKE_LOGGED_IN.
func TestMain(m *testing.M) {
	if out := os.Getenv("CREWBO_FAKE_AGENT"); out != "" {
		if len(os.Args) > 2 && os.Args[1] == "auth" && os.Args[2] == "status" {
			b, _ := json.Marshal(fakeRun{Args: os.Args[1:], Env: os.Environ()})
			_ = os.WriteFile(out, b, 0o600)
			fmt.Printf(`{"loggedIn": %s, "authMethod": "claude.ai"}`, os.Getenv("CREWBO_FAKE_LOGGED_IN"))
			os.Exit(0)
		}
		stdin, _ := io.ReadAll(os.Stdin)
		cwd, _ := os.Getwd()
		b, _ := json.Marshal(fakeRun{Args: os.Args[1:], Env: os.Environ(), Cwd: cwd, Stdin: string(stdin)})
		_ = os.WriteFile(out, b, 0o600)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

type fakeRun struct {
	Args  []string
	Env   []string
	Cwd   string
	Stdin string
}

func fakeEnv(t *testing.T, extra ...string) ([]string, string) {
	t.Helper()
	rec := filepath.Join(t.TempDir(), "run.json")
	return append([]string{"CREWBO_FAKE_AGENT=" + rec, "PATH=" + os.Getenv("PATH"), "SYSTEMROOT=" + os.Getenv("SYSTEMROOT")}, extra...), rec
}

func TestClaudeCommand(t *testing.T) {
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	wt := t.TempDir()
	for _, keep := range []bool{false, true} {
		env, rec := fakeEnv(t, "ANTHROPIC_API_KEY=sk-test", "anthropic_api_key=sk-lower", "HOME=/h")
		text := "--looks like a flag\nSecond line."
		cmd := ClaudeCommand(context.Background(), Launch{Bin: bin, Worktree: wt, Session: "0b6c4c1e-1f53-4a8e-9a43-0d6f3e5f2a10", Text: text, Env: env, KeepAPIKey: keep})
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("run: %v %s", err, out)
		}
		b, err := os.ReadFile(rec)
		if err != nil {
			t.Fatal(err)
		}
		var run fakeRun
		if err := json.Unmarshal(b, &run); err != nil {
			t.Fatal(err)
		}
		want := []string{"-p", "--session-id", "0b6c4c1e-1f53-4a8e-9a43-0d6f3e5f2a10", "--permission-mode", "acceptEdits"}
		if !slices.Equal(run.Args, want) {
			t.Errorf("args = %q, want %q", run.Args, want)
		}
		for _, a := range run.Args {
			if a == "--bare" || a == "bypassPermissions" || a == "--dangerously-skip-permissions" {
				t.Errorf("forbidden argument %q", a)
			}
		}
		if run.Stdin != text {
			t.Errorf("stdin = %q", run.Stdin)
		}
		if gotWT, _ := filepath.EvalSymlinks(run.Cwd); gotWT != mustEval(t, wt) {
			t.Errorf("cwd = %q, want %q", run.Cwd, wt)
		}
		hasKey := slices.ContainsFunc(run.Env, func(kv string) bool { return strings.HasPrefix(strings.ToUpper(kv), "ANTHROPIC_API_KEY=") })
		if hasKey != keep {
			t.Errorf("keep %v: API key in the agent's environment = %v (%q)", keep, hasKey, run.Env)
		}
		if !slices.Contains(run.Env, "HOME=/h") {
			t.Errorf("other variables were dropped: %q", run.Env)
		}
	}
}

func mustEval(t *testing.T, p string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestClaudeSignedIn(t *testing.T) {
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	env, rec := fakeEnv(t, "CREWBO_FAKE_LOGGED_IN=true", "ANTHROPIC_API_KEY=sk-test")
	if err := ClaudeSignedIn(ctx, bin, env, false); err != nil {
		t.Errorf("signed in: %v", err)
	}
	// The check sees the environment the run gets: no API key, so it
	// reports the member's own sign-in.
	var run fakeRun
	if b, err := os.ReadFile(rec); err != nil || json.Unmarshal(b, &run) != nil {
		t.Fatalf("auth status was not run: %v", err)
	}
	if slices.Contains(run.Env, "ANTHROPIC_API_KEY=sk-test") {
		t.Error("auth status ran with the API key the agent will not get")
	}
	env, _ = fakeEnv(t, "CREWBO_FAKE_LOGGED_IN=false")
	if err := ClaudeSignedIn(ctx, bin, env, false); !errors.Is(err, ErrNotSignedIn) {
		t.Errorf("signed out: %v", err)
	}
	env, _ = fakeEnv(t, "CREWBO_FAKE_LOGGED_IN=garbage")
	if err := ClaudeSignedIn(ctx, bin, env, false); err == nil || errors.Is(err, ErrNotSignedIn) {
		t.Errorf("unreadable answer: %v", err)
	}
	if err := ClaudeSignedIn(ctx, filepath.Join(t.TempDir(), "no-such-claude"), nil, false); err == nil {
		t.Error("a missing binary counts as signed in")
	}
}
