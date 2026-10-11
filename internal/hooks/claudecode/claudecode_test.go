package claudecode

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/crewbo/crewbo/internal/claims"
	"github.com/crewbo/crewbo/internal/daemon"
	"github.com/crewbo/crewbo/internal/hooks"
	"github.com/crewbo/crewbo/internal/platform"
)

var fixtures = filepath.Join("..", "..", "..", "test", "fixtures", "hooks", "claude-code")

// kindFor maps a fixture file name (event, tool, outcome) to the event the
// hook registration in docs/architecture/hooks.md would pass.
func kindFor(name string) hooks.Kind {
	switch {
	case strings.HasPrefix(name, "SessionStart"):
		return hooks.SessionStart
	case strings.HasPrefix(name, "UserPromptSubmit"):
		return hooks.Prompt
	case strings.HasPrefix(name, "PreToolUse-Bash"), strings.HasPrefix(name, "PreToolUse-PowerShell"):
		return hooks.PreShell
	case strings.HasPrefix(name, "PreToolUse"):
		return hooks.PreEdit
	case strings.HasPrefix(name, "PostToolUse"):
		return hooks.PostEdit
	case strings.HasPrefix(name, "Stop"):
		return hooks.Stop
	}
	return ""
}

func TestDecodeFixtures(t *testing.T) {
	files, _ := filepath.Glob(filepath.Join(fixtures, "*", "*", "*.json"))
	n := 0
	for _, f := range files {
		name := filepath.Base(f)
		if name == "_meta.json" {
			continue
		}
		kind := kindFor(name)
		if kind == "" {
			t.Fatalf("%s: no kind for fixture", f)
		}
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		ev, err := Adapter{}.Decode(kind, raw)
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		if ev.Session == "" || ev.Cwd == "" || ev.Agent != Name {
			t.Errorf("%s: common fields missing: %+v", f, ev)
		}
		switch kind {
		case hooks.PreEdit:
			if len(ev.Paths) != 1 || !strings.Contains(filepath.ToSlash(strings.ReplaceAll(ev.Paths[0], `\`, "/")), ".") {
				t.Errorf("%s: paths = %q", f, ev.Paths)
			}
		case hooks.PreShell:
			want := hooks.POSIX
			if strings.Contains(name, "PowerShell") {
				want = hooks.PowerShell
			}
			if ev.Command == "" || ev.Shell != want {
				t.Errorf("%s: command %q shell %q, want shell %q", f, ev.Command, ev.Shell, want)
			}
		}
		n++
	}
	if n < 15 {
		t.Errorf("decoded %d fixtures, want at least 15 (Linux and Windows)", n)
	}
}

func TestDecodeShellRegisteredAsEdit(t *testing.T) {
	ev, err := Adapter{}.Decode(hooks.PreEdit, []byte(`{"session_id":"s","cwd":"/r","tool_name":"Bash","tool_input":{"command":"echo x > a"}}`))
	if err != nil || ev.Kind != hooks.PreShell || ev.Command != "echo x > a" {
		t.Fatalf("Decode = %+v, %v; want a pre_shell event", ev, err)
	}
}

func TestDecodeErrors(t *testing.T) {
	for _, p := range []string{`not json`, `{"cwd":"/r"}`} {
		if _, err := (Adapter{}).Decode(hooks.PreEdit, []byte(p)); err == nil {
			t.Errorf("Decode(%q) succeeded", p)
		}
	}
}

func TestEncode(t *testing.T) {
	a := Adapter{}
	deny, code := a.Encode(hooks.PreEdit, hooks.Result{Deny: true, Reason: "src/a.go is claimed"})
	want := `{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"src/a.go is claimed"}}`
	if string(deny) != want || code != 0 {
		t.Errorf("deny = %s (exit %d), want %s", deny, code, want)
	}
	if b, _ := a.Encode(hooks.PreEdit, hooks.Result{}); b != nil {
		t.Errorf("allow printed %s; must print nothing", b)
	}
	b, _ := a.Encode(hooks.Prompt, hooks.Result{Context: "team digest", Warning: "daemon down"})
	if !strings.Contains(string(b), `"hookEventName":"UserPromptSubmit"`) || !strings.Contains(string(b), `"additionalContext":"team digest\ndaemon down"`) {
		t.Errorf("prompt context = %s", b)
	}
	b, _ = a.Encode(hooks.PostEdit, hooks.Result{Warning: "w"})
	if !strings.Contains(string(b), `"hookEventName":"PostToolUse","additionalContext":"w"`) {
		t.Errorf("post_edit warning = %s", b)
	}
	// Stop must stay silent: additionalContext on Stop continues the turn.
	if b, _ := a.Encode(hooks.Stop, hooks.Result{Warning: "daemon down", Context: "x"}); b != nil {
		t.Errorf("stop printed %s; must print nothing", b)
	}
	if a.Name() != "claude-code" {
		t.Error("wrong name")
	}
}

// TestCapturedEditIsDenied replays a real Claude Code Edit payload through
// hooks.Run against a running daemon in which another session holds the
// file.
func TestCapturedEditIsDenied(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(fixtures, "2.1.294", "linux", "PreToolUse-Edit-blocked.json"))
	if err != nil {
		t.Fatal(err)
	}
	var in map[string]any
	if err := json.Unmarshal(raw, &in); err != nil {
		t.Fatal(err)
	}
	root, err := os.MkdirTemp("", "cbcc")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	repo := filepath.Join(root, "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	// Point the captured payload at this machine's temporary repository.
	in["cwd"] = repo
	in["tool_input"].(map[string]any)["file_path"] = filepath.Join(repo, "blocked.txt")
	raw, _ = json.Marshal(in)

	paths := platform.Paths{Data: filepath.Join(root, "d"), Config: filepath.Join(root, "c"), Runtime: filepath.Join(root, "r")}
	set := claims.NewSet()
	if err := set.Add(claims.Claim{ID: "c1", Pattern: "blocked.txt", Holder: claims.Holder{SessionID: "other", MemberID: "alice"}, TaskID: "12", LeaseExpires: time.Now().Add(time.Hour)}, time.Now()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = daemon.New(daemon.Config{Paths: paths, Claims: set}).Run(ctx) }()
	t.Cleanup(func() { cancel(); <-done })
	waitForDaemon(t, paths)

	var out bytes.Buffer
	if code := hooks.Run(context.Background(), Adapter{}, hooks.PreEdit, bytes.NewReader(raw), &out, hooks.Deps{Paths: paths}); code != 0 {
		t.Fatalf("exit %d", code)
	}
	var got output
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("output %q: %v", out.String(), err)
	}
	h := got.HookSpecificOutput
	if h.HookEventName != "PreToolUse" || h.PermissionDecision != "deny" || !strings.Contains(h.PermissionDecisionReason, "blocked.txt is claimed by alice's agent for task 12") {
		t.Errorf("hook output = %s", out.String())
	}
}

func waitForDaemon(t *testing.T, p platform.Paths) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		c, err := daemon.Dial(ctx, p)
		cancel()
		if err == nil {
			_ = c.Close()
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("daemon did not start: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
