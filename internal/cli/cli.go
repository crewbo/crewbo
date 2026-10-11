// Package cli implements the crewbo command line: argument routing,
// usage text and exit codes.
package cli

import (
	"fmt"
	"io"
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
  version   Print the version and commit
  help      Show this help
`

// Run executes the command named in args (without the program name),
// writing to stdout and stderr, and returns the process exit code.
func Run(args []string, stdout, stderr io.Writer, info BuildInfo) int {
	if len(args) == 0 {
		return write(stdout, ExitOK, "%s", usage)
	}
	switch args[0] {
	case "help", "-h", "--help":
		return write(stdout, ExitOK, "%s", usage)
	case "version":
		return write(stdout, ExitOK, "crewbo %s (commit %s)\n", info.Version, info.Commit)
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
