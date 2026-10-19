package antigravity

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
	"github.com/crewbo/crewbo/internal/shellparse"
)

var fixtures = filepath.Join("..", "..", "..", "test", "fixtures", "hooks", "antigravity")

// kindFor maps a fixture name to the event the installer registers it
// under: one PreToolUse hook serves every matched tool.
func kindFor(name string) hooks.Kind {
	switch {
	case strings.HasPrefix(name, "SessionStart"):
		return hooks.SessionStart
	case strings.HasPrefix(name, "PreInvocation"), strings.HasPrefix(name, "PostInvocation"):
		return hooks.Prompt
	case strings.HasPrefix(name, "PreToolUse"):
		return hooks.PreEdit
	case strings.HasPrefix(name, "PostToolUse"):
		return hooks.PostEdit
	case strings.HasPrefix(name, "Stop"):
		return hooks.Stop
	}
	return ""
}

// absolute reports an absolute path from the OS the fixture was captured
// on: the Linux fixtures' paths are not absolute to Windows' filepath.
func absolute(p string) bool { return strings.HasPrefix(p, "/") || filepath.IsAbs(p) }

func TestDecodeFixtures(t *testing.T) {
	files, _ := filepath.Glob(filepath.Join(fixtures, "*", "*", "*.json"))
	n := 0
	for _, f := range files {
		name := filepath.Base(f)
		if name == "_meta.json" {
			continue
		}
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		ev, err := Adapter{}.Decode(kindFor(name), raw)
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		if ev.Session == "" || !absolute(ev.Cwd) || ev.Model == "" || ev.Agent != Name {
			t.Errorf("%s: common fields: %+v", f, ev)
		}
		switch {
		case strings.HasPrefix(name, "PreToolUse-run_command"):
			if ev.Kind != hooks.PreShell || ev.Command == "" || ev.Shell != hooks.POSIX {
				t.Errorf("%s: %+v", f, ev)
			}
			if strings.Contains(ev.Command, "blocked.txt") != strings.Contains(name, "blocked") {
				t.Errorf("%s: command %q does not match the fixture name", f, ev.Command)
			}
		case strings.HasPrefix(name, "PreToolUse"):
			if ev.Kind != hooks.PreEdit || len(ev.Paths) != 1 || !absolute(ev.Paths[0]) {
				t.Fatalf("%s: %+v", f, ev)
			}
			if (filepath.Base(ev.Paths[0]) == "blocked.txt") != strings.Contains(name, "blocked") {
				t.Errorf("%s: path %q does not match the fixture name", f, ev.Paths[0])
			}
		}
		n++
	}
	if n < 19 {
		t.Errorf("decoded %d fixtures, want at least 19 (agy 1.3.3 and 1.4.2)", n)
	}
}

func TestDecode(t *testing.T) {
	a := Adapter{}
	// run_command's Cwd wins over the workspace; a run_command registered
	// under pre_edit is a shell event, and a write tool under pre_shell an
	// edit.
	ev, _ := a.Decode(hooks.PreEdit, []byte(`{"conversationId":"c","workspacePaths":["/w"],"toolCall":{"name":"run_command","args":{"CommandLine":"echo x > a","Cwd":"/w/sub"}}}`))
	if ev.Kind != hooks.PreShell || ev.Cwd != "/w/sub" || ev.Command != "echo x > a" {
		t.Errorf("run_command: %+v", ev)
	}
	ev, _ = Adapter{PowerShell: true}.Decode(hooks.PreShell, []byte(`{"conversationId":"c","toolCall":{"name":"run_command","args":{"CommandLine":"x"}}}`))
	if ev.Shell != hooks.PowerShell {
		t.Errorf("Windows shell: %+v", ev)
	}
	ev, _ = a.Decode(hooks.PreShell, []byte(`{"conversationId":"c","workspacePaths":["/w","/v"],"toolCall":{"name":"replace_file_content","args":{"TargetFile":"/w/a.go"}}}`))
	if ev.Kind != hooks.PreEdit || ev.Cwd != "/w" || !reflect.DeepEqual(ev.Paths, []string{"/w/a.go"}) {
		t.Errorf("replace_file_content: %+v", ev)
	}
	// Tools whose arguments are not captured yet: the fallback keys.
	ev, _ = a.Decode(hooks.PreEdit, []byte(`{"conversationId":"c","toolCall":{"name":"notebook_edit","args":{"NotebookPath":"/w/nb.ipynb","File":"/w/b.ipynb"}}}`))
	if !reflect.DeepEqual(ev.Paths, []string{"/w/b.ipynb", "/w/nb.ipynb"}) {
		t.Errorf("notebook_edit: %+v", ev)
	}
	// Other tools carry no paths, and a write tool without a target none.
	for _, p := range []string{
		`{"conversationId":"c","toolCall":{"name":"view_file","args":{"AbsolutePath":"/w/a.go"}}}`,
		`{"conversationId":"c","toolCall":{"name":"write_to_file","args":{"CodeContent":"x"}}}`,
	} {
		if ev, _ := a.Decode(hooks.PreEdit, []byte(p)); ev.Paths != nil || ev.Command != "" {
			t.Errorf("%s: %+v", p, ev)
		}
	}
	for _, bad := range []string{"nope", `{"workspacePaths":["/w"]}`} {
		if _, err := a.Decode(hooks.PreEdit, []byte(bad)); err == nil {
			t.Errorf("Decode(%q) succeeded", bad)
		}
	}
}

// S9 showed that `{}` blocks a tool call and that no output passes it
// through; "allow" and "auto_approve" would bypass the member's settings.
func TestEncode(t *testing.T) {
	a := Adapter{}
	results := []hooks.Result{{}, {Deny: true, Reason: "r"}, {Warning: "w"}, {Context: "c"}}
	for _, k := range []hooks.Kind{hooks.SessionStart, hooks.Prompt, hooks.PreEdit, hooks.PreShell, hooks.PostEdit, hooks.Stop} {
		for _, r := range results {
			b, code := a.Encode(k, r)
			if code != 0 || string(b) == "{}" || bytes.Contains(b, []byte("allow")) || bytes.Contains(b, []byte("approve")) || bytes.Contains(b, []byte(`"ask"`)) {
				t.Errorf("%s %+v: %s exit %d", k, r, b, code)
			}
			if (k == hooks.SessionStart || k == hooks.PostEdit || k == hooks.Stop) && b != nil {
				t.Errorf("%s printed %s", k, b)
			}
			if (k == hooks.PreEdit || k == hooks.PreShell) && !r.Deny && b != nil {
				t.Errorf("an allowed %s printed %s", k, b)
			}
		}
	}
	for _, k := range []hooks.Kind{hooks.PreEdit, hooks.PreShell} {
		if b, _ := a.Encode(k, hooks.Result{Deny: true, Reason: "src/a.go is claimed"}); string(b) != `{"decision":"deny","reason":"src/a.go is claimed"}` {
			t.Errorf("%s deny = %s", k, b)
		}
	}
	if b, _ := a.Encode(hooks.Prompt, hooks.Result{Context: "notice", Warning: "w"}); string(b) != `{"injectSteps":[{"ephemeralMessage":"notice\nw"}]}` {
		t.Errorf("prompt = %s", b)
	}
	if b, _ := a.Encode(hooks.Prompt, hooks.Result{}); b != nil {
		t.Errorf("an empty prompt result printed %s", b)
	}
	if a.Name() != "antigravity" {
		t.Error("wrong name")
	}
}

// TestCapturedCallsAreDenied replays captured blocked payloads (a file
// write and a shell command, agy 1.4.2) through hooks.Run against a daemon
// where another session holds blocked.txt, and a free write through the
// same daemon.
func TestCapturedCallsAreDenied(t *testing.T) {
	root, err := os.MkdirTemp("", "cbag")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	repo := filepath.Join(root, "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
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

	for name, wantDeny := range map[string]bool{
		"PreToolUse-write_to_file-blocked.json": true,
		"PreToolUse-run_command-blocked.json":   true,
		"PreToolUse-write_to_file-free.json":    false,
	} {
		raw, err := os.ReadFile(filepath.Join(fixtures, "1.4.2", "linux", name))
		if err != nil {
			t.Fatal(err)
		}
		// Re-point the captured payload at this machine's repository.
		var in map[string]any
		if err := json.Unmarshal(raw, &in); err != nil {
			t.Fatal(err)
		}
		old := in["workspacePaths"].([]any)[0].(string)
		in["workspacePaths"] = []string{repo}
		args := in["toolCall"].(map[string]any)["args"].(map[string]any)
		for k, v := range args {
			if s, ok := v.(string); ok {
				args[k] = strings.ReplaceAll(s, old, repo)
			}
		}
		raw, _ = json.Marshal(in)

		var out bytes.Buffer
		hooks.Run(context.Background(), Adapter{}, hooks.PreEdit, bytes.NewReader(raw), &out, hooks.Deps{Paths: paths, Timeout: 5 * time.Second, ParseShell: func(c string, _ hooks.Shell) ([]string, bool) {
			r := shellparse.POSIX(c)
			return r.Targets, r.Opaque
		}})
		denied := strings.HasPrefix(out.String(), `{"decision":"deny","reason":"blocked.txt is claimed by alice's agent`)
		if denied != wantDeny || (!wantDeny && out.Len() != 0) {
			t.Errorf("%s: hook output %q", name, out.String())
		}
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
