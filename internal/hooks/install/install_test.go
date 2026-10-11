package install

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crewbo/crewbo/internal/platform"
)

const userClaude = `{
  "permissions": {"allow": ["Bash(git status)"]},
  "hooks": {
    "PostToolUse": [
      {"matcher": "Edit|Write", "hooks": [{"type": "command", "command": "npx prettier --write"}]}
    ]
  }
}`

func decode(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("invalid JSON %s: %v", b, err)
	}
	return m
}

func countOurs(t *testing.T, agent string, b []byte) int {
	t.Helper()
	hooks, _ := decode(t, b)["hooks"].(map[string]any)
	n := 0
	for _, v := range hooks {
		for _, g := range v.([]any) {
			for _, h := range g.(map[string]any)["hooks"].([]any) {
				if ours(agent, h) {
					n++
				}
			}
		}
	}
	return n
}

func TestClaudeInstallKeepsUserConfig(t *testing.T) {
	out, changed, err := Install(ClaudeCode, []byte(userClaude), "/home/u/.local/bin/crewbo")
	if err != nil || !changed {
		t.Fatalf("Install = %v, %v", changed, err)
	}
	if n := countOurs(t, ClaudeCode, out); n != len(claudeSpecs) {
		t.Errorf("crewbo handlers = %d, want %d", n, len(claudeSpecs))
	}
	s := string(out)
	for _, want := range []string{`"npx prettier --write"`, `"Bash(git status)"`, `"args": [`, `"pre_shell"`, `"Bash|PowerShell"`} {
		if !strings.Contains(s, want) {
			t.Errorf("installed config lacks %s:\n%s", want, s)
		}
	}
	// Idempotent.
	again, changed, err := Install(ClaudeCode, out, "/home/u/.local/bin/crewbo")
	if err != nil || changed || string(again) != string(out) {
		t.Errorf("second install changed the config: %v %v", changed, err)
	}
	// A moved binary replaces the old entries instead of adding more.
	moved, _, _ := Install(ClaudeCode, out, "/opt/crewbo/crewbo")
	if n := countOurs(t, ClaudeCode, moved); n != len(claudeSpecs) || strings.Contains(string(moved), "/home/u/.local/bin/crewbo") {
		t.Errorf("reinstall from a new path: %d handlers\n%s", n, moved)
	}
	// Uninstall restores the user's hooks and settings.
	back, changed, err := Uninstall(ClaudeCode, moved)
	if err != nil || !changed || countOurs(t, ClaudeCode, back) != 0 {
		t.Fatalf("Uninstall = %v %v\n%s", changed, err, back)
	}
	if got, want := decode(t, back), decode(t, []byte(userClaude)); !jsonEqual(got, want) {
		t.Errorf("uninstall did not restore the user config:\n%s", back)
	}
}

func TestCodexInstall(t *testing.T) {
	out, changed, err := Install(Codex, nil, "/home/u/.local/bin/crewbo")
	if err != nil || !changed {
		t.Fatalf("Install = %v %v", changed, err)
	}
	if !strings.Contains(string(out), `"command": "/home/u/.local/bin/crewbo hook codex pre_edit"`) || !strings.Contains(string(out), `"apply_patch|Bash"`) {
		t.Errorf("codex config:\n%s", out)
	}
	if n := countOurs(t, Codex, out); n != len(codexSpecs) {
		t.Errorf("handlers = %d", n)
	}
	// Claude entries are not touched by a Codex uninstall and vice versa.
	both, _, _ := Install(ClaudeCode, out, "/home/u/.local/bin/crewbo")
	only, _, _ := Uninstall(Codex, both)
	if countOurs(t, Codex, only) != 0 || countOurs(t, ClaudeCode, only) != len(claudeSpecs) {
		t.Errorf("uninstalling codex touched claude entries:\n%s", only)
	}
	if empty, changed, _ := Uninstall(ClaudeCode, only); empty != nil || !changed {
		t.Errorf("uninstall leaving nothing = %q %v, want nil (remove the file)", empty, changed)
	}
	if out, changed, _ := Uninstall(Codex, []byte(`{"theme":"dark"}`)); changed || string(out) != `{"theme":"dark"}` {
		t.Errorf("uninstall with nothing installed = %q %v", out, changed)
	}
}

func TestWindowsPathsAndUnsafePaths(t *testing.T) {
	out, _, err := Install(ClaudeCode, nil, `C:\Users\u\AppData\Local\crewbo\bin\crewbo.exe`)
	if err != nil || !strings.Contains(string(out), `"command": "C:/Users/u/AppData/Local/crewbo/bin/crewbo.exe"`) {
		t.Fatalf("windows install: %v\n%s", err, out)
	}
	if countOurs(t, ClaudeCode, out) != len(claudeSpecs) {
		t.Error("windows .exe entries not recognised as crewbo")
	}
	for _, bad := range []string{`C:\Program Files\crewbo\crewbo.exe`, "/opt/a&b/crewbo"} {
		if _, _, err := Install(Codex, nil, bad); !errors.Is(err, ErrUnsafePath) {
			t.Errorf("Install(codex, %q) err = %v, want ErrUnsafePath", bad, err)
		}
	}
	// Claude's exec form has no shell, so spaces are fine there.
	if _, _, err := Install(ClaudeCode, nil, `C:\Program Files\crewbo\crewbo.exe`); err != nil {
		t.Errorf("claude install with a space: %v", err)
	}
}

func TestBadConfigsAreLeftAlone(t *testing.T) {
	for _, cfg := range []string{`{not json`, `{"hooks": []}`} {
		if _, _, err := Install(ClaudeCode, []byte(cfg), "/b/crewbo"); err == nil {
			t.Errorf("Install accepted %q", cfg)
		}
		if _, _, err := Uninstall(Codex, []byte(cfg)); err == nil {
			t.Errorf("Uninstall accepted %q", cfg)
		}
	}
}

func TestApplyBacksUpAndWrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "settings.json")
	install := func(bin string) func([]byte) ([]byte, bool, error) {
		return func(b []byte) ([]byte, bool, error) { return Install(ClaudeCode, b, bin) }
	}
	uninstall := func(b []byte) ([]byte, bool, error) { return Uninstall(ClaudeCode, b) }

	// Missing file: created, no backup; uninstall removes it again.
	if changed, err := Apply(path, ClaudeCode, install("/b/crewbo")); err != nil || !changed {
		t.Fatalf("Apply new = %v %v", changed, err)
	}
	if _, err := os.Stat(path + ".crewbo.bak"); !errors.Is(err, os.ErrNotExist) {
		t.Error("backup written for a file that did not exist")
	}
	if changed, err := Apply(path, ClaudeCode, install("/b/crewbo")); err != nil || changed {
		t.Errorf("Apply unchanged = %v %v", changed, err)
	}
	if changed, err := Apply(path, ClaudeCode, uninstall); err != nil || !changed {
		t.Fatalf("Apply uninstall = %v %v", changed, err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Error("config Crewbo created is left behind after uninstall")
	}
	if changed, err := Apply(path, ClaudeCode, uninstall); err != nil || changed {
		t.Errorf("uninstall with no file = %v %v", changed, err)
	}

	// Existing file: the backup keeps the original through reinstall and uninstall.
	if err := os.WriteFile(path, []byte(userClaude), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, step := range []func([]byte) ([]byte, bool, error){install("/b/crewbo"), install("/c/crewbo"), uninstall} {
		if _, err := Apply(path, ClaudeCode, step); err != nil {
			t.Fatal(err)
		}
		if bak, _ := os.ReadFile(path + ".crewbo.bak"); string(bak) != userClaude {
			t.Fatalf("backup no longer holds the original:\n%s", bak)
		}
	}
	if b, _ := os.ReadFile(path); !jsonEqual(decode(t, b), decode(t, []byte(userClaude))) {
		t.Errorf("uninstall did not restore the config:\n%s", b)
	}
	// A transform error leaves the file alone.
	if _, err := Apply(path, ClaudeCode, func([]byte) ([]byte, bool, error) { return nil, false, errors.New("boom") }); err == nil {
		t.Error("transform error not returned")
	}
}

func TestConfigPath(t *testing.T) {
	env := platform.Env{Home: "/home/u", Getenv: func(k string) string {
		return map[string]string{"CODEX_HOME": "/srv/codex"}[k]
	}}
	if got := ConfigPath(ClaudeCode, env); got != filepath.Join("/home/u", ".claude", "settings.json") {
		t.Errorf("claude path = %q", got)
	}
	if got := ConfigPath(Codex, env); got != filepath.Join("/srv/codex", "hooks.json") {
		t.Errorf("codex path = %q", got)
	}
	env.Getenv = nil
	if got := ConfigPath(Codex, env); got != filepath.Join("/home/u", ".codex", "hooks.json") {
		t.Errorf("codex default = %q", got)
	}
}

func jsonEqual(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}
