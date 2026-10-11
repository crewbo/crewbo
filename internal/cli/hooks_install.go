package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/crewbo/crewbo/internal/hooks/install"
	"github.com/crewbo/crewbo/internal/platform"
)

const hooksUsage = `Usage:
  crewbo hooks install   [--agent all|claude-code|codex] [--dry-run] [--binary PATH]
  crewbo hooks uninstall [--agent all|claude-code|codex] [--dry-run]

Adds or removes Crewbo's hooks in your user-level Claude Code settings
(~/.claude/settings.json) and Codex hooks (~/.codex/hooks.json). Your own
hooks and settings are kept, and a backup is written next to each file.
`

// minClaudeVersion is the first Claude Code release with exec-form hooks
// (CHANGELOG 2.1.139: "Added hook args: string[] field (exec form)").
var minClaudeVersion = [3]int{2, 1, 139}

func runHooksCmd(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || (args[0] != "install" && args[0] != "uninstall") {
		return write(stderr, ExitUsage, "%s", hooksUsage)
	}
	fs := flag.NewFlagSet("hooks "+args[0], flag.ContinueOnError)
	fs.SetOutput(stderr)
	agent := fs.String("agent", "all", "all, claude-code or codex")
	dry := fs.Bool("dry-run", false, "print the resulting config instead of writing it")
	binFlag := fs.String("binary", "", "path to the crewbo binary the hooks should run")
	if err := fs.Parse(args[1:]); err != nil {
		return ExitUsage
	}
	agents := []string{install.ClaudeCode, install.Codex}
	switch *agent {
	case "all":
	case install.ClaudeCode, install.Codex:
		agents = []string{*agent}
	default:
		return write(stderr, ExitUsage, "crewbo: unknown agent %q\n\n%s", *agent, hooksUsage)
	}
	env, err := platform.DefaultEnv()
	if err != nil {
		return write(stderr, ExitError, "crewbo: %v\n", err)
	}
	bin := *binFlag
	if args[0] == "install" && bin == "" {
		if bin, err = selfPath(); err != nil {
			return write(stderr, ExitError, "crewbo: %v\n", err)
		}
	}
	code := ExitOK
	for _, a := range agents {
		path := install.ConfigPath(a, env)
		if args[0] == "install" && *agent == "all" && !agentPresent(a, path) {
			_ = write(stdout, ExitOK, "%s: not found, skipped (use --agent %s to install anyway)\n", agentName(a), a)
			continue
		}
		if a == install.ClaudeCode && args[0] == "install" {
			if v, ok := claudeVersion(ctx); ok && less(v, minClaudeVersion) {
				_ = write(stderr, ExitError, "%s: version %d.%d.%d is older than 2.1.139, which added exec-form hooks; update Claude Code first\n", agentName(a), v[0], v[1], v[2])
				code = ExitError
				continue
			}
		}
		transform := func(b []byte) ([]byte, bool, error) {
			if args[0] == "install" {
				return install.Install(a, b, bin)
			}
			return install.Uninstall(a, b)
		}
		if *dry {
			orig, _ := os.ReadFile(path) //nolint:gosec // the agent's own config file
			out, changed, err := transform(orig)
			if err != nil {
				code = write(stderr, ExitError, "%s: %v\n", agentName(a), err)
				continue
			}
			_ = write(stdout, ExitOK, "%s: %s (would change: %v)\n%s\n", agentName(a), path, changed, out)
			continue
		}
		_, statErr := os.Stat(path)
		existed := statErr == nil
		changed, err := install.Apply(path, transform)
		if err != nil {
			code = write(stderr, ExitError, "%s: %v\n", agentName(a), err)
			continue
		}
		verb := map[bool]string{true: args[0] + "ed", false: "already " + args[0] + "ed, unchanged"}[changed]
		verb = strings.Replace(verb, "uninstalled", "removed", 1)
		_ = write(stdout, ExitOK, "%s: hooks %s in %s\n", agentName(a), verb, path)
		if changed && existed {
			_ = write(stdout, ExitOK, "  backup: %s.crewbo.bak\n", path)
		}
		if a == install.Codex && args[0] == "install" && changed {
			_ = write(stdout, ExitOK, "  Codex runs hooks only after you trust them: open Codex, type /hooks and trust the crewbo hooks.\n")
		}
	}
	return code
}

func agentName(a string) string {
	if a == install.ClaudeCode {
		return "Claude Code"
	}
	return "Codex"
}

// selfPath is the running binary, refusing temporary `go run` builds whose
// path disappears.
func selfPath() (string, error) {
	p, err := os.Executable()
	if err != nil {
		return "", err
	}
	if r, err := filepath.EvalSymlinks(p); err == nil {
		p = r
	}
	if tmp, err := filepath.EvalSymlinks(os.TempDir()); err == nil && strings.HasPrefix(p, tmp+string(filepath.Separator)) {
		return "", fmt.Errorf("this crewbo binary is in a temporary directory (%s); build it with `task build` or pass --binary", p)
	}
	return p, nil
}

// agentPresent reports whether the agent seems installed: its CLI on PATH
// or its config directory present.
func agentPresent(agent, configPath string) bool {
	name := map[string]string{install.ClaudeCode: "claude", install.Codex: "codex"}[agent]
	if _, err := exec.LookPath(name); err == nil {
		return true
	}
	_, err := os.Stat(filepath.Dir(configPath))
	return err == nil
}

var claudeVersionRE = regexp.MustCompile(`^(\d+)\.(\d+)\.(\d+)`)

func claudeVersion(ctx context.Context) ([3]int, bool) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "claude", "--version").Output()
	if err != nil {
		return [3]int{}, false
	}
	return parseVersion3(string(out))
}

func parseVersion3(s string) ([3]int, bool) {
	m := claudeVersionRE.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return [3]int{}, false
	}
	var v [3]int
	for i := range v {
		v[i], _ = strconv.Atoi(m[i+1])
	}
	return v, true
}

func less(a, b [3]int) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}
