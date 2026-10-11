// Package hooks runs `crewbo hook`: it turns an agent's hook payload into a
// normalised event, asks the daemon, and answers the agent. Enforcement lives
// here (ADR 0004); the claudecode and codex packages adapt each agent's JSON
// (docs/architecture/hooks.md).
//
// The hook fails open: when anything goes wrong it allows the action and
// passes a warning to the agent, so a Crewbo fault never blocks work.
package hooks

import "errors"

// Kind is a normalised hook event.
type Kind string

// Normalised events (docs/architecture/hooks.md).
const (
	SessionStart Kind = "session_start"
	Prompt       Kind = "prompt"
	PreEdit      Kind = "pre_edit"
	PreShell     Kind = "pre_shell"
	PostEdit     Kind = "post_edit"
	Stop         Kind = "stop"
)

// Shell names the shell a command is written for.
type Shell string

// Shells a pre_shell command can use.
const (
	POSIX      Shell = "posix"
	PowerShell Shell = "powershell"
)

// Event is one hook call in agent-independent form.
type Event struct {
	Kind    Kind
	Agent   string // "claude-code", "codex"
	Session string
	Cwd     string   // the agent's working directory
	Paths   []string // pre_edit: target files, absolute or relative to Cwd
	Command string   // pre_shell: the command line
	Shell   Shell
	Model   string // when the agent reports it
}

// Result is what the hook tells the agent.
type Result struct {
	Deny    bool
	Reason  string // shown to the agent when Deny is set
	Context string // extra context for the agent (session_start, prompt)
	Warning string // a fail-open notice; the action is allowed
}

// Adapter translates one agent's hook format.
type Adapter interface {
	// Name is the agent name used in `crewbo hook <agent> <event>`.
	Name() string
	// Decode parses the payload for the named normalised event.
	Decode(event Kind, payload []byte) (Event, error)
	// Encode renders r for the event: the bytes to print on stdout (often
	// none) and the process exit code.
	Encode(event Kind, r Result) ([]byte, int)
}

// ErrUnknownEvent is returned by adapters for event names they do not map.
var ErrUnknownEvent = errors.New("hooks: unknown event")
