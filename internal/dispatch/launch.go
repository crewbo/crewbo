package dispatch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// AgentClaudeCode is the Claude Code agent name, as in hook commands.
const AgentClaudeCode = "claude-code"

// APIKeyVar makes `claude -p` bill the API instead of the member's plan
// whenever it is set ("In non-interactive mode (-p), the key is always
// used when present."; https://code.claude.com/docs/en/authentication#authentication-precedence).
const APIKeyVar = "ANTHROPIC_API_KEY" //nolint:gosec // a variable name, not a credential

// ErrNotSignedIn means the agent CLI has no signed-in account.
var ErrNotSignedIn = errors.New("dispatch: the agent is not signed in")

// Launch is one agent run for an approved task.
type Launch struct {
	Bin      string   // agent executable, for example "claude"
	Worktree string   // the task's fresh worktree; the agent's working directory
	Session  string   // UUID for the agent session; hooks report it as session_id
	Text     string   // task text, given on stdin so no part of it is read as a flag
	Env      []string // environment to start from, usually os.Environ()
	// KeepAPIKey keeps ANTHROPIC_API_KEY, which the member must opt in to.
	KeepAPIKey bool
}

// ClaudeCommand returns the command that runs l with Claude Code: print
// mode with an explicit non-bypass permission mode, never --bare (which
// skips hooks, so claims would not be enforced). Flags verified with
// `claude --help` on Claude Code 2.1.294.
func ClaudeCommand(ctx context.Context, l Launch) *exec.Cmd {
	cmd := exec.CommandContext(ctx, l.Bin, "-p", "--session-id", l.Session, "--permission-mode", "acceptEdits") //nolint:gosec // the member's own agent CLI
	cmd.Dir = l.Worktree
	cmd.Env = agentEnv(l.Env, l.KeepAPIKey)
	cmd.Stdin = strings.NewReader(l.Text)
	return cmd
}

// agentEnv drops ANTHROPIC_API_KEY unless kept. Names are compared
// without case, as Windows does.
func agentEnv(env []string, keepAPIKey bool) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		if !keepAPIKey && strings.EqualFold(name, APIKeyVar) {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// ClaudeSignedIn checks `claude auth status` (JSON by default, field
// loggedIn; verified on 2.1.294) with the environment the run will get.
func ClaudeSignedIn(ctx context.Context, bin string, env []string, keepAPIKey bool) error {
	cmd := exec.CommandContext(ctx, bin, "auth", "status") //nolint:gosec // the member's own agent CLI
	cmd.Env = agentEnv(env, keepAPIKey)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	var st struct {
		LoggedIn bool `json:"loggedIn"`
	}
	if jerr := json.Unmarshal(out, &st); jerr != nil {
		if err == nil {
			err = jerr
		}
		return fmt.Errorf("claude auth status: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	if !st.LoggedIn {
		return fmt.Errorf("%w: run claude and sign in, then approve again", ErrNotSignedIn)
	}
	return nil
}
