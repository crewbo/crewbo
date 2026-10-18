// Package install adds and removes Crewbo's user-level hook entries for
// Claude Code (settings.json, exec form), Codex (hooks.json) and
// Antigravity (one named hook set, plus its MCP server). It changes only
// entries that run the crewbo binary, so the user's own hooks and settings
// are kept.
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
	"reflect"
	"slices"
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
// $CLAUDE_CONFIG_DIR/settings.json (default ~/.claude),
// $CODEX_HOME/hooks.json (default ~/.codex), and for Antigravity
// ~/.gemini/config/hooks.json and mcp_config.json (the docs give only the
// ~ form; on Windows the home is assumed, UNVERIFIED).
func ConfigPath(agent string, env platform.Env) string {
	get := env.Getenv
	if get == nil {
		get = func(string) string { return "" }
	}
	switch agent {
	case Antigravity:
		return filepath.Join(env.Home, ".gemini", "config", "hooks.json")
	case AntigravityMCP:
		return filepath.Join(env.Home, ".gemini", "config", "mcp_config.json")
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
	if agent == Antigravity || agent == AntigravityMCP {
		return installAntigravity(agent, cfg, bin)
	}
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
		g := newObject()
		if s.matcher != "" {
			g.set("matcher", s.matcher)
		}
		g.set("hooks", []any{handler(agent, bin, s.kind)})
		list, _ := hooks.get(s.event).([]any)
		hooks.set(s.event, append(list, g))
	}
	root.set("hooks", hooks)
	return render(cfg, root)
}

// Uninstall returns cfg without Crewbo's hooks for agent. out is nil when
// nothing else is left in the config, so the caller can remove the file
// (Codex hooks.json is usually created by the install).
func Uninstall(agent string, cfg []byte) (out []byte, changed bool, err error) {
	if agent == Antigravity || agent == AntigravityMCP {
		return uninstallAntigravity(agent, cfg)
	}
	root, hooks, err := parse(cfg)
	if err != nil {
		return nil, false, err
	}
	if remove(agent, hooks) == 0 {
		return cfg, false, nil
	}
	if len(hooks.keys) == 0 {
		root.del("hooks")
	}
	if len(root.keys) == 0 {
		return nil, true, nil
	}
	return render(cfg, root)
}

// installed reports whether cfg holds any Crewbo hook for agent.
func installed(agent string, cfg []byte) bool {
	if agent == Antigravity || agent == AntigravityMCP {
		_, parent, err := antigravityParent(agent, cfg)
		return err == nil && antigravityOurs(agent, parent.get(antigravityKey))
	}
	_, hooks, err := parse(cfg)
	return err == nil && remove(agent, hooks) > 0
}

// handler builds one entry. Claude Code gets exec form (no shell; a real
// .exe on Windows); Codex only has a command string.
func handler(agent, bin, kind string) *object {
	h := newObject()
	h.set("type", "command")
	if agent == ClaudeCode {
		h.set("command", bin)
		h.set("args", []any{"hook", ClaudeCode, kind})
	} else {
		h.set("command", bin+" hook "+Codex+" "+kind)
	}
	return h
}

// ours reports whether a handler is a Crewbo hook for agent.
func ours(agent string, h any) bool {
	m, ok := h.(*object)
	if !ok {
		return false
	}
	cmd, _ := m.get("command").(string)
	if agent == ClaudeCode {
		args, _ := m.get("args").([]any)
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
func remove(agent string, hooks *object) int {
	n := 0
	for _, event := range slices.Clone(hooks.keys) {
		groups, ok := hooks.get(event).([]any)
		if !ok {
			continue
		}
		var kept []any
		for _, g := range groups {
			gm, ok := g.(*object)
			if !ok {
				kept = append(kept, g)
				continue
			}
			hs, _ := gm.get("hooks").([]any)
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
				gm.set("hooks", keep)
			}
			kept = append(kept, gm)
		}
		if len(kept) == 0 {
			hooks.del(event)
		} else {
			hooks.set(event, kept)
		}
	}
	return n
}

func parse(cfg []byte) (root, hooks *object, err error) {
	root = newObject()
	if len(bytes.TrimSpace(cfg)) > 0 {
		v, err := decodeOrdered(cfg)
		if err != nil {
			return nil, nil, fmt.Errorf("install: config is not valid JSON, leaving it unchanged: %w", err)
		}
		if root, _ = v.(*object); root == nil {
			return nil, nil, errors.New("install: config is not a JSON object, leaving it unchanged")
		}
	}
	hooks, ok := root.get("hooks").(*object)
	if root.get("hooks") != nil && !ok {
		return nil, nil, errors.New(`install: "hooks" is not an object, leaving the config unchanged`)
	}
	if hooks == nil {
		hooks = newObject()
	}
	return root, hooks, nil
}

// render writes root in the config's own key order with two-space indent,
// reporting no change when it means the same as orig.
func render(orig []byte, root *object) ([]byte, bool, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(root); err != nil {
		return nil, false, err
	}
	out := buf.Bytes()
	if len(orig) > 0 && !bytes.HasSuffix(orig, []byte("\n")) {
		out = bytes.TrimSuffix(out, []byte("\n"))
	}
	var before, after any
	if json.Unmarshal(orig, &before) == nil && json.Unmarshal(out, &after) == nil && reflect.DeepEqual(before, after) {
		return orig, false, nil
	}
	return out, true, nil
}
