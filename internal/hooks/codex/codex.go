// Package codex adapts Codex CLI hook payloads to Crewbo's normalised events.
// Contract: https://learn.chatgpt.com/docs/hooks (verified 2026-10-11) and
// the payloads captured in Phase 0 under test/fixtures/hooks/codex.
package codex

import (
	"bufio"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/crewbo/crewbo/internal/hooks"
)

// Name is the agent name in `crewbo hook codex <event>`.
const Name = "codex"

// Adapter implements hooks.Adapter for Codex. PowerShell tells it that
// shell commands on this machine are PowerShell even though Codex names the
// tool "Bash" (set from platform.AgentShellIsPowerShell).
type Adapter struct {
	PowerShell bool
}

// Name returns "codex".
func (Adapter) Name() string { return Name }

type input struct {
	SessionID string `json:"session_id"`
	Cwd       string `json:"cwd"`
	Model     string `json:"model"`
	ToolName  string `json:"tool_name"`
	ToolInput struct {
		Command string `json:"command"` // Bash command or raw apply_patch text
	} `json:"tool_input"`
}

// Decode parses a payload. apply_patch targets come from the patch headers.
func (a Adapter) Decode(kind hooks.Kind, payload []byte) (hooks.Event, error) {
	var in input
	if err := json.Unmarshal(payload, &in); err != nil {
		return hooks.Event{}, err
	}
	if in.SessionID == "" {
		return hooks.Event{}, fmt.Errorf("codex: payload has no session_id")
	}
	ev := hooks.Event{Kind: kind, Agent: Name, Session: in.SessionID, Cwd: in.Cwd, Model: in.Model}
	switch in.ToolName {
	case "apply_patch":
		ev.Paths = PatchPaths(in.ToolInput.Command)
		if kind == hooks.PreShell {
			ev.Kind = hooks.PreEdit
		}
	case "Bash":
		ev.Command = in.ToolInput.Command
		ev.Shell = hooks.POSIX
		if a.PowerShell {
			ev.Shell = hooks.PowerShell
		}
		if kind == hooks.PreEdit {
			ev.Kind = hooks.PreShell
		}
	}
	return ev, nil
}

// PatchPaths returns every file an apply_patch text touches, from its
// "*** Add File:", "*** Update File:", "*** Delete File:" and "*** Move to:"
// headers, in order and without duplicates. Paths are returned as written:
// absolute (Codex 0.161.0, Linux) or relative (0.154.0 Windows, 0.162.1
// macOS).
func PatchPaths(patch string) []string {
	var out []string
	seen := map[string]bool{}
	sc := bufio.NewScanner(strings.NewReader(patch))
	sc.Buffer(make([]byte, 0, 64*1024), hooks.MaxPayload)
	for sc.Scan() {
		line := strings.TrimSuffix(sc.Text(), "\r")
		for _, h := range []string{"*** Add File: ", "*** Update File: ", "*** Delete File: ", "*** Move to: "} {
			if p, ok := strings.CutPrefix(line, h); ok {
				if p = strings.TrimSpace(p); p != "" && !seen[p] {
					seen[p] = true
					out = append(out, p)
				}
			}
		}
	}
	return out
}

// eventNames lists the events whose output accepts additionalContext
// (Codex docs: SessionStart, UserPromptSubmit, PreToolUse, PostToolUse).
var eventNames = map[hooks.Kind]string{
	hooks.SessionStart: "SessionStart",
	hooks.Prompt:       "UserPromptSubmit",
	hooks.PreEdit:      "PreToolUse",
	hooks.PreShell:     "PreToolUse",
	hooks.PostEdit:     "PostToolUse",
}

// output may only contain fields Codex supports: an unsupported field such
// as "ask", "continue", "stopReason" or "suppressOutput" fails the hook and
// lets the tool run.
type output struct {
	HookSpecificOutput specific `json:"hookSpecificOutput"`
}

type specific struct {
	HookEventName            string `json:"hookEventName"`
	PermissionDecision       string `json:"permissionDecision,omitempty"` // only ever "deny"
	PermissionDecisionReason string `json:"permissionDecisionReason,omitempty"`
	AdditionalContext        string `json:"additionalContext,omitempty"`
}

// Encode renders a result. Allowing prints nothing; Stop never prints.
func (Adapter) Encode(kind hooks.Kind, r hooks.Result) ([]byte, int) {
	name, ok := eventNames[kind]
	if !ok {
		return nil, 0
	}
	out := output{HookSpecificOutput: specific{HookEventName: name}}
	if r.Deny && name == "PreToolUse" {
		out.HookSpecificOutput.PermissionDecision = "deny"
		out.HookSpecificOutput.PermissionDecisionReason = r.Reason
	} else {
		ctx := strings.TrimSpace(r.Context + "\n" + r.Warning)
		if ctx == "" {
			return nil, 0
		}
		out.HookSpecificOutput.AdditionalContext = ctx
	}
	b, err := json.Marshal(out)
	if err != nil {
		return nil, 0
	}
	return b, 0
}
