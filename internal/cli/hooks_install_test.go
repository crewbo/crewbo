package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHooksInstallUninstall(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(dir, "claude"))
	t.Setenv("CODEX_HOME", filepath.Join(dir, "codex"))
	claude := filepath.Join(dir, "claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(claude), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(claude, []byte(`{"theme":"dark"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) (int, string, string) {
		var stdout, stderr bytes.Buffer
		code := Run(context.Background(), append([]string{"hooks"}, args...), nil, &stdout, &stderr, BuildInfo{})
		return code, stdout.String(), stderr.String()
	}

	code, out, errOut := run("install", "--agent", "claude-code", "--binary", "/opt/crewbo/crewbo")
	if code != ExitOK || !strings.Contains(out, "Claude Code: hooks installed") {
		t.Fatalf("install: %d %q %q", code, out, errOut)
	}
	b, _ := os.ReadFile(claude)
	if !strings.Contains(string(b), `"theme": "dark"`) || !strings.Contains(string(b), `"/opt/crewbo/crewbo"`) {
		t.Errorf("settings.json:\n%s", b)
	}
	if bak, _ := os.ReadFile(claude + ".crewbo.bak"); string(bak) != `{"theme":"dark"}` {
		t.Errorf("backup = %q", bak)
	}
	if _, out, _ = run("install", "--agent", "claude-code", "--binary", "/opt/crewbo/crewbo"); !strings.Contains(out, "already installed, unchanged") {
		t.Errorf("reinstall: %q", out)
	}
	code, out, _ = run("install", "--agent", "codex", "--binary", "/opt/crewbo/crewbo")
	if code != ExitOK || !strings.Contains(out, "/hooks") {
		t.Errorf("codex install: %d %q", code, out)
	}
	if code, _, errOut = run("install", "--agent", "codex", "--binary", "/opt/my tools/crewbo"); code != ExitError || !strings.Contains(errOut, "space") {
		t.Errorf("spaced path: %d %q", code, errOut)
	}
	if code, out, _ = run("uninstall", "--agent", "claude-code", "--dry-run"); code != ExitOK || !strings.Contains(out, "would change: true") {
		t.Errorf("dry run: %d %q", code, out)
	}
	if code, out, _ = run("uninstall"); code != ExitOK || !strings.Contains(out, "Claude Code: hooks removed") || !strings.Contains(out, "Codex: hooks removed") {
		t.Errorf("uninstall: %d %q", code, out)
	}
	if b, _ := os.ReadFile(claude); strings.Contains(string(b), "crewbo") || !strings.Contains(string(b), `"theme"`) {
		t.Errorf("after uninstall:\n%s", b)
	}
	if bak, _ := os.ReadFile(claude + ".crewbo.bak"); string(bak) != `{"theme":"dark"}` {
		t.Errorf("uninstall replaced the backup: %q", bak)
	}
	codex := filepath.Join(dir, "codex", "hooks.json")
	for _, p := range []string{codex, codex + ".crewbo.bak"} {
		if _, err := os.Stat(p); err == nil {
			t.Errorf("%s left behind; Crewbo created hooks.json", p)
		}
	}
	for _, args := range [][]string{{}, {"frob"}, {"install", "--agent", "cursor"}} {
		if code, _, _ := run(args...); code != ExitUsage {
			t.Errorf("hooks %v: exit %d, want usage", args, code)
		}
	}
}

func TestVersionCompare(t *testing.T) {
	v, ok := parseVersion3("2.1.294 (Claude Code)\n")
	if !ok || v != [3]int{2, 1, 294} {
		t.Fatalf("parse = %v %v", v, ok)
	}
	if !less([3]int{2, 1, 138}, minClaudeVersion) || less([3]int{2, 1, 139}, minClaudeVersion) || less([3]int{3, 0, 0}, minClaudeVersion) {
		t.Error("less around 2.1.139")
	}
	if _, ok := parseVersion3("unknown"); ok {
		t.Error("parsed garbage")
	}
}
