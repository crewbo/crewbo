// Package install adds and removes Crewbo's user-level hook entries for
// Claude Code (settings.json, exec form) and Codex (hooks.json). It changes
// only entries that run the crewbo binary with `hook <agent>`, so the user's
// own hooks and settings are kept.
//
// Contracts: https://code.claude.com/docs/en/hooks and
// https://learn.chatgpt.com/docs/hooks (verified 2026-10-11).
package install

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/crewbo/crewbo/internal/platform"
)

// Agent names, as used in `crewbo hook <agent> <event>`.
const (
	ClaudeCode = "claude-code"
	Codex      = "codex"
)

// ErrUnsafePath rejects a binary path Codex cannot run reliably: an
// unquoted path with a space made Codex skip the hook silently (Phase 0,
// U-22), and cmd.exe treats & as a separator.
var ErrUnsafePath = errors.New("install: binary path contains a space or '&'")

// ConfigPath returns the file each agent reads user hooks from:
// $CLAUDE_CONFIG_DIR/settings.json (default ~/.claude) and
// $CODEX_HOME/hooks.json (default ~/.codex).
func ConfigPath(agent string, env platform.Env) string {
	get := env.Getenv
	if get == nil {
		get = func(string) string { return "" }
	}
	switch agent {
	case ClaudeCode:
		dir := get("CLAUDE_CONFIG_DIR")
		if dir == "" {
			dir = filepath.Join(env.Home, ".claude")
		}
		return filepath.Join(dir, "settings.json")
	default:
		dir := get("CODEX_HOME")
		if dir == "" {
			dir = filepath.Join(env.Home, ".codex")
		}
		return filepath.Join(dir, "hooks.json")
	}
}

// group is one matcher group: {"matcher": "...", "hooks": [handler]}.
type spec struct {
	event   string
	matcher string // "" for events without matchers
	kind    string // crewbo's normalised event
}

var claudeSpecs = []spec{
	{"SessionStart", "", "session_start"},
	{"UserPromptSubmit", "", "prompt"},
	{"PreToolUse", "Edit|Write|NotebookEdit", "pre_edit"},
	{"PreToolUse", "Bash|PowerShell", "pre_shell"},
	{"PostToolUse", "Edit|Write|NotebookEdit|Bash|PowerShell", "post_edit"},
	{"Stop", "", "stop"},
}

var codexSpecs = []spec{
	{"SessionStart", "", "session_start"},
	{"UserPromptSubmit", "", "prompt"},
	{"PreToolUse", "apply_patch|Bash", "pre_edit"},
	{"PostToolUse", "apply_patch|Bash", "post_edit"},
	{"Stop", "", "stop"},
}

// Install returns cfg with Crewbo's hooks for agent pointing at bin, after
// removing any older Crewbo entries. changed is false when cfg already had
// exactly these entries.
func Install(agent string, cfg []byte, bin string) (out []byte, changed bool, err error) {
	// Forward slashes for Windows paths: Git Bash strips backslashes, and the
	// hooks docs recommend forward slashes (docs/architecture/hooks.md).
	if len(bin) >= 3 && bin[1] == ':' {
		bin = strings.ReplaceAll(bin, `\`, "/")
	}
	bin = filepath.ToSlash(bin)
	if agent == Codex && strings.ContainsAny(bin, " &") {
		return nil, false, fmt.Errorf("%w: %s", ErrUnsafePath, bin)
	}
	root, hooks, err := parse(cfg)
	if err != nil {
		return nil, false, err
	}
	remove(agent, hooks)
	specs := claudeSpecs
	if agent == Codex {
		specs = codexSpecs
	}
	for _, s := range specs {
		g := map[string]any{"hooks": []any{handler(agent, bin, s.kind)}}
		if s.matcher != "" {
			g["matcher"] = s.matcher
		}
		list, _ := hooks[s.event].([]any)
		hooks[s.event] = append(list, g)
	}
	root["hooks"] = hooks
	return render(cfg, root)
}

// Uninstall returns cfg without Crewbo's hooks for agent. out is nil when
// nothing else is left in the config, so the caller can remove the file
// (Codex hooks.json is usually created by the install).
func Uninstall(agent string, cfg []byte) (out []byte, changed bool, err error) {
	root, hooks, err := parse(cfg)
	if err != nil {
		return nil, false, err
	}
	if remove(agent, hooks) == 0 {
		return cfg, false, nil
	}
	if len(hooks) == 0 {
		delete(root, "hooks")
	} else {
		root["hooks"] = hooks
	}
	if len(root) == 0 {
		return nil, true, nil
	}
	return render(cfg, root)
}

// installed reports whether cfg holds any Crewbo hook for agent.
func installed(agent string, cfg []byte) bool {
	_, hooks, err := parse(cfg)
	return err == nil && remove(agent, hooks) > 0
}

// handler builds one entry. Claude Code gets exec form (no shell; a real
// .exe on Windows); Codex only has a command string.
func handler(agent, bin, kind string) map[string]any {
	if agent == ClaudeCode {
		return map[string]any{"type": "command", "command": bin, "args": []any{"hook", ClaudeCode, kind}}
	}
	return map[string]any{"type": "command", "command": bin + " hook " + Codex + " " + kind}
}

// ours reports whether a handler is a Crewbo hook for agent.
func ours(agent string, h any) bool {
	m, ok := h.(map[string]any)
	if !ok {
		return false
	}
	cmd, _ := m["command"].(string)
	if agent == ClaudeCode {
		args, _ := m["args"].([]any)
		return isCrewbo(cmd) && len(args) >= 2 && args[0] == "hook" && args[1] == ClaudeCode
	}
	fields := strings.Fields(cmd)
	return len(fields) >= 3 && isCrewbo(fields[0]) && fields[1] == "hook" && fields[2] == Codex
}

func isCrewbo(cmd string) bool {
	base := strings.ToLower(filepath.Base(filepath.FromSlash(strings.ReplaceAll(cmd, `\`, "/"))))
	return base == "crewbo" || base == "crewbo.exe"
}

// remove drops Crewbo handlers for agent, then empty groups and events,
// and returns how many handlers it dropped.
func remove(agent string, hooks map[string]any) int {
	n := 0
	for event, v := range hooks {
		groups, ok := v.([]any)
		if !ok {
			continue
		}
		var kept []any
		for _, g := range groups {
			gm, ok := g.(map[string]any)
			if !ok {
				kept = append(kept, g)
				continue
			}
			hs, _ := gm["hooks"].([]any)
			var keep []any
			for _, h := range hs {
				if ours(agent, h) {
					n++
				} else {
					keep = append(keep, h)
				}
			}
			if len(hs) > 0 && len(keep) == 0 {
				continue // the group only held Crewbo hooks
			}
			if len(keep) != len(hs) {
				gm["hooks"] = keep
			}
			kept = append(kept, gm)
		}
		if len(kept) == 0 {
			delete(hooks, event)
		} else {
			hooks[event] = kept
		}
	}
	return n
}

func parse(cfg []byte) (map[string]any, map[string]any, error) {
	root := map[string]any{}
	if len(bytes.TrimSpace(cfg)) > 0 {
		if err := json.Unmarshal(cfg, &root); err != nil {
			return nil, nil, fmt.Errorf("install: config is not valid JSON, leaving it unchanged: %w", err)
		}
	}
	hooks, ok := root["hooks"].(map[string]any)
	if root["hooks"] != nil && !ok {
		return nil, nil, errors.New(`install: "hooks" is not an object, leaving the config unchanged`)
	}
	if hooks == nil {
		hooks = map[string]any{}
	}
	return root, hooks, nil
}

func render(orig []byte, root map[string]any) ([]byte, bool, error) {
	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return nil, false, err
	}
	out = append(out, '\n')
	var before any
	if json.Unmarshal(orig, &before) == nil {
		var after any
		_ = json.Unmarshal(out, &after)
		if fmt.Sprint(before) == fmt.Sprint(after) {
			return orig, false, nil
		}
	}
	return out, true, nil
}
