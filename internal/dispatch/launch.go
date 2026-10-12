package dispatch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"strings"
)

// Agent names, as in hook commands.
const (
	AgentClaudeCode = "claude-code"
	AgentCodex      = "codex"
)

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
	cmd.Env = agentEnv(l.Env, l.KeepAPIKey, APIKeyVar)
	cmd.Stdin = strings.NewReader(l.Text)
	return cmd
}

// agentEnv drops the named API key variables unless kept. Names are
// compared without case, as Windows does.
func agentEnv(env []string, keepAPIKey bool, keys ...string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		if !keepAPIKey && slices.ContainsFunc(keys, func(k string) bool { return strings.EqualFold(name, k) }) {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// codexKeyVars could make Codex bill an API key instead of the member's
// ChatGPT plan. UNVERIFIED (U-32): removed unless the member opts in, as
// for Claude Code.
var codexKeyVars = []string{"OPENAI_API_KEY", "CODEX_API_KEY"}

// CodexCommand returns the command that runs l with Codex: exec in the
// worktree with the workspace-write sandbox (never danger-full-access or a
// bypass), JSONL events on stdout, and the task text on stdin ("-").
// Flags verified with `codex exec --help` on codex-cli 0.161.0. Codex
// chooses its own session id; the daemon maps hook calls from the
// worktree to the task instead. l.Session is not used.
func CodexCommand(ctx context.Context, l Launch) *exec.Cmd {
	cmd := exec.CommandContext(ctx, l.Bin, "exec", "-C", l.Worktree, "--sandbox", "workspace-write", "--json", "-") //nolint:gosec // the member's own agent CLI
	cmd.Dir = l.Worktree
	cmd.Env = agentEnv(l.Env, l.KeepAPIKey, codexKeyVars...)
	cmd.Stdin = strings.NewReader(l.Text)
	return cmd
}

// CodexSignedIn checks `codex login status`, which exits 0 when signed in
// and 1 with "Not logged in" otherwise (codex-cli 0.161.0).
func CodexSignedIn(ctx context.Context, bin string, env []string, keepAPIKey bool) error {
	cmd := exec.CommandContext(ctx, bin, "login", "status") //nolint:gosec // the member's own agent CLI
	cmd.Env = agentEnv(env, keepAPIKey, codexKeyVars...)
	err := cmd.Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return fmt.Errorf("%w: run codex login, then approve again", ErrNotSignedIn)
	}
	if err != nil {
		return fmt.Errorf("codex login status: %w", err)
	}
	return nil
}

// CodexFailure returns the reason in a Codex JSONL event that ends a turn
// in failure ({"type":"turn.failed","error":{"message":...}}), or "".
// When the message is itself a JSON API error, its inner message is used.
func CodexFailure(line []byte) string {
	var ev struct {
		Type  string `json:"type"`
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(line, &ev) != nil || ev.Type != "turn.failed" {
		return ""
	}
	var inner struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal([]byte(ev.Error.Message), &inner) == nil && inner.Error.Message != "" {
		return inner.Error.Message
	}
	return ev.Error.Message
}

// ClaudeSignedIn checks `claude auth status` (JSON by default, field
// loggedIn; verified on 2.1.294) with the environment the run will get.
func ClaudeSignedIn(ctx context.Context, bin string, env []string, keepAPIKey bool) error {
	cmd := exec.CommandContext(ctx, bin, "auth", "status") //nolint:gosec // the member's own agent CLI
	cmd.Env = agentEnv(env, keepAPIKey, APIKeyVar)
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
