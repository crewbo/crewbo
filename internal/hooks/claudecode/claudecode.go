// Package claudecode adapts Claude Code hook payloads to Crewbo's normalised
// events. Contract: https://code.claude.com/docs/en/hooks (verified
// 2026-10-11) and the payloads captured in Phase 0 under
// test/fixtures/hooks/claude-code.
package claudecode

import (
	"encoding/json"
	"fmt"

	"github.com/crewbo/crewbo/internal/hooks"
)

// Name is the agent name in `crewbo hook claude-code <event>`.
const Name = "claude-code"

// Adapter implements hooks.Adapter for Claude Code.
type Adapter struct{}

// Name returns "claude-code".
func (Adapter) Name() string { return Name }

// input holds the fields Crewbo reads. Common fields: session_id, cwd,
// hook_event_name; SessionStart may carry model; tool events carry
// tool_name and tool_input.
type input struct {
	SessionID string `json:"session_id"`
	Cwd       string `json:"cwd"`
	Model     string `json:"model"`
	ToolName  string `json:"tool_name"`
	ToolUseID string `json:"tool_use_id"`
	ToolInput struct {
		FilePath     string `json:"file_path"`     // Edit, Write
		NotebookPath string `json:"notebook_path"` // NotebookEdit
		Command      string `json:"command"`       // Bash, PowerShell
	} `json:"tool_input"`
}

// Decode parses a payload. The tool name, not the registered event name,
// decides between an edit and a shell command.
func (Adapter) Decode(kind hooks.Kind, payload []byte) (hooks.Event, error) {
	var in input
	if err := json.Unmarshal(payload, &in); err != nil {
		return hooks.Event{}, err
	}
	if in.SessionID == "" {
		return hooks.Event{}, fmt.Errorf("claudecode: payload has no session_id")
	}
	ev := hooks.Event{Kind: kind, Agent: Name, Session: in.SessionID, Cwd: in.Cwd, Model: in.Model, ToolUseID: in.ToolUseID}
	switch in.ToolName {
	case "Bash", "PowerShell":
		ev.Command = in.ToolInput.Command
		ev.Shell = hooks.POSIX
		if in.ToolName == "PowerShell" {
			ev.Shell = hooks.PowerShell
		}
		if kind == hooks.PreEdit {
			ev.Kind = hooks.PreShell
		}
	default:
		for _, p := range []string{in.ToolInput.FilePath, in.ToolInput.NotebookPath} {
			if p != "" {
				ev.Paths = append(ev.Paths, p)
			}
		}
	}
	return ev, nil
}

// eventNames maps normalised events to Claude Code's hookEventName.
var eventNames = map[hooks.Kind]string{
	hooks.SessionStart: "SessionStart",
	hooks.Prompt:       "UserPromptSubmit",
	hooks.PreEdit:      "PreToolUse",
	hooks.PreShell:     "PreToolUse",
	hooks.PostEdit:     "PostToolUse",
}

type output struct {
	HookSpecificOutput specific `json:"hookSpecificOutput"`
}

type specific struct {
	HookEventName            string `json:"hookEventName"`
	PermissionDecision       string `json:"permissionDecision,omitempty"`
	PermissionDecisionReason string `json:"permissionDecisionReason,omitempty"`
	AdditionalContext        string `json:"additionalContext,omitempty"`
}

// Encode renders a result. Allowing prints nothing, which Claude Code reads
// as "no decision", so the user's own permission rules still apply. Stop
// never prints: additionalContext on Stop makes the conversation continue.
func (Adapter) Encode(kind hooks.Kind, r hooks.Result) ([]byte, int) {
	name, ok := eventNames[kind]
	if !ok {
		return nil, 0
	}
	out := output{HookSpecificOutput: specific{HookEventName: name}}
	switch {
	case r.Deny && name == "PreToolUse":
		out.HookSpecificOutput.PermissionDecision = "deny"
		out.HookSpecificOutput.PermissionDecisionReason = r.Reason
	default:
		ctx := r.Context
		if r.Warning != "" {
			if ctx != "" {
				ctx += "\n"
			}
			ctx += r.Warning
		}
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
