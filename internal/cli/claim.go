package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/user"
	"path"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/crewbo/crewbo/internal/claims"
	"github.com/crewbo/crewbo/internal/daemon"
	"github.com/crewbo/crewbo/internal/hooks"
)

const claimUsage = `Usage:
  crewbo claim add [--for 30m] [--task ID] <path or glob>...
  crewbo claim list
  crewbo claim release <id>...

Paths and globs are relative to the current directory. Claims made here
block every agent session in this repository, including your own, until
they are released or expire (default 30m, at most 8h).
`

// runClaim implements `crewbo claim` against the local daemon (M2-10). The
// sync server (M3) will share these claims with the room.
func runClaim(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return write(stderr, ExitUsage, "%s", claimUsage)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return write(stderr, ExitError, "crewbo: %v\n", err)
	}
	root, ok := hooks.RepoRoot(cwd)
	if !ok {
		return write(stderr, ExitError, "crewbo: not inside a git repository\n")
	}
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

	switch args[0] {
	case "add":
		return claimAdd(ctx, c, args[1:], root, cwd, stdout, stderr)
	case "list":
		resp, err := c.Call(ctx, daemon.Request{Method: daemon.MethodClaimList, Repo: root})
		if err != nil {
			return write(stderr, ExitError, "crewbo: %v\n", err)
		}
		if len(resp.Claims) == 0 {
			return write(stdout, ExitOK, "no active claims in this repository\n")
		}
		return printClaims(stdout, resp.Claims)
	case "release":
		if len(args) < 2 {
			return write(stderr, ExitUsage, "%s", claimUsage)
		}
		resp, err := c.Call(ctx, daemon.Request{Method: daemon.MethodClaimRelease, Repo: root, IDs: args[1:]})
		if err != nil {
			return write(stderr, ExitError, "crewbo: %v\n", err)
		}
		if resp.Note != "" {
			_ = write(stdout, ExitOK, "%s\n", resp.Note)
		}
		return write(stdout, ExitOK, "released %d claim(s)\n", resp.Released)
	default:
		return write(stderr, ExitUsage, "crewbo: unknown claim command %q\n\n%s", args[0], claimUsage)
	}
}

func claimAdd(ctx context.Context, c *daemon.Client, args []string, root, cwd string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("claim add", flag.ContinueOnError)
	fs.SetOutput(stderr)
	lease := fs.Duration("for", daemon.DefaultCLILease, "how long to hold the claim (at most 8h)")
	task := fs.String("task", "", "task id the claim belongs to")
	paths, err := parseInterspersed(fs, args)
	if err != nil {
		return ExitUsage
	}
	if len(paths) == 0 || *lease <= 0 {
		return write(stderr, ExitUsage, "%s", claimUsage)
	}
	var patterns []string
	for _, a := range paths {
		pat, err := toPattern(root, cwd, a)
		if err != nil {
			return write(stderr, ExitError, "crewbo: %s: %v\n", a, err)
		}
		patterns = append(patterns, pat)
	}
	resp, err := c.Call(ctx, daemon.Request{
		Method: daemon.MethodClaimAdd, Session: daemon.TerminalSession, Member: username(),
		Repo: root, Patterns: patterns, Task: *task, LeaseSeconds: int64(lease.Seconds()),
	})
	if err != nil {
		if b := resp.BlockedBy; b != nil {
			holder := b.Holder.MemberID
			if holder == "" {
				holder = "agent session " + b.Holder.SessionID
			}
			return write(stderr, ExitError, "crewbo: %s is already claimed by %s until %s; nothing was claimed\n",
				resp.Path, holder, b.LeaseExpires.Local().Format("15:04"))
		}
		return write(stderr, ExitError, "crewbo: %v\n", err)
	}
	code := printClaims(stdout, resp.Claims)
	if resp.Note != "" {
		_ = write(stdout, ExitOK, "%s\n", resp.Note)
	}
	return code
}

// toPattern turns a command-line path or glob, relative to cwd, into a
// canonical repository-relative pattern.
func toPattern(root, cwd, arg string) (string, error) {
	if !claims.IsGlob(arg) {
		return claims.Normalize(root, filepath.Join(cwd, arg))
	}
	rel, err := filepath.Rel(root, cwd)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", claims.ErrOutsideRepo
	}
	pat := path.Clean(path.Join(filepath.ToSlash(rel), filepath.ToSlash(arg)))
	if err := claims.ValidatePattern(pat); err != nil {
		return "", errors.New("glob must stay inside the repository")
	}
	return pat, nil
}

func printClaims(w io.Writer, cs []claims.Claim) int {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "ID\tPATTERN\tHELD BY\tUNTIL")
	for _, c := range cs {
		holder := c.Holder.MemberID
		if daemon.IsTerminalSession(c.Holder.SessionID) {
			holder += " (terminal)"
		} else if holder == "" {
			holder = "agent " + c.Holder.SessionID
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", c.ID, c.Pattern, holder, c.LeaseExpires.Local().Format("15:04"))
	}
	if err := tw.Flush(); err != nil {
		return ExitError
	}
	return ExitOK
}

func username() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		// Windows reports DOMAIN\name.
		return u.Username[strings.LastIndex(u.Username, `\`)+1:]
	}
	return "you"
}

// parseInterspersed parses flags placed before, between or after the
// positional arguments (`claim add src/a.go --task 12`). The standard flag
// package stops at the first positional argument, which turned trailing
// flags into claims. Everything after "--" is positional.
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		rest := fs.Args()
		if len(rest) == 0 {
			return positional, nil
		}
		if len(args) > 0 && len(rest) < len(args) && args[len(args)-len(rest)-1] == "--" {
			return append(positional, rest...), nil
		}
		positional = append(positional, rest[0])
		args = rest[1:]
	}
}
