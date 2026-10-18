package install

import (
	"errors"
	"strings"
	"testing"
)

const userAGHooks = `{
  "lint": {"PostToolUse": [{"matcher": "write_to_file", "hooks": [{"command": "make lint"}]}]}
}`

const userMCP = `{"mcpServers": {"sqlite": {"command": "node", "args": ["db.js"]}}}`

func TestAntigravityHooks(t *testing.T) {
	out, changed, err := Install(Antigravity, []byte(userAGHooks), `C:\Tools\crewbo.exe`)
	if err != nil || !changed {
		t.Fatalf("Install = %v, %v", changed, err)
	}
	m := decode(t, out)
	if _, ok := m["lint"]; !ok {
		t.Errorf("the member's hook is gone:\n%s", out)
	}
	cb := m["crewbo"].(map[string]any)
	if cb["enabled"] != true {
		t.Errorf("enabled = %v", cb["enabled"])
	}
	for event, kind := range map[string]string{"SessionStart": "session_start", "PreInvocation": "prompt", "Stop": "stop"} {
		h := cb[event].([]any)[0].(map[string]any)
		if h["type"] != "command" || h["command"] != "C:/Tools/crewbo.exe hook antigravity "+kind {
			t.Errorf("%s = %v", event, h)
		}
	}
	for event, kind := range map[string]string{"PreToolUse": "pre_edit", "PostToolUse": "post_edit"} {
		g := cb[event].([]any)[0].(map[string]any)
		if g["matcher"] != "write_to_file|replace_file_content|multi_replace_file_content|sed_file|notebook_edit|run_command" {
			t.Errorf("%s matcher = %v", event, g["matcher"])
		}
		if h := g["hooks"].([]any)[0].(map[string]any); h["command"] != "C:/Tools/crewbo.exe hook antigravity "+kind {
			t.Errorf("%s handler = %v", event, h)
		}
	}
	if again, changed, err := Install(Antigravity, out, `C:\Tools\crewbo.exe`); err != nil || changed || string(again) != string(out) {
		t.Errorf("reinstall changed the file: %v %v", changed, err)
	}
	moved, changed, _ := Install(Antigravity, out, "/usr/local/bin/crewbo")
	if !changed || strings.Contains(string(moved), "C:/Tools") || strings.Count(string(moved), "hook antigravity") != 5 {
		t.Errorf("a new binary path:\n%s", moved)
	}
	if !installed(Antigravity, out) || installed(Antigravity, []byte(userAGHooks)) {
		t.Error("installed() is wrong")
	}

	back, changed, err := Uninstall(Antigravity, out)
	if err != nil || !changed || strings.Contains(string(back), "crewbo") || decode(t, back)["lint"] == nil {
		t.Errorf("Uninstall = %s %v %v", back, changed, err)
	}
	if same, changed, _ := Uninstall(Antigravity, []byte(userAGHooks)); changed || string(same) != userAGHooks {
		t.Error("uninstall without Crewbo changed the file")
	}
	only, _, _ := Install(Antigravity, nil, "/b/crewbo")
	if gone, changed, err := Uninstall(Antigravity, only); gone != nil || !changed || err != nil {
		t.Errorf("a file Crewbo created is not removed: %s %v %v", gone, changed, err)
	}
}

func TestAntigravityMCP(t *testing.T) {
	out, changed, err := Install(AntigravityMCP, []byte(userMCP), "/b/crewbo")
	if err != nil || !changed {
		t.Fatalf("Install = %v, %v", changed, err)
	}
	servers := decode(t, out)["mcpServers"].(map[string]any)
	if servers["sqlite"] == nil {
		t.Error("the member's server is gone")
	}
	if s := servers["crewbo"].(map[string]any); s["command"] != "/b/crewbo" || s["args"].([]any)[0] != "mcp" {
		t.Errorf("crewbo server = %v", s)
	}
	back, _, _ := Uninstall(AntigravityMCP, out)
	if strings.Contains(string(back), "crewbo") || decode(t, back)["mcpServers"].(map[string]any)["sqlite"] == nil {
		t.Errorf("uninstall:\n%s", back)
	}
	only, _, _ := Install(AntigravityMCP, []byte(`{"mcpServers": {}}`), "/b/crewbo")
	if gone, _, _ := Uninstall(AntigravityMCP, only); gone != nil {
		t.Errorf("an MCP file left with nothing in it: %s", gone)
	}
	kept, _, _ := Install(AntigravityMCP, []byte(`{"theme": 1}`), "/b/crewbo")
	if back, _, _ := Uninstall(AntigravityMCP, kept); len(decode(t, back)) != 1 || decode(t, back)["theme"] != float64(1) {
		t.Errorf("other settings after uninstall: %s", back)
	}
}

func TestAntigravityRefuses(t *testing.T) {
	for name, tc := range map[string]struct {
		agent, cfg, bin string
		want            string
	}{
		"spaced binary":          {Antigravity, "", "/opt/my tools/crewbo", "space"},
		"ampersand binary":       {AntigravityMCP, "", "/opt/a&b/crewbo", "space"},
		"invalid JSON":           {Antigravity, "{", "/b/crewbo", "not valid JSON"},
		"not an object":          {Antigravity, "[]", "/b/crewbo", "not a JSON object"},
		"mcpServers not object":  {AntigravityMCP, `{"mcpServers": []}`, "/b/crewbo", `"mcpServers" is not an object`},
		"someone else's crewbo":  {Antigravity, `{"crewbo": {"Stop": [{"command": "say done"}]}}`, "/b/crewbo", "does not run the crewbo binary"},
		"someone else's server":  {AntigravityMCP, `{"mcpServers": {"crewbo": {"command": "npx", "args": ["other"]}}}`, "/b/crewbo", "does not run the crewbo binary"},
		"crewbo is not an entry": {Antigravity, `{"crewbo": 3}`, "/b/crewbo", "does not run the crewbo binary"},
		"another agent's hook":   {Antigravity, `{"crewbo": {"Stop": [{"command": "/b/crewbo hook codex stop"}]}}`, "/b/crewbo", "does not run the crewbo binary"},
	} {
		if _, _, err := Install(tc.agent, []byte(tc.cfg), tc.bin); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: install: %v", name, err)
		}
	}
	for _, cfg := range []string{`{"crewbo": {"Stop": [{"command": "say done"}]}}`} {
		if _, _, err := Uninstall(Antigravity, []byte(cfg)); !errors.Is(err, ErrNotOurs) {
			t.Errorf("uninstall of someone else's entry: %v", err)
		}
	}
	// An older entry with only matcher groups is still Crewbo's.
	grouped := `{"crewbo": {"PreToolUse": [{"matcher": "write_to_file", "hooks": [{"command": "/b/crewbo hook antigravity pre_edit"}]}]}, "lint": {}}`
	if back, changed, err := Uninstall(Antigravity, []byte(grouped)); err != nil || !changed || strings.Contains(string(back), "antigravity") {
		t.Errorf("uninstall of a grouped entry: %s %v %v", back, changed, err)
	}
	if _, _, err := Uninstall(AntigravityMCP, []byte(`{"mcpServers": {"crewbo": {"command": "npx"}}}`)); !errors.Is(err, ErrNotOurs) {
		t.Errorf("uninstall of someone else's server: %v", err)
	}
}
