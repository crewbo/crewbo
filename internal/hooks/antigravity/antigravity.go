// Package antigravity adapts Antigravity hook payloads (the agy CLI, the
// IDE and Antigravity 2.0 share one hook system) to Crewbo's normalised
// events. Contract: https://antigravity.google/docs/hooks (fetched
// 2026-10-18) and the payloads captured in spikes S9 (agy 1.3.3) and S9b
// (agy 1.4.2) under test/fixtures/hooks/antigravity
// (docs/research/antigravity-hooks.md).
//
// Payloads carry no event name, so the event comes from the command line
// (`crewbo hook antigravity <event>`), as for the other agents.
package antigravity

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/crewbo/crewbo/internal/hooks"
)

// Name is the agent name in `crewbo hook antigravity <event>`.
const Name = "antigravity"

// Adapter implements hooks.Adapter for Antigravity. PowerShell marks
// run_command lines as PowerShell (Windows, UNVERIFIED: see unknowns.md).
type Adapter struct {
	PowerShell bool
}

// Name returns "antigravity".
func (Adapter) Name() string { return Name }

// RunCommand is the shell tool; WriteTools change files (the PreToolUse
// matcher the installer registers). sed_file and notebook_edit appeared in
// agy 1.4.2's tool list; their arguments are not captured yet, so their
// target comes from the fallback keys in pathKeys.
const RunCommand = "run_command"

// WriteTools are the tools that write files.
var WriteTools = []string{"write_to_file", "replace_file_content", "multi_replace_file_content", "sed_file", "notebook_edit"}

// pathKeys are argument names that hold a target file: TargetFile is
// captured (absolute); the others are a guard for tools whose arguments
// are not captured yet.
var pathKeys = []string{"TargetFile", "AbsolutePath", "FilePath", "File", "Path", "NotebookPath"}

type input struct {
	ConversationID string   `json:"conversationId"`
	WorkspacePaths []string `json:"workspacePaths"`
	ModelName      string   `json:"modelName"`
	ToolCall       struct {
		Name string                     `json:"name"`
		Args map[string]json.RawMessage `json:"args"`
	} `json:"toolCall"`
}

// Decode parses a payload. The working directory is run_command's Cwd, or
// else the first workspace path (Antigravity sends no cwd field).
func (a Adapter) Decode(kind hooks.Kind, payload []byte) (hooks.Event, error) {
	var in input
	if err := json.Unmarshal(payload, &in); err != nil {
		return hooks.Event{}, err
	}
	if in.ConversationID == "" {
		return hooks.Event{}, errors.New("antigravity: payload has no conversationId")
	}
	ev := hooks.Event{Kind: kind, Agent: Name, Session: in.ConversationID, Model: in.ModelName}
	if len(in.WorkspacePaths) > 0 {
		ev.Cwd = in.WorkspacePaths[0]
	}
	arg := func(key string) string {
		var s string
		_ = json.Unmarshal(in.ToolCall.Args[key], &s)
		return s
	}
	if cwd := arg("Cwd"); cwd != "" {
		ev.Cwd = cwd
	}
	switch {
	case in.ToolCall.Name == RunCommand:
		ev.Command = arg("CommandLine")
		ev.Shell = hooks.POSIX
		if a.PowerShell {
			ev.Shell = hooks.PowerShell
		}
		if kind == hooks.PreEdit {
			ev.Kind = hooks.PreShell
		}
	case slices.Contains(WriteTools, in.ToolCall.Name):
		for _, k := range pathKeys { // every one present: a claim on any blocks
			if p := arg(k); p != "" {
				ev.Paths = append(ev.Paths, p)
			}
		}
		if kind == hooks.PreShell {
			ev.Kind = hooks.PreEdit
		}
	}
	return ev, nil
}

type decision struct {
	Decision string `json:"decision"` // only ever "deny"
	Reason   string `json:"reason,omitempty"`
}

type injection struct {
	InjectSteps []step `json:"injectSteps"`
}

type step struct {
	EphemeralMessage string `json:"ephemeralMessage"`
}

// Encode renders a result. A blocked tool call prints a deny decision; an
// allowed one prints nothing, because `{}` blocks the call and "allow" or
// "auto_approve" would bypass the member's own permission settings (S9).
// Notices go out only before a model call (PreInvocation), as an
// ephemeral message; SessionStart, PostToolUse and Stop print nothing.
func (Adapter) Encode(kind hooks.Kind, r hooks.Result) ([]byte, int) {
	var out any
	switch kind {
	case hooks.PreEdit, hooks.PreShell:
		if !r.Deny {
			return nil, 0
		}
		out = decision{Decision: "deny", Reason: r.Reason}
	case hooks.Prompt:
		text := strings.TrimSpace(r.Context + "\n" + r.Warning)
		if text == "" {
			return nil, 0
		}
		out = injection{InjectSteps: []step{{EphemeralMessage: text}}}
	default:
		return nil, 0
	}
	b, err := json.Marshal(out)
	if err != nil {
		return nil, 0
	}
	return b, 0
}
