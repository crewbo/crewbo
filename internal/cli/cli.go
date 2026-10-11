// Package cli implements the crewbo command line: argument routing,
// usage text and exit codes.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/crewbo/crewbo/internal/daemon"
	"github.com/crewbo/crewbo/internal/platform"
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
  version   Print the version and commit
  help      Show this help
`

// Run executes the command named in args (without the program name),
// writing to stdout and stderr, and returns the process exit code. ctx is
// cancelled on a shutdown signal.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer, info BuildInfo) int {
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
	return write(stdout, ExitOK, "crewbo daemon running: version %s, pid %d, up %s, %d active claims\n",
		st.Version, st.PID, time.Since(st.Started).Round(time.Second), st.ActiveClaims)
}
