package install

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"github.com/crewbo/crewbo/internal/hooks/antigravity"
)

// Antigravity owns one named hook set, "crewbo", in ~/.gemini/config/hooks.json
// (https://antigravity.google/docs/hooks: top-level keys are hook names),
// and one server, "crewbo", under mcpServers in
// ~/.gemini/config/mcp_config.json (https://antigravity.google/docs/mcp).
// Everything else in either file is the member's and is kept.
const (
	Antigravity    = "antigravity"
	AntigravityMCP = "antigravity-mcp" // the MCP file, installed with Antigravity
	antigravityKey = "crewbo"
)

// ErrNotOurs refuses to replace or remove a "crewbo" entry Crewbo did not
// write.
var ErrNotOurs = errors.New(`install: an entry named "crewbo" exists but does not run the crewbo binary; rename it or remove it first`)

// antigravityHooks builds the named hook set. Handlers are shell-form
// command strings (Antigravity has no exec form). SessionStart,
// PreInvocation and Stop take handlers directly; tool events take matcher
// groups, as in spike S9.
func antigravityHooks(bin string) *object {
	cmd := func(kind string) []any {
		h := newObject()
		h.set("type", "command")
		h.set("command", bin+" hook "+Antigravity+" "+kind)
		return []any{h}
	}
	group := func(kind string) []any {
		g := newObject()
		g.set("matcher", strings.Join(append(append([]string{}, antigravity.WriteTools...), antigravity.RunCommand), "|"))
		g.set("hooks", cmd(kind))
		return []any{g}
	}
	e := newObject()
	e.set("enabled", true)
	e.set("SessionStart", cmd("session_start"))
	e.set("PreInvocation", cmd("prompt"))
	e.set("PreToolUse", group("pre_edit"))
	e.set("PostToolUse", group("post_edit"))
	e.set("Stop", cmd("stop"))
	return e
}

func antigravityMCP(bin string) *object {
	s := newObject()
	s.set("command", bin)
	s.set("args", []any{"mcp"})
	return s
}

// installAntigravity sets the "crewbo" entry of the hooks file, or of the
// mcpServers object in the MCP file.
func installAntigravity(agent string, cfg []byte, bin string) ([]byte, bool, error) {
	if strings.ContainsAny(bin, " &") {
		return nil, false, fmt.Errorf("%w: %s", ErrUnsafePath, bin)
	}
	root, parent, err := antigravityParent(agent, cfg)
	if err != nil {
		return nil, false, err
	}
	if v := parent.get(antigravityKey); v != nil && !antigravityOurs(agent, v) {
		return nil, false, ErrNotOurs
	}
	if agent == AntigravityMCP {
		parent.set(antigravityKey, antigravityMCP(bin))
		root.set("mcpServers", parent)
	} else {
		parent.set(antigravityKey, antigravityHooks(bin))
	}
	return render(cfg, root)
}

// uninstallAntigravity removes the "crewbo" entry; out is nil when nothing
// else is left, so the caller removes the file.
func uninstallAntigravity(agent string, cfg []byte) ([]byte, bool, error) {
	root, parent, err := antigravityParent(agent, cfg)
	if err != nil {
		return nil, false, err
	}
	v := parent.get(antigravityKey)
	if v == nil {
		return cfg, false, nil
	}
	if !antigravityOurs(agent, v) {
		return nil, false, ErrNotOurs
	}
	parent.del(antigravityKey)
	if agent == AntigravityMCP && len(parent.keys) == 0 {
		root.del("mcpServers")
	}
	if len(root.keys) == 0 {
		return nil, true, nil
	}
	return render(cfg, root)
}

// antigravityParent parses cfg and returns the object holding the "crewbo"
// entry: the root for hooks, mcpServers for MCP.
func antigravityParent(agent string, cfg []byte) (root, parent *object, err error) {
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
	if agent != AntigravityMCP {
		return root, root, nil
	}
	parent, ok := root.get("mcpServers").(*object)
	if root.get("mcpServers") != nil && !ok {
		return nil, nil, errors.New(`install: "mcpServers" is not an object, leaving the config unchanged`)
	}
	if parent == nil {
		parent = newObject()
	}
	return root, parent, nil
}

// antigravityOurs reports whether a "crewbo" entry runs the crewbo binary:
// the MCP server's command, or any hook handler's first word.
func antigravityOurs(agent string, v any) bool {
	e, ok := v.(*object)
	if !ok {
		return false
	}
	if agent == AntigravityMCP {
		cmd, _ := e.get("command").(string)
		return isCrewbo(cmd)
	}
	for _, k := range e.keys {
		list, _ := e.get(k).([]any)
		for _, item := range list {
			h, _ := item.(*object)
			if h == nil {
				continue
			}
			handlers := []any{h}
			if hs, ok := h.get("hooks").([]any); ok {
				handlers = hs
			}
			for _, x := range handlers {
				if m, ok := x.(*object); ok {
					cmd, _ := m.get("command").(string)
					if f := strings.Fields(cmd); len(f) >= 3 && isCrewbo(f[0]) && f[1] == "hook" && f[2] == Antigravity {
						return true
					}
				}
			}
		}
	}
	return false
}
