package cli

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestHooksInstallUninstall(t *testing.T) {
	isolate(t)
	// No real claude or codex: their output must not decide this test, and
	// a claude behind a version manager can stall without network access.
	t.Setenv("PATH", t.TempDir())
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
	// Antigravity: one named hook set and one MCP server under ~/.gemini,
	// next to the member's own.
	home, _ := os.UserHomeDir()
	agHooks := filepath.Join(home, ".gemini", "config", "hooks.json")
	agMCP := filepath.Join(home, ".gemini", "config", "mcp_config.json")
	if err := os.MkdirAll(filepath.Dir(agHooks), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(agHooks, []byte(`{"lint": {"PostToolUse": [{"matcher": "write_to_file", "hooks": [{"command": "make lint"}]}]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	code, out, errOut = run("install", "--agent", "antigravity", "--binary", "/opt/crewbo/crewbo")
	if code != ExitOK || !strings.Contains(out, "Antigravity: hooks installed in "+agHooks) || !strings.Contains(out, "Antigravity MCP: MCP server installed in "+agMCP) || strings.Contains(out, "type /hooks") {
		t.Fatalf("antigravity install: %d %q %q", code, out, errOut)
	}
	if b, _ := os.ReadFile(agHooks); !strings.Contains(string(b), `"lint"`) || !strings.Contains(string(b), `"command": "/opt/crewbo/crewbo hook antigravity pre_edit"`) {
		t.Errorf("hooks.json:\n%s", b)
	}
	if b, _ := os.ReadFile(agMCP); !strings.Contains(string(b), `"mcpServers"`) || !strings.Contains(string(b), `"args": [`) {
		t.Errorf("mcp_config.json:\n%s", b)
	}
	if code, _, errOut = run("install", "--agent", "antigravity", "--binary", "/opt/my tools/crewbo"); code != ExitError || !strings.Contains(errOut, "space") {
		t.Errorf("antigravity spaced path: %d %q", code, errOut)
	}
	if code, out, _ = run("uninstall", "--agent", "antigravity"); code != ExitOK || !strings.Contains(out, "Antigravity: hooks removed") {
		t.Errorf("antigravity uninstall: %d %q", code, out)
	}
	if b, _ := os.ReadFile(agHooks); strings.Contains(string(b), "crewbo") || !strings.Contains(string(b), `"lint"`) {
		t.Errorf("hooks.json after uninstall:\n%s", b)
	}
	if _, err := os.Stat(agMCP); err == nil {
		t.Error("mcp_config.json left behind; Crewbo created it")
	}

	for _, args := range [][]string{{}, {"frob"}, {"install", "--agent", "cursor"}, {"install", "--agent", "antigravity-mcp"}} {
		if code, _, _ := run(args...); code != ExitUsage {
			t.Errorf("hooks %v: exit %d, want usage", args, code)
		}
	}
}

// A claude that hangs and leaves a child holding stdout must not hang
// `crewbo hooks install` (seen with a mise shim and no network).
func TestClaudeVersionDoesNotHang(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell script as the fake claude")
	}
	sleep, err := exec.LookPath("sleep") // before PATH is narrowed below
	if err != nil {
		t.Skip("no sleep binary")
	}
	dir := t.TempDir()
	script := "#!/bin/sh\n" + sleep + " 30 &\n" + sleep + " 30\n"
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte(script), 0o700); err != nil { //nolint:gosec // test script must be executable
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, ok := claudeVersion(ctx); ok {
		t.Error("parsed a version from a hung claude")
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("claudeVersion took %v after its deadline", d)
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
