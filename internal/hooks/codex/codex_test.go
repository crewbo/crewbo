package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/crewbo/crewbo/internal/claims"
	"github.com/crewbo/crewbo/internal/daemon"
	"github.com/crewbo/crewbo/internal/hooks"
	"github.com/crewbo/crewbo/internal/platform"
)

var fixtures = filepath.Join("..", "..", "..", "test", "fixtures", "hooks", "codex")

func kindFor(name string) hooks.Kind {
	switch {
	case strings.HasPrefix(name, "SessionStart"):
		return hooks.SessionStart
	case strings.HasPrefix(name, "UserPromptSubmit"):
		return hooks.Prompt
	case strings.HasPrefix(name, "PreToolUse-Bash"):
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
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		ev, err := Adapter{}.Decode(kind, raw)
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		if ev.Session == "" || ev.Cwd == "" {
			t.Errorf("%s: common fields missing: %+v", f, ev)
		}
		switch kind {
		case hooks.PreEdit:
			// "-free" captures edit an unclaimed file (free.txt, or new.txt
			// in the Linux add-file run); "-blocked" ones edit blocked.txt.
			if len(ev.Paths) != 1 {
				t.Fatalf("%s: paths = %q, want one", f, ev.Paths)
			}
			isBlocked := filepath.Base(strings.ReplaceAll(ev.Paths[0], `\`, "/")) == "blocked.txt"
			if isBlocked != strings.Contains(name, "blocked") {
				t.Errorf("%s: path %q does not match the fixture name", f, ev.Paths[0])
			}
		case hooks.PreShell:
			if ev.Command == "" {
				t.Errorf("%s: empty command", f)
			}
		}
		n++
	}
	if n < 12 {
		t.Errorf("decoded %d fixtures, want at least 12 (Linux, Windows, macOS)", n)
	}
}

func TestDecodeShellDialect(t *testing.T) {
	p := []byte(`{"session_id":"s","cwd":"/r","tool_name":"Bash","tool_input":{"command":"Add-Content -Path a.txt -Value x"}}`)
	ev, _ := Adapter{PowerShell: true}.Decode(hooks.PreEdit, p)
	if ev.Kind != hooks.PreShell || ev.Shell != hooks.PowerShell {
		t.Errorf("Windows: %+v", ev)
	}
	ev, _ = Adapter{}.Decode(hooks.PreShell, p)
	if ev.Shell != hooks.POSIX {
		t.Errorf("Unix: %+v", ev)
	}
	ev, _ = Adapter{}.Decode(hooks.PreShell, []byte(`{"session_id":"s","tool_name":"apply_patch","tool_input":{"command":"*** Begin Patch\n*** Add File: a.go\n*** End Patch"}}`))
	if ev.Kind != hooks.PreEdit || !reflect.DeepEqual(ev.Paths, []string{"a.go"}) {
		t.Errorf("apply_patch registered as shell: %+v", ev)
	}
	for _, bad := range []string{"nope", `{"cwd":"/r"}`} {
		if _, err := (Adapter{}).Decode(hooks.PreEdit, []byte(bad)); err == nil {
			t.Errorf("Decode(%q) succeeded", bad)
		}
	}
}

func TestPatchPaths(t *testing.T) {
	patch := "*** Begin Patch\r\n" +
		"*** Update File: /abs/src/old.go\r\n" +
		"*** Move to: src/new.go\r\n" +
		"@@\r\n-a\r\n+b\r\n" +
		"*** Add File: docs/My Notes.md\r\n+hello\r\n" +
		"*** Delete File: tmp.txt\r\n" +
		"*** Update File: /abs/src/old.go\r\n" + // duplicate header
		"+*** Add File: not-a-header-inside-content.go\r\n" +
		"*** End Patch\r\n"
	want := []string{"/abs/src/old.go", "src/new.go", "docs/My Notes.md", "tmp.txt"}
	if got := PatchPaths(patch); !reflect.DeepEqual(got, want) {
		t.Errorf("PatchPaths = %q, want %q", got, want)
	}
	if got := PatchPaths("no headers here"); got != nil {
		t.Errorf("PatchPaths without headers = %q", got)
	}
}

// FuzzPatchPaths checks that every returned path appears in the input and
// that parsing never panics.
func FuzzPatchPaths(f *testing.F) {
	f.Add("*** Begin Patch\n*** Update File: a.go\n*** Move to: b.go\n*** End Patch")
	f.Add("*** Add File: \n*** Delete File: x\r\n")
	f.Fuzz(func(t *testing.T, s string) {
		for _, p := range PatchPaths(s) {
			if p == "" || !strings.Contains(s, p) {
				t.Fatalf("path %q not in input", p)
			}
		}
	})
}

func TestEncodeNeverEmitsUnsupportedFields(t *testing.T) {
	a := Adapter{}
	results := []hooks.Result{{}, {Deny: true, Reason: "r"}, {Warning: "w"}, {Context: "c"}, {Context: "c", Warning: "w"}}
	kinds := []hooks.Kind{hooks.SessionStart, hooks.Prompt, hooks.PreEdit, hooks.PreShell, hooks.PostEdit, hooks.Stop}
	for _, k := range kinds {
		for _, r := range results {
			b, code := a.Encode(k, r)
			if code != 0 {
				t.Errorf("%s %+v: exit %d", k, r, code)
			}
			for _, bad := range []string{`"ask"`, `"continue"`, `"stopReason"`, `"suppressOutput"`, `"allow"`} {
				if bytes.Contains(b, []byte(bad)) {
					t.Errorf("%s %+v: output %s contains %s", k, r, b, bad)
				}
			}
			if k == hooks.Stop && b != nil {
				t.Errorf("stop printed %s", b)
			}
		}
	}
	deny, _ := a.Encode(hooks.PreEdit, hooks.Result{Deny: true, Reason: "src/a.go is claimed"})
	if want := `{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"src/a.go is claimed"}}`; string(deny) != want {
		t.Errorf("deny = %s, want %s", deny, want)
	}
	if b, _ := a.Encode(hooks.PreEdit, hooks.Result{}); b != nil {
		t.Errorf("allow printed %s", b)
	}
	if b, _ := a.Encode(hooks.Prompt, hooks.Result{Context: "digest", Warning: "w"}); !strings.Contains(string(b), `"additionalContext":"digest\nw"`) {
		t.Errorf("prompt = %s", b)
	}
	if a.Name() != "codex" {
		t.Error("wrong name")
	}
}

// TestCapturedPatchIsDenied replays captured apply_patch payloads (absolute
// paths from Linux, relative from macOS) through hooks.Run against a daemon
// where another session holds blocked.txt.
func TestCapturedPatchIsDenied(t *testing.T) {
	for _, f := range []string{
		filepath.Join(fixtures, "0.161.0", "linux", "PreToolUse-apply_patch-blocked.json"),
		filepath.Join(fixtures, "0.162.1", "macos", "PreToolUse-apply_patch-blocked.json"),
	} {
		t.Run(filepath.Base(filepath.Dir(f)), func(t *testing.T) {
			raw, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			var in map[string]any
			if err := json.Unmarshal(raw, &in); err != nil {
				t.Fatal(err)
			}
			root, err := os.MkdirTemp("", "cbcx")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.RemoveAll(root) })
			repo := filepath.Join(root, "repo")
			if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o700); err != nil {
				t.Fatal(err)
			}
			// Re-point the captured payload at this machine's repository.
			oldCwd := in["cwd"].(string)
			in["cwd"] = repo
			ti := in["tool_input"].(map[string]any)
			ti["command"] = strings.ReplaceAll(ti["command"].(string), oldCwd, repo)
			raw, _ = json.Marshal(in)

			paths := platform.Paths{Data: filepath.Join(root, "d"), Config: filepath.Join(root, "c"), Runtime: filepath.Join(root, "r")}
			set := claims.NewSet()
			if err := set.Add(claims.Claim{ID: "c1", Pattern: "blocked.txt", Holder: claims.Holder{SessionID: "other", MemberID: "alice"}, LeaseExpires: time.Now().Add(time.Hour)}, time.Now()); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			go func() {
				defer close(done)
				_ = daemon.New(daemon.Config{Paths: paths, Claims: map[string]*claims.Set{repo: set}}).Run(ctx)
			}()
			t.Cleanup(func() { cancel(); <-done })
			waitForDaemon(t, paths)

			var out bytes.Buffer
			hooks.Run(context.Background(), Adapter{}, hooks.PreEdit, bytes.NewReader(raw), &out, hooks.Deps{Paths: paths})
			if !strings.Contains(out.String(), `"permissionDecision":"deny"`) || !strings.Contains(out.String(), "blocked.txt is claimed by alice's agent") {
				t.Errorf("hook output = %s", out.String())
			}
		})
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
