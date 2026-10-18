// Package cli implements the crewbo command line: argument routing,
// usage text and exit codes.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/crewbo/crewbo/internal/daemon"
	"github.com/crewbo/crewbo/internal/hooks"
	"github.com/crewbo/crewbo/internal/hooks/claudecode"
	"github.com/crewbo/crewbo/internal/hooks/codex"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/crewbo/crewbo/internal/mcpserver"
	"github.com/crewbo/crewbo/internal/platform"
	"github.com/crewbo/crewbo/internal/shellparse"
)

// Exit codes returned by Run.
const (
	ExitOK    = 0
	ExitError = 1
	ExitUsage = 2
)

// BuildInfo identifies the running binary.
type BuildInfo struct {
	Version string
	Commit  string
}

const usage = `crewbo coordinates AI coding agents across a team.

Usage:
  crewbo <command>

Commands:
  daemon    Run the Crewbo daemon in the foreground
  status    Show whether the daemon is running
  claim     Claim files in this repository so agents leave them alone
  hooks     Install or remove Crewbo's hooks in Claude Code and Codex
  mcp       Serve Crewbo's read-only MCP tools over stdio
  serve     Run the Crewbo sync server in the foreground
  login     Sign in with GitHub to a Crewbo sync server
  room      Create, join or leave the room for this repository
  device    List your device keys, or revoke a lost computer's key
  task      Send a signed task (room lead); list, read, approve or decline yours
  dashboard Print the address of the daemon's read-only dashboard
  api       Speak the daemon's local API on stdin and stdout (for editor extensions)
  hook      Handle an agent hook call (run by Claude Code and Codex)
  version   Print the version and commit
  help      Show this help
`

// Run executes the command named in args (without the program name),
// reading stdin and writing to stdout and stderr, and returns the process
// exit code. ctx is cancelled on a shutdown signal.
func Run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, info BuildInfo) int {
	if len(args) == 0 {
		return write(stdout, ExitOK, "%s", usage)
	}
	switch args[0] {
	case "help", "-h", "--help":
		return write(stdout, ExitOK, "%s", usage)
	case "version":
		return write(stdout, ExitOK, "crewbo %s (commit %s)\n", info.Version, info.Commit)
	case "daemon":
		return runDaemon(ctx, stderr, info)
	case "status":
		return runStatus(ctx, stdout, stderr)
	case "hook":
		return runHook(ctx, args[1:], stdin, stdout)
	case "claim":
		return runClaim(ctx, args[1:], stdout, stderr)
	case "hooks":
		return runHooksCmd(ctx, args[1:], stdout, stderr)
	case "mcp":
		return runMCP(ctx, stderr, info)
	case "serve":
		return runServe(ctx, args[1:], stderr)
	case "login":
		return runLogin(ctx, args[1:], stdout, stderr)
	case "room":
		return runRoom(ctx, args[1:], stdout, stderr)
	case "task":
		return runTask(ctx, args[1:], stdin, stdout, stderr)
	case "api":
		return runAPI(ctx, stdin, stdout, stderr)
	case "device":
		return runDevice(ctx, args[1:], stdout, stderr)
	case "dashboard":
		return runDashboard(ctx, stdout, stderr)
	default:
		return write(stderr, ExitUsage, "crewbo: unknown command %q\n\n%s", args[0], usage)
	}
}

// write prints to w and returns code, or ExitError if the write fails
// (for example a closed pipe), so callers never report success silently.
func write(w io.Writer, code int, format string, a ...any) int {
	if _, err := fmt.Fprintf(w, format, a...); err != nil {
		return ExitError
	}
	return code
}

func paths() (platform.Paths, error) {
	env, err := platform.DefaultEnv()
	if err != nil {
		return platform.Paths{}, err
	}
	return platform.Resolve(env)
}

func runDaemon(ctx context.Context, stderr io.Writer, info BuildInfo) int {
	p, err := paths()
	if err != nil {
		return write(stderr, ExitError, "crewbo: %v\n", err)
	}
	log := slog.New(slog.NewTextHandler(stderr, nil))
	err = daemon.New(daemon.Config{Paths: p, Version: info.Version, Log: log}).Run(ctx)
	switch {
	case errors.Is(err, platform.ErrAlreadyRunning):
		return write(stderr, ExitError, "crewbo: a daemon is already running for this user\n")
	case err != nil:
		return write(stderr, ExitError, "crewbo: %v\n", err)
	}
	return ExitOK
}

// runDashboard prints the address of the dashboard the daemon serves on
// localhost (M5-06), starting it if needed.
func runDashboard(ctx context.Context, stdout, stderr io.Writer) int {
	p, err := paths()
	if err != nil {
		return write(stderr, ExitError, "crewbo: %v\n", err)
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	c, err := daemon.Dial(ctx, p)
	if err != nil {
		return write(stderr, ExitError, "crewbo: the daemon is not running; start it with `crewbo daemon`\n")
	}
	defer func() { _ = c.Close() }()
	resp, err := c.Call(ctx, daemon.Request{Method: daemon.MethodDashboard})
	if err != nil {
		return write(stderr, ExitError, "crewbo: %v\n", err)
	}
	return write(stdout, ExitOK, "Open this address in your browser (only on this computer; it works until the daemon stops):\n%s\n", resp.URL)
}

func runStatus(ctx context.Context, stdout, stderr io.Writer) int {
	p, err := paths()
	if err != nil {
		return write(stderr, ExitError, "crewbo: %v\n", err)
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	c, err := daemon.Dial(ctx, p)
	if err != nil {
		return write(stdout, ExitError, "crewbo daemon is not running\n")
	}
	defer func() { _ = c.Close() }()
	resp, err := c.Call(ctx, daemon.Request{Method: daemon.MethodStatus})
	if err != nil || resp.Status == nil {
		return write(stderr, ExitError, "crewbo: daemon did not answer: %v\n", err)
	}
	st := resp.Status
	violations := ""
	if st.Violations > 0 {
		violations = fmt.Sprintf(", %d claim violations recorded (see the daemon log)", st.Violations)
	}
	sync := ""
	if st.Sync != "" {
		sync = "\nsync: " + st.Sync
	}
	if st.Refused > 0 {
		sync += fmt.Sprintf("\n%d claim(s) made earlier were refused by the sync server and no longer apply (see the daemon log)", st.Refused)
	}
	return write(stdout, ExitOK, "crewbo daemon running: version %s, pid %d, up %s, %d active claims, %d agent sessions%s%s\n",
		st.Version, st.PID, time.Since(st.Started).Round(time.Second), st.ActiveClaims, st.Sessions, violations, sync)
}

// adapterFor returns the hook adapter for an agent name.
func adapterFor(name string) (hooks.Adapter, bool) {
	switch name {
	case claudecode.Name:
		return claudecode.Adapter{}, true
	case codex.Name:
		return codex.Adapter{PowerShell: platform.AgentShellIsPowerShell()}, true
	}
	return nil, false
}

var hookKinds = map[string]hooks.Kind{
	"session_start": hooks.SessionStart, "prompt": hooks.Prompt, "pre_edit": hooks.PreEdit,
	"pre_shell": hooks.PreShell, "post_edit": hooks.PostEdit, "stop": hooks.Stop,
}

// runHook always exits 0 unless an adapter asks otherwise: a misconfigured
// hook must never block the agent (ADR 0004). Problems go to hook.log.
func runHook(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) int {
	p, perr := paths()
	log := hookLog(p, perr)
	if len(args) != 2 {
		log.Warn("hook: want `crewbo hook <agent> <event>`", "args", args)
		return ExitOK
	}
	a, ok := adapterFor(args[0])
	kind, kok := hookKinds[args[1]]
	if !ok || !kok || perr != nil {
		log.Warn("hook: unknown agent or event, allowing", "agent", args[0], "event", args[1], "paths_err", perr)
		return ExitOK
	}
	return hooks.Run(ctx, a, kind, stdin, stdout, hooks.Deps{Paths: p, Log: log, ParseShell: parseShell})
}

// hookLog appends to hook.log in the data directory, or discards when that
// is not possible. stdout is reserved for the agent's JSON.
func hookLog(p platform.Paths, perr error) *slog.Logger {
	if perr == nil && os.MkdirAll(p.Data, 0o700) == nil {
		f, err := os.OpenFile(filepath.Join(p.Data, "hook.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err == nil {
			return slog.New(slog.NewTextHandler(f, nil))
		}
	}
	return slog.New(slog.DiscardHandler)
}

// parseShell finds shell write targets with the parser for the command's
// shell.
func parseShell(command string, sh hooks.Shell) ([]string, bool) {
	r := shellparse.POSIX(command)
	if sh == hooks.PowerShell {
		r = shellparse.PowerShell(command)
	}
	return r.Targets, r.Opaque
}

// runMCP serves the MCP tools on stdin and stdout until the agent closes
// the connection.
func runMCP(ctx context.Context, stderr io.Writer, info BuildInfo) int {
	p, err := paths()
	if err != nil {
		return write(stderr, ExitError, "crewbo: %v\n", err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return write(stderr, ExitError, "crewbo: %v\n", err)
	}
	s := mcpserver.New(mcpserver.Config{Paths: p, Cwd: cwd, Version: info.Version})
	if err := s.Run(ctx, &mcp.StdioTransport{}); err != nil && !errors.Is(err, context.Canceled) {
		return write(stderr, ExitError, "crewbo mcp: %v\n", err)
	}
	return ExitOK
}
